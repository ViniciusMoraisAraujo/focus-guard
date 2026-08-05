package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/dnsserver"
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
	"focusguard/internal/schedule"
	"focusguard/internal/scheduler"
	"focusguard/internal/tamper"
)

// updateTimeout bounds the update/update-check IPC actions. Aplicar uma
// atualização baixa o release (~13 MB), extrai e troca os binários — 30s era
// apertado em conexões lentas; 120s cobre download + apply sem travar o daemon
// (cada conexão IPC roda na própria goroutine, então pings não são bloqueados).
const updateTimeout = 120 * time.Second

// errUpdateNotConfigured is returned when no update checker is wired up
// (dev builds).
var errUpdateNotConfigured = errors.New("auto-update não configurado")

// maxSessionsReturned caps the "sessions" IPC response so the UI never
// receives the whole analytics file (a long-time user can have thousands of
// lines).
const maxSessionsReturned = 50

type Server struct {
	scheduler       *scheduler.Scheduler
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
	dnsCtrl         DNSController
	onUpdateApplied func()
	currentVersion  string

	mu           sync.RWMutex
	updateStatus UpdateStatus
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
// actions fail with a clear message.
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

// DNSController drives the DNS sinkhole server lifecycle used by the
// dns-start/dns-stop/dns-status actions. The daemon wires a
// *dnsserver.Controller (bound to the scheduler as the policy checker); tests
// stub it. The persisted enabled flag lives in the scheduler, not the
// controller — the actions combine both for status.
type DNSController interface {
	Start() error
	Stop() error
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
	return &Server{scheduler: sched}
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

// runUpdateCheck runs the update checker for the given channel, caches the
// result (so the status action surfaces it) and returns it. apply=true also
// applies the update to the binaries. Shared by the "update" and
// "update-check" IPC actions.
func (s *Server) runUpdateCheck(ctx context.Context, apply bool, channel string) (UpdateStatus, error) {
	s.mu.RLock()
	c := s.updateChecker
	s.mu.RUnlock()
	if c == nil {
		return UpdateStatus{}, errUpdateNotConfigured
	}
	st, err := c.Check(ctx, apply, channel)
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

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(&Response{
			Success: false,
			Message: "Request invalid",
		})
		return
	}

	var resp Response
	updateApplied := false

	switch req.Action {
	case "block":
		d, err := time.ParseDuration(req.Duration)
		// d <= 0 também é rejeitado: um bloqueio de 0s expiraria imediatamente
		// (bloqueio sem efeito que ainda aplica/remove regras de firewall).
		if err != nil || d <= 0 {
			resp = Response{
				Success: false,
				Message: "Duration invalid. Ex: --duration 4h, 30m"}
			break
		}
		if req.Preset != "" {
			p, perr := s.catalog().Resolve(req.Preset)
			if perr != nil {
				resp = Response{Success: false, Message: perr.Error()}
				break
			}
			blocks, berr := s.scheduler.BlockDomains(p.Domains, d)
			if berr != nil {
				resp = Response{Success: false, Message: berr.Error()}
			} else if len(blocks) == 0 {
				// Defensivo: nunca indexar blocks[0] — evita pânico no servidor se
				// o scheduler um dia retornar sucesso sem blocos.
				resp = Response{Success: true, Message: fmt.Sprintf("Preset %s: nenhum domínio novo bloqueado", p.Name)}
			} else {
				resp = Response{
					Success: true,
					Message: fmt.Sprintf("Preset %s bloqueado (%d domínios) até %s", p.Name, len(blocks), blocks[0].ExpiresAt.Local().Format("15:04:05 02/01/2006")),
				}
			}
			break
		}
		if req.Domain == "" {
			resp = Response{Success: false, Message: "Informe um domínio ou --preset para bloquear."}
			break
		}
		// --extend: soma a duração ao bloqueio ativo (ou cria um novo se não
		// houver). Não passa pela detecção de conflito.
		if req.Extend {
			block, err := s.scheduler.ExtendBlock(req.Domain, d)
			if err != nil {
				resp = Response{Success: false, Message: err.Error()}
			} else {
				resp = Response{
					Success: true,
					Message: fmt.Sprintf("Domain %s extended until %s", block.Domain, block.ExpiresAt.Local().Format("15:04:05 02/01/2006")),
				}
			}
			break
		}
		// Comportamento padrão (ask-first): um domínio já bloqueado é um
		// CONFLITO a ser resolvido pelo usuário (somar/substituir), não um
		// sobrescrita silenciosa. --replace pula o conflito e reinicia a janela.
		if !req.Replace {
			if existing := s.scheduler.ActiveBlock(req.Domain); existing != nil {
				resp = Response{
					Success:       false,
					Conflict:      true,
					ConflictBlock: existing,
					Message: fmt.Sprintf("Domínio já bloqueado até %s. Use --extend para somar ou --replace para reiniciar.",
						existing.ExpiresAt.Local().Format("15:04:05 02/01/2006")),
				}
				break
			}
		}
		block, err := s.scheduler.Block(req.Domain, d)
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{
				Success: true,
				Message: fmt.Sprintf("Domain %s blocked  %s", block.Domain, block.ExpiresAt.Local().Format("15:04:05 02/01/2006")),
			}
		}

	case "presets":
		resp = Response{Success: true, Presets: s.catalog().List()}

	case "preset-add":
		c := s.catalog()
		err := c.Add(preset.Preset{
			Name:    req.PresetName,
			Label:   req.PresetLabel,
			Domains: req.PresetDomains,
		})
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: fmt.Sprintf("Preset %s criado (%d domínios)", req.PresetName, len(req.PresetDomains))}
		}

	case "preset-remove":
		c := s.catalog()
		if err := c.Remove(req.PresetName); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: fmt.Sprintf("Preset %s removido", req.PresetName)}
		}

	case "block-all":
		d, err := time.ParseDuration(req.Duration)
		if err != nil || d <= 0 {
			resp = Response{
				Success: false,
				Message: "Duration invalid. Ex: --duration 4h, 30m"}
			break
		}
		block, err := s.scheduler.BlockAllInternet(req.Allowlist, d)
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{
				Success: true,
				Message: fmt.Sprintf("Internet bloqueada até %s%s", block.ExpiresAt.Local().Format("15:04:05 02/01/2006"), blockAllModeSuffix(req.Allowlist)),
			}
		}

	case "tamper-log":
		s.mu.RLock()
		p := s.tamperLog
		s.mu.RUnlock()
		if p == nil {
			resp = Response{Success: false, Message: "tamper-log não configurado"}
			break
		}
		events, err := p.Events()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		resp = Response{Success: true, TamperLog: events}

	case "apps-list":
		s.mu.RLock()
		am := s.apps
		s.mu.RUnlock()
		if am == nil {
			resp = Response{Success: false, Message: "denylist de apps não configurada"}
			break
		}
		resp = Response{Success: true, Apps: am.List()}

	case "apps-add":
		s.mu.RLock()
		am := s.apps
		s.mu.RUnlock()
		if am == nil {
			resp = Response{Success: false, Message: "denylist de apps não configurada"}
			break
		}
		if err := am.Add(req.AppName); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: fmt.Sprintf("Processo %s adicionado à denylist", req.AppName)}
		}

	case "apps-remove":
		s.mu.RLock()
		am := s.apps
		s.mu.RUnlock()
		if am == nil {
			resp = Response{Success: false, Message: "denylist de apps não configurada"}
			break
		}
		if err := am.Remove(req.AppName); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: fmt.Sprintf("Processo %s removido da denylist", req.AppName)}
		}

	case "schedule-list":
		s.mu.RLock()
		sm := s.schedules
		s.mu.RUnlock()
		if sm == nil {
			resp = Response{Success: false, Message: "agendamento não configurado"}
			break
		}
		resp = Response{Success: true, Schedules: sm.List()}

	case "schedule-add":
		s.mu.RLock()
		sm := s.schedules
		s.mu.RUnlock()
		if sm == nil {
			resp = Response{Success: false, Message: "agendamento não configurado"}
			break
		}
		if r, err := sm.Add(req.ScheduleRule); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: fmt.Sprintf("Regra %s criada: %s das %s às %s", r.ID, r.Preset, r.Start, r.End)}
		}

	case "schedule-import":
		s.mu.RLock()
		sm := s.schedules
		s.mu.RUnlock()
		if sm == nil {
			resp = Response{Success: false, Message: "agendamento não configurado"}
			break
		}
		if strings.TrimSpace(req.ICSPreset) == "" {
			resp = Response{Success: false, Message: "Informe o preset do calendário (ex: --preset social)."}
			break
		}
		if strings.TrimSpace(req.ICSContent) == "" {
			resp = Response{Success: false, Message: "Arquivo .ics vazio."}
			break
		}
		// Preset inexistente seria importado silenciosamente e nunca aplicado
		// (o worker pula presets que não resolvem) — valida cedo via catálogo.
		if _, err := s.catalog().Resolve(req.ICSPreset); err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		added, err := sm.ImportICS([]byte(req.ICSContent), req.ICSPreset)
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		if len(added) == 0 {
			resp = Response{Success: false, Message: "Nenhum evento semanal encontrado no calendário."}
			break
		}
		resp = Response{
			Success:   true,
			Schedules: added,
			Message:   fmt.Sprintf("%d regras importadas do calendário (preset %s)", len(added), req.ICSPreset),
		}

	case "schedule-remove":
		s.mu.RLock()
		sm := s.schedules
		s.mu.RUnlock()
		if sm == nil {
			resp = Response{Success: false, Message: "agendamento não configurado"}
			break
		}
		if err := sm.Remove(req.ScheduleID); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: fmt.Sprintf("Regra %s removida", req.ScheduleID)}
		}

	case "pomodoro":
		resp = handlePomodoro(s, req)

	case "pomodoro-defaults":
		s.mu.RLock()
		prefs := s.pomodoroPrefs
		s.mu.RUnlock()
		if prefs == nil {
			resp = Response{Success: false, Message: "preferências de pomodoro não configuradas"}
			break
		}
		// Consulta os padrões atuais: work/cycles não informados (0) e rest
		// não informado (-1) — 0 é um valor legítimo para rest (sem descanso).
		work, rest, cycles := prefs.Resolve(0, -1, 0)
		resp = Response{
			Success:       true,
			PomodoroWork:  work,
			PomodoroRest:  rest,
			PomodoroCycle: cycles,
			Message:       fmt.Sprintf("Padrões atuais: %dm trabalho / %dm descanso / %d ciclos", work, rest, cycles),
		}

	case "pomodoro-stop":
		s.mu.RLock()
		r := s.pomodoro
		s.mu.RUnlock()
		if r == nil {
			resp = Response{Success: false, Message: "pomodoro não configurado"}
			break
		}
		if st, err := r.Stop(); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: "Pomodoro encerrado", Pomodoro: &st}
		}

	case "goal-get":
		s.mu.RLock()
		g := s.goalStore
		s.mu.RUnlock()
		if g == nil {
			resp = Response{Success: false, Message: "meta diária não configurada"}
			break
		}
		resp = Response{Success: true, Goal: g.Get()}

	case "goal-set":
		s.mu.RLock()
		g := s.goalStore
		s.mu.RUnlock()
		if g == nil {
			resp = Response{Success: false, Message: "meta diária não configurada"}
			break
		}
		if req.GoalMinutes <= 0 || req.GoalMinutes > 24*60 {
			resp = Response{Success: false, Message: "meta inválida (entre 1 e 1440 minutos)"}
			break
		}
		if err := g.Set(time.Duration(req.GoalMinutes) * time.Minute); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Goal: g.Get(), Message: fmt.Sprintf("Meta diária definida: %s", (time.Duration(req.GoalMinutes) * time.Minute).Round(time.Minute))}
		}

	case "stats":
		s.mu.RLock()
		p := s.analytics
		s.mu.RUnlock()
		if p == nil {
			resp = Response{Success: false, Message: "analytics não configurado"}
			break
		}
		sessions, err := p.Sessions()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		var st *analytics.Stats
		if req.Mission != "" {
			st = analytics.SummarizeByLabel(sessions, req.Mission, 7, time.Now())
		} else {
			st = analytics.Summarize(sessions, 7, time.Now())
		}
		resp = Response{Success: true, Stats: st}

	case "missions":
		s.mu.RLock()
		p := s.analytics
		s.mu.RUnlock()
		if p == nil {
			resp = Response{Success: false, Message: "analytics não configurado"}
			break
		}
		sessions, err := p.Sessions()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		resp = Response{Success: true, LabelStats: analytics.SummarizeLabels(sessions)}

	case "sessions":
		s.mu.RLock()
		p := s.analytics
		s.mu.RUnlock()
		if p == nil {
			resp = Response{Success: false, Message: "analytics não configurado"}
			break
		}
		sessions, err := p.Sessions()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		resp = Response{Success: true, Sessions: analytics.RecentSessions(sessions, maxSessionsReturned)}

	case "dns-start":
		s.mu.RLock()
		c := s.dnsCtrl
		s.mu.RUnlock()
		if c == nil {
			resp = Response{Success: false, Message: "servidor DNS não configurado"}
			break
		}
		if err := c.Start(); err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		// Persiste o flag só depois de o listener subir; se a gravação falhar,
		// desliga o servidor para o estado nunca ficar "ligado mas não
		// persistido" (no próximo boot voltaria desligado).
		if err := s.scheduler.SetDNSEnabled(true); err != nil {
			_ = c.Stop()
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		resp = Response{Success: true, Message: "Servidor DNS iniciado em " + c.Status().Addr}
		mergeDNS(&resp, c.Status(), s.scheduler.DNSEnabled())

	case "dns-stop":
		s.mu.RLock()
		c := s.dnsCtrl
		s.mu.RUnlock()
		if c == nil {
			resp = Response{Success: false, Message: "servidor DNS não configurado"}
			break
		}
		if err := c.Stop(); err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		if err := s.scheduler.SetDNSEnabled(false); err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		resp = Response{Success: true, Message: "Servidor DNS desligado"}
		mergeDNS(&resp, c.Status(), s.scheduler.DNSEnabled())

	case "dns-status":
		s.mu.RLock()
		c := s.dnsCtrl
		s.mu.RUnlock()
		if c == nil {
			resp = Response{Success: false, Message: "servidor DNS não configurado"}
			break
		}
		resp = Response{Success: true}
		mergeDNS(&resp, c.Status(), s.scheduler.DNSEnabled())

	case "status":
		blocks, err := s.scheduler.ListBlocks()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Blocks: blocks}
		}
		if ps, err := s.scheduler.ProtectionStatus(); err == nil {
			resp.ExpectedDoH = ps.ExpectedDoH
			resp.DoHActive = ps.DoHActive
			resp.FirewallRules = ps.FirewallRules
		} else {
			resp.ProtectionError = err.Error()
		}
		s.mu.RLock()
		us := s.updateStatus
		pg := s.pomodoro
		gs := s.goalStore
		cur := s.currentVersion
		c := s.dnsCtrl
		s.mu.RUnlock()
		resp.UpdateAvailable = us.Available
		resp.UpdateVersion = us.NewVersion
		resp.CurrentVersion = us.CurrentVersion
		if pg != nil {
			st := pg.Status()
			resp.Pomodoro = &st
		}
		if gs != nil {
			resp.Goal = gs.Get()
		}
		if resp.CurrentVersion == "" {
			resp.CurrentVersion = cur
		}
		// DNS sinkhole: enabled vem do scheduler (persistido); o restante vem
		// do controller (estado vivo + contadores). Sem controller (dev), o
		// status ainda informa o flag persistido.
		if c != nil {
			mergeDNS(&resp, c.Status(), s.scheduler.DNSEnabled())
		} else {
			resp.DNSEnabled = s.scheduler.DNSEnabled()
		}

	case "ping":
		if err := s.scheduler.Ping(); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: "pong"}
		}

	case "update":
		ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
		st, err := s.runUpdateCheck(ctx, true, req.Channel)
		cancel()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		resp = Response{
			Success:             true,
			UpdateAvailable:     st.Available,
			UpdateVersion:       st.NewVersion,
			CurrentVersion:      st.CurrentVersion,
			UpdatePendingReboot: st.PendingReboot,
		}
		if st.Applied {
			updateApplied = true
			resp.Message = fmt.Sprintf("Atualização aplicada: %s → %s. O daemon será reiniciado automaticamente.", st.CurrentVersion, st.NewVersion)
		} else if st.PendingReboot {
			// Fallback move-on-reboot: a troca completa no próximo boot — o
			// daemon NÃO reinicia (updateApplied fica false) e segue servindo.
			resp.Message = fmt.Sprintf("Atualização será concluída no próximo reinício do computador: %s → %s", st.CurrentVersion, st.NewVersion)
		} else if st.Available {
			resp.Message = fmt.Sprintf("Atualização disponível: %s → %s", st.CurrentVersion, st.NewVersion)
		} else {
			resp.Message = "Nenhuma atualização disponível."
		}

	case "update-check":
		// Verificação explícita (sem aplicar): usada pela UI no botão
		// "Verificar" para consultar o GitHub na hora, em vez de ler o cache
		// do status (que só atualiza a cada 24h).
		ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
		st, err := s.runUpdateCheck(ctx, false, req.Channel)
		cancel()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		resp = Response{
			Success:         true,
			UpdateAvailable: st.Available,
			UpdateVersion:   st.NewVersion,
			CurrentVersion:  st.CurrentVersion,
		}
		if st.Available {
			resp.Message = fmt.Sprintf("Atualização disponível: %s → %s", st.CurrentVersion, st.NewVersion)
		} else {
			resp.Message = "Nenhuma atualização disponível."
		}
	default:
		resp = Response{Success: false, Message: "Not suported action: " + req.Action}
	}

	_ = json.NewEncoder(conn).Encode(resp)

	// Notifica o hook de restart APÓS a resposta já ter sido escrita no socket
	// (o CLI foca no fflush antes do daemon sair do processo). O hook faz o
	// daemon encerrar para o systemd/watchdog subirem o binário novo — sem
	// isso ficaria o "daemon zumbi" rodando a versão antiga em RAM.
	if updateApplied {
		s.mu.RLock()
		fn := s.onUpdateApplied
		s.mu.RUnlock()
		if fn != nil {
			go fn()
		}
	}
}

