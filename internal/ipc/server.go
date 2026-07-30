package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"focusguard/internal/scheduler"
)

type Server struct {
	scheduler *scheduler.Scheduler
	listener  net.Listener
}

func NewServer(sched *scheduler.Scheduler) *Server {
	return &Server{scheduler: sched}
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

	default:
		resp = Response{Success: false, Message: "Not suported action: " + req.Action}
	}

	_ = json.NewEncoder(conn).Encode(resp)
}
