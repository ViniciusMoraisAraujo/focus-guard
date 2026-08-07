// Handlers das ações stats/missions/sessions — cada ação é um Handler
// auto-contido que depende só da interface mínima Provider (DIP); o
// *analytics.Recorder a satisfaz estruturalmente. Handlers usam tipos
// próprios; o transporte os adapta via ipc.DomainAction (pós-reorg item 1).
package analytics

import "context"

// Tipos de entrada/saída das ações — adaptados pelo transporte (DIP).

// NoInput: ações sem payload de entrada.
type NoInput struct{}

// StatsInput/StatsResult: o relatório agregado (7 dias), opcionalmente
// filtrado por missão (--mission).
type StatsInput struct{ Mission string }
type StatsResult struct{ Stats *Stats }

// MissionsResult: o foco agregado por missão nomeada.
type MissionsResult struct{ LabelStats []LabelStat }

// SessionsResult: as sessões concluídas mais recentes.
type SessionsResult struct{ Sessions []Session }

// ---------------------------------------------------------------------------
// stats
// ---------------------------------------------------------------------------

// StatsHandler executes "stats".
type StatsHandler struct {
	svc *Service
}

// NewStatsHandler builds the "stats" handler. A nil provider makes the action
// fail with the "não configurado" message (tests/dev builds), as the switch
// did.
func NewStatsHandler(p Provider) *StatsHandler { return &StatsHandler{svc: NewService(p)} }

func (h *StatsHandler) Action() string { return "stats" }

func (h *StatsHandler) Validate(*StatsInput) error { return nil }

func (h *StatsHandler) Handle(ctx context.Context, in *StatsInput) (*StatsResult, error) {
	st, err := h.svc.Stats(ctx, in.Mission)
	if err != nil {
		return nil, err
	}
	return &StatsResult{Stats: st}, nil
}

// ---------------------------------------------------------------------------
// missions
// ---------------------------------------------------------------------------

// MissionsHandler executes "missions".
type MissionsHandler struct {
	svc *Service
}

// NewMissionsHandler builds the "missions" handler.
func NewMissionsHandler(p Provider) *MissionsHandler { return &MissionsHandler{svc: NewService(p)} }

func (h *MissionsHandler) Action() string { return "missions" }

func (h *MissionsHandler) Validate(*NoInput) error { return nil }

func (h *MissionsHandler) Handle(ctx context.Context, _ *NoInput) (*MissionsResult, error) {
	ls, err := h.svc.Missions(ctx)
	if err != nil {
		return nil, err
	}
	return &MissionsResult{LabelStats: ls}, nil
}

// ---------------------------------------------------------------------------
// sessions
// ---------------------------------------------------------------------------

// SessionsHandler executes "sessions".
type SessionsHandler struct {
	svc *Service
}

// NewSessionsHandler builds the "sessions" handler.
func NewSessionsHandler(p Provider) *SessionsHandler { return &SessionsHandler{svc: NewService(p)} }

func (h *SessionsHandler) Action() string { return "sessions" }

func (h *SessionsHandler) Validate(*NoInput) error { return nil }

func (h *SessionsHandler) Handle(ctx context.Context, _ *NoInput) (*SessionsResult, error) {
	sessions, err := h.svc.Sessions(ctx)
	if err != nil {
		return nil, err
	}
	return &SessionsResult{Sessions: sessions}, nil
}