// blockAllModeSuffix describes the block-all flavor for the success message:
// panic mode (all internet) vs deep-focus mode (only the allowlist reachable).
func blockAllModeSuffix(allowlist []string) string {
	if len(allowlist) == 0 {
		return " (toda a internet)"
	}
	return fmt.Sprintf(" (apenas %s permitido)", strings.Join(allowlist, ", "))
}

// handlePomodoro validates a pomodoro request, resolves the preset to domains
// and hands the session to the runner.
func handlePomodoro(s *Server, req Request) Response {
	s.mu.RLock()
	r := s.pomodoro
	prefs := s.pomodoroPrefs
	s.mu.RUnlock()
	if r == nil {
		return Response{Success: false, Message: "pomodoro não configurado"}
	}

	if strings.TrimSpace(req.Preset) == "" {
		return Response{Success: false, Message: "Informe um preset (ex: --preset social)."}
	}
	// Tetos defensivos: time.Duration(req.WorkMin)*time.Minute faria overflow
	// (wrap) no int64 para valores gigantes — um --work 1e9 virava uma sessão
	// de ~147 anos em vez de erro. O mesmo vale para o acumulador focus do
	// controller (focus += s.Work por ciclo): um --cycles 1e9 transbordaria o
	// tempo registrado no analytics. Uma semana por fase / 1k ciclos já é
	// absurdo para um pomodoro, então estes tetos nunca atrapalham o uso real.
	const (
		maxPomodoroMinutes = 7 * 24 * 60 // 7 dias por fase
		maxPomodoroCycles  = 1000
	)

	// Resolve defaults salvos: a CLI envia 0 (não informado) para work/cycles
	// e -1 (não informado) para rest — 0 é um valor legítimo para rest (sem
	// descanso). Sem prefs configuradas, caem no clássico 25/5/4.
	work, rest, cycles := req.WorkMin, req.RestMin, req.Cycles
	if prefs != nil {
		work, rest, cycles = prefs.Resolve(work, rest, cycles)
	}
	if work <= 0 || work > maxPomodoroMinutes {
		return Response{Success: false, Message: fmt.Sprintf("Duração de trabalho inválida (--work entre 1 e %d minutos).", maxPomodoroMinutes)}
	}
	if rest < 0 || rest > maxPomodoroMinutes || cycles < 1 || cycles > maxPomodoroCycles {
		return Response{Success: false, Message: fmt.Sprintf("Parâmetros de pomodoro inválidos (--rest entre 0 e %d minutos, --cycles entre 1 e %d).", maxPomodoroMinutes, maxPomodoroCycles)}
	}

	p, err := s.catalog().Resolve(req.Preset)
	if err != nil {
		return Response{Success: false, Message: err.Error()}
	}

	sess := pomodoro.Session{
		Preset:  p.Name,
		Label:   req.Label,
		Domains: p.Domains,
		Work:    time.Duration(work) * time.Minute,
		Rest:    time.Duration(rest) * time.Minute,
		Cycles:  cycles,
		Strict:  req.Strict,
	}
	st, err := r.Start(sess)
	if err != nil {
		return Response{Success: false, Message: err.Error()}
	}

	msg := fmt.Sprintf("Pomodoro %s iniciado: %d ciclos de %dm trabalho / %dm descanso", p.Name, sess.Cycles, work, rest)
	if req.Save && prefs != nil {
		prefs.Remember(work, rest, cycles)
		msg += " (padrões salvos para a próxima sessão)"
	}
	return Response{
		Success:  true,
		Message:  msg,
		Pomodoro: &st,
	}
}
