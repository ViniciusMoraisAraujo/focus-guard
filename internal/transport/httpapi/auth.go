// Autenticação da interface web: login/logout/auth-status com sessões em
// memória (cookie HttpOnly) e rate limit de login. Vive no processo
// user-space focusguard-web — a validação das credenciais é feita pelo daemon
// via IPC (user-verify, bcrypt), e aqui só são emitidos/validados tokens.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"focusguard/internal/transport/ipc"
)

const (
	// sessionCookieName is the HttpOnly session cookie set on login.
	sessionCookieName = "fg_session"
	// sessionTTL bounds how long a login lasts before re-authentication.
	sessionTTL = 12 * time.Hour
	// maxLoginFailures / loginLockout: brute-force guard on the admin account.
	maxLoginFailures = 5
	loginLockout     = 30 * time.Second
	// sessionPurgeThreshold: when the session map grows past this, expired
	// sessions are swept lazily on the next login — no background goroutine.
	sessionPurgeThreshold = 100
)

// session is one authenticated browser session (in-memory only: a restart of
// the user-space web process logs everyone out, which is acceptable here).
type session struct {
	username string
	isAdmin  bool
	expires  time.Time
}

// authManager owns the in-memory sessions and the login rate limiter. Both
// maps are small (localhost-only) and guarded by their own mutex.
type authManager struct {
	mu       sync.RWMutex
	sessions map[string]session
	ttl      time.Duration

	limitMu     sync.Mutex
	failures    map[string]int
	lockedUntil map[string]time.Time
	maxFailures int
	lockout     time.Duration
}

// newAuthManager builds a manager with the given session TTL and login
// lockout policy (parameters are injectable for tests).
func newAuthManager(ttl time.Duration, maxFailures int, lockout time.Duration) *authManager {
	return &authManager{
		sessions:    make(map[string]session),
		ttl:         ttl,
		failures:    make(map[string]int),
		lockedUntil: make(map[string]time.Time),
		maxFailures: maxFailures,
		lockout:     lockout,
	}
}

// create registers a session and returns its token (64 hex chars from 32
// random bytes).
func (a *authManager) create(username string, isAdmin bool) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions) >= sessionPurgeThreshold {
		a.purgeExpiredLocked()
	}
	a.sessions[token] = session{
		username: username,
		isAdmin:  isAdmin,
		expires:  time.Now().Add(a.ttl),
	}
	return token, nil
}

// get returns the session for a token when it exists and has not expired.
func (a *authManager) get(token string) (session, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s, ok := a.sessions[token]
	if !ok || time.Now().After(s.expires) {
		return session{}, false
	}
	return s, true
}

func (a *authManager) delete(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, token)
}

// purgeExpiredLocked sweeps expired sessions. Caller must hold a.mu.
func (a *authManager) purgeExpiredLocked() {
	now := time.Now()
	for t, s := range a.sessions {
		if now.After(s.expires) {
			delete(a.sessions, t)
		}
	}
}

// allowLogin reports whether the client may attempt a login now (false = the
// IP is inside the lockout window).
func (a *authManager) allowLogin(ip string) bool {
	a.limitMu.Lock()
	defer a.limitMu.Unlock()
	until, locked := a.lockedUntil[ip]
	return !(locked && time.Now().Before(until))
}

// recordFailure counts one failed login; maxFailures consecutive failures
// lock the IP for the lockout window.
func (a *authManager) recordFailure(ip string) {
	a.limitMu.Lock()
	defer a.limitMu.Unlock()
	if until, locked := a.lockedUntil[ip]; locked && time.Now().Before(until) {
		return // já travado — mantém o lock ativo
	}
	a.failures[ip]++
	if a.failures[ip] >= a.maxFailures {
		a.lockedUntil[ip] = time.Now().Add(a.lockout)
		a.failures[ip] = 0
	}
}

func (a *authManager) clearFailures(ip string) {
	a.limitMu.Lock()
	defer a.limitMu.Unlock()
	delete(a.failures, ip)
	delete(a.lockedUntil, ip)
}

// newToken returns 64 hex chars from 32 bytes of crypto/rand.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// Sessão no request context (preenchida pelo requireAuth)
// ---------------------------------------------------------------------------

type ctxKey int

const sessionCtxKey ctxKey = 0

func withSession(ctx context.Context, s session) context.Context {
	return context.WithValue(ctx, sessionCtxKey, s)
}

func sessionFrom(ctx context.Context) (session, bool) {
	s, ok := ctx.Value(sessionCtxKey).(session)
	return s, ok
}

// setSessionCookie sets the HttpOnly session cookie. No Secure flag: the
// server is plain HTTP on loopback, and Secure would make browsers drop the
// cookie on http://127.0.0.1.
func setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// clientIP extracts the client host from RemoteAddr. On this loopback-only
// server every connection comes from the same local address, so the login
// lockout is machine-global: 5 failed attempts from any browser lock the login
// for 30s for all browsers. That is acceptable here — a local process already
// holds far bigger powers over the daemon — and it is simpler than keying on
// something the client could trivially forge.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// requireAuth gates an endpoint behind a valid session cookie, putting the
// session into the request context for the handler.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "não autenticado — faça login")
			return
		}
		sess, ok := s.auth.get(c.Value)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "sessão expirada — faça login novamente")
			return
		}
		next(w, r.WithContext(withSession(r.Context(), sess)))
	}
}

// loginRequest is the POST /api/login body.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleLogin validates credentials against the daemon (IPC user-verify) and
// issues the session cookie on success. Failures are rate-limited per IP.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	// Mesma política do /api/action: Content-Type application/json obrigatório
	// força o preflight CORS e mata o CSRF por simple-request.
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeJSONError(w, http.StatusUnsupportedMediaType, "Content-Type deve ser application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	ip := clientIP(r)
	if !s.auth.allowLogin(ip) {
		writeJSONError(w, http.StatusTooManyRequests, "muitas tentativas de login — aguarde e tente novamente")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}

	resp, err := s.client.SendWithTimeout(ipc.Request{
		Action:       "user-verify",
		UserName:     req.Username,
		UserPassword: req.Password,
	}, proxyTimeout)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"daemon indisponível — verifique se o serviço FocusGuard está rodando")
		return
	}
	if !resp.Success {
		// Mensagem única do daemon ("usuário ou senha inválidos") — não revela
		// se foi o usuário ou a senha.
		s.auth.recordFailure(ip)
		writeJSONError(w, http.StatusUnauthorized, resp.Message)
		return
	}
	s.auth.clearFailures(ip)

	token, err := s.auth.create(req.Username, resp.UserIsAdmin)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "falha ao criar sessão")
		return
	}
	setSessionCookie(w, token, s.auth.ttl)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"username": req.Username,
		"is_admin": resp.UserIsAdmin,
	})
}

// handleLogout invalidates the session (the requireAuth wrapper already
// validated the cookie) and clears it.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		s.auth.delete(c.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// handleAuthStatus reports whether the browser is authenticated and, when so,
// who it is — the SPA uses it to decide between the login screen and the app.
// Always 200: "not logged in" is a normal state, not an error.
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	sess, ok := s.auth.get(c.Value)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"username":      sess.username,
		"is_admin":      sess.isAdmin,
	})
}
