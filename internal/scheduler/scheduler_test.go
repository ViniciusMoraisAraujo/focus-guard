package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/policy"
	"focusguard/internal/store"
)

type mockEnforcer struct {
	mu               sync.Mutex
	blockedDomains   map[string][]string
	unblockedDomains map[string][]string
	syncedBlocks     map[string][]string
	syncCalls        int
	allBlockCalls    [][]string
	unblockAllCalls  int
}

func newMockEnforcer() *mockEnforcer {
	return &mockEnforcer{
		blockedDomains:   make(map[string][]string),
		unblockedDomains: make(map[string][]string),
		syncedBlocks:     make(map[string][]string),
	}
}

func (m *mockEnforcer) BlockDomain(domain string, ips []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockedDomains[domain] = ips
	return nil
}

func (m *mockEnforcer) UnblockDomain(domain string, ips []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unblockedDomains[domain] = ips
	return nil
}

func (m *mockEnforcer) Sync(activeBlocks map[string][]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncCalls++
	m.syncedBlocks = make(map[string][]string)
	for k, v := range activeBlocks {
		m.syncedBlocks[k] = v
	}
	return nil
}

func (m *mockEnforcer) BlockDoH() error   { return nil }
func (m *mockEnforcer) UnblockDoH() error { return nil }
func (m *mockEnforcer) BlockAll(allowlist []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allBlockCalls = append(m.allBlockCalls, append([]string(nil), allowlist...))
	return nil
}
func (m *mockEnforcer) UnblockAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unblockAllCalls++
	return nil
}

func (m *mockEnforcer) Status() (enforcer.EnforcerStatus, error) {
	return enforcer.EnforcerStatus{}, nil
}

func setupTestScheduler(t *testing.T) (*Scheduler, *mockEnforcer, *store.Store) {
	t.Helper()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("error creating store: %v", err)
	}

	enf := newMockEnforcer()
	sched := NewScheduler(st, enf)

	return sched, enf, st
}

func waitForCondition(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return check()
}

func TestScheduler_BlockAndList(t *testing.T) {
	sched, enf, _ := setupTestScheduler(t)

	domain := "localhost"
	duration := 1 * time.Hour

	block, err := sched.Block(domain, duration)
	if err != nil {
		t.Fatalf("Failed to block domain: %v", err)
	}

	if block.Domain != domain {
		t.Errorf("Expected domain %s, got %s", domain, block.Domain)
	}

	enf.mu.Lock()
	if _, exists := enf.blockedDomains[domain]; !exists {
		t.Errorf("Expected enforcer to have received block for %s", domain)
	}
	enf.mu.Unlock()

	blocks, err := sched.ListBlocks()
	if err != nil {
		t.Fatalf("Failed to list blocks: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("Expected 1 active block, found %d", len(blocks))
	}

	if blocks[0].Domain != domain {
		t.Errorf("Expected domain %s in listing, got %s", domain, blocks[0].Domain)
	}
}

// TestScheduler_ActiveBlock_ConflictDetection verifies the query that backs the
// ask-first conflict flow (IPC/CLI/Web): it only reports ACTIVE blocks.
func TestScheduler_ActiveBlock_ConflictDetection(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	if b := sched.ActiveBlock("x.com"); b != nil {
		t.Fatalf("ActiveBlock before any block = %v, want nil", b)
	}

	block, err := sched.Block("x.com", time.Hour)
	if err != nil {
		t.Fatalf("Block: %v", err)
	}

	got := sched.ActiveBlock("x.com")
	if got == nil {
		t.Fatal("ActiveBlock after Block = nil, want the active block")
	}
	if got.Domain != "x.com" || !got.ExpiresAt.Equal(block.ExpiresAt) {
		t.Errorf("ActiveBlock = %+v, want %+v", got, block)
	}

	// Bloqueio já expirado NÃO é conflito: o caminho user-driven pode
	// simplesmente re-bloquear.
	sched.mu.Lock()
	sched.blocks["expired.com"] = policy.Block{
		Domain:    "expired.com",
		StartedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	sched.mu.Unlock()
	if b := sched.ActiveBlock("expired.com"); b != nil {
		t.Errorf("ActiveBlock for expired block = %v, want nil", b)
	}
}

// TestScheduler_ExtendBlock_SumsToActiveExpiry verifies the "somar" semantics:
// ExtendBlock adds duration to the CURRENT expiry (never restarts/shortens),
// preserves the block's IPs and does not re-apply the enforcer (already
// blocked — the hot path skips DNS and netsh entirely).
func TestScheduler_ExtendBlock_SumsToActiveExpiry(t *testing.T) {
	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) { return []string{"1.2.3.4"}, nil }
	t.Cleanup(func() { resolveFunc = origResolve })

	sched, enf, _ := setupTestScheduler(t)

	block, err := sched.Block("x.com", time.Hour)
	if err != nil {
		t.Fatalf("Block: %v", err)
	}
	expBefore := block.ExpiresAt

	ext, err := sched.ExtendBlock("x.com", 30*time.Minute)
	if err != nil {
		t.Fatalf("ExtendBlock: %v", err)
	}
	if !ext.ExpiresAt.Equal(expBefore.Add(30 * time.Minute)) {
		t.Errorf("ExpiresAt = %v, want %v (soma sobre o vencimento atual)", ext.ExpiresAt, expBefore.Add(30*time.Minute))
	}

	// IPs preservados (sem re-resolver).
	if !slices.Equal(ext.ResolvedIPs, []string{"1.2.3.4"}) {
		t.Errorf("ResolvedIPs = %v, want [1.2.3.4] preservados", ext.ResolvedIPs)
	}

	// O enforcer não é re-aplicado: o domínio já está bloqueado.
	enf.mu.Lock()
	if len(enf.blockedDomains) != 1 {
		t.Errorf("BlockDomain chamado %d vezes, want 1 (apenas o Block inicial)", len(enf.blockedDomains))
	}
	enf.mu.Unlock()

	// O timer foi re-armado com o novo vencimento.
	sched.mu.RLock()
	timer, has := sched.timers["x.com"]
	sched.mu.RUnlock()
	if !has || timer == nil {
		t.Fatal("timer do domínio não foi re-armado após o extend")
	}
}

// TestScheduler_ExtendBlock_NoActiveBlockFallsBackToBlock verifies ExtendBlock
// is idempotent: extending a domain that is not blocked just blocks it.
func TestScheduler_ExtendBlock_NoActiveBlockFallsBackToBlock(t *testing.T) {
	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) { return []string{"1.2.3.4"}, nil }
	t.Cleanup(func() { resolveFunc = origResolve })

	sched, enf, _ := setupTestScheduler(t)

	ext, err := sched.ExtendBlock("novo.com", 45*time.Minute)
	if err != nil {
		t.Fatalf("ExtendBlock on a non-blocked domain: %v", err)
	}
	if ext.Domain != "novo.com" {
		t.Errorf("Domain = %s, want novo.com", ext.Domain)
	}
	want := time.Until(ext.ExpiresAt)
	if want > 46*time.Minute || want < 44*time.Minute {
		t.Errorf("ExpiresAt = %v, want ~45m from now", ext.ExpiresAt)
	}
	enf.mu.Lock()
	_, blocked := enf.blockedDomains["novo.com"]
	enf.mu.Unlock()
	if !blocked {
		t.Error("fallback Block deve ter aplicado o bloqueio no enforcer")
	}
}

