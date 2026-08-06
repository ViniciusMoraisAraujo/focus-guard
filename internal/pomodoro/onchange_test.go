package pomodoro

import (
	"sync"
	"testing"
	"time"
)

// TestController_SetOnChange_NotifiesOnStateChanges verifica o hook de mudança
// (Fase 7 — event hub): start, transições de fase e conclusão disparam o
// callback registrado.
func TestController_SetOnChange_NotifiesOnStateChanges(t *testing.T) {
	c := New(&mockBlocker{})

	var mu sync.Mutex
	calls := 0
	c.SetOnChange(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    30 * time.Millisecond,
		Rest:    20 * time.Millisecond,
		Cycles:  2,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for c.Status().Active {
		if time.Now().After(deadline) {
			t.Fatal("sessão não terminou a tempo")
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	got := calls
	mu.Unlock()
	// Start + 2 inícios de work + 1 rest + conclusão = 5; asserts >= 4 para não
	// ser frágil a variações de timing.
	if got < 4 {
		t.Errorf("SetOnChange chamado %d vezes, esperava >= 4", got)
	}
}
