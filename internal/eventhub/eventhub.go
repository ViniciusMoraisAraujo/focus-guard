// Package eventhub is a tiny in-process pub/sub for daemon state changes
// (Fase 7 do refactor-plan.md). The daemon publishes coarse events ("blocks
// changed", "pomodoro complete") and long-poll subscribers wait for the next
// one — the focusguard-web process polls the IPC action event-subscribe, which
// blocks here, and relays the events to the browser over SSE.
//
// Events carry no payload on purpose: the daemon state is the source of truth,
// so a subscriber re-fetches the affected data (status/stats) when an event
// arrives. A small ring buffer lets a subscriber that reconnects catch up on
// events it missed while away; only events beyond the window are lost (the
// subscriber re-fetches on connect anyway).
package eventhub

import (
	"context"
	"sync"
	"time"
)

// Event is one coarse state-change notification. Type is a stable token
// ("blocks-changed", "pomodoro-changed", "pomodoro-complete",
// "schedule-changed"); At is when it happened.
type Event struct {
	Type string    `json:"type"`
	At   time.Time `json:"at"`
}

type entry struct {
	seq int64
	ev  Event
}

// Hub fans out events to long-poll subscribers. Publish is cheap and never
// blocks (the send paths — block/pomodoro/expiry — stay latency-sensitive);
// a slow or absent subscriber is woken later and catches up from the ring.
type Hub struct {
	mu   sync.Mutex
	seq  int64   // last event sequence (the "rev" exposed over IPC)
	ring []entry // ring buffer of recent events (oldest first)
	head int     // next ring write slot
	size int     // events currently in the ring
	cap  int
	wake map[int64]chan struct{} // subscriber wake-ups, by id
	next int64                   // next subscriber id
}

// New returns a hub that remembers the last cap events for late subscribers.
func New(cap int) *Hub {
	if cap < 1 {
		cap = 1
	}
	return &Hub{
		ring: make([]entry, cap),
		cap:  cap,
		wake: make(map[int64]chan struct{}),
	}
}

// Publish records an event of the given type and wakes every subscriber.
func (h *Hub) Publish(typ string) {
	h.mu.Lock()
	h.seq++
	h.ring[h.head] = entry{seq: h.seq, ev: Event{Type: typ, At: time.Now()}}
	h.head = (h.head + 1) % h.cap
	if h.size < h.cap {
		h.size++
	}
	for _, ch := range h.wake {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
}

// Rev returns the current sequence number (0 when nothing was published yet).
func (h *Hub) Rev() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// Wait blocks until at least one event with seq > since is available (then
// returns it, oldest first, plus the new latest seq) or ctx ends. With no new
// events it returns ctx.Err() — the caller (the IPC long-poll) treats a
// deadline as a normal empty cycle and re-issues. Events older than the ring
// window are dropped: the caller re-fetches state on connect anyway.
func (h *Hub) Wait(ctx context.Context, since int64) ([]Event, int64, error) {
	h.mu.Lock()
	if evs, rev := h.collectLocked(since); len(evs) > 0 {
		h.mu.Unlock()
		return evs, rev, nil
	}
	id := h.next
	h.next++
	ch := make(chan struct{}, 1)
	h.wake[id] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.wake, id)
		h.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, h.Rev(), ctx.Err()
	case <-ch:
		h.mu.Lock()
		evs, rev := h.collectLocked(since)
		h.mu.Unlock()
		return evs, rev, nil
	}
}

// collectLocked scans the ring for events newer than since. The caller holds
// h.mu.
func (h *Hub) collectLocked(since int64) ([]Event, int64) {
	if h.size == 0 {
		return nil, h.seq
	}
	start := (h.head - h.size + h.cap) % h.cap
	var out []Event
	for i := 0; i < h.size; i++ {
		e := h.ring[(start+i)%h.cap]
		if e.seq > since {
			out = append(out, e.ev)
		}
	}
	return out, h.seq
}