func TestScheduler_StartReconciliation(t *testing.T) {
	t.Run("Boot reconciliation: cleans expired blocks and reapplies active ones", func(t *testing.T) {
		sched, enf, st := setupTestScheduler(t)

		now := time.Now().UTC().Round(time.Second)

		initialState := &store.State{
			Blocks: map[string]policy.Block{
				"expired.com": {
					Domain:      "expired.com",
					StartedAt:   now.Add(-48 * time.Hour),
					ExpiresAt:   now.Add(-24 * time.Hour),
					ResolvedIPs: []string{"1.1.1.1"},
				},
				"active.com": {
					Domain:      "active.com",
					StartedAt:   now,
					ExpiresAt:   now.Add(24 * time.Hour),
					ResolvedIPs: []string{"2.2.2.2"},
				},
			},
		}

		if err := st.Save(initialState); err != nil {
			t.Fatalf("Failed to prepare test state: %v", err)
		}

		if err := sched.Start(); err != nil {
			t.Fatalf("Failed to run Start(): %v", err)
		}

		enf.mu.Lock()
		_, unblocked := enf.unblockedDomains["expired.com"]
		_, synced := enf.syncedBlocks["active.com"]
		enf.mu.Unlock()

		if !unblocked {
			t.Errorf("Expected expired.com to be unblocked during boot")
		}

		if !synced {
			t.Errorf("Expected active.com to be present in boot synchronization")
		}

		loadedState, err := st.Load()
		if err != nil {
			t.Fatalf("Failed to load state post-Start: %v", err)
		}

		if len(loadedState.Blocks) != 1 {
			t.Fatalf("Expected only 1 block in post-boot state, found %d", len(loadedState.Blocks))
		}

		if _, exists := loadedState.Blocks["active.com"]; !exists {
			t.Errorf("Expected active.com in state.json post-boot")
		}
	})
}

func TestScheduler_TimerExpiration(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	domain := "localhost"
	shortDuration := -1 * time.Second

	_, err := sched.Block(domain, shortDuration)
	if err != nil {
		t.Fatalf("Failed to create short block: %v", err)
	}

	unblocked := waitForCondition(2*time.Second, func() bool {
		enf.mu.Lock()
		defer enf.mu.Unlock()
		_, ok := enf.unblockedDomains[domain]
		return ok
	})

	if !unblocked {
		t.Errorf("Expected %s to be automatically unblocked after timer expiration", domain)
	}

	var stateRemoved bool
	_ = waitForCondition(2*time.Second, func() bool {
		state, err := st.Load()
		if err != nil {
			return false
		}
		_, exists := state.Blocks[domain]
		stateRemoved = !exists
		return stateRemoved
	})

	if !stateRemoved {
		t.Errorf("Expected %s to be removed from state.json after expiration", domain)
	}
}

type syncFailingEnforcer struct {
	*mockEnforcer
}

func (e *syncFailingEnforcer) Sync(_ map[string][]string) error {
	return errors.New("sync failed: permission denied")
}

func (e *syncFailingEnforcer) BlockAll(_ []string) error {
	return errors.New("block-all failed: permission denied")
}

func TestScheduler_Start_SyncError(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	now := time.Now()
	initialState := &store.State{
		Blocks: map[string]policy.Block{
			"active.com": {
				Domain:      "active.com",
				StartedAt:   now,
				ExpiresAt:   now.Add(24 * time.Hour),
				ResolvedIPs: []string{"1.2.3.4"},
			},
		},
	}
	if err := st.Save(initialState); err != nil {
		t.Fatalf("save state: %v", err)
	}

	enf := &syncFailingEnforcer{mockEnforcer: newMockEnforcer()}
	sched := NewScheduler(st, enf)

	err = sched.Start()
	if err == nil {
		t.Fatal("expected Start() to return an error when Sync() fails")
	}
	if err.Error() != "sync failed: permission denied" {
		t.Errorf("expected 'sync failed: permission denied', got: %v", err)
	}
}

func TestScheduler_Reconcile_ActiveBlocks(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)

	initialState := &store.State{
		Blocks: map[string]policy.Block{
			"active.com": {
				Domain:      "active.com",
				StartedAt:   now,
				ExpiresAt:   now.Add(24 * time.Hour),
				ResolvedIPs: []string{"1.2.3.4"},
			},
		},
	}

	if err := st.Save(initialState); err != nil {
		t.Fatalf("Failed to prepare test state: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() failed: %v", err)
	}

	enf.mu.Lock()
	synced, exists := enf.syncedBlocks["active.com"]
	enf.mu.Unlock()

	if !exists {
		t.Errorf("Expected active.com to be synced after Reconcile")
	} else if len(synced) == 0 {
		t.Errorf("Expected active.com to have IPs after Reconcile")
	}
}

func TestScheduler_Reconcile_CleansExpired(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)

	initialState := &store.State{
		Blocks: map[string]policy.Block{
			"expired.com": {
				Domain:      "expired.com",
				StartedAt:   now.Add(-48 * time.Hour),
				ExpiresAt:   now.Add(-24 * time.Hour),
				ResolvedIPs: []string{"1.1.1.1"},
			},
		},
	}

	if err := st.Save(initialState); err != nil {
		t.Fatalf("Failed to prepare test state: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() failed: %v", err)
	}

	enf.mu.Lock()
	_, unblocked := enf.unblockedDomains["expired.com"]
	enf.mu.Unlock()

	if !unblocked {
		t.Errorf("Expected expired.com to be unblocked after Reconcile")
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if _, exists := state.Blocks["expired.com"]; exists {
		t.Errorf("Expected expired.com to be removed from state after Reconcile")
	}
}

