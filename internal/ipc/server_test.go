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

	"focusguard/internal/analytics"
	"focusguard/internal/enforcer"
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
	"focusguard/internal/scheduler"
	"focusguard/internal/store"
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
func (m *mockEnforcer) Status() (enforcer.EnforcerStatus, error) {
	return enforcer.EnforcerStatus{}, nil
}

func setupTestServer(t *testing.T) *Server {
	t.Helper()

	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "state.json"))
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	sched := scheduler.NewScheduler(st, &mockEnforcer{})
	return NewServer(sched)
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
func (f *fakePomodoroRunner) Status() pomodoro.State { return f.state }

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
