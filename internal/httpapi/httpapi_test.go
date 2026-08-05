package httpapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"focusguard/internal/ipc"
)

// stubClient is a fake DaemonClient that records the last request and returns
// either the canned response or the canned error.
type stubClient struct {
	lastReq     ipc.Request
	fn          func(req ipc.Request) (*ipc.Response, error)
	withTimeout time.Duration
}

func (s *stubClient) SendWithTimeout(req ipc.Request, timeout time.Duration) (*ipc.Response, error) {
	s.lastReq = req
	s.withTimeout = timeout
	if s.fn != nil {
		return s.fn(req)
	}
	return &ipc.Response{Success: true}, nil
}

func newTestServer(client DaemonClient, assets fs.FS) http.Handler {
	return New(client, assets).Handler()
}

func uiFS() fs.FS {
	return fstest.MapFS{
		"index.html":       {Data: []byte("<html>FocusGuard UI</html>")},
		"assets/app.js":    {Data: []byte("console.log('app')")},
		"assets/style.css": {Data: []byte("body{}")},
	}
}

func doJSON(t *testing.T, h http.Handler, method, path, contentType, body, host string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if host != "" {
		req.Host = host
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeResp(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("resposta não é JSON: %v", err)
	}
	return m
}

func TestHealthOK(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "GET", "/api/health", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if m := decodeResp(t, rec); m["status"] != "ok" {
		t.Fatalf("health = %v, want ok", m)
	}
}

func TestHealthRejectsPOST(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "POST", "/api/health", "application/json", "{}", "127.0.0.1:48902")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestForeignHostRejected(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	for _, host := range []string{"evil.example.com:48902", "attacker.com"} {
		rec := doJSON(t, h, "POST", "/api/action", "application/json", `{"action":"status"}`, host)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("host %q: status = %d, want 403", host, rec.Code)
		}
	}
}

func TestLocalHostVariantsAccepted(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	for _, host := range []string{"127.0.0.1:48902", "localhost:48902", "[::1]:48902", "127.0.0.1"} {
		rec := doJSON(t, h, "POST", "/api/action", "application/json", `{"action":"status"}`, host)
		if rec.Code != http.StatusOK {
			t.Fatalf("host %q: status = %d, want 200", host, rec.Code)
		}
	}
}

func TestActionRejectsNonJSONContentType(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "application/jsonp", "application/notjson", ""} {
		rec := doJSON(t, h, "POST", "/api/action", ct, `{"action":"status"}`, "127.0.0.1:48902")
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("content-type %q: status = %d, want 415", ct, rec.Code)
		}
	}
}

