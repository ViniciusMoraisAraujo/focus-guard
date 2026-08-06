package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/dnsserver"
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
	"focusguard/internal/schedule"
	"focusguard/internal/scheduler"
	"focusguard/internal/tamper"
	"focusguard/internal/user"
)

// updateTimeout bounds the update/update-check IPC actions. Aplicar uma
// atualização baixa o release (~13 MB), extrai e troca os binários — 30s era
// apertado em conexões lentas; 120s cobre download + apply sem travar o daemon
// (cada conexão IPC roda na própria goroutine, então pings não são bloqueados).
const updateTimeout = 120 * time.Second

type Server struct {
	scheduler *scheduler.Scheduler
	// registry holds the action handlers: o roteador despacha por
	// registry.Get → Validate → Handle; ação desconhecida vira
	// CodeUnknownAction (wire protocol preservado do switch legado).
	registry        *Registry
	listener        net.Listener
	updateChecker   UpdateChecker
	pomodoro        PomodoroRunner
	pomodoroPrefs   PomodoroPrefs
	analytics       AnalyticsProvider
	presets         PresetManager
	schedules       ScheduleManager
	goalStore       GoalManager
	apps            AppsManager
	tamperLog       TamperProvider
	users           UserManager
	dnsCtrl         DNSController
	onUpdateApplied func()
	onDNSStarted    func()
	currentVersion  string

	mu           sync.RWMutex
	updateStatus UpdateStatus
	// updateApplied é o latch armado quando a ação "update" aplica um binário
	// novo; o roteador o consome (takeUpdateApplied) APÓS escrever a resposta
	// para disparar o onUpdateApplied (restart do daemon) — mesma ordem do
	// switch legado.
	updateApplied bool
}

// PresetManager is the preset catalog used by the block/pomodoro/presets
// actions. The daemon wires a *preset.Store (built-ins + user presets); when
// no manager is configured the server falls back to the built-in catalog.
type PresetManager interface {
	List() []preset.Preset
	Resolve(name string) (preset.Preset, error)
	Add(p preset.Preset) error
	Remove(name string) error
}

// builtinCatalog serves the read-only built-in presets when no user store is
// configured (tests and dev builds); Add/Remove are unavailable.
type builtinCatalog struct{}

func (builtinCatalog) List() []preset.Preset                      { return preset.List() }
func (builtinCatalog) Resolve(name string) (preset.Preset, error) { return preset.Resolve(name) }
func (builtinCatalog) Add(preset.Preset) error {
	return errors.New("presets personalizados não configurados")
}
func (builtinCatalog) Remove(string) error {
	return errors.New("presets personalizados não configurados")
}

// SetPresets wires the preset catalog (built-ins + user-defined) into the
// server. Nil resets to the built-in-only fallback.
func (s *Server) SetPresets(m PresetManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presets = m
}

// ScheduleManager is the recurring-rule catalog used by the schedule-*
// actions. The daemon wires a *schedule.Manager (file-backed); tests stub it.
type ScheduleManager interface {
	List() []schedule.Rule
	Add(r schedule.Rule) (schedule.Rule, error)
	Remove(id string) error
	ImportICS(data []byte, preset string) ([]schedule.Rule, error)
}

// GoalManager is the daily focus goal store used by goal-get/goal-set. The
// daemon wires a *goal.Store; tests stub it.
type GoalManager interface {
	Get() time.Duration
	Set(d time.Duration) error
}

// SetGoal wires the daily goal store into the server. Nil makes goal-*
// actions fail with a clear message.
func (s *Server) SetGoal(g GoalManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goalStore = g
}

// SetSchedules wires the recurring-rule manager into the server. Nil makes the
// schedule-* actions fail with a clear message.
func (s *Server) SetSchedules(m ScheduleManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules = m
}

// AppsManager is the process-app denylist used by the apps-* actions. The
// daemon wires a *apps.Store that also refreshes the live process guard.
type AppsManager interface {
	List() []string
	Add(name string) error
	Remove(name string) error
}

// SetApps wires the process denylist into the server. Nil makes the apps-*
// actions fail with a clear message. Retained for the in-package test
// reference handlers (handlers_ref_test.go) — the daemon registers the real
// domain handlers at the composition root.
func (s *Server) SetApps(m AppsManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps = m
}

