// Package presets implements the domain service for the presets/preset-add/
// preset-remove IPC actions (Fase 4 do refactor-plan). The catalog is a
// minimal interface (DIP) — *preset.Store and the read-only built-in catalog
// both satisfy it.
package presets

import (
	"context"
	"fmt"

	"focusguard/internal/ipc"
	"focusguard/internal/domain/preset"
)

// Catalog is the preset surface the presets actions need.
type Catalog interface {
	List() []preset.Preset
	Add(p preset.Preset) error
	Remove(name string) error
}

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

func (h *ListHandler) Validate(*ipc.Request) error { return nil }

func (h *ListHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	return &ipc.Response{Success: true, Presets: h.catalog.List()}, nil
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

func (h *AddHandler) Validate(*ipc.Request) error { return nil }

func (h *AddHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.catalog == nil {
		// Mesmo caso do builtin-only fallback do server: sem store de presets
		// personalizados, a mutação é rejeitada (tests/dev builds).
		return nil, ipc.Err(ipc.CodeNotConfigured, "presets personalizados não configurados")
	}
	err := h.catalog.Add(preset.Preset{
		Name:    req.PresetName,
		Label:   req.PresetLabel,
		Domains: req.PresetDomains,
	})
	if err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf("Preset %s criado (%d domínios)", req.PresetName, len(req.PresetDomains))}, nil
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

func (h *RemoveHandler) Validate(*ipc.Request) error { return nil }

func (h *RemoveHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.catalog == nil {
		return nil, ipc.Err(ipc.CodeNotConfigured, "presets personalizados não configurados")
	}
	if err := h.catalog.Remove(req.PresetName); err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf("Preset %s removido", req.PresetName)}, nil
}