func TestActionAcceptsJSONWithCharset(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "POST", "/api/action", "application/json; charset=utf-8",
		`{"action":"status"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestUnknownAPIEndpointReturns404JSON(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "GET", "/api/nonexistent", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	m := decodeResp(t, rec)
	if m["success"] != false {
		t.Fatalf("resposta = %v, want success:false", m)
	}
}

func TestActionForwardsRequestAndReturnsResponse(t *testing.T) {
	sc := &stubClient{}
	h := newTestServer(sc, uiFS())
	rec := doJSON(t, h, "POST", "/api/action", "application/json",
		`{"action":"block","domain":"youtube.com","duration":"4h"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if sc.lastReq.Action != "block" || sc.lastReq.Domain != "youtube.com" || sc.lastReq.Duration != "4h" {
		t.Fatalf("request não repassado: %+v", sc.lastReq)
	}
	if sc.withTimeout != proxyTimeout {
		t.Fatalf("timeout = %v, want %v", sc.withTimeout, proxyTimeout)
	}
	if m := decodeResp(t, rec); m["success"] != true {
		t.Fatalf("resposta = %v, want success", m)
	}
}

// TestActionUpdateUsesLongTimeout garante que o update NÃO usa o timeout curto
// do proxy (5s): aplicar uma atualização baixa/extrai/troca os binários e pode
// levar mais tempo — com 5s a UI reportaria "daemon indisponível" mesmo quando
// o update continua e termina com sucesso.
func TestActionUpdateUsesLongTimeout(t *testing.T) {
	sc := &stubClient{}
	h := newTestServer(sc, uiFS())
	rec := doJSON(t, h, "POST", "/api/action", "application/json",
		`{"action":"update","channel":"stable"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if sc.lastReq.Action != "update" || sc.lastReq.Channel != "stable" {
		t.Fatalf("request não repassado: %+v", sc.lastReq)
	}
	if sc.withTimeout != updateTimeout {
		t.Fatalf("timeout = %v, want %v", sc.withTimeout, updateTimeout)
	}
}

func TestActionUpdateCheckUsesLongTimeout(t *testing.T) {
	sc := &stubClient{}
	h := newTestServer(sc, uiFS())
	rec := doJSON(t, h, "POST", "/api/action", "application/json",
		`{"action":"update-check"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if sc.lastReq.Action != "update-check" {
		t.Fatalf("request não repassado: %+v", sc.lastReq)
	}
	if sc.withTimeout != updateTimeout {
		t.Fatalf("timeout = %v, want %v", sc.withTimeout, updateTimeout)
	}
}

func TestActionDaemonDownReturns503(t *testing.T) {
	sc := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return nil, errFake("connection refused")
	}}
	h := newTestServer(sc, uiFS())
	rec := doJSON(t, h, "POST", "/api/action", "application/json", `{"action":"status"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if m := decodeResp(t, rec); m["success"] != false {
		t.Fatalf("resposta = %v, want success:false", m)
	}
}

func TestPingDaemonDownReturns503(t *testing.T) {
	sc := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return nil, errFake("dial error")
	}}
	h := newTestServer(sc, uiFS())
	rec := doJSON(t, h, "GET", "/api/ping", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestActionInvalidJSON(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "POST", "/api/action", "application/json", `{not json`, "127.0.0.1:48902")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestStaticServesIndexHTML(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "GET", "/", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FocusGuard UI") {
		t.Fatalf("index.html não servido: %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
}

func TestStaticSPAFallback(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "GET", "/configuracoes", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FocusGuard UI") {
		t.Fatalf("fallback SPA não serviu index.html: %q", rec.Body.String())
	}
}

func TestStaticServesAssetWithContentType(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "GET", "/assets/app.js", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("content-type = %q, want text/javascript", rec.Header().Get("Content-Type"))
	}
}

func TestStaticStubWhenNoUI(t *testing.T) {
	h := newTestServer(&stubClient{}, fstest.MapFS{})
	rec := doJSON(t, h, "GET", "/", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "make ui") {
		t.Fatalf("stub não menciona make ui: %q", rec.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, "GET", "/", "", "", "127.0.0.1:48902")
	for _, hdr := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Content-Security-Policy"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("header %s ausente", hdr)
		}
	}
}

func TestActionGzipWhenAccepted(t *testing.T) {
	// payload grande o bastante para o gzip valer a pena: o estado real com
	// muitos bloqueios (1.4 MB a 1000) é o alvo do P1 de compressão.
	resp := &ipc.Response{Success: true, Message: strings.Repeat("abc", 20000)}
	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	stub := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return resp, nil
	}}
	h := newTestServer(stub, uiFS())

	req := httptest.NewRequest("POST", "/api/action", strings.NewReader(`{"action":"status"}`))
	req.Host = "127.0.0.1:48902"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Vary") == "" {
		t.Error("Vary: Accept-Encoding ausente")
	}
	if rec.Body.Len() >= len(payload) {
		t.Errorf("gzip não reduziu o payload: %d bytes >= %d", rec.Body.Len(), len(payload))
	}

	gz, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("corpo não é gzip válido: %v", err)
	}
	plain, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if got := strings.TrimSuffix(string(plain), "\n"); got != string(payload) {
		t.Errorf("corpo descompactado != payload original (%d bytes)", len(plain))
	}
}

func TestActionGzipOnlyWhenAccepted(t *testing.T) {
	stub := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return &ipc.Response{Success: true, Message: "ok"}, nil
	}}
	h := newTestServer(stub, uiFS())
	rec := doJSON(t, h, "POST", "/api/action", "application/json", `{"action":"status"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("sem Accept-Encoding não deve gzipar")
	}
	if got := rec.Body.String(); got != "{\"success\":true,\"message\":\"ok\"}\n" {
		t.Errorf("corpo = %q, want JSON simples", got)
	}
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func errFake(msg string) error { return fakeErr(msg) }
