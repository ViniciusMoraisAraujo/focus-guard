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
	"encoding/json"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"focusguard/internal/ipc"
)

// DefaultAddr is where focusguard-web listens (loopback only). The CLI probes
// the same address to decide whether to spawn a new server or reuse one.
const DefaultAddr = "127.0.0.1:48902"

// proxyTimeout bounds each IPC call made on behalf of the browser. It mirrors
// the tray's SendWithTimeout discipline: a hung daemon must never hang the UI.
const proxyTimeout = 5 * time.Second

// updateTimeout bounds the update/update-check actions, which can take minutes:
// they make the daemon download the release archive, extract it and swap the
// binaries. It must be at least as generous as the daemon's own IPC budget
// (internal/ipc.updateTimeout) so a slow-but-successful update is not reported
// as "daemon indisponível".
const updateTimeout = 150 * time.Second

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
}

// New builds the web server around the IPC client and the UI assets.
func New(client DaemonClient, assets fs.FS) *Server {
	return &Server{client: client, assets: assets}
}

// Handler returns the full HTTP handler, including the localhost guards.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/ping", s.handlePing)
	mux.HandleFunc("/api/action", s.handleAction)
	mux.HandleFunc("/", s.handleStatic)
	return s.secure(mux)
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

	// O update/update-check precisam de um orçamento generoso (download +
	// troca de binários); as demais ações usam o timeout curto do proxy.
	timeout := proxyTimeout
	if req.Action == "update" || req.Action == "update-check" {
		timeout = updateTimeout
	}
	resp, err := s.client.SendWithTimeout(req, timeout)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"daemon indisponível — verifique se o serviço FocusGuard está rodando")
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
