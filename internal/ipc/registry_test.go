package ipc

import (
	"context"
	"strings"
	"testing"
)

// testHandler is a minimal Handler for registry tests.
type testHandler struct{ action string }

func (h testHandler) Action() string { return h.action }
func (h testHandler) Validate(*Request) error {
	return nil
}
func (h testHandler) Handle(context.Context, *Request) (*Response, error) {
	return &Response{Success: true}, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	h := testHandler{action: "ping"}
	r.Register(h)

	got, ok := r.Get("ping")
	if !ok {
		t.Fatal("ping deveria estar registrado")
	}
	if got.Action() != "ping" {
		t.Errorf("Action = %q, want ping", got.Action())
	}
	if _, ok := r.Get("nao-existe"); ok {
		t.Fatal("ação desconhecida não deveria existir no registry")
	}
}

func TestRegistry_Register_Overwrites(t *testing.T) {
	r := NewRegistry()
	r.Register(testHandler{action: "ping"})
	r.Register(testHandler{action: "ping"})

	got, ok := r.Get("ping")
	if !ok || got.Action() != "ping" {
		t.Fatalf("esperava o último handler de ping, got %v ok=%v", got, ok)
	}
	if n := len(r.Actions()); n != 1 {
		t.Errorf("Actions = %d, want 1 (overwrite não deve duplicar)", n)
	}
}

func TestRegistry_Actions_Sorted(t *testing.T) {
	r := NewRegistry()
	r.Register(testHandler{action: "zeta"})
	r.Register(testHandler{action: "alpha"})
	r.Register(testHandler{action: "mid"})

	got := r.Actions()
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Actions = %v, want %v (ordenadas)", got, want)
		}
	}
}

func TestRegistry_ValidateSpecs_OK(t *testing.T) {
	r := NewRegistry()
	r.Register(testHandler{action: "ping"}) // ping tem spec
	if err := r.ValidateSpecs(); err != nil {
		t.Fatalf("ValidateSpecs: %v", err)
	}
}

func TestRegistry_ValidateSpecs_MissingSpec(t *testing.T) {
	r := NewRegistry()
	r.Register(testHandler{action: "nao-specado"})
	err := r.ValidateSpecs()
	if err == nil {
		t.Fatal("esperava erro: handler sem ActionSpec")
	}
	if !strings.Contains(err.Error(), "nao-specado") {
		t.Errorf("erro deveria citar a ação faltante, got %q", err)
	}
}

func TestRegistry_ValidateSpecs_WebOnlyExempt(t *testing.T) {
	r := NewRegistry()
	r.Register(testHandler{action: "user-verify"}) // web-only, sem spec
	if err := r.ValidateSpecs("user-verify"); err != nil {
		t.Fatalf("web-only deveria ser isenta, got %v", err)
	}
	if err := r.ValidateSpecs(); err == nil {
		t.Fatal("sem a isenção, user-verify sem spec deveria falhar")
	}
}