func TestScheduler_ProtectionStatus(t *testing.T) {
	sched, _, st := setupTestScheduler(t)

	ps, err := sched.ProtectionStatus()
	if err != nil {
		t.Fatalf("ProtectionStatus() erro: %v", err)
	}
	if ps.ExpectedDoH {
		t.Error("ExpectedDoH deve ser false sem bloqueios ativos")
	}

	now := time.Now().UTC().Round(time.Second)
	state, _ := st.Load()
	state.Blocks["active.com"] = policy.Block{
		Domain:      "active.com",
		StartedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
		ResolvedIPs: []string{"1.2.3.4"},
	}
	if err := st.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// A RAM é a fonte da verdade: o estado persistido no disco precisa ser
	// carregado por uma reconciliação (boot) para refletir na RAM.
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}

	ps, err = sched.ProtectionStatus()
	if err != nil {
		t.Fatalf("ProtectionStatus() erro: %v", err)
	}
	if !ps.ExpectedDoH {
		t.Error("ExpectedDoH deve ser true com bloqueio ativo")
	}
}

type statusEnforcer struct {
	*mockEnforcer
	st  enforcer.EnforcerStatus
	err error
}

func (e *statusEnforcer) Status() (enforcer.EnforcerStatus, error) {
	return e.st, e.err
}

func TestScheduler_ProtectionStatus_PropagatesEnforcerStatus(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.json")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	enf := &statusEnforcer{
		mockEnforcer: newMockEnforcer(),
		st:           enforcer.EnforcerStatus{DoHActive: true, FirewallRules: 7},
	}
	sched := NewScheduler(st, enf)

	ps, err := sched.ProtectionStatus()
	if err != nil {
		t.Fatalf("ProtectionStatus() erro: %v", err)
	}
	if !ps.DoHActive {
		t.Error("DoHActive deve refletir o status do enforcer")
	}
	if ps.FirewallRules != 7 {
		t.Errorf("FirewallRules = %d, want 7", ps.FirewallRules)
	}

	enf.err = errors.New("permission denied")
	if _, err := sched.ProtectionStatus(); err == nil {
		t.Error("expected error when enforcer.Status() fails")
	}
}

func TestScheduler_Reconcile_EmptyState(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() on empty state should not error: %v", err)
	}
}

// TestScheduler_Start_CorruptedStateFileDoesNotAbort verifies that a corrupted
// state.json at boot (invalid JSON or 0 bytes) does not prevent the daemon from
// starting: the scheduler must boot with a clean RAM state and overwrite the
// corrupted disk copy with the (clean) in-memory state.
func TestScheduler_Start_CorruptedStateFileDoesNotAbort(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")

	// Corrupted JSON — exactly what Load() must tolerate instead of erroring.
	if err := os.WriteFile(dbPath, []byte(`{not valid json`), 0644); err != nil {
		t.Fatalf("write corrupted state: %v", err)
	}

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	enf := newMockEnforcer()
	sched := NewScheduler(st, enf)

	if err := sched.Start(); err != nil {
		t.Fatalf("Start() should not abort on corrupted state, got: %v", err)
	}

	if sched.HasActiveBlocks() {
		t.Error("expected no active blocks after boot with corrupted state")
	}

	// The disk mirror must have been healed: readable, valid, empty state.
	loaded, err := st.Load()
	if err != nil {
		t.Fatalf("Load() after Start() should succeed (file healed): %v", err)
	}
	if len(loaded.Blocks) != 0 {
		t.Errorf("expected healed state with no blocks, got %d", len(loaded.Blocks))
	}
}

// TestScheduler_Start_ZeroByteStateFileDoesNotAbort is the same guarantee for
// a 0-byte state.json (crash mid-write).
func TestScheduler_Start_ZeroByteStateFileDoesNotAbort(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")

	if err := os.WriteFile(dbPath, nil, 0644); err != nil {
		t.Fatalf("truncate state: %v", err)
	}

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	enf := newMockEnforcer()
	sched := NewScheduler(st, enf)

	if err := sched.Start(); err != nil {
		t.Fatalf("Start() should not abort on empty state file, got: %v", err)
	}

	loaded, err := st.Load()
	if err != nil {
		t.Fatalf("Load() after Start() should succeed (file healed): %v", err)
	}
	if len(loaded.Blocks) != 0 {
		t.Errorf("expected healed state with no blocks, got %d", len(loaded.Blocks))
	}
}

// TestScheduler_Reconcile_CorruptedFileWithEmptyRAMHeals covers the post-boot
// live-heal gap: with the disk corrupted (or emptied to 0 bytes) and NO active
// blocks in RAM, a Reconcile triggered by statewatch must still rewrite the
// clean RAM state over the corrupted disk. Without healing inside Load(),
// statesEqual(cleanState, cleanState) is true and the file would stay
// corrupted until the next daemon restart.
func TestScheduler_Reconcile_CorruptedFileWithEmptyRAMHeals(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	enf := newMockEnforcer()
	sched := NewScheduler(st, enf)

	if err := sched.Start(); err != nil {
		t.Fatalf("Start(): %v", err)
	}

	// Boot with no blocks; now corrupt the disk mirror (as an external editor
	// or a crash mid-write would).
	if err := os.WriteFile(dbPath, []byte(`{not valid json`), 0644); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() on corrupted disk with empty RAM: %v", err)
	}

	// The raw disk copy must have been healed to valid JSON even though RAM is
	// empty — otherwise the corrupted file lingers until the next restart.
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read state file after heal: %v", err)
	}
	var healed store.State
	if err := json.Unmarshal(data, &healed); err != nil {
		t.Fatalf("state file should be valid JSON after Reconcile, got: %v (%q)", err, string(data))
	}
	if len(healed.Blocks) != 0 {
		t.Errorf("expected healed file with no blocks, got %d", len(healed.Blocks))
	}
}

func TestScheduler_Reconcile_MixedBlocks(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)

	initialState := &store.State{
		Blocks: map[string]policy.Block{
			"expired.com": {
				Domain:      "expired.com",
				StartedAt:   now.Add(-48 * time.Hour),
				ExpiresAt:   now.Add(-24 * time.Hour),
				ResolvedIPs: []string{"1.1.1.1"},
			},
			"active.com": {
				Domain:      "active.com",
				StartedAt:   now,
				ExpiresAt:   now.Add(24 * time.Hour),
				ResolvedIPs: []string{"2.2.2.2"},
			},
		},
	}

	if err := st.Save(initialState); err != nil {
		t.Fatalf("Failed to prepare test state: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() failed: %v", err)
	}

	enf.mu.Lock()
	_, unblocked := enf.unblockedDomains["expired.com"]
	_, synced := enf.syncedBlocks["active.com"]
	enf.mu.Unlock()

	if !unblocked {
		t.Errorf("Expected expired.com to be unblocked")
	}
	if !synced {
		t.Errorf("Expected active.com to be synced")
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if len(state.Blocks) != 1 {
		t.Errorf("Expected exactly 1 block in state after Reconcile, got %d", len(state.Blocks))
	}
	if _, exists := state.Blocks["expired.com"]; exists {
		t.Errorf("Expected expired.com to be removed from state")
	}
}

