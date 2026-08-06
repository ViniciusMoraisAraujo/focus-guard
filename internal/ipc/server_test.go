package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"focusguard/internal/domain/analytics"
	"focusguard/internal/enforcer"
	"focusguard/internal/domain/pomodoro"

	"focusguard/internal/domain/preset"
	"focusguard/internal/domain/schedule"
	"focusguard/internal/domain/scheduler"
	"focusguard/internal/store"
	"focusguard/internal/tamper"
)

type fakeUpdateChecker struct {
	status  UpdateStatus
	err     error
	apply   bool
	channel string
}

func (f *fakeUpdateChecker) Check(_ context.Context, apply bool, channel string) (UpdateStatus, error) {
	f.apply = apply
	f.channel = channel
	return f.status, f.err
}

type mockEnforcer struct{}

func (m *mockEnforcer) BlockDomain(_ string, _ []string) error   { return nil }
func (m *mockEnforcer) UnblockDomain(_ string, _ []string) error { return nil }
func (m *mockEnforcer) Sync(_ map[string][]string) error         { return nil }
func (m *mockEnforcer) BlockDoH() error                          { return nil }
func (m *mockEnforcer) UnblockDoH() error                        { return nil }
func (m *mockEnforcer) BlockAll(_ []string) error                { return nil }
func (m *mockEnforcer) UnblockAll() error                        { return nil }
func (m *mockEnforcer) Status() (enforcer.EnforcerStatus, error) {
	return enforcer.EnforcerStatus{}, nil
}

// setupTestServer monta um servidor de teste com os adapters de referência e
// deps vazias (apps/users/hook desconfigurados — as ações falham com "não
// configurado"). Use setupTestServerWithDeps para injetar fakes.
func setupTestServer(t *testing.T) *Server {
	return setupTestServerWithDeps(t, nil)
}

// setupTestServerWithDeps é o setupTestServer com as deps de referência
// (apps/users/onDNSStarted) explícitas. Os Set* correspondentes foram
// removidos do Server (Fase 5) — os testes que precisam de um fake passam por
// aqui, espelhando o construtor dos handlers reais de domínio no composition
// root do daemon. deps nil = apps/users/hook desconfigurados (as ações falham
// com "não configurado").
func setupTestServerWithDeps(t *testing.T, deps *refDeps) *Server {
	t.Helper()

	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "state.json"))
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	sched := scheduler.NewScheduler(st, &mockEnforcer{})
	s := NewServer(sched)
	// Ações de domínio (block/apps/goal/presets/users/dns): o daemon real as
	// registra com os handlers dos pacotes de domínio (composition root — Fase
	// 5); aqui os testes internos usam os adapters de referência
	// (handlers_ref_test.go), que reproduzem 1:1 o comportamento legado.
	registerDomainReferenceHandlers(s, deps)
	return s
}

func executeRequest(t *testing.T, server *Server, req Request) Response {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	defer func() {
		_ = clientConn.Close()
	}()

	go server.handleConnection(serverConn)

	if err := json.NewEncoder(clientConn).Encode(req); err != nil {
		t.Fatalf("failed to encode request: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	return resp
}

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestClientSend_DialError(t *testing.T) {
	setTestEndpoint(t)

	c := NewClient()
	_, err := c.Send(Request{Action: "ping"})
	if err == nil {
		t.Fatal("expected dial error when no server is running")
	}
	if !strings.Contains(err.Error(), "error connecting to ipc") {
		t.Errorf("expected connection error message, got: %v", err)
	}
}

func TestClientSendWithTimeout_Success(t *testing.T) {
	server := setupTestServer(t)

	ln := newTestListener(t)
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			server.handleConnection(conn)
		}
	}()

	c := NewClient()
	resp, err := c.SendWithTimeout(Request{Action: "ping"}, 2*time.Second)
	if err != nil {
		t.Fatalf("SendWithTimeout failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

func TestClientSendWithTimeout_DialError(t *testing.T) {
	setTestEndpoint(t)

	c := NewClient()
	_, err := c.SendWithTimeout(Request{Action: "ping"}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected dial error when no server is running")
	}
	if !strings.Contains(err.Error(), "error connecting to ipc") {
		t.Errorf("expected connection error message, got: %v", err)
	}
}

func TestClientSend_Success(t *testing.T) {
	server := setupTestServer(t)

	ln := newTestListener(t)
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			server.handleConnection(conn)
		}
	}()

	c := NewClient()
	resp, err := c.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

func TestClientSend_DecodeError(t *testing.T) {
	ln := newTestListener(t)
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Write([]byte("invalid-json\n"))
			conn.Close()
		}
	}()

	c := NewClient()
	_, err := c.Send(Request{Action: "ping"})
	if err == nil {
		t.Fatal("expected decode error with invalid response")
	}
	if !strings.Contains(err.Error(), "error decoding") {
		t.Errorf("expected decode error message, got: %v", err)
	}
}

func TestServer_NewServer(t *testing.T) {
	server := setupTestServer(t)

	if server == nil {
		t.Fatal("expected NewServer to return a non-nil instance")
	}
	if server.scheduler == nil {
		t.Error("expected scheduler to be set on server instance")
	}
}

func TestServer_Stop_NilListener(t *testing.T) {
	server := setupTestServer(t)

	if err := server.Stop(); err != nil {
		t.Errorf("expected Stop() to return nil when listener is uninitialized, got: %v", err)
	}
}

func TestServer_HandleConnection_InvalidJSON(t *testing.T) {
	server := setupTestServer(t)

	clientConn, serverConn := net.Pipe()
	defer func() {
		_ = clientConn.Close()
	}()

	go server.handleConnection(serverConn)

	if _, err := clientConn.Write([]byte("{invalid-json-payload\n")); err != nil {
		t.Fatalf("failed to write raw data: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Success {
		t.Error("expected response success to be false for invalid JSON payload")
	}
	if resp.Message != "Request invalid" {
		t.Errorf("expected message %q, got %q", "Request invalid", resp.Message)
	}
}

func TestServer_HandleConnection_UnsupportedAction(t *testing.T) {
	server := setupTestServer(t)

	req := Request{
		Action: "delete",
	}

	resp := executeRequest(t, server, req)

	if resp.Success {
		t.Error("expected response success to be false for unsupported action")
	}
	expectedMsg := "Not suported action: delete"
	if resp.Message != expectedMsg {
		t.Errorf("expected message %q, got %q", expectedMsg, resp.Message)
	}
}

func TestServer_HandleConnection_Block_InvalidDuration(t *testing.T) {
	server := setupTestServer(t)

	testCases := []struct {
		name     string
		duration string
	}{
		{name: "malformed duration string", duration: "invalid-time"},
		{name: "negative duration value", duration: "-30m"},
		{name: "zero duration value", duration: "0s"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := Request{
				Action:   "block",
				Domain:   "example.com",
				Duration: tc.duration,
			}

			resp := executeRequest(t, server, req)

			if resp.Success {
				t.Errorf("expected request to fail for %s", tc.name)
			}
			if !strings.HasPrefix(resp.Message, "Duration invalid") {
				t.Errorf("expected error message starting with 'Duration invalid', got %q", resp.Message)
			}
		})
	}
}

