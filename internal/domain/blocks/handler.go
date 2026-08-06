// Package blocks implements the domain service for the block/block-all IPC
// actions (Fase 4 do refactor-plan). It replaces the block switch cases of the
// ipc.Server with self-contained handlers that depend only on minimal
// interfaces (DIP) — the *scheduler.Scheduler satisfies Blocker by structure,
// without any change to it.
package blocks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"focusguard/internal/ipc"
	"focusguard/internal/domain/policy"
	"focusguard/internal/domain/preset"
)

// Blocker is the scheduler surface the block actions need. The
// *scheduler.Scheduler implements it structurally — handlers never depend on
// the concrete type (B3).
type Blocker interface {
	Block(domain string, duration time.Duration) (*policy.Block, error)
	BlockDomains(domains []string, duration time.Duration) ([]policy.Block, error)
	ExtendBlock(domain string, duration time.Duration) (*policy.Block, error)
	ActiveBlock(domain string) *policy.Block
	BlockAllInternet(allowlist []string, duration time.Duration) (*policy.Block, error)
}

// Catalog resolves presets by name (same surface the ipc.PresetManager
// exposes); *preset.Store and the built-in catalog both satisfy it.
type Catalog interface {
	Resolve(name string) (preset.Preset, error)
}

// Handler executes the "block" action, preserving the switch behavior and
// message order: duration is validated before the target; preset is a valid
// target alone; --extend sums to the active block; the default ask-first
// behavior reports an already-active block as a conflict (never a silent
// overwrite).
type Handler struct {
	blocks  Blocker
	catalog Catalog
}

// New builds the "block" handler. catalog may be nil — a request with a preset
// then fails on resolve (same as the built-in-only fallback of the server).
func New(blocks Blocker, catalog Catalog) *Handler {
	return &Handler{blocks: blocks, catalog: catalog}
}

func (h *Handler) Action() string { return "block" }

// Validate is pure and preserves the error order of the switch: duration is
// validated before the target (a request with both invalid returns the
// duration error, as today).
func (h *Handler) Validate(req *ipc.Request) error {
	d, err := time.ParseDuration(req.Duration)
	if err != nil || d <= 0 {
		return ipc.Err(ipc.CodeDurationInvalid, "Duration invalid. Ex: --duration 4h, 30m")
	}
	if req.Preset == "" && req.Domain == "" {
		return ipc.Err(ipc.CodeDomainRequired, "Informe um domínio ou --preset para bloquear.")
	}
	return nil
}

func (h *Handler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	// Validate garantiu o parse; aqui o re-parse é barato e mantém o Handle
	// independente caso alguém o chame sem Validate.
	d, _ := time.ParseDuration(req.Duration)

	switch {
	case req.Preset != "":
		return h.blockPreset(req, d)
	case req.Extend:
		return h.extend(req, d)
	default:
		return h.blockOrConflict(req, d)
	}
}

func (h *Handler) blockPreset(req *ipc.Request, d time.Duration) (*ipc.Response, error) {
	p, err := h.catalog.Resolve(req.Preset)
	if err != nil {
		return nil, err
	}
	blocks, err := h.blocks.BlockDomains(p.Domains, d)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		// Defensivo: nunca indexar blocks[0] (pânico se um dia vier vazio).
		return &ipc.Response{Success: true, Message: fmt.Sprintf("Preset %s: nenhum domínio novo bloqueado", p.Name)}, nil
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf(
		"Preset %s bloqueado (%d domínios) até %s", p.Name, len(blocks),
		blocks[0].ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}

func (h *Handler) extend(req *ipc.Request, d time.Duration) (*ipc.Response, error) {
	block, err := h.blocks.ExtendBlock(req.Domain, d)
	if err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf(
		"Domain %s extended until %s", block.Domain,
		block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}

func (h *Handler) blockOrConflict(req *ipc.Request, d time.Duration) (*ipc.Response, error) {
	// Ask-first: domínio já bloqueado é CONFLITO para o usuário resolver
	// (somar/substituir), não sobrescrita silenciosa. --replace pula.
	if !req.Replace {
		if existing := h.blocks.ActiveBlock(req.Domain); existing != nil {
			return &ipc.Response{
				Success:       false,
				Code:          ipc.CodeDomainConflict,
				Conflict:      true,
				ConflictBlock: existing,
				Message: fmt.Sprintf("Domínio já bloqueado até %s. Use --extend para somar ou --replace para reiniciar.",
					existing.ExpiresAt.Local().Format("15:04:05 02/01/2006")),
			}, nil
		}
	}
	block, err := h.blocks.Block(req.Domain, d)
	if err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf(
		"Domain %s blocked  %s", block.Domain,
		block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}

// BlockAllHandler executes the "block-all" action: the all-internet sentinel
// with an optional allowlist (deep-focus mode).
type BlockAllHandler struct {
	blocks Blocker
}

// NewBlockAll builds the "block-all" handler.
func NewBlockAll(blocks Blocker) *BlockAllHandler {
	return &BlockAllHandler{blocks: blocks}
}

func (h *BlockAllHandler) Action() string { return "block-all" }

func (h *BlockAllHandler) Validate(req *ipc.Request) error {
	d, err := time.ParseDuration(req.Duration)
	if err != nil || d <= 0 {
		return ipc.Err(ipc.CodeDurationInvalid, "Duration invalid. Ex: --duration 4h, 30m")
	}
	return nil
}

func (h *BlockAllHandler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	d, _ := time.ParseDuration(req.Duration)
	block, err := h.blocks.BlockAllInternet(req.Allowlist, d)
	if err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf(
		"Internet bloqueada até %s%s", block.ExpiresAt.Local().Format("15:04:05 02/01/2006"),
		blockAllModeSuffix(req.Allowlist))}, nil
}

// blockAllModeSuffix describes the block-all flavor for the success message:
// panic mode (all internet) vs deep-focus mode (only the allowlist reachable).
func blockAllModeSuffix(allowlist []string) string {
	if len(allowlist) == 0 {
		return " (toda a internet)"
	}
	return fmt.Sprintf(" (apenas %s permitido)", strings.Join(allowlist, ", "))
}
