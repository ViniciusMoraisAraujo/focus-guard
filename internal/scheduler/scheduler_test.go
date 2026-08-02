package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