func TestScheduler_PeriodicIPRefresh_SkipsWithoutBlocks(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	var resolveCalls int32
	origResolve := resolveFuncCtx
	resolveFuncCtx = func(_ context.Context, _ string) ([]string, error) {
		atomic.AddInt32(&resolveCalls, 1)
		return nil, nil
	}
	defer func() { resolveFuncCtx = origResolve }()

	go sched.startPeriodicIPRefresh(20 * time.Millisecond)
	defer close(sched.refreshStop)
	time.Sleep(120 * time.Millisecond)

	if got := atomic.LoadInt32(&resolveCalls); got != 0 {
		t.Errorf("expected 0 DNS resolutions with no active blocks, got %d", got)
	}
}

// TestMergeResolvedIPs covers the pure IP merge: existing + new resolved IPs,
// de-duplicated, reporting whether anything new was added.
func TestMergeResolvedIPs(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		newIPs   []string
		want     []string
		wantNew  bool
	}{
		{"new ip appended", []string{"1.1.1.1"}, []string{"2.2.2.2"}, []string{"1.1.1.1", "2.2.2.2"}, true},
		{"no new ips", []string{"1.1.1.1"}, []string{"1.1.1.1"}, []string{"1.1.1.1"}, false},
		{"dedupe new", []string{"1.1.1.1"}, []string{"2.2.2.2", "2.2.2.2"}, []string{"1.1.1.1", "2.2.2.2"}, true},
		{"empty new", []string{"1.1.1.1"}, nil, []string{"1.1.1.1"}, false},
		{"empty existing", nil, []string{"2.2.2.2"}, []string{"2.2.2.2"}, true},
		{"overlap only", []string{"1.1.1.1", "2.2.2.2"}, []string{"1.1.1.1", "2.2.2.2"}, []string{"1.1.1.1", "2.2.2.2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hasNew := mergeResolvedIPs(tt.existing, tt.newIPs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeResolvedIPs() = %v, want %v", got, tt.want)
			}
			if hasNew != tt.wantNew {
				t.Errorf("mergeResolvedIPs() hasNew = %v, want %v", hasNew, tt.wantNew)
			}
		})
	}
}

// TestRefreshResolvedIPs_RunsInParallel proves the refresh resolves multiple
// domains concurrently (not strictly sequentially): with several entries that
// each sleep inside the resolver, the peak concurrency must exceed 1.
func TestRefreshResolvedIPs_RunsInParallel(t *testing.T) {
	var current, peak int32
	resolve := func(_ context.Context, _ string) ([]string, error) {
		cur := atomic.AddInt32(&current, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		return []string{"10.0.0.1"}, nil
	}

	entries := []refreshEntry{
		{domain: "a.com", ips: nil},
		{domain: "b.com", ips: nil},
		{domain: "c.com", ips: nil},
		{domain: "d.com", ips: nil},
	}

	refreshed := refreshResolvedIPs(entries, 2*time.Second, resolve)

	if got := atomic.LoadInt32(&peak); got < 2 {
		t.Errorf("expected parallel resolution (peak concurrency >= 2), got %d", got)
	}
	if len(refreshed) != len(entries) {
		t.Errorf("expected %d domains refreshed, got %d: %v", len(entries), len(refreshed), refreshed)
	}
}

// TestRefreshResolvedIPs_AppliesPerDomainTimeout verifies a resolver that
// hangs (never answers on its own) is cut off by the per-domain timeout: the
// whole refresh returns quickly and the timed-out domains are not refreshed.
func TestRefreshResolvedIPs_AppliesPerDomainTimeout(t *testing.T) {
	var timeouts int32
	resolve := func(ctx context.Context, _ string) ([]string, error) {
		<-ctx.Done() // hangs until the per-domain timeout fires
		atomic.AddInt32(&timeouts, 1)
		return nil, ctx.Err()
	}

	entries := []refreshEntry{
		{domain: "slow1.com", ips: []string{"1.1.1.1"}},
		{domain: "slow2.com", ips: []string{"2.2.2.2"}},
		{domain: "slow3.com", ips: []string{"3.3.3.3"}},
	}

	start := time.Now()
	refreshed := refreshResolvedIPs(entries, 50*time.Millisecond, resolve)
	elapsed := time.Since(start)

	// All three hang in parallel, so the total must stay near the timeout,
	// not 3x it.
	if elapsed > time.Second {
		t.Errorf("refresh should be bounded by the per-domain timeout, took %v", elapsed)
	}
	if got := atomic.LoadInt32(&timeouts); got != 3 {
		t.Errorf("expected all 3 lookups to hit the timeout, got %d", got)
	}
	if len(refreshed) != 0 {
		t.Errorf("expected no refreshed domains after timeouts, got %v", refreshed)
	}
}

// TestRefreshResolvedIPs_SkipsFailuresAndMerges verifies that domains whose
// resolution errors (or returns nothing) are skipped, while successful ones
// have their IPs merged into the refreshed map.
func TestRefreshResolvedIPs_SkipsFailuresAndMerges(t *testing.T) {
	resolve := func(_ context.Context, domain string) ([]string, error) {
		switch domain {
		case "err.com":
			return nil, errors.New("dns failure")
		case "empty.com":
			return nil, nil
		default:
			return []string{"10.0.0.9", "10.0.0.1"}, nil
		}
	}

	entries := []refreshEntry{
		{domain: "err.com", ips: []string{"1.1.1.1"}},
		{domain: "empty.com", ips: []string{"2.2.2.2"}},
		{domain: "ok.com", ips: []string{"10.0.0.1"}},
	}

	refreshed := refreshResolvedIPs(entries, 2*time.Second, resolve)

	if _, ok := refreshed["err.com"]; ok {
		t.Error("err.com should not be refreshed")
	}
	if _, ok := refreshed["empty.com"]; ok {
		t.Error("empty.com should not be refreshed")
	}
	want := []string{"10.0.0.1", "10.0.0.9"}
	if !reflect.DeepEqual(refreshed["ok.com"], want) {
		t.Errorf("ok.com = %v, want %v", refreshed["ok.com"], want)
	}
}

// TestRefreshResolvedIPs_EmptyEntries verifies an empty batch performs no
// lookups and returns an empty map.
func TestRefreshResolvedIPs_EmptyEntries(t *testing.T) {
	called := false
	refreshed := refreshResolvedIPs(nil, time.Second, func(_ context.Context, _ string) ([]string, error) {
		called = true
		return []string{"1.1.1.1"}, nil
	})
	if called {
		t.Error("resolver must not be called with no entries")
	}
	if len(refreshed) != 0 {
		t.Errorf("expected empty result, got %v", refreshed)
	}
}

func TestScheduler_PeriodicIPRefresh(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)
	domain := "localhost"

	initialState := &store.State{
		Blocks: map[string]policy.Block{
			domain: {
				Domain:      domain,
				StartedAt:   now,
				ExpiresAt:   now.Add(1 * time.Hour),
				ResolvedIPs: []string{"192.0.2.1"},
			},
		},
	}

	if err := st.Save(initialState); err != nil {
		t.Fatalf("Failed to prepare test state: %v", err)
	}
	// Reconcile carrega o estado do disco para a RAM, como no boot real do daemon.
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	go sched.startPeriodicIPRefresh(20 * time.Millisecond)
	defer close(sched.refreshStop)

	updated := waitForCondition(2*time.Second, func() bool {
		state, err := st.Load()
		if err != nil {
			return false
		}
		b, ok := state.Blocks[domain]
		return ok && len(b.ResolvedIPs) > 1
	})

	if !updated {
		t.Errorf("Expected periodic refresh worker to aggregate newly resolved IPs for %s", domain)
	}

	enf.mu.Lock()
	ips, exists := enf.blockedDomains[domain]
	enf.mu.Unlock()

	if !exists || len(ips) <= 1 {
		t.Errorf("Expected enforcer to receive refreshed IPs for %s", domain)
	}
}

