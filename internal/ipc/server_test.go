package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/scheduler"
	"focusguard/internal/store"
)

type fakeUpdateChecker struct {
	status UpdateStatus
	err    error
	apply  bool
}

func (f *fakeUpdateChecker) Check(_ context.Context, apply bool) (UpdateStatus, error) {
	f.apply = apply
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
