package pomodoro

import (
	"sync"
	"testing"
	"time"

	"focusguard/internal/analytics"
)

// blockingRecorder enters Record, signals blocked once, then waits until
// release is closed. Used to hold session A's run goroutine inside record()
// while a second session B is started — exposing whether A's goroutine closes
// B's done channel (bug) or its own captured channel (fix).
type blockingRecorder struct {
	once    sync.Once
	blocked chan struct{}
	release chan struct{}
}

func (f *blockingRecorder) Record(s analytics.Session) {
	f.once.Do(func() { close(f.blocked) })
	<-f.release
}

// TestRace_NewSessionDuringFinalize_ClosesOwnChannel is a regression test for
// the race where session A finishes its last cycle and resets the controller
// state, and a new session B is started before A's goroutine closes its done
// channel. Without the fix, A closed B's done channel (it read c.done from the
// struct instead of a captured local) and B's goroutine panicked with "close of
// closed channel" when it later finished — and a concurrent Stop could deadlock
// waiting on the old channel. With the fix, A closes its own channel and the
// test completes cleanly.
func TestRace_NewSessionDuringFinalize_ClosesOwnChannel(t *testing.T) {
	blk := &mockBlocker{}
	rec := &blockingRecorder{blocked: make(chan struct{}), release: make(chan struct{})}
	c := New(blk)
	c.SetRecorder(rec)

	// Sessão A: 1 ciclo, work curtíssimo — termina quase imediatamente.
	if _, err := c.Start(Session{
		Preset: "a", Domains: []string{"a.com"},
		Work: time.Millisecond, Rest: 0, Cycles: 1,
	}); err != nil {
		t.Fatalf("Start A: %v", err)
	}

	// Aguarda A chegar em record() (preso dentro do recorder bloqueante).
	<-rec.blocked

	// A já resetou o estado (state = State{}): uma nova sessão B pode começar.
	if _, err := c.Start(Session{
		Preset: "b", Domains: []string{"b.com"},
		Work: time.Hour, Rest: 0, Cycles: 1,
	}); err != nil {
		t.Fatalf("Start B: %v", err)
	}

	// Libera A: seu goroutine fará close(c.done) — que agora aponta para o
	// canal done de B (bug) em vez do done capturado de A.
	close(rec.release)

	// Dá tempo para o goroutine de A fechar o done de B.
	time.Sleep(50 * time.Millisecond)

	// Para B: B encerra o ciclo e, ao finalizar, fecha c.done de novo — panic
	// "close of closed channel" no goroutine de B (o teste deve crashar sem o fix).
	st, err := c.Stop()
	if err != nil {
		t.Fatalf("Stop B: %v", err)
	}
	if st.Active {
		t.Error("session B should be inactive after Stop")
	}

	// Dá tempo para o goroutine de B completar (e, sem o fix, panicar).
	time.Sleep(50 * time.Millisecond)
}