func TestServer_HandleConnection_Block_Success(t *testing.T) {
	server := setupTestServer(t)

	req := Request{
		Action:   "block",
		Domain:   "example.com",
		Duration: "1h",
	}

	resp := executeRequest(t, server, req)

	if !resp.Success {
		t.Fatalf("expected block request to succeed, got error: %s", resp.Message)
	}

	if !strings.Contains(resp.Message, "Domain example.com blocked") {
		t.Errorf("expected success message containing domain confirmation, got %q", resp.Message)
	}
}

type statusEnforcer struct {
	*mockEnforcer
	st enforcer.EnforcerStatus
}

func (e *statusEnforcer) Status() (enforcer.EnforcerStatus, error) { return e.st, nil }

func TestServer_HandleConnection_Status_IncludesProtection(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	enf := &statusEnforcer{
		mockEnforcer: &mockEnforcer{},
		st:           enforcer.EnforcerStatus{DoHActive: true, FirewallRules: 5},
	}
	sched := scheduler.NewScheduler(st, enf)
	server := NewServer(sched)

	resp := executeRequest(t, server, Request{Action: "status"})

	if !resp.Success {
		t.Fatalf("expected status to succeed, got: %s", resp.Message)
	}
	if !resp.DoHActive {
		t.Error("expected DoHActive=true in status response")
	}
	if resp.FirewallRules != 5 {
		t.Errorf("expected FirewallRules=5, got %d", resp.FirewallRules)
	}
}

func TestServer_Update_NotConfigured(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "update"})

	if resp.Success {
		t.Error("expected failure when no update checker is configured")
	}
	if !strings.Contains(resp.Message, "não configurado") {
		t.Errorf("expected 'não configurado' message, got %q", resp.Message)
	}
}

func TestServer_Update_NoUpdate(t *testing.T) {
	server := setupTestServer(t)
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0"},
	})

	resp := executeRequest(t, server, Request{Action: "update"})

	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if resp.UpdateAvailable {
		t.Error("expected UpdateAvailable=false when no update exists")
	}
	if resp.CurrentVersion != "1.0.0" {
		t.Errorf("expected CurrentVersion 1.0.0, got %q", resp.CurrentVersion)
	}
	if !strings.Contains(resp.Message, "Nenhuma atualização disponível") {
		t.Errorf("expected up-to-date message, got %q", resp.Message)
	}
}

func TestServer_Update_Applied(t *testing.T) {
	server := setupTestServer(t)
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{
			CurrentVersion: "1.0.0",
			NewVersion:     "1.1.0",
			Available:      true,
			Applied:        true,
		},
	})

	resp := executeRequest(t, server, Request{Action: "update"})

	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if !resp.UpdateAvailable {
		t.Error("expected UpdateAvailable=true")
	}
	if resp.UpdateVersion != "1.1.0" {
		t.Errorf("expected UpdateVersion 1.1.0, got %q", resp.UpdateVersion)
	}
	if resp.CurrentVersion != "1.0.0" {
		t.Errorf("expected CurrentVersion 1.0.0, got %q", resp.CurrentVersion)
	}
	if !strings.Contains(resp.Message, "Atualização aplicada") {
		t.Errorf("expected applied message, got %q", resp.Message)
	}
}

// TestServer_Update_Applied_NotifiesOnUpdateApplied verifies the server
// invokes the restart hook once an update has been applied — the daemon uses
// it to exit and let systemd/watchdog respawn with the new binary (Bug 2).
func TestServer_Update_Applied_NotifiesOnUpdateApplied(t *testing.T) {
	server := setupTestServer(t)
	applied := make(chan struct{}, 1)
	server.SetOnUpdateApplied(func() { applied <- struct{}{} })
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{
			CurrentVersion: "1.0.0",
			NewVersion:     "1.1.0",
			Available:      true,
			Applied:        true,
		},
	})

	resp := executeRequest(t, server, Request{Action: "update"})
	if !resp.Success {
		t.Fatalf("update falhou: %s", resp.Message)
	}

	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("SetOnUpdateApplied não foi notificado após update aplicado")
	}
}

// TestServer_Update_Applied_HookAfterResponse verifies the restart hook fires
// only AFTER the response has been written to the client: the hook blocks on a
// channel, and the client must still receive the response — proving the encode
// happens before (and independently of) the hook. If the hook ran before the
// flush, the client would hang waiting for the response.
func TestServer_Update_Applied_HookAfterResponse(t *testing.T) {
	server := setupTestServer(t)
	hookBlocked := make(chan struct{})
	releaseHook := make(chan struct{})
	server.SetOnUpdateApplied(func() {
		close(hookBlocked)
		<-releaseHook // trava o hook para provar que a resposta não depende dele
	})
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{
			CurrentVersion: "1.0.0",
			NewVersion:     "1.1.0",
			Available:      true,
			Applied:        true,
		},
	})

	clientConn, serverConn := net.Pipe()
	defer func() {
		_ = clientConn.Close()
		close(releaseHook)
	}()

	go server.handleConnection(serverConn)
	_ = json.NewEncoder(clientConn).Encode(Request{Action: "update"})

	// O decode da resposta deve completar mesmo com o hook bloqueado.
	var resp Response
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("resposta não chegou com o hook travado (ordem errada?): %v", err)
	}
	if !resp.Success || !resp.UpdateAvailable {
		t.Errorf("resposta inesperada: %+v", resp)
	}

	// E o hook realmente foi disparado (após a resposta).
	select {
	case <-hookBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("hook de restart não foi notificado após update aplicado")
	}
}

// TestServer_Update_NotApplied_NoNotify verifies the restart hook is NOT fired
// when no update is applied (check-only or failure) — the daemon must not
// restart into an un-updated binary.
func TestServer_Update_NotApplied_NoNotify(t *testing.T) {
	server := setupTestServer(t)
	notified := make(chan struct{}, 1)
	server.SetOnUpdateApplied(func() { notified <- struct{}{} })
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0", Available: false},
	})

	resp := executeRequest(t, server, Request{Action: "update"})
	if !resp.Success {
		t.Fatalf("update falhou: %s", resp.Message)
	}

	select {
	case <-notified:
		t.Fatal("não deveria notificar restart quando não há update aplicado")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestServer_Update_CheckerError(t *testing.T) {
	server := setupTestServer(t)
	server.SetUpdateChecker(&fakeUpdateChecker{
		err: errors.New("github api unreachable"),
	})

	resp := executeRequest(t, server, Request{Action: "update"})

	if resp.Success {
		t.Error("expected failure when checker returns error")
	}
	if !strings.Contains(resp.Message, "github api unreachable") {
		t.Errorf("expected error message, got %q", resp.Message)
	}
}

func TestServer_Update_PassesApplyFlag(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0"},
	}
	server.SetUpdateChecker(fake)

	executeRequest(t, server, Request{Action: "update"})

	if !fake.apply {
		t.Error("expected checker.Check to be called with apply=true")
	}
}

func TestServer_UpdateCheck_NotConfigured(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "update-check"})

	if resp.Success {
		t.Error("expected failure when no update checker is configured")
	}
	if !strings.Contains(resp.Message, "não configurado") {
		t.Errorf("expected 'não configurado' message, got %q", resp.Message)
	}
}

