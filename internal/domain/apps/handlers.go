package apps

import (
	"context"
	"fmt"

	"focusguard/internal/domain/ipcerr"
)

// Manager is the process denylist surface the apps-* actions need. The
// *apps.Store and the daemon's guardApps (store + live process guard refresh)
// both satisfy it.
type Manager interface {
	List() []string
	Add(name string) error
	Remove(name string) error
}

// Tipos de entrada/saída das ações — o transporte adapta via ipc.DomainAction
// (os pacotes de domínio nunca importam ipc; DIP — pós-reorg item 2).

// NoInput: ações sem payload de entrada.
type NoInput struct{}

// ListResult é a saída de apps-list.
type ListResult struct{ Apps []string }

// AddInput/AddResult são a entrada/saída de apps-add.
type AddInput struct{ AppName string }
type AddResult struct{ Message string }

// RemoveInput/RemoveResult são a entrada/saída de apps-remove.
type RemoveInput struct{ AppName string }
type RemoveResult struct{ Message string }

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

func (h *ListHandler) Validate(*NoInput) error { return nil }

func (h *ListHandler) Handle(ctx context.Context, _ *NoInput) (*ListResult, error) {
	if h.manager == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "denylist de apps não configurada")
	}
	return &ListResult{Apps: h.manager.List()}, nil
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

func (h *AddHandler) Validate(*AddInput) error { return nil }

func (h *AddHandler) Handle(ctx context.Context, req *AddInput) (*AddResult, error) {
	if h.manager == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := h.manager.Add(req.AppName); err != nil {
		return nil, err
	}
	return &AddResult{Message: fmt.Sprintf("Processo %s adicionado à denylist", req.AppName)}, nil
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

func (h *RemoveHandler) Validate(*RemoveInput) error { return nil }

func (h *RemoveHandler) Handle(ctx context.Context, req *RemoveInput) (*RemoveResult, error) {
	if h.manager == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := h.manager.Remove(req.AppName); err != nil {
		return nil, err
	}
	return &RemoveResult{Message: fmt.Sprintf("Processo %s removido da denylist", req.AppName)}, nil
}
