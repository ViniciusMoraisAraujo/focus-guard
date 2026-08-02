// Package pomodoro implements focus sessions as alternating work/rest cycles
// over a preset's domains. Each work phase blocks the preset domains for the
// work duration (the block expires automatically through the scheduler); rest
// phases block nothing. The session state lives in RAM only. Completed
// sessions are pushed to an optional analytics recorder for "focusguard stats".
package pomodoro

import (
	"errors"
	"fmt"
	"os"
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
// preset by the caller (daemon/server). Label is an optional session name (a
// "mission", e.g. "Estudar ENEM") recorded in the analytics. Strict sessions
// are inviolable: Stop is refused while active, so the work/rest cycle always
// runs to completion.
type Session struct {
	Preset  string
	Label   string
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

// CompletionSummary describes a finished session for the daemon's post-session
// summary and prefs persistence (saving the resolved work/rest/cycles).
type CompletionSummary struct {
	Preset  string
	Work    time.Duration
	Rest    time.Duration
	Cycles  int
	Focus   time.Duration
	Strict  bool
	Stopped bool
}

// BeepFunc emits an audible/terminal transition cue. The production
// implementation writes the terminal bell; tests stub it.
type BeepFunc func(kind string)

// Notifier emits transition cues for the pomodoro cycle: a beep when a work
// phase starts, another when rest starts and a final one when the session
// completes. Beeps are fire-and-forget and must never block the session loop.
type Notifier struct {
	beep BeepFunc
}

// terminalBell writes the ASCII bell character to stdout. It is best-effort:
// terminals without audible bells simply ignore it.
func terminalBell(kind string) {
	switch kind {
	case "work":
		fmt.Fprint(os.Stdout, "\a")
	case "rest":
		fmt.Fprint(os.Stdout, "\a\a")
	case "done":
		fmt.Fprint(os.Stdout, "\a\a\a")
	}
}

// NewNotifier returns a Notifier wired to the terminal bell.
func NewNotifier() *Notifier {
	return &Notifier{beep: terminalBell}
}

// WorkStart cues the beginning of a work phase.
func (n *Notifier) WorkStart() { n.beep("work") }

// RestStart cues the beginning of a rest phase.
func (n *Notifier) RestStart() { n.beep("rest") }

// Finish cues the completion of the whole session.
func (n *Notifier) Finish() { n.beep("done") }

// Controller runs at most one pomodoro session at a time.
type Controller struct {
	mu       sync.Mutex
	blocker  Blocker
	state    State
	stop     chan struct{}
	done     chan struct{}
	rec      SessionRecorder
	notifier *Notifier
	strict   bool
	watchCh  chan CompletionSummary
	lastSum  CompletionSummary
}

// New returns a Controller that blocks via the given Blocker.
func New(blocker Blocker) *Controller {
	return &Controller{blocker: blocker}
}

// SetNotifier registers transition cues (beeps). Nil disables them.
func (c *Controller) SetNotifier(n *Notifier) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifier = n
}

// WatchCompletion returns a channel that receives one CompletionSummary per
// finished session. The daemon uses it to persist the resolved defaults and
// log a post-session summary. The channel stays open while idle (never
// closed), so the daemon can watch across many sessions. Safe to call once at
// startup, before any session starts.
func (c *Controller) WatchCompletion() <-chan CompletionSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watchCh == nil {
		c.watchCh = make(chan CompletionSummary, 4)
	}
	return c.watchCh
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
	n := c.notifier
	if n == nil {
		n = &Notifier{beep: func(string) {}} // no-op
	}
	for cycle := 1; cycle <= s.Cycles; cycle++ {
		c.setPhase(PhaseWork, cycle, s, s.Work)
		n.WorkStart()
		_, _ = c.blocker.BlockDomains(s.Domains, s.Work)
		focus += s.Work
		if !wait(stop, s.Work) {
			break
		}
		if s.Rest > 0 && cycle < s.Cycles {
			c.setPhase(PhaseRest, cycle, s, s.Rest)
			n.RestStart()
			if !wait(stop, s.Rest) {
				break
			}
		}
	}

	n.Finish()

	stopped := false
	select {
	case <-stop:
		stopped = true
	default:
	}

	c.mu.Lock()
	c.state = State{}
	c.stop = nil
	c.done = nil // simétrico a c.stop: o canal foi capturado localmente
	watchCh := c.watchCh
	c.lastSum = CompletionSummary{
		Preset:  s.Preset,
		Work:    s.Work,
		Rest:    s.Rest,
		Cycles:  s.Cycles,
		Focus:   focus,
		Strict:  s.Strict,
		Stopped: stopped,
	}
	c.mu.Unlock()

	c.record(s, startedAt, focus)

	// Entrega o resumo de forma não-bloqueante: se ninguém está assistindo
	// (watchCh nil ou cheio), a sessão simplesmente não gera resumo.
	if watchCh != nil {
		select {
		case watchCh <- c.lastSum:
		default:
		}
	}
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
		Label:   s.Label,
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
