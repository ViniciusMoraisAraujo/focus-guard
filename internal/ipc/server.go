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
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
	"focusguard/internal/scheduler"
)

type Server struct {
	scheduler       *scheduler.Scheduler
	listener        net.Listener
	updateChecker   UpdateChecker
	pomodoro        PomodoroRunner
	analytics       AnalyticsProvider
	onUpdateApplied func()

	mu           sync.RWMutex
	updateStatus UpdateStatus
}

// PomodoroRunner is what the server uses to start/stop/query pomodoro
// sessions. The daemon wires a *pomodoro.Controller; tests stub it.
type PomodoroRunner interface {
	Start(pomodoro.Session) (pomodoro.State, error)
	Stop() (pomodoro.State, error)
	Status() pomodoro.State
}

func (s *Server) SetPomodoro(r PomodoroRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pomodoro = r
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
			p, perr := preset.Resolve(req.Preset)
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
		resp = Response{Success: true, Presets: preset.List()}

	case "pomodoro":
		resp = handlePomodoro(s, req)

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
		st := analytics.Summarize(sessions, 7, time.Now())
		resp = Response{Success: true, Stats: st}

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
		s.mu.RUnlock()
		resp.UpdateAvailable = us.Available
		resp.UpdateVersion = us.NewVersion
		resp.CurrentVersion = us.CurrentVersion
		if pg != nil {
			st := pg.Status()
			resp.Pomodoro = &st
		}

	case "ping":
		if err := s.scheduler.Ping(); err != nil {
			resp = Response{Success: false, Message: err.Error()}
		} else {
			resp = Response{Success: true, Message: "pong"}
		}

	case "update":
		s.mu.RLock()
		c := s.updateChecker
		s.mu.RUnlock()
		if c == nil {
			resp = Response{Success: false, Message: "auto-update não configurado"}
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		st, err := c.Check(ctx, true, req.Channel)
		cancel()
		if err != nil {
			resp = Response{Success: false, Message: err.Error()}
			break
		}
		s.mu.Lock()
		s.updateStatus = st
		s.mu.Unlock()
		resp = Response{
			Success:         true,
			UpdateAvailable: st.Available,
			UpdateVersion:   st.NewVersion,
			CurrentVersion:  st.CurrentVersion,
		}
		if st.Applied {
			updateApplied = true
			resp.Message = fmt.Sprintf("Atualização aplicada: %s → %s. O daemon será reiniciado automaticamente.", st.CurrentVersion, st.NewVersion)
		} else if st.Available {
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

// handlePomodoro validates a pomodoro request, resolves the preset to domains
// and hands the session to the runner.
func handlePomodoro(s *Server, req Request) Response {
	s.mu.RLock()
	r := s.pomodoro
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
	if req.WorkMin <= 0 || req.WorkMin > maxPomodoroMinutes {
		return Response{Success: false, Message: fmt.Sprintf("Duração de trabalho inválida (--work entre 1 e %d minutos).", maxPomodoroMinutes)}
	}
	if req.RestMin < 0 || req.RestMin > maxPomodoroMinutes || req.Cycles < 1 || req.Cycles > maxPomodoroCycles {
		return Response{Success: false, Message: fmt.Sprintf("Parâmetros de pomodoro inválidos (--rest entre 0 e %d minutos, --cycles entre 1 e %d).", maxPomodoroMinutes, maxPomodoroCycles)}
	}

	p, err := preset.Resolve(req.Preset)
	if err != nil {
		return Response{Success: false, Message: err.Error()}
	}

	sess := pomodoro.Session{
		Preset:  p.Name,
		Domains: p.Domains,
		Work:    time.Duration(req.WorkMin) * time.Minute,
		Rest:    time.Duration(req.RestMin) * time.Minute,
		Cycles:  req.Cycles,
		Strict:  req.Strict,
	}
	st, err := r.Start(sess)
	if err != nil {
		return Response{Success: false, Message: err.Error()}
	}
	return Response{
		Success:  true,
		Message:  fmt.Sprintf("Pomodoro %s iniciado: %d ciclos de %dm trabalho / %dm descanso", p.Name, sess.Cycles, req.WorkMin, req.RestMin),
		Pomodoro: &st,
	}
}
