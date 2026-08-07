// Package httpapi serves the FocusGuard web interface and proxies the daemon
// IPC actions over HTTP, so a browser can drive the daemon without socket or
// TCP access of its own. It lives in the user-space focusguard-web process
// (never in the admin daemon), keeping the privileged surface small.
//
// Security model: the API binds to loopback only and applies localhost guards
// — Host validation (kills DNS rebinding), Content-Type enforcement on writes
// (a cross-origin browser POST with a simple content type would bypass CORS
// preflight), body limits and hardened response headers.
package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"focusguard/internal/transport/ipc"
	"focusguard/internal/transport/metrics"
)

// DefaultAddr is where focusguard-web listens (loopback only). The CLI probes
// the same address to decide whether to spawn a new server or reuse one.
const DefaultAddr = "127.0.0.1:48902"

// proxyTimeout bounds each quick IPC call made on behalf of the browser
// (ping, presets, schedule, apps, stats...). It mirrors the tray's
// SendWithTimeout discipline: a hung daemon must never hang the UI.
const proxyTimeout = 5 * time.Second

// Os orçamentos por ação (status 15s, block/block-all/pomodoro 30s,
// update/update-check 150s, demais 5s) vivem na tabela declarativa
// ipc.SpecFor — a MESMA fonte que o daemon conhece (B7). O proxy só precisa
// esperar a resposta; cada conexão IPC roda na própria goroutine do daemon,
// então um orçamento maior para ações lentas nunca bloqueia os probes rápidos
// (ping mantém o timeout curto e continua sendo o sinal de conectividade).
// O orçamento do spec é sempre ≥ o interno do daemon, para um update
// lento-mas-bem-sucedido não virar "daemon indisponível".

// maxBodyBytes caps the action payload. Actions carry a handful of fields
// (domains, durations); anything bigger is a local process misbehaving.
const maxBodyBytes = 1 << 20 // 1 MiB

// DaemonClient is the IPC surface the web server proxies to. The production
// implementation is *ipc.Client; tests stub it.
type DaemonClient interface {
	SendWithTimeout(req ipc.Request, timeout time.Duration) (*ipc.Response, error)
}

// Server serves the UI and the /api surface. assets is the compiled UI (an
// fs.FS with index.html at its root — embedded, or os.DirFS in dev). An FS
// without index.html yields a helpful "UI não compilada" page.
type Server struct {
	client DaemonClient
	assets fs.FS
	// auth owns the in-memory sessions and login rate limiter (auth.go).
	auth *authManager
	// metrics records the HTTP proxy latency per action (Fase 8 — C3), so
	// /api/metrics shows the browser→web→daemon round-trip alongside the
	// daemon's own IPC stats.
	metrics *metrics.Registry
}

// New builds the web server around the IPC client and the UI assets.
func New(client DaemonClient, assets fs.FS) *Server {
	return &Server{
		client:  client,
		assets:  assets,
		auth:    newAuthManager(sessionTTL, maxLoginFailures, loginLockout),
		metrics: metrics.New(256),
	}
}

// Handler returns the full HTTP handler, including the localhost guards and
// the auth gate. /api/action and /api/logout require a valid session cookie;
// login/auth-status/health/ping and the static UI stay public (the SPA shows
// the login screen when auth-status says the browser is not authenticated).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/ping", s.handlePing)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("/api/action", s.requireAuth(s.handleAction))
	mux.HandleFunc("/api/events", s.requireAuth(s.handleEvents))
	mux.HandleFunc("/api/metrics", s.requireAuth(s.handleMetrics))
	mux.HandleFunc("/", s.handleStatic)
	return s.secure(gzipMiddleware(mux))
}

