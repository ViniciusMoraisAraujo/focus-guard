// Handler do domínio telemetry para a ação IPC "dns-telemetry" (Fase 1.2 do
// features-plan): lista as queries bloqueadas recentes + o resumo agregado por
// domínio, alimentando a seção "Atividade bloqueada" da tela Rede. Segue o
// padrão DIP pós-reorg: tipos próprios, adaptado ao wire via ipc.DomainAction
// no composition root.
package telemetry

import (
	"context"
)

// Querier é a superfície de leitura que o handler precisa (satisfeita por
// *Recorder).
type Querier interface {
	Queries() ([]BlockedQuery, error)
}

// TelemetryInput é o payload da ação: quantas entradas recentes devolver.
type TelemetryInput struct {
	Limit int
}

// TelemetryResult é a resposta: as entradas recentes (mais novas primeiro) e
// o resumo agregado por domínio.
type TelemetryResult struct {
	Entries      []BlockedQuery `json:"entries"`
	TotalBlocked int            `json:"total_blocked"`
	Summary      []Summary      `json:"summary"`
	// Limit echoa o limite aplicado pelo handler (default 50), para o wire
	// informar a UI quantas entradas vieram.
	Limit int `json:"limit,omitempty"`
}

// GetHandler executa "dns-telemetry".
type GetHandler struct {
	rec Querier
}

// NewGetHandler builds the "dns-telemetry" handler.
func NewGetHandler(rec Querier) *GetHandler {
	return &GetHandler{rec: rec}
}

func (h *GetHandler) Action() string { return "dns-telemetry" }

func (h *GetHandler) Validate(*TelemetryInput) error { return nil }

func (h *GetHandler) Handle(_ context.Context, in *TelemetryInput) (*TelemetryResult, error) {
	qs, err := h.rec.Queries()
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return &TelemetryResult{
		Entries:      Recent(qs, limit),
		TotalBlocked: len(qs),
		Summary:      Summarize(qs),
		Limit:        limit,
	}, nil
}
