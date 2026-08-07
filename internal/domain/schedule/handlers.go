// Handlers das ações schedule-list/add/import/remove — cada ação é um Handler
// auto-contido que depende só das interfaces mínimas do Service (RuleStore,
// PresetResolver — DIP); o *schedule.Manager e o catálogo de presets as
// satisfazem estruturalmente. Handlers usam tipos próprios; o transporte os
// adapta via ipc.DomainAction (pós-reorg item 1).
package schedule

import (
	"context"
	"fmt"
)

// Tipos de entrada/saída das ações — adaptados pelo transporte (DIP).

// NoInput: ações sem payload de entrada.
type NoInput struct{}

// ListResult: o catálogo de regras recorrentes.
type ListResult struct{ Schedules []Rule }

// AddInput/AddResult: criar uma regra recorrente.
type AddInput struct{ Rule Rule }
type AddResult struct {
	Rule    Rule
	Message string
}

// ImportInput/ImportResult: importar um calendário .ics como regras.
type ImportInput struct {
	ICSContent string
	ICSPreset  string
}
type ImportResult struct {
	Schedules []Rule
	Message   string
}

// RemoveInput/RemoveResult: remover uma regra pelo ID.
type RemoveInput struct{ ScheduleID string }
type RemoveResult struct{ Message string }

// ---------------------------------------------------------------------------
// schedule-list
// ---------------------------------------------------------------------------

// ListHandler executes "schedule-list".
type ListHandler struct {
	svc *Service
}

// NewListHandler builds the "schedule-list" handler. A nil store makes the
// action fail with the "não configurado" message (tests/dev builds), as the
// switch did.
func NewListHandler(store RuleStore, resolver PresetResolver) *ListHandler {
	return &ListHandler{svc: NewService(store, resolver)}
}

func (h *ListHandler) Action() string { return "schedule-list" }

func (h *ListHandler) Validate(*NoInput) error { return nil }

func (h *ListHandler) Handle(ctx context.Context, _ *NoInput) (*ListResult, error) {
	rules, err := h.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	return &ListResult{Schedules: rules}, nil
}

// ---------------------------------------------------------------------------
// schedule-add
// ---------------------------------------------------------------------------

// AddHandler executes "schedule-add".
type AddHandler struct {
	svc *Service
}

// NewAddHandler builds the "schedule-add" handler.
func NewAddHandler(store RuleStore, resolver PresetResolver) *AddHandler {
	return &AddHandler{svc: NewService(store, resolver)}
}

func (h *AddHandler) Action() string { return "schedule-add" }

func (h *AddHandler) Validate(*AddInput) error { return nil }

func (h *AddHandler) Handle(ctx context.Context, in *AddInput) (*AddResult, error) {
	r, err := h.svc.Add(ctx, in.Rule)
	if err != nil {
		return nil, err
	}
	return &AddResult{
		Rule:    r,
		Message: fmt.Sprintf("Regra %s criada: %s das %s às %s", r.ID, r.Preset, r.Start, r.End),
	}, nil
}

// ---------------------------------------------------------------------------
// schedule-import
// ---------------------------------------------------------------------------

// ImportHandler executes "schedule-import".
type ImportHandler struct {
	svc *Service
}

// NewImportHandler builds the "schedule-import" handler.
func NewImportHandler(store RuleStore, resolver PresetResolver) *ImportHandler {
	return &ImportHandler{svc: NewService(store, resolver)}
}

func (h *ImportHandler) Action() string { return "schedule-import" }

func (h *ImportHandler) Validate(*ImportInput) error { return nil }

func (h *ImportHandler) Handle(ctx context.Context, in *ImportInput) (*ImportResult, error) {
	added, err := h.svc.Import(ctx, in.ICSContent, in.ICSPreset)
	if err != nil {
		return nil, err
	}
	return &ImportResult{
		Schedules: added,
		Message:   fmt.Sprintf("%d regras importadas do calendário (preset %s)", len(added), in.ICSPreset),
	}, nil
}

// ---------------------------------------------------------------------------
// schedule-remove
// ---------------------------------------------------------------------------

// RemoveHandler executes "schedule-remove".
type RemoveHandler struct {
	svc *Service
}

// NewRemoveHandler builds the "schedule-remove" handler.
func NewRemoveHandler(store RuleStore, resolver PresetResolver) *RemoveHandler {
	return &RemoveHandler{svc: NewService(store, resolver)}
}

func (h *RemoveHandler) Action() string { return "schedule-remove" }

func (h *RemoveHandler) Validate(*RemoveInput) error { return nil }

func (h *RemoveHandler) Handle(ctx context.Context, in *RemoveInput) (*RemoveResult, error) {
	if err := h.svc.Remove(ctx, in.ScheduleID); err != nil {
		return nil, err
	}
	return &RemoveResult{Message: fmt.Sprintf("Regra %s removida", in.ScheduleID)}, nil
}
