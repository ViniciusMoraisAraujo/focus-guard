package ipc

import (
	"context"
	"fmt"

	"focusguard/internal/domain/schedule"
)

// scheduleManager returns the configured schedule manager under the lock —
// espelho do padrão catalog() usado pelos demais handlers (o
// daemon pode configurá-lo depois de NewServer).
func (s *Server) scheduleManager() ScheduleManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schedules
}

// scheduleService monta o serviço de domínio com as dependências atuais do
// servidor. O Service não pode importar ipc (ipc importa schedule para os
// tipos do wire — Request.ScheduleRule/Response.Schedules), então o adapter
// fica aqui: extrai os campos do Request, chama o serviço e monta o Response.
// Erros estáveis chegam como *ipcerr.Error e o writeError os traduz.
func (s *Server) scheduleService() *schedule.Service {
	return schedule.NewService(s.scheduleManager(), s.catalog())
}

// handleScheduleList devolve o catálogo de regras recorrentes.
func (s *Server) handleScheduleList(ctx context.Context, _ *Request) (*Response, error) {
	rules, err := s.scheduleService().List(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Schedules: rules}, nil
}

// handleScheduleAdd cria uma regra recorrente.
func (s *Server) handleScheduleAdd(ctx context.Context, req *Request) (*Response, error) {
	r, err := s.scheduleService().Add(ctx, req.ScheduleRule)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Regra %s criada: %s das %s às %s", r.ID, r.Preset, r.Start, r.End)}, nil
}

// handleScheduleImport importa um calendário .ics como regras recorrentes.
func (s *Server) handleScheduleImport(ctx context.Context, req *Request) (*Response, error) {
	added, err := s.scheduleService().Import(ctx, req.ICSContent, req.ICSPreset)
	if err != nil {
		return nil, err
	}
	return &Response{
		Success:   true,
		Schedules: added,
		Message:   fmt.Sprintf("%d regras importadas do calendário (preset %s)", len(added), req.ICSPreset),
	}, nil
}

// handleScheduleRemove remove uma regra recorrente.
func (s *Server) handleScheduleRemove(ctx context.Context, req *Request) (*Response, error) {
	if err := s.scheduleService().Remove(ctx, req.ScheduleID); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Regra %s removida", req.ScheduleID)}, nil
}
