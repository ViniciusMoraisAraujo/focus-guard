package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"focusguard/internal/transport/ipc"
	"focusguard/internal/transport/metrics"
)

func TestMetricsRequiresAuth(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, nil, "GET", "/api/metrics", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMetricsRejectsNonGET(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	cookie := adminCookie(t, srv)
	rec := doJSON(t, h, cookie, "POST", "/api/metrics", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestMetrics_ReturnsIPCAggregates: o endpoint devolve o snapshot do daemon
// (via ação metrics) junto com o snapshot local do proxy HTTP.
func TestMetrics_ReturnsIPCAggregates(t *testing.T) {
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		// O stub atende tanto o proxy de ping (setup abaixo) quanto a ação
		// metrics; para metrics devolve o snapshot do "daemon" (ordenado).
		if req.Action == "metrics" {
			return &ipc.Response{Success: true, Metrics: []metrics.ActionStat{
				{Action: "block", Count: 1},
				{Action: "ping", Count: 5},
			}}, nil
		}
		return &ipc.Response{Success: true}, nil
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	// Dispara um proxy para o registro local ter uma amostra.
	doJSON(t, h, cookie, "POST", "/api/action", "application/json",
		`{"action":"ping"}`, "127.0.0.1:48902")

	rec := doJSON(t, h, cookie, "GET", "/api/metrics", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		IPC  []metrics.ActionStat `json:"ipc"`
		HTTP []metrics.ActionStat `json:"http"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("resposta não é JSON: %v", err)
	}
	if len(body.IPC) != 2 || body.IPC[0].Action != "block" {
		t.Errorf("ipc = %+v, want 2 ações ordenadas começando por block", body.IPC)
	}
	if body.IPC[0].Count != 1 || body.IPC[1].Count != 5 {
		t.Errorf("ipc counts inesperados: %+v", body.IPC)
	}
	// O registro local tem ao menos a ação ping (e a própria metrics não é
	// medida — o snapshot é tirado antes do record do request atual).
	found := false
	for _, st := range body.HTTP {
		if st.Action == "ping" && st.Count >= 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("http não contém o proxy de ping: %+v", body.HTTP)
	}
}

func TestMetricsDaemonDownReturns503(t *testing.T) {
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		return nil, errFake("connection refused")
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)
	rec := doJSON(t, h, cookie, "GET", "/api/metrics", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