// secure applies the loopback-only guards to every request.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			writeJSONError(w, http.StatusForbidden, "acesso permitido apenas via localhost")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// gzipResponseWriter compresses body writes; WriteHeader and headers pass
// through to the underlying ResponseWriter.
type gzipResponseWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.Writer.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if gw, ok := g.Writer.(*gzip.Writer); ok {
		gw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// gzipMiddleware compresses the response body when the client advertises gzip
// support. The status payload — the full block list — is the heaviest object
// the web UI fetches (1.4 MB at 1000 blocks) and compresses to ~10%;
// browsers always send Accept-Encoding, so the UI gets the compact form
// automatically while plain clients (tests, curl) keep the uncompressed JSON.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		// O stream de eventos não é compactado de propósito: SSE + gzip é
		// suscetível a bufferização de proxies, e o payload por ciclo é mínimo
		// (eventos coarse sem dados).
		if r.URL.Path == "/api/events" {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

// isLocalHost reports whether the request Host is a loopback hostname. The
// port is ignored: only the hostname matters for DNS-rebinding protection (an
// attacker domain carries the attacker's hostname even when it resolves to
// 127.0.0.1).
func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "127.0.0.1", "localhost", "::1", "0:0:0:0:0:0:0:1":
		return true
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	// O health é do focusguard-web, não do daemon: responde sempre que o
	// servidor está de pé (o CLI usa para detectar spawn bem-sucedido).
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	resp, err := s.client.SendWithTimeout(ipc.Request{Action: "ping"}, proxyTimeout)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "daemon indisponível: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	// Content-Type enforcement: a cross-origin POST with a simple content type
	// (form/text) would be sent by browsers without a CORS preflight. Requiring
	// application/json (exact media type; charset is allowed) forces the
	// preflight, so a malicious site cannot write.
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeJSONError(w, http.StatusUnsupportedMediaType, "Content-Type deve ser application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req ipc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}

	// Autorização por ação vem da tabela declarativa ipc.SpecFor (B6) — a
	// mesma fonte usada pelo daemon. O daemon não repete esta checagem: ele
	// confia no IPC local (mesmo nível do CLI/tray), a autorização vive na
	// camada web. user-verify e ações desconhecidas NÃO têm spec (allowlist):
	// o proxy não as encaminha (403) — user-verify só é legítimo pelo
	// /api/login (que fala direto com o daemon); encaminhá-lo daria a qualquer
	// usuário autenticado um oráculo de senha SEM o rate limit do login.
	sess, ok := sessionFrom(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "não autenticado — faça login")
		return
	}
	spec, ok := ipc.SpecFor(req.Action)
	if !ok {
		writeJSONError(w, http.StatusForbidden, "use /api/login para autenticar")
		return
	}
	switch spec.Permission {
	case ipc.PermAdmin:
		if !sess.isAdmin {
			writeJSONError(w, http.StatusForbidden, "apenas o administrador gerencia usuários")
			return
		}
	case ipc.PermSelf:
		if !sess.isAdmin && !strings.EqualFold(req.UserName, sess.username) {
			writeJSONError(w, http.StatusForbidden, "você só pode alterar a própria senha")
			return
		}
	}

	// Timeout por ação vem do spec (B7): o mesmo orçamento que o daemon
	// conhece, sem tabela duplicada (antes: actionTimeoutFor).
	start := time.Now()
	resp, err := s.client.SendWithTimeout(req, spec.Timeout)
	s.recordProxy(req.Action, start, resp, err)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"daemon indisponível — verifique se o serviço FocusGuard está rodando")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// recordProxy mede a latência do proxy HTTP por ação (Fase 8 — C3) e loga os
// proxies lentos. event-subscribe é excluído: o loop SSE usa a própria rota
// (não passa por aqui), e um long-poll de 20s não é sinal de regressão.
func (s *Server) recordProxy(action string, start time.Time, resp *ipc.Response, err error) {
	if action == "event-subscribe" {
		return
	}
	d := time.Since(start)
	s.metrics.Record(action, d)
	if d < slowProxyThreshold {
		return
	}
	if err != nil {
		log.Printf("[Metrics] http action=%s duration_ms=%d error=%v", action, d.Milliseconds(), err)
	} else {
		log.Printf("[Metrics] http action=%s duration_ms=%d ok=%t code=%s",
			action, d.Milliseconds(), resp.Success, resp.Code)
	}
}

// handleStatic serves the compiled UI with SPA fallback (unknown paths render
// index.html, since routing is owned by the React app). Without a compiled UI
// it serves a page explaining how to build it.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	// Rotas /api/* desconhecidas não são parte da SPA: devolvem 404 JSON em
	// vez de cair no fallback que renderiza index.html.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSONError(w, http.StatusNotFound, "endpoint não existe")
		return
	}
	if _, err := fs.Stat(s.assets, "index.html"); err != nil {
		serveStubUI(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.assets, name)
	if err != nil {
		name = "index.html"
		data, err = fs.ReadFile(s.assets, name)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "não encontrado")
			return
		}
	}
	w.Header().Set("Content-Type", contentTypeFor(name))
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

// serveStubUI renders a short page telling the developer to build the UI when
// the binary ships without embedded assets (plain `go build`, no `make ui`).
func serveStubUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	page := `<!doctype html><html lang="pt-BR"><meta charset="utf-8">
<title>FocusGuard — UI não compilada</title>
<style>body{font-family:system-ui,sans-serif;background:#0b1020;color:#e6e9f5;display:grid;place-items:center;min-height:100vh;margin:0}
.card{background:#151d38;border:1px solid #232c4d;border-radius:14px;padding:2rem;max-width:520px;text-align:center}
code{background:#0b1020;padding:.2rem .5rem;border-radius:6px}</style>
<div class="card"><h1>🛡 FocusGuard UI</h1>
<p>Este binário foi compilado sem a interface web embutida.</p>
<p>Compile a UI primeiro e recompile o servidor:</p>
<p><code>make ui</code><br><code>go build ./cmd/focusguard-web</code></p></div>`
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, page)
	}
}

// contentTypeFor maps the static asset extensions Vite emits to content types.
func contentTypeFor(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".json", ".map":
		return "application/json"
	case ".woff2":
		return "font/woff2"
	}
	return "application/octet-stream"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"success": false, "message": msg})
}
