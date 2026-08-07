// Handler das ações update/update-check — um Handler auto-contido que depende
// só da interface Checker (DIP); o daemon injeta o checker LAZY (via provider)
// porque o wiring do updater (SetUpdateChecker) acontece depois do registro
// dos handlers no composition root. O estado de processo (latch de restart e
// o cache do status para a ação "status") permanece no ipc.Server, setado pelo
// wrapper do composition root — o Handler é puro domínio. Handlers usam tipos
// próprios; o transporte os adapta via ipc.DomainAction (pós-reorg item 1).
package update

import (
	"context"

	"focusguard/internal/domain/ipcerr"
)

// UpdateInput: canal de release (""/stable pula prereleases, "beta" opt-in).
type UpdateInput struct{ Channel string }

// UpdateHandler executa "update" (apply=true) ou "update-check" (apply=false).
// O checker é lido via provider a cada execução (o daemon o configura depois
// do registro); nil (dev builds) devolve o erro "não configurado" mapeado
// para CodeNotConfigured — a semântica exata do switch legado.
type UpdateHandler struct {
	apply   bool
	checker func() Checker
}

// NewUpdateHandler builds the handler. apply=true registra "update",
// apply=false registra "update-check".
func NewUpdateHandler(checker func() Checker, apply bool) *UpdateHandler {
	return &UpdateHandler{apply: apply, checker: checker}
}

func (h *UpdateHandler) Action() string {
	if h.apply {
		return "update"
	}
	return "update-check"
}

func (h *UpdateHandler) Validate(*UpdateInput) error { return nil }

// Handle roda o check/apply e devolve o Result do Service (Status/Applied/
// Message). Erros do checker (dev builds: checker ausente) viram
// CodeNotConfigured, semântica exata do switch legado.
func (h *UpdateHandler) Handle(ctx context.Context, in *UpdateInput) (*Result, error) {
	res, err := NewService(h.checker(), h.apply).Run(ctx, in.Channel)
	if err != nil {
		// O único erro conhecido aqui é o checker ausente (dev builds) — a
		// semântica exata de CodeNotConfigured.
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, err.Error())
	}
	return &res, nil
}
