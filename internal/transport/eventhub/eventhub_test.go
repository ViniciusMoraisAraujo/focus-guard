package eventhub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestPublishWait_Immediate returns an event already published before Wait.
func TestPublishWait_Immediate(t *testing.T) {
	h := New(8)
	h.Publish("blocks-changed")

	evs, rev, err := h.Wait(context.Background(), 0)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "blocks-changed" {
		t.Fatalf("esperava 1 blocks-changed, got %+v", evs)
	}
	if rev != 1 {
		t.Fatalf("rev = %d, want 1", rev)
	}
}

// TestPublishWait_BlocksUntilEvent waits on a live subscription and wakes when
// a new event lands.
func TestPublishWait_BlocksUntilEvent(t *testing.T) {
	h := New(8)

	done := make(chan struct{})
	var got []Event
	var rev int64
	var err error
	go func() {
		got, rev, err = h.Wait(context.Background(), 0)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Wait retornou antes de qualquer evento")
	case <-time.After(50 * time.Millisecond):
	}

	h.Publish("pomodoro-complete")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait não acordou após o Publish")
	}
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(got) != 1 || got[0].Type != "pomodoro-complete" {
		t.Fatalf("evento inesperado: %+v", got)
	}
	if rev != 1 {
		t.Fatalf("rev = %d, want 1", rev)
	}
}

// TestWait_SinceSkipsOldEvents returns only events newer than since.
func TestWait_SinceSkipsOldEvents(t *testing.T) {
	h := New(8)
	h.Publish("a")
	h.Publish("b")
	h.Publish("c")

	evs, rev, err := h.Wait(context.Background(), 2)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "c" {
		t.Fatalf("esperava só o evento c, got %+v", evs)
	}
	if rev != 3 {
		t.Fatalf("rev = %d, want 3", rev)
	}
}

// TestWait_Timeout returns the context error when nothing arrives — the
// long-poll cycle the IPC layer treats as an empty heartbeat.
func TestWait_Timeout(t *testing.T) {
	h := New(8)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	_, _, err := h.Wait(ctx, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("esperava DeadlineExceeded, got %v", err)
	}
}

// TestRing_WrapsAndCatchUp: with a ring smaller than the number of published
// events, a subscriber with since=0 still sees the events that fit (newest
// window); old ones are dropped.
func TestRing_WrapsAndCatchUp(t *testing.T) {
	h := New(2)
	for i := 0; i < 5; i++ {
		h.Publish("ev")
	}
	// seq = 5; ring guarda os 2 últimos (seq 4 e 5).
	evs, rev, err := h.Wait(context.Background(), 3)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("esperava 2 eventos do ring, got %d", len(evs))
	}
	if rev != 5 {
		t.Fatalf("rev = %d, want 5", rev)
	}
}

// TestWait_BlocksWhenNothingNew: um Wait com since = rev atual (nada novo)
// BLOQUEIA até o próximo evento — é a semântica de long-poll; o ctx manda.
func TestWait_BlocksWhenNothingNew(t *testing.T) {
	h := New(2)
	h.Publish("ev")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := h.Wait(ctx, 1); err == nil {
		t.Fatal("esperava timeout com since = rev atual")
	}

	// Com um evento novo, o mesmo since acorda imediatamente.
	h.Publish("novo")
	evs, rev, err := h.Wait(context.Background(), 1)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "novo" || rev != 2 {
		t.Fatalf("esperava só o evento novo, got %+v (rev %d)", evs, rev)
	}
}

// TestPublish_DoesNotBlockWithManySubscribers: Publish is fire-and-forget even
// when subscribers never read their wake channel.
func TestPublish_DoesNotBlockWithManySubscribers(t *testing.T) {
	h := New(8)
	for i := 0; i < 64; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _, _ = h.Wait(ctx, 0)
		}()
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.Publish("x")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish bloqueou com muitos assinantes")
	}
}

// TestConcurrentPublishWait races publishers against waiters — the ring and
// wake map stay consistent under -race.
func TestConcurrentPublishWait(t *testing.T) {
	h := New(16)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				h.Publish("blocks-changed")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			// Avança o since a cada ciclo: quando alcança o rev atual, o Wait
			// bloqueia até o próximo evento (ou o ctx expira) — sem isso o
			// Wait com since fixo 0 devolveria o ring inteiro para sempre
			// (busy-loop, o bug que este teste pegou).
			var since int64
			for {
				_, rev, err := h.Wait(ctx, since)
				if err != nil {
					return
				}
				since = rev
			}
		}()
	}
	wg.Wait()
}