type failingBlockEnforcer struct {
	*mockEnforcer
}

func (e *failingBlockEnforcer) BlockDomain(_ string, _ []string) error {
	return errors.New("block failed: permission denied")
}

func TestScheduler_Block_EnforcerErrorRollsBackRAM(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	enf := &failingBlockEnforcer{mockEnforcer: newMockEnforcer()}
	sched := NewScheduler(st, enf)

	if _, err := sched.Block("localhost", time.Hour); err == nil {
		t.Fatal("expected Block() to fail when enforcer.BlockDomain fails")
	}

	blocks, err := sched.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("expected RAM blocks to be rolled back after failed block, got %d: %v", len(blocks), blocks)
	}

	if sched.HasActiveBlocks() {
		t.Error("expected HasActiveBlocks to be false after failed block")
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Errorf("expected state file to be rolled back after failed block, got %d blocks", len(state.Blocks))
	}
}

func TestScheduler_Reconcile_TamperExpiryToPast(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)
	active := policy.Block{
		Domain:      "active.com",
		StartedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
		ResolvedIPs: []string{"1.2.3.4"},
	}
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{"active.com": active}}); err != nil {
		t.Fatalf("prepare state: %v", err)
	}
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}
	if !sched.HasActiveBlocks() {
		t.Fatal("expected active block after bootstrap")
	}

	// Adulteração: mover a expiração para o passado, como um admin faria no editor.
	tampered := active
	tampered.ExpiresAt = now.Add(-1 * time.Hour)
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{"active.com": tampered}}); err != nil {
		t.Fatalf("tamper state: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() após adulteração: %v", err)
	}

	// O disco deve ter sido restaurado a partir da RAM (data original).
	loaded, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	restored, ok := loaded.Blocks["active.com"]
	if !ok {
		t.Fatal("expected active.com to remain in state")
	}
	if !restored.ExpiresAt.Equal(active.ExpiresAt) {
		t.Errorf("expected disk expiry restored to %v, got %v", active.ExpiresAt, restored.ExpiresAt)
	}

	// O domínio não deve ter sido desbloqueado pelo arquivo adulterado.
	enf.mu.Lock()
	_, unblocked := enf.unblockedDomains["active.com"]
	enf.mu.Unlock()
	if unblocked {
		t.Error("expected active.com NOT to be unblocked by tampered state")
	}
	if !sched.HasActiveBlocks() {
		t.Error("expected block to remain active in RAM")
	}
}

func TestScheduler_Reconcile_TamperRemovedBlock(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)
	active := policy.Block{
		Domain:      "active.com",
		StartedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
		ResolvedIPs: []string{"1.2.3.4"},
	}
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{"active.com": active}}); err != nil {
		t.Fatalf("prepare state: %v", err)
	}
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}

	// Adulteração: remover o domínio do disco.
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{}}); err != nil {
		t.Fatalf("tamper state: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() após adulteração: %v", err)
	}

	loaded, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := loaded.Blocks["active.com"]; !ok {
		t.Error("expected removed block to be restored to disk")
	}

	enf.mu.Lock()
	_, unblocked := enf.unblockedDomains["active.com"]
	enf.mu.Unlock()
	if unblocked {
		t.Error("expected active.com NOT to be unblocked by tampered state")
	}
}

func TestScheduler_Reconcile_TamperDeletedFile(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	enf := newMockEnforcer()
	sched := NewScheduler(st, enf)

	now := time.Now().UTC().Round(time.Second)
	active := policy.Block{
		Domain:      "active.com",
		StartedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
		ResolvedIPs: []string{"1.2.3.4"},
	}
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{"active.com": active}}); err != nil {
		t.Fatalf("prepare state: %v", err)
	}
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}

	// Adulteração: excluir o arquivo de estado inteiro.
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove state file: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile() após exclusão: %v", err)
	}

	loaded, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := loaded.Blocks["active.com"]; !ok {
		t.Error("expected state file to be recreated from RAM after deletion")
	}

	enf.mu.Lock()
	_, unblocked := enf.unblockedDomains["active.com"]
	enf.mu.Unlock()
	if unblocked {
		t.Error("expected active.com NOT to be unblocked after file deletion")
	}
}

// loadCountingStore wraps *store.Store and counts Load calls so tests can
// assert that query methods are served entirely from RAM and that the disk is
// read exactly once during bootstrap.
type loadCountingStore struct {
	*store.Store
	loads int32
}

func (s *loadCountingStore) Load() (*store.State, error) {
	atomic.AddInt32(&s.loads, 1)
	return s.Store.Load()
}

// TestScheduler_BootReadsDiskOnce verifies the daemon reads state.json exactly
// once at startup (bootstrap), not once to load blocks and again to compare
// the mirror — the just-loaded disk copy IS the in-memory state.
func TestScheduler_BootReadsDiskOnce(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	counting := &loadCountingStore{Store: st}
	sched := NewScheduler(counting, newMockEnforcer())

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile (boot): %v", err)
	}

	if got := atomic.LoadInt32(&counting.loads); got != 1 {
		t.Errorf("expected exactly 1 disk read at boot, got %d", got)
	}
}