func TestServer_UpdateCheck_NoUpdate(t *testing.T) {
	server := setupTestServer(t)
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0"},
	})

	resp := executeRequest(t, server, Request{Action: "update-check"})

	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if resp.UpdateAvailable {
		t.Error("expected UpdateAvailable=false when no update exists")
	}
	if resp.CurrentVersion != "1.0.0" {
		t.Errorf("expected CurrentVersion 1.0.0, got %q", resp.CurrentVersion)
	}
	if !strings.Contains(resp.Message, "Nenhuma atualização disponível") {
		t.Errorf("expected up-to-date message, got %q", resp.Message)
	}
}

func TestServer_UpdateCheck_Available(t *testing.T) {
	server := setupTestServer(t)
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{
			CurrentVersion: "1.0.0",
			NewVersion:     "1.1.0",
			Available:      true,
		},
	})

	resp := executeRequest(t, server, Request{Action: "update-check"})

	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if !resp.UpdateAvailable {
		t.Error("expected UpdateAvailable=true")
	}
	if resp.UpdateVersion != "1.1.0" {
		t.Errorf("expected UpdateVersion 1.1.0, got %q", resp.UpdateVersion)
	}
	if !strings.Contains(resp.Message, "Atualização disponível") {
		t.Errorf("expected available message, got %q", resp.Message)
	}
}

// TestServer_UpdateCheck_DoesNotApply verifies update-check runs check-only:
// apply=false é repassado ao checker e o hook de restart nunca é disparado.
func TestServer_UpdateCheck_DoesNotApply(t *testing.T) {
	server := setupTestServer(t)
	notified := make(chan struct{}, 1)
	server.SetOnUpdateApplied(func() { notified <- struct{}{} })
	fake := &fakeUpdateChecker{
		status: UpdateStatus{
			CurrentVersion: "1.0.0",
			NewVersion:     "1.1.0",
			Available:      true,
		},
	}
	server.SetUpdateChecker(fake)

	resp := executeRequest(t, server, Request{Action: "update-check"})

	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if fake.apply {
		t.Error("expected checker.Check to be called with apply=false")
	}
	select {
	case <-notified:
		t.Fatal("update-check não deveria disparar o hook de restart")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestServer_UpdateCheck_CheckerError(t *testing.T) {
	server := setupTestServer(t)
	server.SetUpdateChecker(&fakeUpdateChecker{
		err: errors.New("github api unreachable"),
	})

	resp := executeRequest(t, server, Request{Action: "update-check"})

	if resp.Success {
		t.Error("expected failure when checker returns error")
	}
	if !strings.Contains(resp.Message, "github api unreachable") {
		t.Errorf("expected error message, got %q", resp.Message)
	}
}

func TestServer_UpdateCheck_PassesChannel(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0"},
	}
	server.SetUpdateChecker(fake)

	executeRequest(t, server, Request{Action: "update-check", Channel: "beta"})

	if fake.channel != "beta" {
		t.Errorf("expected checker.Check channel=beta, got %q", fake.channel)
	}
}

// TestServer_Update_PassesChannel verifies the update action forwards the
// requested release channel ("stable"/"beta") to the checker — the daemon
// configures the updater to include prereleases only for the beta channel.
func TestServer_Update_PassesChannel(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0"},
	}
	server.SetUpdateChecker(fake)

	executeRequest(t, server, Request{Action: "update", Channel: "beta"})

	if fake.channel != "beta" {
		t.Errorf("expected checker.Check channel=beta, got %q", fake.channel)
	}
}

// TestServer_RefreshUpdateStatus_DefaultChannel verifies the background check
// (check-only, no apply) uses the stable channel — background checks must
// never surprise a stable user with a prerelease.
func TestServer_RefreshUpdateStatus_DefaultChannel(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0"},
	}
	server.SetUpdateChecker(fake)

	if _, err := server.RefreshUpdateStatus(context.Background()); err != nil {
		t.Fatalf("RefreshUpdateStatus: %v", err)
	}

	if fake.channel != "" {
		t.Errorf("expected default channel (empty), got %q", fake.channel)
	}
}

// TestServer_SetCurrentVersion_StatusFallback verifies that SetCurrentVersion
// feeds the status response before any update check has run.
func TestServer_SetCurrentVersion_StatusFallback(t *testing.T) {
	server := setupTestServer(t)
	server.SetCurrentVersion("9.9.9")

	resp := executeRequest(t, server, Request{Action: "status"})

	if !resp.Success {
		t.Fatalf("status failed: %s", resp.Message)
	}
	if resp.CurrentVersion != "9.9.9" {
		t.Errorf("expected CurrentVersion 9.9.9 from fallback, got %q", resp.CurrentVersion)
	}
}

// TestServer_Status_UpdateCacheTakesPrecedence verifies that once an update
// check has run, its cached version wins over the fallback.
func TestServer_Status_UpdateCacheTakesPrecedence(t *testing.T) {
	server := setupTestServer(t)
	server.SetCurrentVersion("9.9.9")
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{CurrentVersion: "1.0.0"},
	})

	if _, err := server.RefreshUpdateStatus(context.Background()); err != nil {
		t.Fatalf("RefreshUpdateStatus: %v", err)
	}

	resp := executeRequest(t, server, Request{Action: "status"})

	if !resp.Success {
		t.Fatalf("status failed: %s", resp.Message)
	}
	if resp.CurrentVersion != "1.0.0" {
		t.Errorf("expected cached CurrentVersion 1.0.0, got %q", resp.CurrentVersion)
	}
}

func TestServer_RefreshUpdateStatus_CachesResult(t *testing.T) {
	server := setupTestServer(t)
	server.SetUpdateChecker(&fakeUpdateChecker{
		status: UpdateStatus{
			CurrentVersion: "1.0.0",
			NewVersion:     "1.2.0",
			Available:      true,
		},
	})

	st, err := server.RefreshUpdateStatus(context.Background())
	if err != nil {
		t.Fatalf("RefreshUpdateStatus: %v", err)
	}
	if !st.Available || st.NewVersion != "1.2.0" {
		t.Errorf("unexpected status: %+v", st)
	}

	resp := executeRequest(t, server, Request{Action: "status"})
	if !resp.Success {
		t.Fatalf("status failed: %s", resp.Message)
	}
	if !resp.UpdateAvailable {
		t.Error("expected cached UpdateAvailable in status response")
	}
	if resp.UpdateVersion != "1.2.0" {
		t.Errorf("expected cached UpdateVersion 1.2.0, got %q", resp.UpdateVersion)
	}
}

func TestServer_RefreshUpdateStatus_NoChecker(t *testing.T) {
	server := setupTestServer(t)

	st, err := server.RefreshUpdateStatus(context.Background())
	if err != nil {
		t.Fatalf("expected no error without checker, got %v", err)
	}
	if st.Available {
		t.Error("expected no update when checker is nil")
	}
}

// fakePomodoroRunner records Start/Stop calls and returns canned state.
type fakePomodoroRunner struct {
	state    pomodoro.State
	started  pomodoro.Session
	startErr error
	stopped  bool
}

func (f *fakePomodoroRunner) Start(s pomodoro.Session) (pomodoro.State, error) {
	f.started = s
	return f.state, f.startErr
}
func (f *fakePomodoroRunner) Stop() (pomodoro.State, error) {
	f.stopped = true
	return f.state, nil
}

// ---------------------------------------------------------------------------
// Presets personalizados (preset-add / preset-remove)
// ---------------------------------------------------------------------------

// fakePresetManager is a stubbable PresetManager used to test the server's
// catalog wiring without touching disk.
type fakePresetManager struct {
	list    []preset.Preset
	added   []preset.Preset
	removed []string
	addErr  error
	remErr  error
}