// TamperProvider supplies the detected tamper events for the tamper-log
// action. The daemon wires the tamper recorder; tests stub it.
type TamperProvider interface {
	Events() ([]tamper.Event, error)
}

// SetTamper wires the tamper-log provider into the server. Nil makes the
// tamper-log action fail with a clear message.
func (s *Server) SetTamper(p TamperProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tamperLog = p
}

// UserManager is the credential store backing the user-* actions (web login
// and user management). The daemon wires a *user.Store; when no manager is
// configured the actions fail with a clear message (tests and dev builds).
type UserManager interface {
	List() []string
	Verify(username, password string) (user.User, bool)
	Add(username, password string) error
	Remove(username string) error
	SetPassword(username, password string) error
}

// SetUsers wires the credential store into the server. Nil makes the user-*
// actions fail with a clear message. Retained for the in-package test
// reference handlers (handlers_ref_test.go) — the daemon registers the real
// domain handlers at the composition root.
func (s *Server) SetUsers(m UserManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = m
}

// DNSController drives the DNS sinkhole server lifecycle used by the
// dns-start/dns-stop/dns-status/dns-set-upstream actions. The daemon wires a
// *dnsserver.Controller (bound to the scheduler as the policy checker); tests
// stub it. The persisted enabled flag and upstream live in the scheduler, not
// the controller — the actions combine both for status.
type DNSController interface {
	Start() error
	Stop() error
	SetUpstream(upstream string) error
	Status() dnsserver.Status
}

// SetDNS wires the DNS sinkhole controller into the server. Nil makes the
// dns-* actions fail with a clear message.
func (s *Server) SetDNS(c DNSController) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dnsCtrl = c
}

// mergeDNS copies the live DNS controller state into an IPC response together
// with the persisted enabled flag (which lives in the scheduler).
func mergeDNS(resp *Response, st dnsserver.Status, enabled bool) {
	resp.DNSEnabled = enabled
	resp.DNSListening = st.Listening
	resp.DNSAddr = st.Addr
	resp.DNSUpstream = st.Upstream
	resp.DNSQueries = st.Queries
	resp.DNSBlocked = st.Blocked
	resp.DNSBindError = st.BindError
}

// catalog returns the configured PresetManager or the built-in fallback.
func (s *Server) catalog() PresetManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.presets != nil {
		return s.presets
	}
	return builtinCatalog{}
}

// PomodoroRunner is what the server uses to start/stop/query pomodoro
// sessions. The daemon wires a *pomodoro.Controller; tests stub it.
type PomodoroRunner interface {
	Start(pomodoro.Session) (pomodoro.State, error)
	Stop() (pomodoro.State, error)
	Status() pomodoro.State
	// WatchCompletion exposes the post-session summary stream (daemon-only;
	// tests stub it with a never-sending channel).
	WatchCompletion() <-chan pomodoro.CompletionSummary
}

func (s *Server) SetPomodoro(r PomodoroRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pomodoro = r
}

// PomodoroPrefs persists/reads the pomodoro defaults (work/rest/cycles) so a
// plain "focusguard pomodoro --preset x" reuses the last session's values.
type PomodoroPrefs interface {
	Resolve(work, rest, cycles int) (int, int, int)
	Remember(work, rest, cycles int)
}

func (s *Server) SetPomodoroPrefs(p PomodoroPrefs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pomodoroPrefs = p
}

// AnalyticsProvider supplies the recorded sessions for the stats action. The
// daemon wires the analytics recorder; tests stub it.
type AnalyticsProvider interface {
	Sessions() ([]analytics.Session, error)
}

func (s *Server) SetAnalytics(p AnalyticsProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.analytics = p
}

// HasActiveSession reports whether a pomodoro session is currently running, so
// the daemon can refuse to shut down mid-session (strict or not).
func (s *Server) HasActiveSession() bool {
	s.mu.RLock()
	r := s.pomodoro
	s.mu.RUnlock()
	return r != nil && r.Status().Active
}

func NewServer(sched *scheduler.Scheduler) *Server {
	s := &Server{scheduler: sched}
	s.registry = NewRegistry()
	s.registerHandlers()
	return s
}