// TestScheduler_QueriesDoNotReadDisk verifies ListBlocks/HasActiveBlocks/Ping
// are served 100% from RAM: the mutex must never block on state.json I/O for
// read-only query paths.
func TestScheduler_QueriesDoNotReadDisk(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	counting := &loadCountingStore{Store: st}
	sched := NewScheduler(counting, newMockEnforcer())

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile (boot): %v", err)
	}
	atomic.StoreInt32(&counting.loads, 0)

	if _, err := sched.ListBlocks(); err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	_ = sched.HasActiveBlocks()
	_ = sched.Ping()
	if _, err := sched.ProtectionStatus(); err != nil {
		t.Fatalf("ProtectionStatus: %v", err)
	}

	if got := atomic.LoadInt32(&counting.loads); got != 0 {
		t.Errorf("query methods must not read the disk, got %d Load calls", got)
	}
}

// ---------------------------------------------------------------------------
// BlockAllInternet (modo pânico / allowlist deep-focus)
// ---------------------------------------------------------------------------

func TestScheduler_BlockAllInternet_AppliesAndExpires(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)
	stubResolveFuncCtx(t, map[string][]string{
		"docs.com":     {"1.1.1.1"},
		"www.docs.com": {"2.2.2.2"},
	})

	blk, err := sched.BlockAllInternet([]string{"docs.com"}, 2*time.Hour)
	if err != nil {
		t.Fatalf("BlockAllInternet: %v", err)
	}
	if blk.Domain != enforcer.AllInternetDomain {
		t.Errorf("Domain = %q, want sentinel %q", blk.Domain, enforcer.AllInternetDomain)
	}

	// enforcer.BlockAll deve ter sido chamado com os IPs resolvidos da allowlist
	if len(enf.allBlockCalls) != 1 {
		t.Fatalf("BlockAll chamado %d vezes, want 1", len(enf.allBlockCalls))
	}
	if !reflect.DeepEqual(enf.allBlockCalls[0], []string{"1.1.1.1", "2.2.2.2"}) {
		t.Errorf("BlockAll allowlist = %v, want [1.1.1.1 2.2.2.2]", enf.allBlockCalls[0])
	}

	// HasActiveBlocks reflete o sentinela
	if !sched.HasActiveBlocks() {
		t.Error("BlockAllInternet deve deixar HasActiveBlocks=true")
	}

	// persistido no state.json
	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := state.Blocks[enforcer.AllInternetDomain]; !ok {
		t.Error("sentinela deveria estar persistido no state.json")
	}

	// bootstrap (RAM = fonte da verdade; o primeiro Reconcile lê o disco)
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile (bootstrap): %v", err)
	}
	if !sched.HasActiveBlocks() {
		t.Fatal("sentinela deveria estar ativo após bootstrap")
	}

	// expiração: encurta o ExpiresAt na RAM (fonte da verdade) e reconcilia
	sched.mu.Lock()
	b := sched.blocks[enforcer.AllInternetDomain]
	b.ExpiresAt = time.Now().Add(-time.Second)
	sched.blocks[enforcer.AllInternetDomain] = b
	sched.mu.Unlock()

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile (expiração): %v", err)
	}

	if enf.unblockAllCalls != 1 {
		t.Errorf("UnblockAll chamado %d vezes após expiração, want 1", enf.unblockAllCalls)
	}
	if sched.HasActiveBlocks() {
		t.Error("após expiração não deve haver bloqueios ativos")
	}
}

func TestScheduler_BlockAllInternet_NoAllowlist(t *testing.T) {
	sched, enf, _ := setupTestScheduler(t)

	if _, err := sched.BlockAllInternet(nil, time.Hour); err != nil {
		t.Fatalf("BlockAllInternet: %v", err)
	}
	if len(enf.allBlockCalls) != 1 || len(enf.allBlockCalls[0]) != 0 {
		t.Errorf("BlockAll sem allowlist deve chamar com lista vazia, got %v", enf.allBlockCalls)
	}
}

func TestScheduler_Reconcile_ReappliesActiveSentinel(t *testing.T) {
	sched, enf, _ := setupTestScheduler(t)
	stubResolveFuncCtx(t, map[string][]string{})

	// seed: sentinela ativo direto no store (como após um restart)
	now := time.Now()
	sched.mu.Lock()
	sched.blocks[enforcer.AllInternetDomain] = policy.Block{
		Domain:      enforcer.AllInternetDomain,
		StartedAt:   now,
		ExpiresAt:   now.Add(time.Hour),
		ResolvedIPs: []string{"1.1.1.1"},
	}
	sched.mu.Unlock()

	sched.Reconcile()

	// Reconcile deve reaplicar o BlockAll no enforcer (bootstrap/re-aplicação)
	if len(enf.allBlockCalls) != 1 {
		t.Errorf("Reconcile deveria reaplicar BlockAll com o sentinela ativo, got %d chamadas", len(enf.allBlockCalls))
	}
	if !reflect.DeepEqual(enf.allBlockCalls[0], []string{"1.1.1.1"}) {
		t.Errorf("BlockAll reaplicado com %v, want [1.1.1.1]", enf.allBlockCalls[0])
	}
}

func TestScheduler_BlockAllInternet_SyncFailureRollsBack(t *testing.T) {
	st, _ := store.NewStore(filepath.Join(t.TempDir(), "state.json"))
	enf := &syncFailingEnforcer{mockEnforcer: newMockEnforcer()}
	sched := NewScheduler(st, enf)

	if _, err := sched.BlockAllInternet(nil, time.Hour); err == nil {
		t.Fatal("esperava erro quando o enforcer.BlockAll falha")
	}
	if sched.HasActiveBlocks() {
		t.Error("após falha não deve haver bloqueio ativo")
	}
}

// stubResolveFuncCtx replaces resolveFuncCtx with a fixed table (including
// www. variants), so BlockDomains tests avoid real DNS.
func stubResolveFuncCtx(t *testing.T, table map[string][]string) {
	t.Helper()
	orig := resolveFuncCtx
	resolveFuncCtx = func(_ context.Context, domain string) ([]string, error) {
		ips, ok := table[domain]
		if !ok {
			return nil, errors.New("no such host: " + domain)
		}
		return ips, nil
	}
	t.Cleanup(func() { resolveFuncCtx = orig })
}