func (f *fakePresetManager) List() []preset.Preset                      { return f.list }
func (f *fakePresetManager) Resolve(name string) (preset.Preset, error) { return preset.Preset{}, nil }
func (f *fakePresetManager) Add(p preset.Preset) error {
	f.added = append(f.added, p)
	return f.addErr
}
func (f *fakePresetManager) Remove(name string) error {
	f.removed = append(f.removed, name)
	return f.remErr
}

func TestServer_PresetAdd_Success(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePresetManager{}
	server.SetPresets(fake)

	resp := executeRequest(t, server, Request{
		Action:        "preset-add",
		PresetName:    "estudo",
		PresetLabel:   "Estudo",
		PresetDomains: []string{"khanacademy.org", "coursera.org"},
	})
	if !resp.Success {
		t.Fatalf("preset-add falhou: %s", resp.Message)
	}
	if len(fake.added) != 1 {
		t.Fatalf("added = %d, want 1", len(fake.added))
	}
	if fake.added[0].Name != "estudo" || len(fake.added[0].Domains) != 2 {
		t.Errorf("preset adicionado inesperado: %+v", fake.added[0])
	}
}

func TestServer_PresetAdd_ValidationError(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePresetManager{addErr: errors.New("preset: nome não pode ser vazio")}
	server.SetPresets(fake)

	resp := executeRequest(t, server, Request{Action: "preset-add", PresetName: ""})
	if resp.Success {
		t.Fatal("expected failure when the store rejects the preset")
	}
	if !strings.Contains(resp.Message, "nome não pode ser vazio") {
		t.Errorf("message deveria carregar o erro do store, got %q", resp.Message)
	}
}

func TestServer_PresetAdd_WithoutStore(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "preset-add", PresetName: "x", PresetDomains: []string{"a.com"}})
	if resp.Success {
		t.Fatal("expected failure without a preset store")
	}
}

func TestServer_PresetRemove_Success(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePresetManager{}
	server.SetPresets(fake)

	resp := executeRequest(t, server, Request{Action: "preset-remove", PresetName: "estudo"})
	if !resp.Success {
		t.Fatalf("preset-remove falhou: %s", resp.Message)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "estudo" {
		t.Errorf("removed = %v, want [estudo]", fake.removed)
	}
}

func TestServer_Presets_UsesConfiguredStore(t *testing.T) {
	server := setupTestServer(t)
	server.SetPresets(&fakePresetManager{
		list: []preset.Preset{{Name: "estudo", Label: "Estudo", Domains: []string{"a.com"}}},
	})

	resp := executeRequest(t, server, Request{Action: "presets"})
	if !resp.Success || len(resp.Presets) != 1 || resp.Presets[0].Name != "estudo" {
		t.Errorf("presets deveria vir do store configurado, got %+v", resp.Presets)
	}
}

func (f *fakePomodoroRunner) Status() pomodoro.State { return f.state }

func (f *fakePomodoroRunner) WatchCompletion() <-chan pomodoro.CompletionSummary {
	ch := make(chan pomodoro.CompletionSummary)
	return ch
}

// fakePomodoroPrefs is a stubbable PomodoroPrefs for the server tests.
type fakePomodoroPrefs struct {
	work, rest, cycles int
	remembered         [3]int
}

func (f *fakePomodoroPrefs) Resolve(work, rest, cycles int) (int, int, int) {
	if work <= 0 {
		work = f.work
	}
	if work <= 0 {
		work = 25
	}
	// rest: -1 = não informado (usa salvo/default); 0 = explicitamente sem
	// descanso (mantém 0); >0 = explícito. O default salvo segue o real
	// Prefs: rest não configurado (0 no fake) cai para o clássico 5.
	switch rest {
	case -1:
		if f.rest == 0 {
			rest = 5
		} else {
			rest = f.rest
		}
	case 0:
		// sem descanso explícito — mantém 0
	default:
		// explícito — mantém
	}
	if cycles <= 0 {
		cycles = f.cycles
	}
	if cycles <= 0 {
		cycles = 4
	}
	return work, rest, cycles
}

func (f *fakePomodoroPrefs) Remember(work, rest, cycles int) {
	f.remembered = [3]int{work, rest, cycles}
}

// ---------------------------------------------------------------------------
// Agendamento recorrente (schedule-list / schedule-add / schedule-remove)
// ---------------------------------------------------------------------------

// fakeScheduleManager is a stubbable ScheduleManager used to test the server's
// schedule wiring without touching disk.
type fakeScheduleManager struct {
	list           []schedule.Rule
	added          []schedule.Rule
	removed        []string
	importedICS    []string
	importedPreset string
	imported       []schedule.Rule
	addErr         error
	remErr         error
	importErr      error
}

func (f *fakeScheduleManager) List() []schedule.Rule { return f.list }
func (f *fakeScheduleManager) Add(r schedule.Rule) (schedule.Rule, error) {
	f.added = append(f.added, r)
	if r.ID == "" {
		r.ID = "abc123"
	}
	return r, f.addErr
}
func (f *fakeScheduleManager) Remove(id string) error {
	f.removed = append(f.removed, id)
	return f.remErr
}

func (f *fakeScheduleManager) ImportICS(data []byte, preset string) ([]schedule.Rule, error) {
	f.importedICS = append(f.importedICS, string(data))
	f.importedPreset = preset
	if f.imported == nil && f.importErr == nil {
		return []schedule.Rule{{ID: "abc1", Preset: preset, Label: "Aula", Days: []int{1, 3}, Windows: []string{"08:00-12:00"}, Enabled: true}}, nil
	}
	return f.imported, f.importErr
}

// ---------------------------------------------------------------------------
// Block-all (modo pânico / allowlist deep-focus)
// ---------------------------------------------------------------------------

func TestServer_BlockAll_PanicMode(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "block-all", Duration: "30m"})
	if !resp.Success {
		t.Fatalf("block-all falhou: %s", resp.Message)
	}
	if !strings.Contains(resp.Message, "toda a internet") {
		t.Errorf("mensagem deveria indicar modo pânico, got %q", resp.Message)
	}

	// O sentinela deve aparecer no status
	status := executeRequest(t, server, Request{Action: "status"})
	if !status.Success {
		t.Fatalf("status falhou: %s", status.Message)
	}
	found := false
	for _, b := range status.Blocks {
		if b.Domain == enforcer.AllInternetDomain {
			found = true
		}
	}
	if !found {
		t.Errorf("sentinela do block-all deveria aparecer no status: %+v", status.Blocks)
	}
}

func TestServer_BlockAll_DeepFocusMode(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "block-all", Duration: "1h", Allowlist: []string{"docs.google.com"}})
	if !resp.Success {
		t.Fatalf("block-all falhou: %s", resp.Message)
	}
	if !strings.Contains(resp.Message, "docs.google.com") {
		t.Errorf("mensagem deveria mencionar a allowlist, got %q", resp.Message)
	}
}

func TestServer_BlockAll_InvalidDuration(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "block-all", Duration: "0s"})
	if resp.Success {
		t.Fatal("block-all com duração inválida deve falhar")
	}
	if !strings.HasPrefix(resp.Message, "Duration invalid") {
		t.Errorf("mensagem inesperada: %q", resp.Message)
	}
}

