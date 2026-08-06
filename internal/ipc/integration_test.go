package ipc

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/scheduler"
	"focusguard/internal/store"
)

type integrationMockEnforcer struct {
	mu             sync.Mutex
	blockedCount   int
	unblockedCount int
}

func (m *integrationMockEnforcer) BlockDomain(_ string, _ []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockedCount++
	return nil
}

func (m *integrationMockEnforcer) UnblockDomain(_ string, _ []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unblockedCount++
	return nil
}

func (m *integrationMockEnforcer) Sync(_ map[string][]string) error { return nil }
func (m *integrationMockEnforcer) BlockDoH() error                  { return nil }
func (m *integrationMockEnforcer) UnblockDoH() error                { return nil }
func (m *integrationMockEnforcer) BlockAll(_ []string) error        { return nil }
func (m *integrationMockEnforcer) UnblockAll() error                { return nil }
func (m *integrationMockEnforcer) Status() (enforcer.EnforcerStatus, error) {
	return enforcer.EnforcerStatus{}, nil
}

// startIntegrationServer points the package Listen/Dial endpoints at a
// test-only address — a unix socket in a temp dir on Linux, an ephemeral TCP
// port on Windows — so the real Server+Scheduler chain runs without root.
func startIntegrationServer(t *testing.T) {
	t.Helper()

	setTestEndpoint(t)

	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	sched := scheduler.NewScheduler(st, &integrationMockEnforcer{})
	if err := sched.Start(); err != nil {
		t.Fatalf("scheduler.Start: %v", err)
	}

	srv := NewServer(sched)
	// Ações de domínio: o daemon real as registra com os handlers dos pacotes
	// de domínio (Fase 5); aqui os testes de socket usam os adapters de
	// referência (handlers_ref_test.go), sem deps (apps/users/hook nil).
	registerDomainReferenceHandlers(srv, nil)
	go func() {
		if err := srv.Start(); err != nil {
			t.Logf("integration server stopped: %v", err)
		}
	}()

	t.Cleanup(func() { srv.Stop() })

	time.Sleep(50 * time.Millisecond)
}

func TestIntegration_BlockAndStatus(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "1h",
	})
	if err != nil {
		t.Fatalf("block failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if !strings.Contains(resp.Message, "Domain localhost blocked") {
		t.Errorf("unexpected message: %s", resp.Message)
	}

	resp, err = client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if len(resp.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(resp.Blocks))
	}
	if resp.Blocks[0].Domain != "localhost" {
		t.Errorf("expected localhost, got %s", resp.Blocks[0].Domain)
	}
	if len(resp.Blocks[0].ResolvedIPs) == 0 {
		t.Error("expected resolved IPs for localhost")
	}
}

func TestIntegration_EmptyStatus(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Message)
	}
	if len(resp.Blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(resp.Blocks))
	}
}

// TestIntegration_DuplicateBlock verifies the ask-first conflict flow: a block
// on an already-active domain returns Conflict=true (not a silent upsert), and
// the caller resolves it with extend (soma à duração atual) or replace
// (reinicia a janela a partir de agora).
func TestIntegration_DuplicateBlock(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "1h",
	})
	if err != nil {
		t.Fatalf("first block: %v", err)
	}
	if !resp.Success {
		t.Fatalf("first block should succeed: %s", resp.Message)
	}

	// Bloqueio sem flag sobre domínio já ativo → CONFLITO, não sobrescrita.
	resp, err = client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "2h",
	})
	if err != nil {
		t.Fatalf("second block: %v", err)
	}
	if resp.Success || !resp.Conflict {
		t.Fatalf("block on active domain should be a conflict, got success=%v conflict=%v msg=%s", resp.Success, resp.Conflict, resp.Message)
	}
	if resp.ConflictBlock == nil || resp.ConflictBlock.Domain != "localhost" {
		t.Fatalf("ConflictBlock should carry the existing block, got %+v", resp.ConflictBlock)
	}

	// --extend: soma ao vencimento atual (1h + 30m ≈ 1h30m).
	resp, err = client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "30m",
		Extend:   true,
	})
	if err != nil {
		t.Fatalf("extend block: %v", err)
	}
	if !resp.Success {
		t.Fatalf("extend should succeed: %s", resp.Message)
	}

	resp, err = client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(resp.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(resp.Blocks))
	}
	remaining := time.Until(resp.Blocks[0].ExpiresAt)
	if remaining < 85*time.Minute || remaining > 95*time.Minute {
		t.Errorf("extend should sum 30m to the 1h expiry, got remaining: %v", remaining)
	}

	// --replace: reinicia a janela a partir de agora (≈45m), descartando o
	// bloqueio anterior.
	resp, err = client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "45m",
		Replace:  true,
	})
	if err != nil {
		t.Fatalf("replace block: %v", err)
	}
	if !resp.Success {
		t.Fatalf("replace should succeed: %s", resp.Message)
	}

	resp, err = client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(resp.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(resp.Blocks))
	}
	remaining = time.Until(resp.Blocks[0].ExpiresAt)
	if remaining < 40*time.Minute || remaining > 50*time.Minute {
		t.Errorf("replace should restart the window (~45m), got remaining: %v", remaining)
	}
}

