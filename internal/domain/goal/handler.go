// Package goal implements the domain service for the goal-get/goal-set IPC
// actions (daily focus goal — Fase 4 do refactor-plan). Each action is a
// self-contained Handler depending only on the minimal Manager interface (DIP);
// the *goal.Store satisfies it structurally.
package goal

import (
	"context"
	"fmt"
	"time"

	"focusguard/internal/transport/ipc"
)

// Manager is the daily focus goal surface the goal-* actions need.
type Manager interface {
	Get() time.Duration
	Set(d time.Duration) error
}

// ---------------------------------------------------------------------------
// goal-get
// ---------------------------------------------------------------------------

// GetHandler executes "goal-get" (current daily focus goal).
type GetHandler struct {
	store Manager
}

// NewGet builds the "goal-get" handler. A nil store makes the action fail
// with the "não configurado" message (tests/dev builds), as the switch did.
func NewGet(store Manager) *GetHandler { return &GetHandler{store: store} }

func (h *GetHandler) Action() string { return "goal-get" }

func (h *GetHandler) Validate(*ipc.Request) error { return nil }

func (h *GetHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.store == nil {
		return nil, ipc.Err(ipc.CodeNotConfigured, "meta diária não configurada")
	}
	return &ipc.Response{Success: true, Goal: h.store.Get()}, nil
}

// ---------------------------------------------------------------------------
// goal-set
// ---------------------------------------------------------------------------

// SetHandler executes "goal-set". A validação fica no Handle (e não no
// Validate) para preservar a ordem do switch legado: o store não configurado
// é verificado antes do range — comportamento idêntico.
type SetHandler struct {
	store Manager
}

// NewSet builds the "goal-set" handler.
func NewSet(store Manager) *SetHandler { return &SetHandler{store: store} }

func (h *SetHandler) Action() string { return "goal-set" }

func (h *SetHandler) Validate(*ipc.Request) error { return nil }

func (h *SetHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.store == nil {
		return nil, ipc.Err(ipc.CodeNotConfigured, "meta diária não configurada")
	}
	if req.GoalMinutes <= 0 || req.GoalMinutes > 24*60 {
		return nil, ipc.Err(ipc.CodeInvalid, "meta inválida (entre 1 e 1440 minutos)")
	}
	d := time.Duration(req.GoalMinutes) * time.Minute
	if err := h.store.Set(d); err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Goal: h.store.Get(), Message: fmt.Sprintf("Meta diária definida: %s", d.Round(time.Minute))}, nil
}