// ---------------------------------------------------------------------------
// Meta diária (goal-get / goal-set)
// ---------------------------------------------------------------------------

// fakeGoalStore is a stubbable daily-goal provider.
type fakeGoalStore struct {
	goal    time.Duration
	setGoal time.Duration
}

func (f *fakeGoalStore) Get() time.Duration { return f.goal }
func (f *fakeGoalStore) Set(d time.Duration) error {
	f.setGoal = d
	f.goal = d
	return nil
}

func TestServer_GoalGet_ReturnsGoal(t *testing.T) {
	server := setupTestServer(t)
	server.SetGoal(&fakeGoalStore{goal: 4 * time.Hour})

	resp := executeRequest(t, server, Request{Action: "goal-get"})
	if !resp.Success {
		t.Fatalf("goal-get falhou: %s", resp.Message)
	}
	if resp.Goal != 4*time.Hour {
		t.Errorf("Goal = %v, want 4h", resp.Goal)
	}
}

func TestServer_GoalGet_WithoutStore(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "goal-get"})
	if resp.Success {
		t.Fatal("goal-get sem store deveria falhar")
	}
}

func TestServer_GoalSet_Success(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeGoalStore{}
	server.SetGoal(fake)

	resp := executeRequest(t, server, Request{Action: "goal-set", GoalMinutes: 240})
	if !resp.Success {
		t.Fatalf("goal-set falhou: %s", resp.Message)
	}
	if fake.setGoal != 4*time.Hour {
		t.Errorf("Set chamado com %v, want 4h", fake.setGoal)
	}
}

func TestServer_GoalSet_InvalidMinutes(t *testing.T) {
	server := setupTestServer(t)
	server.SetGoal(&fakeGoalStore{})

	resp := executeRequest(t, server, Request{Action: "goal-set", GoalMinutes: -30})
	if resp.Success {
		t.Fatal("goal-set com minutos negativos deve falhar")
	}
}

func TestServer_Stats_IncludesStreak(t *testing.T) {
	server := setupTestServer(t)
	now := time.Now()
	server.SetAnalytics(&fakeAnalyticsProvider{
		sessions: []analytics.Session{
			{
				Start:  now.Add(-time.Hour),
				End:    now,
				Preset: "social",
				Focus:  time.Hour,
			},
			{
				Start:  now.AddDate(0, 0, -1),
				End:    now.AddDate(0, 0, -1).Add(time.Hour),
				Preset: "social",
				Focus:  time.Hour,
			},
		},
	})

	resp := executeRequest(t, server, Request{Action: "stats"})
	if !resp.Success {
		t.Fatalf("stats falhou: %s", resp.Message)
	}
	if resp.Stats == nil {
		t.Fatal("Stats nulo")
	}
	if resp.Stats.Streak < 1 {
		t.Errorf("Streak = %d, want >= 1 (sessões hoje e ontem)", resp.Stats.Streak)
	}
}

// ---------------------------------------------------------------------------
// Import iCal (schedule-import)
// ---------------------------------------------------------------------------

const testICSContent = `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:1
SUMMARY:Aula
DTSTART:20260202T080000
DTEND:20260202T120000
RRULE:FREQ=WEEKLY;BYDAY=MO,WE
END:VEVENT
END:VCALENDAR
`

func TestServer_ScheduleImport_AddsRules(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeScheduleManager{}
	server.SetSchedules(fake)

	resp := executeRequest(t, server, Request{Action: "schedule-import", ICSContent: testICSContent, ICSPreset: "social"})
	if !resp.Success {
		t.Fatalf("schedule-import falhou: %s", resp.Message)
	}
	if fake.importedPreset != "social" {
		t.Errorf("importedPreset = %q, want social", fake.importedPreset)
	}
	if !strings.Contains(fake.importedICS[0], "FREQ=WEEKLY") {
		t.Errorf("ICS content should reach the manager raw")
	}
}

func TestServer_ScheduleImport_MissingPreset(t *testing.T) {
	server := setupTestServer(t)
	server.SetSchedules(&fakeScheduleManager{})

	resp := executeRequest(t, server, Request{Action: "schedule-import", ICSContent: testICSContent})
	if resp.Success {
		t.Error("expected failure when the preset is missing")
	}
}

func TestServer_ScheduleImport_EmptyContent(t *testing.T) {
	server := setupTestServer(t)
	server.SetSchedules(&fakeScheduleManager{})

	resp := executeRequest(t, server, Request{Action: "schedule-import", ICSPreset: "social"})
	if resp.Success {
		t.Error("expected failure for empty ICS content")
	}
}

func TestServer_ScheduleImport_UnknownPreset(t *testing.T) {
	server := setupTestServer(t)
	server.SetSchedules(&fakeScheduleManager{})

	resp := executeRequest(t, server, Request{Action: "schedule-import", ICSContent: testICSContent, ICSPreset: "nao-existe"})
	if resp.Success {
		t.Error("expected failure for an unknown preset (would silently never apply)")
	}
}

func TestServer_ScheduleImport_NoWeeklyEvents(t *testing.T) {
	server := setupTestServer(t)
	// importErr força o "nenhum evento semanal" na resposta do manager.
	server.SetSchedules(&fakeScheduleManager{importErr: errors.New("Nenhum evento semanal encontrado no calendário.")})

	resp := executeRequest(t, server, Request{Action: "schedule-import", ICSContent: testICSContent, ICSPreset: "social"})
	if resp.Success {
		t.Error("expected failure when no weekly events are found")
	}
}

func TestServer_ScheduleList_ReturnsRules(t *testing.T) {
	server := setupTestServer(t)
	server.SetSchedules(&fakeScheduleManager{
		list: []schedule.Rule{{ID: "abc1", Preset: "social", Days: []int{1, 2}, Start: "08:00", End: "12:00", Enabled: true}},
	})

	resp := executeRequest(t, server, Request{Action: "schedule-list"})
	if !resp.Success {
		t.Fatalf("schedule-list falhou: %s", resp.Message)
	}
	if len(resp.Schedules) != 1 || resp.Schedules[0].Preset != "social" {
		t.Errorf("Schedules inesperado: %+v", resp.Schedules)
	}
}

func TestServer_ScheduleAdd_Success(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeScheduleManager{}
	server.SetSchedules(fake)

	resp := executeRequest(t, server, Request{
		Action:       "schedule-add",
		ScheduleRule: schedule.Rule{Preset: "video", Days: []int{6}, Start: "20:00", End: "23:00", Enabled: true},
	})
	if !resp.Success {
		t.Fatalf("schedule-add falhou: %s", resp.Message)
	}
	if len(fake.added) != 1 {
		t.Fatalf("added = %d, want 1", len(fake.added))
	}
	if fake.added[0].Preset != "video" || fake.added[0].Start != "20:00" {
		t.Errorf("regra adicionada inesperada: %+v", fake.added[0])
	}
}

func TestServer_ScheduleAdd_ValidationError(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeScheduleManager{addErr: errors.New("schedule: informe um preset")}
	server.SetSchedules(fake)

	resp := executeRequest(t, server, Request{Action: "schedule-add"})
	if resp.Success {
		t.Fatal("expected failure when the manager rejects the rule")
	}
	if !strings.Contains(resp.Message, "informe um preset") {
		t.Errorf("message deveria carregar o erro do manager, got %q", resp.Message)
	}
}

