// Package presets implements the domain service for the presets/preset-add/
// preset-remove IPC actions (Fase 4 do refactor-plan). The catalog is a
// minimal interface (DIP) — *preset.Store and the read-only built-in catalog
// both satisfy it. Handlers use package-local types; the transport adapts them
// via ipc.DomainAction (pós-reorg item 2).
package presets

import (
	"context"
	"fmt"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/domain/preset"
)

// Catalog is the preset surface the presets actions need.
type Catalog interface {
	List() []preset.Preset
	Add(p preset.Preset) error
	Remove(name string) error
}

// Tipos de entrada/saída das ações — adaptados pelo transporte (DIP).
type NoInput struct{}
type ListResult struct{ Presets []preset.Preset }
type AddInput struct {
	PresetName    string
	PresetLabel   string
	PresetDomains []string
}
type AddResult struct{ Message string }
type RemoveInput struct{ PresetName string }
type RemoveResult struct{ Message string }

// ---------------------------------------------------------------------------
// presets
// ---------------------------------------------------------------------------

// ListHandler executes "presets" (list the whole catalog).
type ListHandler struct {
	catalog Catalog
}

// NewList builds the "presets" handler.
func NewList(catalog Catalog) *ListHandler { return &ListHandler{catalog: catalog} }

func (h *ListHandler) Action() string { return "presets" }

func (h *ListHandler) Validate(*NoInput) error { return nil }

func (h *ListHandler) Handle(ctx context.Context, _ *NoInput) (*ListResult, error) {
	return &ListResult{Presets: h.catalog.List()}, nil
}

// ---------------------------------------------------------------------------
// preset-add
// ---------------------------------------------------------------------------

// AddHandler executes "preset-add".
type AddHandler struct {
	catalog Catalog
}

// NewAdd builds the "preset-add" handler.
func NewAdd(catalog Catalog) *AddHandler { return &AddHandler{catalog: catalog} }

func (h *AddHandler) Action() string { return "preset-add" }

func (h *AddHandler) Validate(*AddInput) error { return nil }

func (h *AddHandler) Handle(ctx context.Context, req *AddInput) (*AddResult, error) {
	if h.catalog == nil {
		// Mesmo caso do builtin-only fallback do server: sem store de presets
		// personalizados, a mutação é rejeitada (tests/dev builds).
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "presets personalizados não configurados")
	}
	err := h.catalog.Add(preset.Preset{
		Name:    req.PresetName,
		Label:   req.PresetLabel,
		Domains: req.PresetDomains,
	})
	if err != nil {
		return nil, err
	}
	return &AddResult{Message: fmt.Sprintf("Preset %s criado (%d domínios)", req.PresetName, len(req.PresetDomains))}, nil
}

// ---------------------------------------------------------------------------
// preset-remove
// ---------------------------------------------------------------------------

// RemoveHandler executes "preset-remove".
type RemoveHandler struct {
	catalog Catalog
}

// NewRemove builds the "preset-remove" handler.
func NewRemove(catalog Catalog) *RemoveHandler { return &RemoveHandler{catalog: catalog} }

func (h *RemoveHandler) Action() string { return "preset-remove" }

func (h *RemoveHandler) Validate(*RemoveInput) error { return nil }

func (h *RemoveHandler) Handle(ctx context.Context, req *RemoveInput) (*RemoveResult, error) {
	if h.catalog == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "presets personalizados não configurados")
	}
	if err := h.catalog.Remove(req.PresetName); err != nil {
		return nil, err
	}
	return &RemoveResult{Message: fmt.Sprintf("Preset %s removido", req.PresetName)}, nil
}
