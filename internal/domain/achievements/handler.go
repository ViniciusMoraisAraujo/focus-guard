// Handlers da ação achievements-get — handler auto-contido que depende só da
// interface mínima Provider (DIP); o *analytics.Recorder a satisfaz
// estruturalmente. O transporte o adapta via ipc.DomainAction.
package achievements

import (
	"context"
	"time"

	"focusguard/internal/domain/analytics"
	"focusguard/internal/domain/ipcerr"
)

// NoInput: a ação não precisa de payload.
type NoInput struct{}

// Result é a resposta de achievements-get: as badges com unlocked/progress
// derivados das stats atuais.
type Result struct {
	Achievements []Achievement
}

// Provider fornece as sessões (satisfeito por *analytics.Recorder).
type Provider interface {
	Sessions() ([]analytics.Session, error)
}

// Handler executa "achievements-get".
type Handler struct {
	provider Provider
}

// New builds the "achievements-get" handler. Provider nil → CodeNotConfigured.
func New(provider Provider) *Handler { return &Handler{provider: provider} }

func (h *Handler) Action() string { return "achievements-get" }

func (h *Handler) Validate(*NoInput) error { return nil }

func (h *Handler) Handle(_ context.Context, _ *NoInput) (*Result, error) {
	if h.provider == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "achievements não configurado")
	}
	sessions, err := h.provider.Sessions()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	st := analytics.Summarize(sessions, 7, now)
	return &Result{Achievements: Calculate(st, sessions, now)}, nil
}