func TestServer_ScheduleAdd_WithoutManager(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "schedule-add"})
	if resp.Success {
		t.Fatal("expected failure without a schedule manager")
	}
}

func TestServer_ScheduleRemove_Success(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeScheduleManager{}
	server.SetSchedules(fake)

	resp := executeRequest(t, server, Request{Action: "schedule-remove", ScheduleID: "abc1"})
	if !resp.Success {
		t.Fatalf("schedule-remove falhou: %s", resp.Message)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "abc1" {
		t.Errorf("removed = %v, want [abc1]", fake.removed)
	}
}

func TestServer_ScheduleRemove_Error(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakeScheduleManager{remErr: errors.New("schedule: regra não encontrada")}
	server.SetSchedules(fake)

	resp := executeRequest(t, server, Request{Action: "schedule-remove", ScheduleID: "zzz"})
	if resp.Success {
		t.Fatal("expected failure when the manager rejects the removal")
	}
}

// fakeAppsManager is a stubbable process-app denylist provider.
type fakeAppsManager struct {
	list    []string
	addErr  error
	remErr  error
	removed []string
}

func (f *fakeAppsManager) List() []string { return f.list }
func (f *fakeAppsManager) Add(name string) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.list = append(f.list, name)
	return nil
}
func (f *fakeAppsManager) Remove(name string) error {
	if f.remErr != nil {
		return f.remErr
	}
	f.removed = append(f.removed, name)
	return nil
}

func TestServer_AppsList_ReturnsDenylist(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{apps: &fakeAppsManager{list: []string{"steam", "discord"}}})

	resp := executeRequest(t, server, Request{Action: "apps-list"})
	if !resp.Success {
		t.Fatalf("apps-list falhou: %s", resp.Message)
	}
	if len(resp.Apps) != 2 || resp.Apps[0] != "steam" {
		t.Errorf("Apps inesperado: %+v", resp.Apps)
	}
}

func TestServer_AppsList_WithoutManager(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "apps-list"})
	if resp.Success {
		t.Fatal("apps-list sem manager deveria falhar")
	}
}

func TestServer_AppsAdd_Success(t *testing.T) {
	fake := &fakeAppsManager{}
	server := setupTestServerWithDeps(t, &refDeps{apps: fake})

	resp := executeRequest(t, server, Request{Action: "apps-add", AppName: "spotify"})
	if !resp.Success {
		t.Fatalf("apps-add falhou: %s", resp.Message)
	}
	if len(fake.list) != 1 || fake.list[0] != "spotify" {
		t.Errorf("Add deveria registrar spotify, got %v", fake.list)
	}
}

func TestServer_AppsAdd_ManagerError(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{apps: &fakeAppsManager{addErr: errors.New("apps: nome inválido")}})

	resp := executeRequest(t, server, Request{Action: "apps-add", AppName: ""})
	if resp.Success {
		t.Fatal("expected failure when the store rejects the app")
	}
	if !strings.Contains(resp.Message, "inválido") {
		t.Errorf("message deveria carregar o erro do store, got %q", resp.Message)
	}
}

func TestServer_AppsRemove_Success(t *testing.T) {
	fake := &fakeAppsManager{}
	server := setupTestServerWithDeps(t, &refDeps{apps: fake})

	resp := executeRequest(t, server, Request{Action: "apps-remove", AppName: "steam"})
	if !resp.Success {
		t.Fatalf("apps-remove falhou: %s", resp.Message)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "steam" {
		t.Errorf("removed = %v, want [steam]", fake.removed)
	}
}

func TestServer_AppsRemove_ManagerError(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{apps: &fakeAppsManager{remErr: errors.New("apps: não encontrado")}})

	resp := executeRequest(t, server, Request{Action: "apps-remove", AppName: "zzz"})
	if resp.Success {
		t.Fatal("expected failure when the store rejects the removal")
	}
}

// fakeTamperProvider is a stubbable tamper-log provider.
type fakeTamperProvider struct {
	events []tamper.Event
}

func (f *fakeTamperProvider) Events() ([]tamper.Event, error) { return f.events, nil }

func TestServer_TamperLog_ReturnsEvents(t *testing.T) {
	server := setupTestServer(t)
	server.SetTamper(&fakeTamperProvider{events: []tamper.Event{
		{At: time.Now(), Source: "hosts", Action: "restore", Detail: "twitter.com"},
	}})

	resp := executeRequest(t, server, Request{Action: "tamper-log"})
	if !resp.Success {
		t.Fatalf("tamper-log falhou: %s", resp.Message)
	}
	if len(resp.TamperLog) != 1 || resp.TamperLog[0].Source != "hosts" {
		t.Errorf("TamperLog inesperado: %+v", resp.TamperLog)
	}
}

func TestServer_TamperLog_WithoutProvider(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "tamper-log"})
	if resp.Success {
		t.Fatal("tamper-log sem provider deveria falhar")
	}
}

func TestServer_Presets_ReturnsCatalog(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "presets"})
	if !resp.Success {
		t.Fatalf("presets falhou: %s", resp.Message)
	}
	if len(resp.Presets) == 0 {
		t.Fatal("expected a non-empty preset catalog")
	}
	found := false
	for _, p := range resp.Presets {
		if p.Name == "social" && len(p.Domains) > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the social preset in the catalog, got %+v", resp.Presets)
	}
}

// ---------------------------------------------------------------------------
// Pomodoro prefs (pomodoro-defaults / --save)
// ---------------------------------------------------------------------------

func TestServer_PomodoroDefaults_NoPrefs(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "pomodoro-defaults"})
	if resp.Success {
		t.Error("expected failure when no prefs store is configured")
	}
}

func TestServer_PomodoroDefaults_ResolvesSaved(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoroPrefs(&fakePomodoroPrefs{work: 50, rest: 10, cycles: 2})

	resp := executeRequest(t, server, Request{Action: "pomodoro-defaults"})
	if !resp.Success {
		t.Fatalf("pomodoro-defaults falhou: %s", resp.Message)
	}
	if resp.PomodoroWork != 50 || resp.PomodoroRest != 10 || resp.PomodoroCycle != 2 {
		t.Errorf("defaults = %d/%d/%d, want 50/10/2", resp.PomodoroWork, resp.PomodoroRest, resp.PomodoroCycle)
	}
}

func TestServer_Pomodoro_SavePersistsDefaults(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{}
	server.SetPomodoro(fake)
	prefs := &fakePomodoroPrefs{}
	server.SetPomodoroPrefs(prefs)

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 50, RestMin: 10, Cycles: 2, Save: true})
	if !resp.Success {
		t.Fatalf("pomodoro falhou: %s", resp.Message)
	}
	if prefs.remembered != [3]int{50, 10, 2} {
		t.Errorf("remembered = %v, want [50 10 2]", prefs.remembered)
	}
	if !strings.Contains(resp.Message, "padrões salvos") {
		t.Errorf("message should mention saved defaults, got %q", resp.Message)
	}
}

func TestServer_Pomodoro_NoSaveSkipsPersist(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{})
	prefs := &fakePomodoroPrefs{}
	server.SetPomodoroPrefs(prefs)

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 50, RestMin: 10, Cycles: 2})
	if !resp.Success {
		t.Fatalf("pomodoro falhou: %s", resp.Message)
	}
	if prefs.remembered != [3]int{0, 0, 0} {
		t.Errorf("remembered should stay zero without Save, got %v", prefs.remembered)
	}
}

