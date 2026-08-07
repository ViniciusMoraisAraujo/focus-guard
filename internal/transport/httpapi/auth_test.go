package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"focusguard/internal/transport/ipc"
)

// userCookie creates a non-admin session for the given username.
func userCookie(t *testing.T, srv *Server, username string) *http.Cookie {
	t.Helper()
	token, err := srv.auth.create(username, false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token}
}

func TestLogin_Success_SetsSessionCookie(t *testing.T) {
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		if req.Action != "user-verify" {
			t.Errorf("esperado user-verify, got %q", req.Action)
		}
		return &ipc.Response{Success: true, UserIsAdmin: true}, nil
	}}
	_, h := newTestServer(sc, uiFS())

	rec := doJSON(t, h, nil, "POST", "/api/login", "application/json",
		`{"username":"admin","password":"qualquer"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	m := decodeResp(t, rec)
	if m["username"] != "admin" || m["is_admin"] != true {
		t.Errorf("resposta = %v, want username=admin is_admin=true", m)
	}
	// O daemon recebeu o user-verify com as credenciais.
	if sc.lastReq.Action != "user-verify" || sc.lastReq.UserName != "admin" {
		t.Errorf("request não repassado: %+v", sc.lastReq)
	}

	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "fg_session=") {
		t.Fatalf("cookie de sessão ausente: %q", setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Errorf("cookie deveria ser HttpOnly: %q", setCookie)
	}
	if !strings.Contains(setCookie, "SameSite=Strict") {
		t.Errorf("cookie deveria ser SameSite=Strict: %q", setCookie)
	}
}

func TestLogin_Success_NonAdmin(t *testing.T) {
	sc := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return &ipc.Response{Success: true, UserIsAdmin: false}, nil
	}}
	_, h := newTestServer(sc, uiFS())

	rec := doJSON(t, h, nil, "POST", "/api/login", "application/json",
		`{"username":"maria","password":"senha-forte-1"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if m := decodeResp(t, rec); m["is_admin"] != false {
		t.Errorf("is_admin = %v, want false", m["is_admin"])
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	sc := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return &ipc.Response{Success: false, Message: "usuário ou senha inválidos"}, nil
	}}
	_, h := newTestServer(sc, uiFS())

	rec := doJSON(t, h, nil, "POST", "/api/login", "application/json",
		`{"username":"admin","password":"errada"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Error("login falho não deveria setar cookie")
	}
	if m := decodeResp(t, rec); m["success"] != false {
		t.Errorf("resposta = %v, want success:false", m)
	}
}

func TestLogin_DaemonDown(t *testing.T) {
	sc := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return nil, errFake("connection refused")
	}}
	_, h := newTestServer(sc, uiFS())

	rec := doJSON(t, h, nil, "POST", "/api/login", "application/json",
		`{"username":"admin","password":"x"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestLogin_RejectsInvalidInput(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())

	if rec := doJSON(t, h, nil, "POST", "/api/login", "application/json", `{not json`, "127.0.0.1:48902"); rec.Code != http.StatusBadRequest {
		t.Errorf("JSON inválido: status = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, h, nil, "POST", "/api/login", "text/plain", `{}`, "127.0.0.1:48902"); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Content-Type errado: status = %d, want 415", rec.Code)
	}
	if rec := doJSON(t, h, nil, "GET", "/api/login", "", "", "127.0.0.1:48902"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status = %d, want 405", rec.Code)
	}
}

// TestLogin_RateLimitsAfterFailures verifica o guard de brute force: após 5
// logins falhos consecutivos da mesma origem, a 6ª tentativa é 429.
func TestLogin_RateLimitsAfterFailures(t *testing.T) {
	sc := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return &ipc.Response{Success: false, Message: "usuário ou senha inválidos"}, nil
	}}
	_, h := newTestServer(sc, uiFS())

	for i := 1; i <= maxLoginFailures; i++ {
		rec := doJSON(t, h, nil, "POST", "/api/login", "application/json",
			`{"username":"admin","password":"errada"}`, "127.0.0.1:48902")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("tentativa %d: status = %d, want 401", i, rec.Code)
		}
	}
	rec := doJSON(t, h, nil, "POST", "/api/login", "application/json",
		`{"username":"admin","password":"errada"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6ª tentativa: status = %d, want 429", rec.Code)
	}
}

func TestLogout_InvalidatesSession(t *testing.T) {
	sc := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
		return &ipc.Response{Success: true, UserIsAdmin: true}, nil
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	rec := doJSON(t, h, cookie, "POST", "/api/logout", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=0") &&
		!strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=-1") {
		t.Errorf("logout deveria expirar o cookie: %q", rec.Header().Get("Set-Cookie"))
	}

	// O mesmo cookie não autentica mais o /api/action.
	after := doJSON(t, h, cookie, "POST", "/api/action", "application/json", `{"action":"status"}`, "127.0.0.1:48902")
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("após logout: status = %d, want 401", after.Code)
	}
}

func TestLogout_WithoutSession_Unauthorized(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, nil, "POST", "/api/logout", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthStatus_NotAuthenticated(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, nil, "GET", "/api/auth/status", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if m := decodeResp(t, rec); m["authenticated"] != false {
		t.Errorf("resposta = %v, want authenticated:false", m)
	}
}

func TestAuthStatus_Authenticated(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	cookie := adminCookie(t, srv)
	rec := doJSON(t, h, cookie, "GET", "/api/auth/status", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	m := decodeResp(t, rec)
	if m["authenticated"] != true || m["username"] != "admin" || m["is_admin"] != true {
		t.Errorf("resposta = %v, want authenticated+admin", m)
	}
}

func TestAction_RequiresSession(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, nil, "POST", "/api/action", "application/json", `{"action":"status"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestAction_ExpiredSession verifica que uma sessão vencida não autentica.
func TestAction_ExpiredSession(t *testing.T) {
	srv := New(&stubClient{}, uiFS())
	srv.auth = newAuthManager(-time.Minute, maxLoginFailures, loginLockout)
	h := srv.Handler()

	token, err := srv.auth.create("admin", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cookie := &http.Cookie{Name: sessionCookieName, Value: token}
	rec := doJSON(t, h, cookie, "POST", "/api/action", "application/json", `{"action":"status"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (sessão expirada)", rec.Code)
	}
}

// TestAction_UserVerifyRejected trava o gap de segurança: user-verify só é
// legítimo pelo /api/login. Permitido via /api/action, viraria um oráculo de
// senha sem o rate limit do login — brute force do admin por um usuário comum.
func TestAction_UserVerifyRejected(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())

	// Mesmo o admin não usa user-verify pelo /api/action — o login é o único
	// caminho.
	rec := doJSON(t, h, adminCookie(t, srv), "POST", "/api/action", "application/json",
		`{"action":"user-verify","user_name":"admin","user_password":"x"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// TestAction_UnknownAction_Forbidden trava a allowlist por spec (B6): ação
// sem ActionSpec (desconhecida ou web-only) não é encaminhada ao daemon — o
// proxy responde 403 em vez de repassar e depender do "Not suported action".
func TestAction_UnknownAction_Forbidden(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, adminCookie(t, srv), "POST", "/api/action", "application/json",
		`{"action":"delete"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAction_AdminOnly_UserList(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, userCookie(t, srv, "maria"), "POST", "/api/action", "application/json",
		`{"action":"user-list"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAction_AdminOnly_UserAdd(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, userCookie(t, srv, "maria"), "POST", "/api/action", "application/json",
		`{"action":"user-add","user_name":"joao","user_password":"senha-forte-1"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAction_AdminOnly_UserRemove(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, userCookie(t, srv, "maria"), "POST", "/api/action", "application/json",
		`{"action":"user-remove","user_name":"joao"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAction_UserSetPassword_SelfAllowed(t *testing.T) {
	sc := &stubClient{}
	srv, h := newTestServer(sc, uiFS())
	rec := doJSON(t, h, userCookie(t, srv, "maria"), "POST", "/api/action", "application/json",
		`{"action":"user-set-password","user_name":"maria","user_password":"nova-senha-123"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if sc.lastReq.Action != "user-set-password" || sc.lastReq.UserName != "maria" {
		t.Errorf("request não repassado: %+v", sc.lastReq)
	}
}

func TestAction_UserSetPassword_OtherForbidden(t *testing.T) {
	srv, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, userCookie(t, srv, "maria"), "POST", "/api/action", "application/json",
		`{"action":"user-set-password","user_name":"joao","user_password":"nova-senha-123"}`, "127.0.0.1:48902")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAction_AdminCanManageUsers(t *testing.T) {
	sc := &stubClient{}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	for _, body := range []string{
		`{"action":"user-list"}`,
		`{"action":"user-add","user_name":"joao","user_password":"senha-forte-1"}`,
		`{"action":"user-remove","user_name":"joao"}`,
		`{"action":"user-set-password","user_name":"joao","user_password":"nova-senha-123"}`,
	} {
		rec := doJSON(t, h, cookie, "POST", "/api/action", "application/json", body, "127.0.0.1:48902")
		if rec.Code != http.StatusOK {
			t.Fatalf("admin %s: status = %d, want 200 (%s)", body, rec.Code, rec.Body.String())
		}
	}
}

// TestAuthStatus_RejectsNonGET garante que o auth-status só aceita GET.
func TestAuthStatus_RejectsNonGET(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, nil, "POST", "/api/auth/status", "application/json", `{}`, "127.0.0.1:48902")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestPingStaysPublic confirma que o ping do daemon não exige login (a UI usa
// para o indicador de conectividade antes de autenticar).
func TestPingStaysPublic(t *testing.T) {
	_, h := newTestServer(&stubClient{}, uiFS())
	rec := doJSON(t, h, nil, "GET", "/api/ping", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
