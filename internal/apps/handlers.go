package apps

import (
	"context"
	"fmt"

	"focusguard/internal/ipc"
)

// Manager is the process denylist surface the apps-* actions need. The
// *apps.Store and the daemon's guardApps (store + live process guard refresh)
// both satisfy it.
type Manager interface {
	List() []string
	Add(name string) error
	Remove(name string) error
}

// ---------------------------------------------------------------------------
// apps-list
// ---------------------------------------------------------------------------

// ListHandler executes "apps-list".
type ListHandler struct {
	manager Manager
}

// NewList builds the "apps-list" handler. A nil store makes the action fail
// with the "não configurado" message (tests/dev builds), as the switch did.
func NewList(manager Manager) *ListHandler { return &ListHandler{manager: manager} }

func (h *ListHandler) Action() string { return "apps-list" }

func (h *ListHandler) Validate(*ipc.Request) error { return nil }

func (h *ListHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.manager == nil {
		return nil, ipc.Err(ipc.CodeNotConfigured, "denylist de apps não configurada")
	}
	return &ipc.Response{Success: true, Apps: h.manager.List()}, nil
}

// ---------------------------------------------------------------------------
// apps-add
// ---------------------------------------------------------------------------

// AddHandler executes "apps-add".
type AddHandler struct {
	manager Manager
}

// NewAdd builds the "apps-add" handler.
func NewAdd(manager Manager) *AddHandler { return &AddHandler{manager: manager} }

func (h *AddHandler) Action() string { return "apps-add" }

func (h *AddHandler) Validate(*ipc.Request) error { return nil }

func (h *AddHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.manager == nil {
		return nil, ipc.Err(ipc.CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := h.manager.Add(req.AppName); err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf("Processo %s adicionado à denylist", req.AppName)}, nil
}

// ---------------------------------------------------------------------------
// apps-remove
// ---------------------------------------------------------------------------

// RemoveHandler executes "apps-remove".
type RemoveHandler struct {
	manager Manager
}

// NewRemove builds the "apps-remove" handler.
func NewRemove(manager Manager) *RemoveHandler { return &RemoveHandler{manager: manager} }

func (h *RemoveHandler) Action() string { return "apps-remove" }

func (h *RemoveHandler) Validate(*ipc.Request) error { return nil }

func (h *RemoveHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.manager == nil {
		return nil, ipc.Err(ipc.CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := h.manager.Remove(req.AppName); err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf("Processo %s removido da denylist", req.AppName)}, nil
}
