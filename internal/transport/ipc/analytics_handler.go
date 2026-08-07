package ipc

import (
	"context"

	"focusguard/internal/domain/analytics"
)

// analyticsProvider returns the configured analytics provider under the lock —
// espelho do padrão catalog() usado pelos demais handlers (o
// daemon pode configurá-lo depois de NewServer).
func (s *Server) analyticsProvider() AnalyticsProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.analytics
}

// ---------------------------------------------------------------------------
// stats / missions / sessions
// ---------------------------------------------------------------------------
//
// Os três relatórios de foco delegam ao serviço de domínio analytics.Service
// (Fase 4). O Service não pode importar ipc (ipc importa analytics para os
// tipos do wire — Response embute Stats/LabelStat/Session), então o adapter
// fica aqui: extrai o campo do Request, chama o serviço e monta o Response.
// Erros estáveis chegam como *ipcerr.Error e o writeError os traduz.

// handleStats devolve o relatório agregado de foco (últimos 7 dias),
// opcionalmente filtrado por missão.
func (s *Server) handleStats(ctx context.Context, req *Request) (*Response, error) {
	st, err := analytics.NewService(s.analyticsProvider()).Stats(ctx, req.Mission)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Stats: st}, nil
}

// handleMissions devolve o foco agregado por missão nomeada.
func (s *Server) handleMissions(ctx context.Context, _ *Request) (*Response, error) {
	ls, err := analytics.NewService(s.analyticsProvider()).Missions(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, LabelStats: ls}, nil
}

// handleSessions devolve as sessões concluídas mais recentes (mais novas
// primeiro, limitadas — o teto vive no serviço de domínio).
func (s *Server) handleSessions(ctx context.Context, _ *Request) (*Response, error) {
	sessions, err := analytics.NewService(s.analyticsProvider()).Sessions(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Sessions: sessions}, nil
}