func TestServer_Pomodoro_ResolvesDefaultsWhenZero(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{}
	server.SetPomodoro(fake)
	prefs := &fakePomodoroPrefs{work: 50, rest: 10, cycles: 2}
	server.SetPomodoroPrefs(prefs)

	// CLI sem flags explícitas: work=0, rest=-1, cycles=0 → defaults salvos.
	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 0, RestMin: -1, Cycles: 0})
	if !resp.Success {
		t.Fatalf("pomodoro falhou: %s", resp.Message)
	}
	if fake.started.Work != 50*time.Minute || fake.started.Rest != 10*time.Minute || fake.started.Cycles != 2 {
		t.Errorf("started = %v/%v/%d, want 50m/10m/2", fake.started.Work, fake.started.Rest, fake.started.Cycles)
	}
}

func TestServer_Pomodoro_LabelPassthrough(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{}
	server.SetPomodoro(fake)

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 5, Cycles: 4, Label: "Estudar ENEM"})
	if !resp.Success {
		t.Fatalf("pomodoro falhou: %s", resp.Message)
	}
	if fake.started.Label != "Estudar ENEM" {
		t.Errorf("started.Label = %q, want Estudar ENEM", fake.started.Label)
	}
}

func TestServer_Stats_FilterByMission(t *testing.T) {
	server := setupTestServer(t)
	server.SetAnalytics(&fakeAnalyticsProvider{
		sessions: []analytics.Session{
			{Start: time.Now().Add(-time.Hour), End: time.Now(), Preset: "social", Label: "ENEM", Domains: []string{"twitter.com"}, Focus: time.Hour},
			{Start: time.Now().Add(-2 * time.Hour), End: time.Now().Add(-time.Hour), Preset: "video", Domains: []string{"youtube.com"}, Focus: 2 * time.Hour},
		},
	})

	resp := executeRequest(t, server, Request{Action: "stats", Mission: "ENEM"})
	if !resp.Success {
		t.Fatalf("stats falhou: %s", resp.Message)
	}
	if resp.Stats.TotalSessions != 1 || resp.Stats.TotalFocus != time.Hour {
		t.Errorf("filtered stats = %d sessions / %v, want 1 / 1h", resp.Stats.TotalSessions, resp.Stats.TotalFocus)
	}
}

func TestServer_Missions_AggregatesLabels(t *testing.T) {
	server := setupTestServer(t)
	server.SetAnalytics(&fakeAnalyticsProvider{
		sessions: []analytics.Session{
			{Start: time.Now().Add(-time.Hour), End: time.Now(), Preset: "social", Label: "ENEM", Domains: []string{"twitter.com"}, Focus: time.Hour},
			{Start: time.Now().Add(-2 * time.Hour), End: time.Now().Add(-time.Hour), Preset: "video", Label: "ENEM", Domains: []string{"youtube.com"}, Focus: 2 * time.Hour},
		},
	})

	resp := executeRequest(t, server, Request{Action: "missions"})
	if !resp.Success {
		t.Fatalf("missions falhou: %s", resp.Message)
	}
	if len(resp.LabelStats) != 1 {
		t.Fatalf("LabelStats = %d, want 1", len(resp.LabelStats))
	}
	if resp.LabelStats[0].Label != "ENEM" || resp.LabelStats[0].Duration != 3*time.Hour {
		t.Errorf("LabelStats[0] = %+v, want ENEM/3h", resp.LabelStats[0])
	}
}

func TestServer_Pomodoro_NotConfigured(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 5, Cycles: 4})
	if resp.Success {
		t.Error("expected failure when no pomodoro runner is configured")
	}
	if !strings.Contains(resp.Message, "não configurado") {
		t.Errorf("expected 'não configurado' message, got %q", resp.Message)
	}
}

func TestServer_Pomodoro_StartsSessionWithPresetDomains(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{}
	server.SetPomodoro(fake)

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 5, Cycles: 4})
	if !resp.Success {
		t.Fatalf("pomodoro falhou: %s", resp.Message)
	}

	social, err := preset.Resolve("social")
	if err != nil {
		t.Fatalf("Resolve(social): %v", err)
	}
	if fake.started.Preset != "social" {
		t.Errorf("started.Preset = %q, want social", fake.started.Preset)
	}
	if !reflect.DeepEqual(fake.started.Domains, social.Domains) {
		t.Errorf("started.Domains = %v, want preset domains %v", fake.started.Domains, social.Domains)
	}
	if fake.started.Work != 25*time.Minute {
		t.Errorf("started.Work = %v, want 25m", fake.started.Work)
	}
	if fake.started.Rest != 5*time.Minute {
		t.Errorf("started.Rest = %v, want 5m", fake.started.Rest)
	}
	if fake.started.Cycles != 4 {
		t.Errorf("started.Cycles = %d, want 4", fake.started.Cycles)
	}
	if resp.Pomodoro == nil {
		t.Error("expected pomodoro state in response")
	}
}

func TestServer_Pomodoro_UnknownPreset(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{})

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "nonexistent", WorkMin: 25, RestMin: 5, Cycles: 4})
	if resp.Success {
		t.Error("expected failure for unknown preset")
	}
	if !strings.Contains(strings.ToLower(resp.Message), "preset") {
		t.Errorf("error should mention the preset, got %q", resp.Message)
	}
}

func TestServer_Pomodoro_InvalidParams(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{})

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 0, RestMin: 5, Cycles: 4})
	if resp.Success {
		t.Error("expected failure for WorkMin=0")
	}
}

// TestServer_Pomodoro_RejectsWorkMinOverflow verifies the defensive cap: a
// WorkMin large enough to overflow int64 when converted to time.Duration would
// wrap to a small positive duration (a ~147-year session) instead of failing.
func TestServer_Pomodoro_RejectsWorkMinOverflow(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{})

	// time.Duration(1e9)*time.Minute wraps para um valor positivo (~147 anos).
	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 1_000_000_000, RestMin: 5, Cycles: 4})
	if resp.Success {
		t.Error("expected failure for WorkMin that would overflow int64")
	}
}

// TestServer_Pomodoro_RejectsRestMinOverflow mirrors the work cap for RestMin.
func TestServer_Pomodoro_RejectsRestMinOverflow(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{})

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 1_000_000_000, Cycles: 4})
	if resp.Success {
		t.Error("expected failure for RestMin that would overflow int64")
	}
}

// TestServer_Pomodoro_RejectsCyclesOverflow caps Cycles too: a huge Cycles
// would overflow the focus accumulator (focus += s.Work) in the controller,
// recording a negative duration in the analytics.
func TestServer_Pomodoro_RejectsCyclesOverflow(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{})

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 5, Cycles: 1_000_000_000})
	if resp.Success {
		t.Error("expected failure for Cycles that would overflow the focus accumulator")
	}
}

// TestServer_Pomodoro_AcceptsMaxCycles verifies the boundary of the cycles cap
// is inclusive (1000 cycles is allowed).
func TestServer_Pomodoro_AcceptsMaxCycles(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{}
	server.SetPomodoro(fake)

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 5, Cycles: 1000})
	if !resp.Success {
		t.Fatalf("expected 1000 cycles to be accepted, got: %s", resp.Message)
	}
	if fake.started.Cycles != 1000 {
		t.Errorf("started.Cycles = %d, want 1000", fake.started.Cycles)
	}
}

