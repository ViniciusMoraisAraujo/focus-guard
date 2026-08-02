// Package pomodoro implements focus sessions as alternating work/rest cycles
// over a preset's domains. Each work phase blocks the preset domains for the
// work duration (the block expires automatically through the scheduler); rest
// phases block nothing. The session state lives in RAM only. Completed
// sessions are pushed to an optional analytics recorder for "focusguard stats".
package pomodoro

import (
	"errors"
	"sync"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/policy"
)

// Phase is the current pomodoro cycle phase.
type Phase string

const (
	PhaseWork Phase = "work"
	PhaseRest Phase = "rest"
)

// Session describes a pomodoro to start. Domains are pre-resolved from a
// preset by the caller (daemon/server). Strict sessions are inviolable: Stop
// is refused while active, so the work/rest cycle always runs to completion.
type Session struct {
	Preset  string
	Domains []string
	Work    time.Duration
	Rest    time.Duration
	Cycles  int
	Strict  bool
}

// State is the observable session snapshot (also sent over IPC).
type State struct {
	Active     bool      `json:"active"`
	Preset     string    `json:"preset,omitempty"`
	Phase      Phase     `json:"phase,omitempty"`
	Cycle      int       `json:"cycle,omitempty"`
	Cycles     int       `json:"cycles,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	PhaseUntil time.Time `json:"phase_until,omitempty"`
}

// Blocker applies domain blocks for a work phase. The scheduler satisfies it
// via BlockDomains (single batched hosts rewrite + firewall sync).
type Blocker interface {
	BlockDomains(domains []string, duration time.Duration) ([]policy.Block, error)
}

// SessionRecorder receives the completed focus session for analytics
// ("focusguard stats"). The daemon wires the analytics recorder.
type SessionRecorder interface {
	Record(analytics.Session)
}

// Controller runs at most one pomodoro session at a time.
type Controller struct {
	mu      sync.Mutex
	blocker Blocker
	state   State
	stop    chan struct{}
	done    chan struct{}
	rec     SessionRecorder
	strict  bool
}

// New returns a Controller that blocks via the given Blocker.
func New(blocker Blocker) *Controller {
	return &Controller{blocker: blocker}
}

// SetRecorder registers an analytics recorder that receives exactly one
// session record per completed session (natural end or stop). Nil disables
// recording.
func (c *Controller) SetRecorder(rec SessionRecorder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rec = rec
}

func validate(s Session) error {
	switch {
	case len(s.Domains) == 0:
		return errors.New("pomodoro: nenhum domínio para bloquear (preset vazio)")
	case s.Work <= 0:
		return errors.New("pomodoro: duração de trabalho deve ser positiva")
	case s.Rest < 0:
		return errors.New("pomodoro: duração de descanso não pode ser negativa")
	case s.Cycles < 1:
		return errors.New("pomodoro: ao menos 1 ciclo")
	}
	return nil
}

// Start begins a pomodoro session and returns its initial state. Starting a
// second session while one is active is an error.
func (c *Controller) Start(s Session) (State, error) {
	if err := validate(s); err != nil {
		return State{}, err
	}

	startedAt := time.Now()
	c.mu.Lock()
	if c.state.Active {
		c.mu.Unlock()
		return State{}, errors.New("pomodoro: sessão já ativa (use pomodoro-stop para encerrar)")
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	c.stop = stop
	c.done = done
	c.strict = s.Strict
	c.state = State{
		Active:     true,
		Preset:     s.Preset,
		Phase:      PhaseWork,
		Cycle:      1,
		Cycles:     s.Cycles,
		StartedAt:  startedAt,
		PhaseUntil: startedAt.Add(s.Work),
	}
	c.mu.Unlock()

	// done é capturado localmente (como stop): cada sessão fecha o SEU canal.
	// Fechar c.done direto aqui criaria uma corrida com o Start de uma nova
	// sessão — se B começasse entre o reset de estado e o close, o goroutine
	// de A fecharia o canal de B e o run de B panicaria com "close of closed
	// channel" ao terminar (além de travar um Stop concorrente esperando o
	// done antigo).
	go c.run(s, startedAt, stop, done)
	return c.Status(), nil
}

// run drives the work/rest cycle loop. Each work phase re-blocks the preset
// domains for the work duration; a rest phase (if any) waits before the next
// cycle. Stopping aborts the loop; any block still active expires on its own
// scheduler timer (there is intentionally no forced unblock). On completion
// (natural or stopped) the session is pushed to the analytics recorder.
// stop and done are this session's OWN channels (captured by Start) — run
// must close done, never the controller field, so a concurrently started
// session B cannot have its channel closed by session A's finalizer.
func (c *Controller) run(s Session, startedAt time.Time, stop, done chan struct{}) {
	var focus time.Duration
	for cycle := 1; cycle <= s.Cycles; cycle++ {
		c.setPhase(PhaseWork, cycle, s, s.Work)
		_, _ = c.blocker.BlockDomains(s.Domains, s.Work)
		focus += s.Work
		if !wait(stop, s.Work) {
			break
		}
		if s.Rest > 0 && cycle < s.Cycles {
			c.setPhase(PhaseRest, cycle, s, s.Rest)
			if !wait(stop, s.Rest) {
				break
			}
		}
	}

	c.mu.Lock()
	c.state = State{}
	c.stop = nil
	c.done = nil // simétrico a c.stop: o canal foi capturado localmente
	c.mu.Unlock()

	c.record(s, startedAt, focus)
	close(done)
}

// record pushes the completed session to the analytics recorder, exactly once.
func (c *Controller) record(s Session, startedAt time.Time, focus time.Duration) {
	c.mu.Lock()
	rec := c.rec
	c.mu.Unlock()
	if rec == nil {
		return
	}
	rec.Record(analytics.Session{
		Start:   startedAt,
		End:     time.Now(),
		Preset:  s.Preset,
		Domains: append([]string(nil), s.Domains...),
		WorkMin: int(s.Work / time.Minute),
		RestMin: int(s.Rest / time.Minute),
		Cycles:  s.Cycles,
		Focus:   focus,
		Strict:  s.Strict,
	})
}

func (c *Controller) setPhase(phase Phase, cycle int, s Session, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Phase = phase
	c.state.Cycle = cycle
	c.state.PhaseUntil = time.Now().Add(d)
}

// wait blocks until d elapses or stop is closed, reporting whether it was the
// timer that fired (true) or the session was stopped (false).
func wait(stop chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-stop:
		return false
	}
}

// Stop ends the active session (if any) and returns the resulting state.
// Strict sessions are inviolable: Stop is refused while active, so the cycle
// always runs to completion. For non-strict sessions the stop channel is
// claimed (set to nil) under the mutex before it is closed, so concurrent
// Stop calls cannot double-close it: the first caller closes the channel and
// the others simply wait for the session to finish.
func (c *Controller) Stop() (State, error) {
	c.mu.Lock()
	if !c.state.Active {
		c.mu.Unlock()
		return State{}, nil
	}
	if c.strict {
		c.mu.Unlock()
		return State{}, errors.New("pomodoro: sessão estrita não pode ser encerrada antecipadamente")
	}
	stop := c.stop
	done := c.done
	c.stop = nil // reivindica o canal — só o capturador pode fechá-lo
	c.mu.Unlock()

	if stop == nil {
		// Outro Stop já está em andamento: aguarda a sessão terminar.
		<-done
		return c.Status(), nil
	}

	close(stop)
	<-done
	return c.Status(), nil
}

// Status returns the current session snapshot.
func (c *Controller) Status() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}
