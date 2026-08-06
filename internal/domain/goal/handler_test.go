package goal

import (
	"context"
	"errors"
	"testing"
	"time"

	"focusguard/internal/ipc"
)

type fakeStore struct {
	goal time.Duration
}

func (f *fakeStore) Get() time.Duration        { return f.goal }
func (f *fakeStore) Set(d time.Duration) error { f.goal = d; return nil }

func assertActionError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var ae *ipc.ActionError
	if !errors.As(err, &ae) {
		t.Fatalf("esperava ActionError, got %v", err)
	}
	if ae.Code != wantCode {
		t.Fatalf("esperava código %q, got %q (%v)", wantCode, ae.Code, err)
	}
}

func TestGoalGet_OK(t *testing.T) {
	h := NewGet(&fakeStore{goal: 4 * time.Hour})
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "goal-get"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Goal != 4*time.Hour {
		t.Fatalf("esperava meta de 4h, got %+v", resp)
	}
}

func TestGoalGet_SemStore(t *testing.T) {
	h := NewGet(nil)
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "goal-get"})
	assertActionError(t, err, ipc.CodeNotConfigured)
}

func TestGoalSet_OK(t *testing.T) {
	st := &fakeStore{}
	h := NewSet(st)
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "goal-set", GoalMinutes: 240})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Goal != 4*time.Hour {
		t.Fatalf("esperava sucesso + meta de 4h, got %+v", resp)
	}
	if st.goal != 4*time.Hour {
		t.Fatalf("Set não foi chamado com 4h, got %v", st.goal)
	}
	if resp.Message != "Meta diária definida: 4h0m0s" {
		t.Fatalf("mensagem inesperada: %q", resp.Message)
	}
}

func TestGoalSet_Invalido(t *testing.T) {
	h := NewSet(&fakeStore{})
	for _, m := range []int{0, -5, 24*60 + 1} {
		_, err := h.Handle(context.Background(), &ipc.Request{Action: "goal-set", GoalMinutes: m})
		assertActionError(t, err, ipc.CodeInvalid)
	}
}

func TestGoalSet_SemStore_AntesDoRange(t *testing.T) {
	h := NewSet(nil)
	// Ordem do switch legado: store não configurado é verificado antes do
	// range da meta — mesmo com valor inválido o erro é o de configuração.
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "goal-set", GoalMinutes: 0})
	assertActionError(t, err, ipc.CodeNotConfigured)
}