func TestIntegration_MultipleBlocks(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	for _, domain := range []string{"localhost", "127.0.0.1", "0.0.0.0"} {
		resp, err := client.Send(Request{
			Action:   "block",
			Domain:   domain,
			Duration: "30m",
		})
		if err != nil {
			t.Fatalf("block %s: %v", domain, err)
		}
		if !resp.Success {
			t.Fatalf("block %s failed: %s", domain, resp.Message)
		}
	}

	resp, err := client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !resp.Success {
		t.Fatalf("status failed: %s", resp.Message)
	}
	if len(resp.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(resp.Blocks))
	}

	domains := make(map[string]bool)
	for _, b := range resp.Blocks {
		domains[b.Domain] = true
	}
	for _, d := range []string{"localhost", "127.0.0.1", "0.0.0.0"} {
		if !domains[d] {
			t.Errorf("missing domain: %s", d)
		}
	}
}

func TestIntegration_InvalidDuration(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "not-a-duration",
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure for invalid duration")
	}
	if !strings.HasPrefix(resp.Message, "Duration invalid") {
		t.Errorf("unexpected error: %s", resp.Message)
	}

	resp, err = client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "-30m",
	})
	if err != nil {
		t.Fatalf("negative duration request: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure for negative duration")
	}
	if !strings.HasPrefix(resp.Message, "Duration invalid") {
		t.Errorf("unexpected error for negative: %s", resp.Message)
	}
}

func TestIntegration_UnsupportedAction(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{
		Action: "unsupported",
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure for unsupported action")
	}
	if resp.Message != "Not suported action: unsupported" {
		t.Errorf("unexpected message: %s", resp.Message)
	}
}

func TestIntegration_InvalidJSON(t *testing.T) {
	startIntegrationServer(t)

	conn, err := Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.Write([]byte("{invalid-json\n"))

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure for invalid JSON")
	}
	if resp.Message != "Request invalid" {
		t.Errorf("unexpected message: %s", resp.Message)
	}
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	blockIDs := make(chan int, 10)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp, err := client.Send(Request{
				Action:   "block",
				Domain:   "127.0.0.1",
				Duration: "1h",
			})
			if err != nil {
				errs <- err
				return
			}
			if !resp.Success {
				errs <- nil
				return
			}
			blockIDs <- id
		}(i)
	}

	wg.Wait()
	close(errs)
	close(blockIDs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent block error: %v", err)
		}
	}

	resp, err := client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !resp.Success {
		t.Fatalf("status failed: %s", resp.Message)
	}
	if len(resp.Blocks) != 1 {
		t.Errorf("expected exactly 1 block after concurrent requests, got %d", len(resp.Blocks))
	}
}