// TestScheduler_BlockDomains_SingleSaveAndSingleSync verifies the batched
// path: N domains are persisted with a single state.json write and applied
// with a single enforcer.Sync (hosts rewritten once), instead of one Save +
// one BlockDomain per domain.
func TestScheduler_BlockDomains_SingleSaveAndSingleSync(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)
	var saves int32
	st.SetOnSave(func() { atomic.AddInt32(&saves, 1) })

	stubResolveFuncCtx(t, map[string][]string{
		"a.com":     {"1.1.1.1", "2.2.2.2"},
		"www.a.com": {"3.3.3.3"},
		"b.com":     {"4.4.4.4"},
		"www.b.com": {"5.5.5.5"},
	})

	blocks, err := sched.BlockDomains([]string{"a.com", "b.com"}, time.Hour)
	if err != nil {
		t.Fatalf("BlockDomains: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	if got := atomic.LoadInt32(&saves); got != 1 {
		t.Errorf("expected exactly 1 state.json save for the batch, got %d", got)
	}

	enf.mu.Lock()
	defer enf.mu.Unlock()
	if enf.syncCalls != 1 {
		t.Errorf("expected exactly 1 enforcer.Sync for the batch, got %d", enf.syncCalls)
	}
	if len(enf.blockedDomains) != 0 {
		t.Errorf("BlockDomains must use Sync, not per-domain BlockDomain; got %v", enf.blockedDomains)
	}
	if len(enf.syncedBlocks) != 2 {
		t.Errorf("expected Sync to receive both domains, got %v", enf.syncedBlocks)
	}

	list, err := sched.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 blocks in RAM, got %d", len(list))
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Blocks) != 2 {
		t.Errorf("expected 2 blocks persisted, got %d", len(state.Blocks))
	}
}

// TestScheduler_BlockDomains_ResolutionFailureRollsBack verifies that a domain
// that fails to resolve aborts the whole batch without touching RAM, disk or
// the enforcer.
func TestScheduler_BlockDomains_ResolutionFailureRollsBack(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)
	stubResolveFuncCtx(t, map[string][]string{
		"ok.com":     {"1.1.1.1"},
		"www.ok.com": {"2.2.2.2"},
	})

	blocks, err := sched.BlockDomains([]string{"ok.com", "bad.com"}, time.Hour)
	if err == nil {
		t.Fatal("expected error when a domain fails to resolve")
	}
	if blocks != nil {
		t.Errorf("expected nil blocks on failure, got %v", blocks)
	}

	list, _ := sched.ListBlocks()
	if len(list) != 0 {
		t.Errorf("expected no RAM blocks after failure, got %v", list)
	}
	state, _ := st.Load()
	if len(state.Blocks) != 0 {
		t.Errorf("expected no disk blocks after failure, got %d", len(state.Blocks))
	}
	enf.mu.Lock()
	defer enf.mu.Unlock()
	if len(enf.blockedDomains) != 0 || len(enf.syncedBlocks) != 0 {
		t.Errorf("enforcer must be untouched after resolution failure: %v %v", enf.blockedDomains, enf.syncedBlocks)
	}
}

// TestScheduler_BlockDomains_SyncErrorRollsBack verifies that when the
// enforcer.Sync fails, the batch is rolled back from RAM and disk (no zombie
// blocks without timers).
func TestScheduler_BlockDomains_SyncErrorRollsBack(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	enf := &syncFailingEnforcer{mockEnforcer: newMockEnforcer()}
	sched := NewScheduler(st, enf)
	stubResolveFuncCtx(t, map[string][]string{
		"a.com":     {"1.1.1.1"},
		"www.a.com": {"2.2.2.2"},
	})

	if _, err := sched.BlockDomains([]string{"a.com"}, time.Hour); err == nil {
		t.Fatal("expected error when enforcer.Sync fails")
	}

	blocks, _ := sched.ListBlocks()
	if len(blocks) != 0 {
		t.Errorf("expected RAM rolled back after Sync failure, got %v", blocks)
	}
	state, _ := st.Load()
	if len(state.Blocks) != 0 {
		t.Errorf("expected disk rolled back after Sync failure, got %d", len(state.Blocks))
	}
}

// TestScheduler_BlockDomains_EmptyInput verifies an empty batch is rejected.
func TestScheduler_BlockDomains_EmptyInput(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	if _, err := sched.BlockDomains(nil, time.Hour); err == nil {
		t.Error("expected error for nil domains")
	}
	if _, err := sched.BlockDomains([]string{}, time.Hour); err == nil {
		t.Error("expected error for empty domains")
	}
}

// TestScheduler_BlockDomains_Deduplicates verifies duplicate domains collapse
// into a single block.
func TestScheduler_BlockDomains_Deduplicates(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	stubResolveFuncCtx(t, map[string][]string{
		"a.com":     {"1.1.1.1"},
		"www.a.com": {"2.2.2.2"},
	})

	blocks, err := sched.BlockDomains([]string{"a.com", "a.com", "a.com"}, time.Hour)
	if err != nil {
		t.Fatalf("BlockDomains: %v", err)
	}
	if len(blocks) != 1 {
		t.Errorf("expected duplicates collapsed to 1 block, got %d", len(blocks))
	}
}

// failingUnblockEnforcer wraps mockEnforcer and makes UnblockDomain fail while
// failUnblocks is true — simulating a transient failure (e.g. the Windows
// Firewall service still starting at boot) that previously left stale rules in
// the OS after state.json had already been cleaned.
type failingUnblockEnforcer struct {
	*mockEnforcer
	failMu       sync.Mutex
	failUnblocks bool
}

func (e *failingUnblockEnforcer) UnblockDomain(domain string, ips []string) error {
	e.failMu.Lock()
	fail := e.failUnblocks
	e.failMu.Unlock()
	if fail {
		return errors.New("netsh: firewall service not running")
	}
	return e.mockEnforcer.UnblockDomain(domain, ips)
}

func (e *failingUnblockEnforcer) setFail(fail bool) {
	e.failMu.Lock()
	e.failUnblocks = fail
	e.failMu.Unlock()
}

