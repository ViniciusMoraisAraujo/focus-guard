// Testes do handler das ações update/update-check (pós-reorg item 1: handler +
// handler_test por pacote).
package update

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/domain/ipcerr"
)

type handlerChecker struct {
	st  Status
	err error
}

func (f *handlerChecker) Check(_ context.Context, _ bool, _ string) (Status, error) {
	return f.st, f.err
}

func assertActionError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var ae *ipcerr.Error
	if !errors.As(err, &ae) || ae.Code != wantCode {
		t.Fatalf("esperava código %q, got %v", wantCode, err)
	}
}

func TestUpdate_SemChecker(t *testing.T) {
	h := NewUpdateHandler(func() Checker { return nil }, true)
	_, err := h.Handle(context.Background(), &UpdateInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestUpdate_ErroDoCheckerViraNotConfigured(t *testing.T) {
	// Semântica legada (switch + adapter de referência): QUALQUER erro do
	// checker (rede, selfupdate) vira CodeNotConfigured no wire.
	h := NewUpdateHandler(func() Checker { return &handlerChecker{err: errors.New("rede fora")} }, true)
	_, err := h.Handle(context.Background(), &UpdateInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestUpdate_NenhumaDisponivel(t *testing.T) {
	h := NewUpdateHandler(func() Checker {
		return &handlerChecker{st: Status{CurrentVersion: "0.16.1"}}
	}, true)
	resp, err := h.Handle(context.Background(), &UpdateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Applied || resp.Status.Available || resp.Message != "Nenhuma atualização disponível." {
		t.Fatalf("esperava nenhuma disponível, got %+v", resp)
	}
}

func TestUpdate_Disponivel(t *testing.T) {
	h := NewUpdateHandler(func() Checker {
		return &handlerChecker{st: Status{CurrentVersion: "0.16.1", NewVersion: "0.17.0", Available: true}}
	}, false)
	resp, err := h.Handle(context.Background(), &UpdateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Status.Available || resp.Status.NewVersion != "0.17.0" || resp.Applied {
		t.Fatalf("esperava disponível sem aplicar, got %+v", resp)
	}
	if resp.Message == "" {
		t.Fatal("esperava mensagem")
	}
}

func TestUpdate_Aplicado(t *testing.T) {
	h := NewUpdateHandler(func() Checker {
		return &handlerChecker{st: Status{CurrentVersion: "0.16.1", NewVersion: "0.17.0", Available: true, Applied: true}}
	}, true)
	resp, err := h.Handle(context.Background(), &UpdateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Applied || !resp.Status.Applied {
		t.Fatalf("esperava Applied, got %+v", resp)
	}
}

func TestUpdate_PendingReboot(t *testing.T) {
	// Fallback move-on-reboot: NÃO aplica (Applied fica false) e o daemon
	// segue servindo a versão antiga.
	h := NewUpdateHandler(func() Checker {
		return &handlerChecker{st: Status{CurrentVersion: "0.16.1", NewVersion: "0.17.0", PendingReboot: true}}
	}, true)
	resp, err := h.Handle(context.Background(), &UpdateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Applied || !resp.Status.PendingReboot || resp.Message == "" {
		t.Fatalf("esperava pending-reboot sem aplicar, got %+v", resp)
	}
}

func TestUpdateHandlers_ActionNames(t *testing.T) {
	if NewUpdateHandler(func() Checker { return nil }, true).Action() != "update" {
		t.Fatal("update action name errado")
	}
	if NewUpdateHandler(func() Checker { return nil }, false).Action() != "update-check" {
		t.Fatal("update-check action name errado")
	}
}