func TestIntegration_BlockExpires(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{
		Action:   "block",
		Domain:   "127.0.0.1",
		Duration: "2s",
	})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if !resp.Success {
		t.Fatalf("block: %s", resp.Message)
	}

	time.Sleep(3 * time.Second)

	resp, err = client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !resp.Success {
		t.Fatalf("status: %s", resp.Message)
	}
	if len(resp.Blocks) != 0 {
		t.Errorf("expected 0 blocks after expiration, got %d", len(resp.Blocks))
	}
}

func TestIntegration_BlockResponseIncludesTimestamp(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{
		Action:   "block",
		Domain:   "127.0.0.1",
		Duration: "2h",
	})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if !resp.Success {
		t.Fatalf("block: %s", resp.Message)
	}
	if !strings.Contains(resp.Message, "blocked") {
		t.Errorf("expected 'blocked' in message: %s", resp.Message)
	}
}

func TestIntegration_StatusResponseStructure(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	client.Send(Request{
		Action:   "block",
		Domain:   "127.0.0.1",
		Duration: "3h",
	})

	client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "1h",
	})

	resp, err := client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	for _, block := range resp.Blocks {
		if block.Domain == "" {
			t.Error("block has empty domain")
		}
		if block.StartedAt.IsZero() {
			t.Error("block has zero StartedAt")
		}
		if block.ExpiresAt.IsZero() {
			t.Error("block has zero ExpiresAt")
		}
		if block.ExpiresAt.Before(block.StartedAt) {
			t.Error("block ExpiresAt before StartedAt")
		}
		if len(block.ResolvedIPs) == 0 {
			t.Errorf("block %s has no resolved IPs", block.Domain)
		}
	}
}

func TestIntegration_DomainSanitization(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	for _, tc := range []struct {
		name      string
		raw       string
		inMessage string
	}{
		{name: "http prefix", raw: "http://localhost", inMessage: "localhost"},
		{name: "https prefix", raw: "https://127.0.0.1", inMessage: "127.0.0.1"},
		{name: "trailing slash", raw: "127.0.0.1/", inMessage: "127.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Send(Request{
				Action:   "block",
				Domain:   tc.raw,
				Duration: "10m",
			})
			if err != nil {
				t.Fatalf("block: %v", err)
			}
			if !resp.Success {
				t.Fatalf("block failed: %s", resp.Message)
			}
			if !strings.Contains(resp.Message, tc.inMessage) {
				t.Errorf("expected message containing %q, got: %s", tc.inMessage, resp.Message)
			}

			resp, err = client.Send(Request{Action: "status"})
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			found := false
			for _, b := range resp.Blocks {
				if strings.Contains(b.Domain, strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(tc.raw, "http://"), "https://"), "/")) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("block with raw input %q not found in status response", tc.raw)
				for _, b := range resp.Blocks {
					t.Logf("  block: %s", b.Domain)
				}
			}
		})
	}
}

func TestIntegration_ResponseEncoding(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	client.Send(Request{
		Action:   "block",
		Domain:   "localhost",
		Duration: "1h",
	})

	resp, err := client.Send(Request{Action: "status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if decoded.Success != resp.Success {
		t.Error("success field lost in marshal/unmarshal")
	}
	if len(decoded.Blocks) != len(resp.Blocks) {
		t.Error("blocks field lost in marshal/unmarshal")
	}
	if len(decoded.Blocks) > 0 {
		if decoded.Blocks[0].Domain != resp.Blocks[0].Domain {
			t.Error("block domain field lost in marshal/unmarshal")
		}
	}
}

func TestIntegration_NonExistentDomain(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	resp, err := client.Send(Request{
		Action:   "block",
		Domain:   "this-domain-does-not-exist-12345.com",
		Duration: "1h",
	})
	if err != nil {
		t.Fatalf("block request: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure for non-existent domain")
	}
	if !strings.Contains(resp.Message, "failed to resolve") {
		t.Errorf("expected resolution error message, got: %s", resp.Message)
	}
}

func TestIntegration_ClientReuseMultipleRequests(t *testing.T) {
	startIntegrationServer(t)
	client := NewClient()

	for i := 0; i < 5; i++ {
		resp, err := client.Send(Request{Action: "status"})
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if !resp.Success {
			t.Fatalf("request %d: %s", i, resp.Message)
		}
	}
}