func TestServer_PomodoroStop(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{}
	server.SetPomodoro(fake)

	resp := executeRequest(t, server, Request{Action: "pomodoro-stop"})
	if !resp.Success {
		t.Fatalf("pomodoro-stop falhou: %s", resp.Message)
	}
	if !fake.stopped {
		t.Error("expected the runner Stop to be called")
	}
}

func TestServer_Status_IncludesPomodoro(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{state: pomodoro.State{Active: true, Preset: "social", Phase: pomodoro.PhaseWork, Cycle: 1, Cycles: 4}}
	server.SetPomodoro(fake)

	resp := executeRequest(t, server, Request{Action: "status"})
	if !resp.Success {
		t.Fatalf("status falhou: %s", resp.Message)
	}
	if resp.Pomodoro == nil || !resp.Pomodoro.Active {
		t.Errorf("expected active pomodoro in status, got %+v", resp.Pomodoro)
	}
	if resp.Pomodoro.Preset != "social" || resp.Pomodoro.Phase != pomodoro.PhaseWork {
		t.Errorf("unexpected pomodoro state: %+v", resp.Pomodoro)
	}
}

func TestServer_Block_WithUnknownPreset(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "block", Preset: "nonexistent", Duration: "1h"})
	if resp.Success {
		t.Error("expected failure for unknown preset")
	}
}

func TestServer_Block_RequiresDomainOrPreset(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "block", Duration: "1h"})
	if resp.Success {
		t.Error("expected failure when neither domain nor preset is given")
	}
}

// ---------------------------------------------------------------------------
// Strict Mode & Analytics
// ---------------------------------------------------------------------------

// fakeAnalyticsProvider returns canned sessions to the stats action.
type fakeAnalyticsProvider struct {
	sessions []analytics.Session
}

func (f *fakeAnalyticsProvider) Sessions() ([]analytics.Session, error) {
	return f.sessions, nil
}

func TestServer_Stats_NotConfigured(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "stats"})
	if resp.Success {
		t.Error("expected failure when no analytics provider is configured")
	}
	if !strings.Contains(resp.Message, "não configurado") {
		t.Errorf("expected 'não configurado' message, got %q", resp.Message)
	}
}

func TestServer_Stats_ReturnsSummary(t *testing.T) {
	server := setupTestServer(t)
	now := time.Now()
	server.SetAnalytics(&fakeAnalyticsProvider{
		sessions: []analytics.Session{
			{
				Start:   now.Add(-time.Hour),
				End:     now,
				Preset:  "social",
				Domains: []string{"twitter.com"},
				WorkMin: 25,
				RestMin: 5,
				Cycles:  4,
				Focus:   time.Hour,
				Strict:  false,
			},
		},
	})

	resp := executeRequest(t, server, Request{Action: "stats"})
	if !resp.Success {
		t.Fatalf("stats falhou: %s", resp.Message)
	}
	if resp.Stats == nil {
		t.Fatal("expected Stats in response")
	}
	if resp.Stats.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", resp.Stats.TotalSessions)
	}
	if resp.Stats.TotalFocus != time.Hour {
		t.Errorf("TotalFocus = %v, want 1h", resp.Stats.TotalFocus)
	}
}

func TestServer_Sessions_NotConfigured(t *testing.T) {
	server := setupTestServer(t)

	resp := executeRequest(t, server, Request{Action: "sessions"})
	if resp.Success {
		t.Error("expected failure when no analytics provider is configured")
	}
	if !strings.Contains(resp.Message, "não configurado") {
		t.Errorf("expected 'não configurado' message, got %q", resp.Message)
	}
}

// TestServer_Sessions_ReturnsRecentNewestFirst verifies the sessions action
// surfaces the recent sessions sorted newest first (the cap and ordering live
// in analytics.RecentSessions).
func TestServer_Sessions_ReturnsRecentNewestFirst(t *testing.T) {
	server := setupTestServer(t)
	now := time.Now()
	server.SetAnalytics(&fakeAnalyticsProvider{
		sessions: []analytics.Session{
			{End: now.Add(-2 * time.Hour), Preset: "social", Domains: []string{"twitter.com"}, Focus: time.Hour},
			{End: now.Add(-time.Hour), Preset: "video", Domains: []string{"youtube.com"}, Focus: 30 * time.Minute},
			{End: now, Preset: "news", Domains: []string{"g1.globo.com"}, Focus: 45 * time.Minute},
		},
	})

	resp := executeRequest(t, server, Request{Action: "sessions"})
	if !resp.Success {
		t.Fatalf("sessions falhou: %s", resp.Message)
	}
	if len(resp.Sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(resp.Sessions))
	}
	if resp.Sessions[0].Preset != "news" || resp.Sessions[2].Preset != "social" {
		t.Errorf("order = %q,..,%q, want news,..,social", resp.Sessions[0].Preset, resp.Sessions[2].Preset)
	}
}

func TestServer_Pomodoro_StrictPassthrough(t *testing.T) {
	server := setupTestServer(t)
	fake := &fakePomodoroRunner{}
	server.SetPomodoro(fake)

	resp := executeRequest(t, server, Request{Action: "pomodoro", Preset: "social", WorkMin: 25, RestMin: 5, Cycles: 4, Strict: true})
	if !resp.Success {
		t.Fatalf("pomodoro falhou: %s", resp.Message)
	}
	if !fake.started.Strict {
		t.Error("Strict flag should be passed through to the session")
	}
}

func TestServer_HasActiveSession_NoRunner(t *testing.T) {
	server := setupTestServer(t)
	if server.HasActiveSession() {
		t.Error("expected false when no pomodoro runner is configured")
	}
}

func TestServer_HasActiveSession_Active(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{state: pomodoro.State{Active: true}})
	if !server.HasActiveSession() {
		t.Error("expected true when the pomodoro session is active")
	}
}

func TestServer_HasActiveSession_Inactive(t *testing.T) {
	server := setupTestServer(t)
	server.SetPomodoro(&fakePomodoroRunner{})
	if server.HasActiveSession() {
		t.Error("expected false when the pomodoro session is inactive")
	}
}

func TestServer_HandleConnection_Status(t *testing.T) {
	server := setupTestServer(t)

	statusReq := Request{Action: "status"}
	resp := executeRequest(t, server, statusReq)

	if !resp.Success {
		t.Fatalf("expected status request to succeed, got error: %s", resp.Message)
	}
	if len(resp.Blocks) != 0 {
		t.Errorf("expected 0 active blocks initially, found %d", len(resp.Blocks))
	}

	blockReq := Request{
		Action:   "block",
		Domain:   "socialmedia.com",
		Duration: "30m",
	}
	blockResp := executeRequest(t, server, blockReq)
	if !blockResp.Success {
		t.Fatalf("failed to set up active block: %s", blockResp.Message)
	}

	respAfterBlock := executeRequest(t, server, statusReq)

	if !respAfterBlock.Success {
		t.Fatalf("expected status request to succeed, got error: %s", respAfterBlock.Message)
	}
	if len(respAfterBlock.Blocks) != 1 {
		t.Fatalf("expected 1 active block in status response, found %d", len(respAfterBlock.Blocks))
	}
	if respAfterBlock.Blocks[0].Domain != "socialmedia.com" {
		t.Errorf("expected domain %q in active block, got %q", "socialmedia.com", respAfterBlock.Blocks[0].Domain)
	}
}
