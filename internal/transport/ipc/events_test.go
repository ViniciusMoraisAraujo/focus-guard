package ipc

import (
	"testing"

	"focusguard/internal/transport/eventhub"
)

// TestEventSubscribe_NotConfigured: sem hub (tests/dev) a ação falha com o
// código estável — o proxy web nunca vê um 403 silencioso.
func TestEventSubscribe_NotConfigured(t *testing.T) {
	s := setupTestServer(t)
	resp := executeRequest(t, s, Request{Action: "event-subscribe"})
	if resp.Success || resp.Code != CodeNotConfigured {
		t.Fatalf("esperava não configurado, got %+v", resp)
	}
}

// TestEventSubscribe_ReturnsNewEvents: eventos já publicados antes do long-poll
// voltam imediatamente (em ordem, com o novo rev).
func TestEventSubscribe_ReturnsNewEvents(t *testing.T) {
	s := setupTestServer(t)
	hub := eventhub.New(16)
	s.SetEventHub(hub)
	hub.Publish("blocks-changed")
	hub.Publish("pomodoro-complete")

	resp := executeRequest(t, s, Request{Action: "event-subscribe", Since: 0})
	if !resp.Success {
		t.Fatalf("esperava sucesso, got %+v", resp)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("esperava 2 eventos, got %d", len(resp.Events))
	}
	if resp.Events[0].Type != "blocks-changed" || resp.Events[1].Type != "pomodoro-complete" {
		t.Fatalf("tipos inesperados: %+v", resp.Events)
	}
	if resp.Rev != 2 {
		t.Fatalf("rev = %d, want 2", resp.Rev)
	}
}

// TestEventSubscribe_SinceSkipsOld: com since = rev antigo, só os eventos
// novos voltam.
func TestEventSubscribe_SinceSkipsOld(t *testing.T) {
	s := setupTestServer(t)
	hub := eventhub.New(16)
	s.SetEventHub(hub)
	hub.Publish("a")
	hub.Publish("b")

	resp := executeRequest(t, s, Request{Action: "event-subscribe", Since: 1})
	if !resp.Success {
		t.Fatalf("esperava sucesso, got %+v", resp)
	}
	if len(resp.Events) != 1 || resp.Events[0].Type != "b" || resp.Rev != 2 {
		t.Fatalf("resposta inesperada: events=%+v rev=%d", resp.Events, resp.Rev)
	}
}