// ValidateRegistry closes the specs↔registry contract at boot: every
// registered handler must have an ActionSpec (user-verify is web-only and
// exempt) and every spec must have a handler. The daemon calls it after wiring
// the dependencies; a drift is a boot-time bug, not a runtime surprise.
func (s *Server) ValidateRegistry() error {
	if err := s.registry.ValidateSpecs("user-verify"); err != nil {
		return err
	}
	for _, action := range SpecActions() {
		if _, ok := s.registry.Get(action); !ok {
			return fmt.Errorf("spec sem handler: %q (registre o handler)", action)
		}
	}
	return nil
}

func (s *Server) SetUpdateChecker(c UpdateChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateChecker = c
}

// SetOnUpdateApplied registers a hook invoked after an update has been applied
// successfully. The daemon uses it to exit and let systemd/watchdog respawn it
// with the new binary; without it the daemon would keep running the old binary
// in RAM after the update (the "zombie daemon").
func (s *Server) SetOnUpdateApplied(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onUpdateApplied = fn
}

// SetOnDNSStarted registers a hook invoked after the DNS sinkhole server
// started and its enabled flag was persisted. The daemon uses it to apply the
// DoH firewall block (so browsers cannot bypass the sinkhole over port 853).
func (s *Server) SetOnDNSStarted(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDNSStarted = fn
}

// SetCurrentVersion registra a versão do sistema, permitindo que o status a
// reporte mesmo quando a verificação de atualizações ainda não rodou (ou está
// desativada em builds de desenvolvimento).
func (s *Server) SetCurrentVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentVersion = v
}

// RefreshUpdateStatus runs a check-only update check (no apply) and caches the
// result so it can be surfaced by the status action.
func (s *Server) RefreshUpdateStatus(ctx context.Context) (UpdateStatus, error) {
	s.mu.RLock()
	c := s.updateChecker
	s.mu.RUnlock()
	if c == nil {
		return UpdateStatus{}, nil
	}
	// A checagem em background sempre usa o canal estável — nunca surpreender
	// um usuário estável com uma prerelease.
	st, err := c.Check(ctx, false, "")
	if err != nil {
		return st, err
	}
	s.mu.Lock()
	s.updateStatus = st
	s.mu.Unlock()
	return st, nil
}

func (s *Server) Start() error {
	l, err := Listen()
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.listener = l

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) Stop() error {
	if s.listener != nil {
		s.listener.Close()
	}
	return nil
}

// Register adds (or replaces) an action handler. The composition root
// (cmd/focusguard-daemon) uses it to wire the domain handlers; NewServer keeps
// the server-level handlers (ping/status/tamper/services) as defaults.
func (s *Server) Register(h Handler) {
	if s.registry == nil {
		s.registry = NewRegistry()
	}
	s.registry.Register(h)
}

// Dispatch routes one request through the registry (Get → Validate → Handle)
// and returns the wire Response. The transport layer (handleConnection) only
// encodes it and fires the post-response update hook. Ação desconhecida (ou
// Server sem registry — dev build) recebe a mensagem do switch legado
// preservada (wire protocol inalterado) com CodeUnknownAction.
func (s *Server) Dispatch(req *Request) *Response {
	if s.registry != nil {
		if h, ok := s.registry.Get(req.Action); ok {
			if err := h.Validate(req); err != nil {
				return errorResponse(err)
			}
			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				return errorResponse(err)
			}
			return resp
		}
	}
	return &Response{
		Success: false,
		Code:    CodeUnknownAction,
		Message: "Not suported action: " + req.Action,
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(&Response{
			Success: false,
			Code:    CodeInvalid,
			Message: "Request invalid",
		})
		return
	}

	// Roteador: Dispatch (registry.Get → Validate → Handle). O switch legado
	// foi eliminado (Fase 4) — toda ação conhecida tem handler registrado.
	resp := s.Dispatch(&req)
	_ = json.NewEncoder(conn).Encode(resp)
	// Hook de restart pós-resposta (mesma ordem do legado): o update é o único
	// handler que arma o latch — para os demais é um no-op.
	if s.takeUpdateApplied() {
		s.dispatchUpdateHook()
	}
}
