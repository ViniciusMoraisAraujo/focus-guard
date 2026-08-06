package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"focusguard/internal/ipc"
)

func TestEventsRequiresAuth(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, nil, "GET", "/api/events", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestEventsRejectsNonGET(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	cookie := adminCookie(t, srv)
	rec := doJSON(t, h, cookie, "POST", "/api/events", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestEventsStreamsAndEchoesRev cobre o ciclo completo do loop: primeiro poll
// entrega os eventos do ring (since=0), o ciclo quieto vira keepalive, e o
// próximo poll reusa o rev devolvido (sem re-entrega). O stub termina o loop
// com um erro de daemon para o handler retornar.
func TestEventsStreamsAndEchoesRev(t *testing.T) {
	calls := 0
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		calls++
		switch calls {
		case 1:
			return &ipc.Response{Success: true, Rev: 2, Events: []ipc.Event{
				{Type: "blocks-changed", At: time.Now()},
				{Type: "pomodoro-complete", At: time.Now()},
			}}, nil
		case 2:
			// Ciclo quieto: orçamento expirou sem mudanças → keepalive.
			return &ipc.Response{Success: true, Rev: 2}, nil
		default:
			return nil, errors.New("daemon down")
		}
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	rec := doJSON(t, h, cookie, "GET", "/api/events", "", "", "127.0.0.1:48902")
	body := rec.Body.String()

	if !strings.Contains(body, "event: blocks-changed") ||
		!strings.Contains(body, "event: pomodoro-complete") {
		t.Errorf("stream não contém os eventos do ring:\n%s", body)
	}
	if !strings.Contains(body, ": keepalive") {
		t.Errorf("stream não contém keepalive no ciclo quieto:\n%s", body)
	}
	if !strings.Contains(body, "event: error") {
		t.Errorf("stream não encerra com evento de erro ao cair o daemon:\n%s", body)
	}
	// O último poll (após o ciclo quieto) reusou o rev 2 devolvido no primeiro.
	if sc.lastReq.Action != "event-subscribe" || sc.lastReq.Since != 2 {
		t.Errorf("último request = %+v, want event-subscribe since=2", sc.lastReq)
	}
	// O timeout do poll respeita o orçamento do spec + margem.
	if sc.withTimeout < specTimeout(t, "event-subscribe") {
		t.Errorf("poll timeout = %v, want >= spec %v", sc.withTimeout, specTimeout(t, "event-subscribe"))
	}
}

func TestEventsDaemonDownClosesWithError(t *testing.T) {
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		return nil, errors.New("connection refused")
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	rec := doJSON(t, h, cookie, "GET", "/api/events", "", "", "127.0.0.1:48902")
	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("daemon down deveria encerrar com event: error, got:\n%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}
