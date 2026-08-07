// Handlers das ações pomodoro/pomodoro-defaults/pomodoro-stop — cada ação é
// um Handler auto-contido que depende só das interfaces mínimas do Service
// (Runner, PrefsStore, Catalog — DIP); o *pomodoro.Controller, *pomodoro.Prefs
// e o catálogo de presets as satisfazem estruturalmente. Handlers usam tipos
// próprios; o transporte os adapta via ipc.DomainAction (pós-reorg item 1).
package pomodoro

import "context"

// Tipos de entrada das ações — as saídas reutilizam os resultados do Service
// (StartResult/DefaultsResult/StopResult). Adaptados pelo transporte (DIP).

// NoInput: ações sem payload de entrada.
type NoInput struct{}

// StartInput: parâmetros da sessão vindos do wire. 0/-1 em work/rest/cycles
// significa "não informado" (o Resolve aplica os padrões salvos).
type StartInput struct {
	Preset  string
	Label   string
	WorkMin int
	RestMin int
	Cycles  int
	Strict  bool
	Save    bool
}

// ---------------------------------------------------------------------------
// pomodoro
// ---------------------------------------------------------------------------

// StartHandler executes "pomodoro".
type StartHandler struct {
	svc *Service
}

// NewStartHandler builds the "pomodoro" handler. A nil runner makes the action
// fail with the "não configurado" message (tests/dev builds), as the switch
// did.
func NewStartHandler(runner Runner, prefs PrefsStore, catalog Catalog) *StartHandler {
	return &StartHandler{svc: NewService(runner, prefs, catalog)}
}

func (h *StartHandler) Action() string { return "pomodoro" }

func (h *StartHandler) Validate(*StartInput) error { return nil }

func (h *StartHandler) Handle(ctx context.Context, in *StartInput) (*StartResult, error) {
	res, err := h.svc.Start(ctx, in.Preset, in.Label, in.WorkMin, in.RestMin, in.Cycles, in.Strict, in.Save)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// ---------------------------------------------------------------------------
// pomodoro-defaults
// ---------------------------------------------------------------------------

// DefaultsHandler executes "pomodoro-defaults".
type DefaultsHandler struct {
	svc *Service
}

// NewDefaultsHandler builds the "pomodoro-defaults" handler.
func NewDefaultsHandler(prefs PrefsStore) *DefaultsHandler {
	return &DefaultsHandler{svc: NewService(nil, prefs, nil)}
}

func (h *DefaultsHandler) Action() string { return "pomodoro-defaults" }

func (h *DefaultsHandler) Validate(*NoInput) error { return nil }

func (h *DefaultsHandler) Handle(ctx context.Context, _ *NoInput) (*DefaultsResult, error) {
	res, err := h.svc.Defaults(ctx)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// ---------------------------------------------------------------------------
// pomodoro-stop
// ---------------------------------------------------------------------------

// StopHandler executes "pomodoro-stop".
type StopHandler struct {
	svc *Service
}

// NewStopHandler builds the "pomodoro-stop" handler.
func NewStopHandler(runner Runner) *StopHandler {
	return &StopHandler{svc: NewService(runner, nil, nil)}
}

func (h *StopHandler) Action() string { return "pomodoro-stop" }

func (h *StopHandler) Validate(*NoInput) error { return nil }

func (h *StopHandler) Handle(ctx context.Context, _ *NoInput) (*StopResult, error) {
	res, err := h.svc.Stop(ctx)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
