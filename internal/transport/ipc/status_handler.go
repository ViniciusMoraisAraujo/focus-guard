package ipc

import (
	"context"
)

// handleStatus agrega o estado completo do sistema: bloqueios ativos, proteção
// DoH/firewall, update disponível (cache), pomodoro, meta diária, versão e DNS.
// Sucesso não implica bloqueios: a resposta vai com Success=false só quando o
// ListBlocks falha (mesma semântica do switch legado — os demais campos são
// preenchidos mesmo assim).
func (s *Server) handleStatus(_ context.Context, _ *Request) (*Response, error) {
	resp := &Response{}
	blocks, err := s.scheduler.ListBlocks()
	if err != nil {
		resp.Success = false
		resp.Message = err.Error()
	} else {
		resp.Success = true
		resp.Blocks = blocks
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
	// Focus Interceptor Page (Fase 3): flag persistido para a tela
	// Configurações ligar/desligar a página de bloqueio.
	resp.InterceptorEnabled = s.scheduler.InterceptorEnabled()
	return resp, nil
}