// TestScheduler_Reconcile_BootFailingUnblockKeepsBlockInState reproduces the
// reported bug: a boot reconcile persisted the cleaned state.json before the
// enforcer removed the OS rules, so a failed UnblockDomain (firewall service
// not ready at boot) left stale hosts/firewall rules forever with the state
// mirror already "clean" and no retry. Now the block must remain in RAM and in
// state.json until the removal succeeds, and the retry must clean both.
func TestScheduler_Reconcile_BootFailingUnblockKeepsBlockInState(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	now := time.Now().UTC().Round(time.Second)
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{
		"expired.com": {
			Domain:      "expired.com",
			StartedAt:   now.Add(-48 * time.Hour),
			ExpiresAt:   now.Add(-24 * time.Hour),
			ResolvedIPs: []string{"1.1.1.1"},
		},
	}}); err != nil {
		t.Fatalf("prepare state: %v", err)
	}

	enf := &failingUnblockEnforcer{mockEnforcer: newMockEnforcer()}
	enf.setFail(true)
	sched := NewScheduler(st, enf)

	// Boot reconcile with the enforcer failing (firewall still coming up).
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// The block must NOT have been dropped: state.json must still reflect it,
	// otherwise the UI would report "clean" while the OS rules remain.
	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := state.Blocks["expired.com"]; !ok {
		t.Error("expired block removed from state.json despite failed unblock")
	}
	blocks, _ := sched.ListBlocks()
	if len(blocks) != 1 || blocks[0].Domain != "expired.com" {
		t.Errorf("block should remain in RAM, got %+v", blocks)
	}
	enf.mu.Lock()
	_, unblocked := enf.unblockedDomains["expired.com"]
	enf.mu.Unlock()
	if unblocked {
		t.Error("enforcer recorded an unblock that failed")
	}

	// A retry timer must be armed so the cleanup self-heals.
	sched.mu.Lock()
	_, hasRetry := sched.timers["expired.com"]
	sched.mu.Unlock()
	if !hasRetry {
		t.Error("expected a retry timer to be armed after the failed unblock")
	}

	// The enforcer recovers; the retry must now clean RAM + state + rules.
	enf.setFail(false)
	sched.onExpire("expired.com")

	state, _ = st.Load()
	if _, ok := state.Blocks["expired.com"]; ok {
		t.Error("expired block should be removed from state.json after successful retry")
	}
	blocks, _ = sched.ListBlocks()
	if len(blocks) != 0 {
		t.Errorf("expected no blocks in RAM after successful retry, got %+v", blocks)
	}
	enf.mu.Lock()
	_, unblocked = enf.unblockedDomains["expired.com"]
	enf.mu.Unlock()
	if !unblocked {
		t.Error("expected the enforcer unblock after successful retry")
	}
}

// TestScheduler_OnExpire_FailingUnblockKeepsBlock verifies the timer-expiry
// path has the same guarantee: a block whose OS rules fail to be removed is
// kept in RAM and state (with a retry) instead of being dropped while the
// rules remain.
func TestScheduler_OnExpire_FailingUnblockKeepsBlock(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)
	block := policy.Block{
		Domain:      "expired.com",
		StartedAt:   now.Add(-48 * time.Hour),
		ExpiresAt:   now.Add(-24 * time.Hour),
		ResolvedIPs: []string{"1.1.1.1"},
	}
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{"expired.com": block}}); err != nil {
		t.Fatalf("prepare state: %v", err)
	}
	sched.mu.Lock()
	sched.blocks["expired.com"] = block
	sched.mu.Unlock()

	failing := &failingUnblockEnforcer{mockEnforcer: enf}
	failing.setFail(true)
	sched.enforcer = failing

	sched.onExpire("expired.com")

	blocks, _ := sched.ListBlocks()
	if len(blocks) != 1 {
		t.Errorf("block should remain in RAM after failed unblock, got %+v", blocks)
	}
	state, _ := st.Load()
	if _, ok := state.Blocks["expired.com"]; !ok {
		t.Error("state.json should still contain the block after failed unblock")
	}

	failing.setFail(false)
	sched.onExpire("expired.com")

	blocks, _ = sched.ListBlocks()
	if len(blocks) != 0 {
		t.Errorf("block should be gone after successful retry, got %+v", blocks)
	}
	state, _ = st.Load()
	if _, ok := state.Blocks["expired.com"]; ok {
		t.Error("state.json should be clean after successful retry")
	}
}

func TestScheduler_Reconcile_MatchNoOp(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{
		"active.com": {
			Domain:      "active.com",
			StartedAt:   now,
			ExpiresAt:   now.Add(24 * time.Hour),
			ResolvedIPs: []string{"1.2.3.4"},
		},
	}}); err != nil {
		t.Fatalf("prepare state: %v", err)
	}
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("bootstrap Reconcile: %v", err)
	}

	enf.mu.Lock()
	callsAfterBootstrap := enf.syncCalls
	enf.mu.Unlock()

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}

	enf.mu.Lock()
	defer enf.mu.Unlock()
	if enf.syncCalls != callsAfterBootstrap {
		t.Errorf("expected no enforcer re-sync when disk matches RAM, got %d -> %d calls",
			callsAfterBootstrap, enf.syncCalls)
	}
}

// TestScheduler_SetOnChange_NotifiesOnMutation verifica o hook de mudança
// (Fase 7 — event hub): cada mutação de blocos (Block, ExtendBlock,
// BlockDomains, BlockAllInternet) dispara o callback registrado.
func TestScheduler_SetOnChange_NotifiesOnMutation(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	// O caminho em lote (BlockDomains) resolve via resolveFuncCtx; os demais
	// via resolveFunc — stub os dois para não tocar DNS real.
	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) { return []string{"1.2.3.4"}, nil }
	t.Cleanup(func() { resolveFunc = origResolve })
	origResolveCtx := resolveFuncCtx
	resolveFuncCtx = func(_ context.Context, _ string) ([]string, error) { return []string{"1.2.3.4"}, nil }
	t.Cleanup(func() { resolveFuncCtx = origResolveCtx })

	var mu sync.Mutex
	var calls int
	sched.SetOnChange(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	if _, err := sched.Block("example.com", time.Hour); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if _, err := sched.ExtendBlock("example.com", time.Hour); err != nil {
		t.Fatalf("ExtendBlock: %v", err)
	}
	if _, err := sched.BlockDomains([]string{"a.com", "b.com"}, time.Hour); err != nil {
		t.Fatalf("BlockDomains: %v", err)
	}
	if _, err := sched.BlockAllInternet(nil, time.Hour); err != nil {
		t.Fatalf("BlockAllInternet: %v", err)
	}
	// Ajustes de configuração do DNS (visíveis no status) também avisam — o
	// review da Fase 7 apontou o gap de staleness do status DNS.
	if err := sched.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled: %v", err)
	}
	if err := sched.SetDNSUpstream("1.1.1.1"); err != nil {
		t.Fatalf("SetDNSUpstream: %v", err)
	}
	// Repetir o mesmo valor é no-op — não notifica (sem evento espúrio).
	before := calls
	if err := sched.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled repetido: %v", err)
	}
	if before != calls {
		t.Errorf("SetDNSEnabled com o mesmo valor notificou (esperava no-op)")
	}

	mu.Lock()
	got := calls
	mu.Unlock()
	if got < 6 {
		t.Fatalf("SetOnChange chamado %d vezes, esperava >= 6 (Block/Extend/Batch/Panic/DNS)", got)
	}
}
