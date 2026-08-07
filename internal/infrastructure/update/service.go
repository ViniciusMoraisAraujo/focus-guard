// Package update implements the domain service for the update/update-check IPC
// actions (Fase 4 do refactor-plan). Each action is a Service flavor sharing
// the check/apply/message logic that lived in the legacy switch; the ipc layer
// owns the timeout budget (updateTimeout), the status cache and the restart-hook
// signaling.
//
// The Service deliberately does NOT import internal/transport/ipc (ipc imports update for
// the adapter, so importing it back would create an import cycle — ipc's wire
// UpdateStatus is mirrored here as Status, converted by the adapter).
package update

import (
	"context"
	"errors"
	"fmt"
)

// Status mirrors ipc.UpdateStatus (the wire copy) so the domain stays
// cycle-free. The ipc adapter converts between the two shapes.
type Status struct {
	CurrentVersion string
	NewVersion     string
	Available      bool
	Applied        bool
	// PendingReboot marca o fallback move-on-reboot: a troca dos binários foi
	// agendada para o próximo boot (Windows) e o daemon segue na versão antiga.
	PendingReboot bool
}

// Checker performs an auto-update check (and apply, when the caller asks). The
// daemon wires a *daemonUpdater (cmd/focusguard-daemon) through the ipc bridge;
// tests stub it.
type Checker interface {
	Check(ctx context.Context, apply bool, channel string) (Status, error)
}

// errNotConfigured is returned when no checker is wired up (dev builds). The
// ipc adapter maps it to CodeNotConfigured, matching the legacy switch.
var errNotConfigured = errors.New("auto-update não configurado")

// Service checks for updates; apply=true also applies them to the binaries.
// The two flavors are NewService(checker, false) for "update-check" (read-only
// — botão "Verificar" da UI) and NewService(checker, true) for "update".
type Service struct {
	checker Checker
	apply   bool
}

// NewService builds the update service.
func NewService(checker Checker, apply bool) *Service {
	return &Service{checker: checker, apply: apply}
}

// Result is the outcome of a check/apply: the raw status, whether the daemon
// must restart onto the new binary and the PT-BR confirmation message.
type Result struct {
	Status  Status
	Applied bool
	Message string
}

// Run performs the check (and apply, if apply=true) honoring ctx. The timeout
// budget lives in the ipc layer (updateTimeout), as in the legacy switch.
func (s *Service) Run(ctx context.Context, channel string) (Result, error) {
	if s.checker == nil {
		return Result{}, errNotConfigured
	}
	st, err := s.checker.Check(ctx, s.apply, channel)
	if err != nil {
		return Result{Status: st}, err
	}
	res := Result{Status: st, Applied: st.Applied}
	switch {
	case st.Applied:
		// Bug 2: o daemon reinicia automaticamente para subir o binário novo
		// (o restart só dispara se o Response chegar ao cliente).
		res.Message = fmt.Sprintf("Atualização aplicada: %s → %s. O daemon será reiniciado automaticamente.", st.CurrentVersion, st.NewVersion)
	case st.PendingReboot:
		// Fallback move-on-reboot: a troca completa no próximo boot — o
		// daemon NÃO reinicia (Applied fica false) e segue servindo.
		res.Message = fmt.Sprintf("Atualização será concluída no próximo reinício do computador: %s → %s", st.CurrentVersion, st.NewVersion)
	case st.Available:
		res.Message = fmt.Sprintf("Atualização disponível: %s → %s", st.CurrentVersion, st.NewVersion)
	default:
		res.Message = "Nenhuma atualização disponível."
	}
	return res, nil
}
