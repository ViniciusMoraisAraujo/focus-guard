package httpapi

// Etapa 5 do bug-hunt — reconexão SSE com Last-Event-ID. O EventSource do
// browser reconecta automaticamente após um `event: error` e manda o header
// Last-Event-ID com o id do último lote recebido; o loop do handleEvents deve
// retomar dali (since = id) e entregar só os eventos do intervalo — nunca
// reentregar o ring inteiro.

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"focusguard/internal/transport/ipc"
)

// TestEvents_ReconnectResumesFromLastEventID é o teste do critério de saída:
// reconexão com eventos publicados no intervalo. O browser manda
// Last-Event-ID=5 (o rev do último lote); o hub (stub) entrega só os eventos
// do gap (rev 6..7) e o loop ecoa `id: 7` (o rev do lote, high-water mark) —
// reconectar com esse id não duplica nada. O ciclo quieto seguinte reusa o rev
// (since=7) e um erro de daemon encerra o stream com `event: error`.
func TestEvents_ReconnectResumesFromLastEventID(t *testing.T) {
	calls := 0
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		calls++
		switch calls {
		case 1:
			// Reconexão: retoma do gap, NÃO reentrega o ring desde o início.
			if req.Since != 5 {
				t.Errorf("primeiro poll Since = %d, want 5 (Last-Event-ID)", req.Since)
			}
			return &ipc.Response{Success: true, Rev: 7, Events: []ipc.Event{
				{Type: "blocks-changed", At: time.Now()},
				{Type: "pomodoro-complete", At: time.Now()},
			}}, nil
		case 2:
			// Ciclo quieto: o orçamento expirou sem mudanças → keepalive.
			if req.Since != 7 {
				t.Errorf("segundo poll Since = %d, want 7 (rev do lote anterior)", req.Since)
			}
			return &ipc.Response{Success: true, Rev: 7}, nil
		default:
			return nil, errors.New("daemon down") // encerra o loop do handler
		}
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	req := httptest.NewRequest("GET", "/api/events", nil)
	req.Host = "127.0.0.1:48902"
	req.AddCookie(cookie)
	req.Header.Set("Last-Event-ID", "5") // o que o EventSource manda ao reconectar
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()

	if !strings.Contains(body, "event: blocks-changed") ||
		!strings.Contains(body, "event: pomodoro-complete") {
		t.Errorf("eventos do intervalo ausentes no stream:\n%s", body)
	}
	// id: ecoa o rev do lote (7) — os DOIS eventos do mesmo lote compartilham
	// o id (o rev é o high-water mark da entrega, não o seq individual).
	if strings.Count(body, "id: 7") != 2 {
		t.Errorf("esperava id: 7 em cada um dos 2 eventos do lote (got %d):\n%s", strings.Count(body, "id: 7"), body)
	}
	if strings.Contains(body, "id: 5") || strings.Contains(body, "id: 6") {
		t.Errorf("reconexão reentregou ids antigos (deveria retomar do gap):\n%s", body)
	}
	if !strings.Contains(body, ": keepalive") {
		t.Errorf("ciclo quieto sem keepalive:\n%s", body)
	}
	if !strings.Contains(body, "event: error") {
		t.Errorf("loop não encerrou com event: error:\n%s", body)
	}
}

// TestEvents_InvalidLastEventIDFallsBackToZero: um Last-Event-ID que não é
// inteiro decimal é ignorado (since=0) — o handler nunca pode quebrar a
// reconexão por causa de um id corrompido.
func TestEvents_InvalidLastEventIDFallsBackToZero(t *testing.T) {
	for _, id := range []string{"abc", "12abc", "1.5", " 5 "} {
		t.Run(id, func(t *testing.T) {
			sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
				if req.Since != 0 {
					t.Errorf("Since = %d, want 0 para Last-Event-ID %q", req.Since, id)
				}
				return nil, errors.New("done")
			}}
			srv, h := newTestServer(sc, uiFS())
			cookie := adminCookie(t, srv)
			req := httptest.NewRequest("GET", "/api/events", nil)
			req.Host = "127.0.0.1:48902"
			req.AddCookie(cookie)
			req.Header.Set("Last-Event-ID", id)
			h.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}

// TestEvents_NegativeLastEventID_PropagatesVerbatim congela um edge do parse:
// strconv.ParseInt aceita "-1", então um Last-Event-ID negativo chega ao hub
// com since=-1 (o Wait entrega o ring inteiro — seq > -1). Um EventSource real
// nunca manda isso (só ecoa ids recebidos, sempre ≥ 0); é hardening candidato
// clampar negativos em 0 no parse — registrado no doc, sem mudança de
// comportamento agora.
func TestEvents_NegativeLastEventID_PropagatesVerbatim(t *testing.T) {
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		if req.Since != -1 {
			t.Errorf("Since = %d, want -1 (propagado verbatim)", req.Since)
		}
		return nil, errors.New("done")
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)
	req := httptest.NewRequest("GET", "/api/events", nil)
	req.Host = "127.0.0.1:48902"
	req.AddCookie(cookie)
	req.Header.Set("Last-Event-ID", "-1")
	h.ServeHTTP(httptest.NewRecorder(), req)
}
