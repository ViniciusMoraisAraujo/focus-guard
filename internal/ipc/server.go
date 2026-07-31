package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"focusguard/internal/scheduler"
)

type Server struct {
	scheduler     *scheduler.Scheduler
	listener      net.Listener
	updateChecker UpdateChecker

	mu           sync.RWMutex
	updateStatus UpdateStatus
}

func NewServer(sched *scheduler.Scheduler) *Server {
	return &Server{scheduler: sched}
}

func (s *Server) SetUpdateChecker(c UpdateChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateChecker = c
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
	st, err := c.Check(ctx, false)
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

	switch req.Action {
	case "block":
		d, err := time.ParseDuration(req.Duration)
		if err != nil || d < 0 {
			resp = Response{
				Success: false,
				Message: "Duration invalid. Ex: --duration 4h, 30m"}
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
		s.mu.RUnlock()
		resp.UpdateAvailable = us.Available
		resp.UpdateVersion = us.NewVersion
		resp.CurrentVersion = us.CurrentVersion

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
		st, err := c.Check(ctx, true)
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
		if st.Available {
			resp.Message = fmt.Sprintf("Atualização aplicada: %s → %s", st.CurrentVersion, st.NewVersion)
		} else {
			resp.Message = "Nenhuma atualização disponível."
		}

	default:
		resp = Response{Success: false, Message: "Not suported action: " + req.Action}
	}

	_ = json.NewEncoder(conn).Encode(resp)
}
