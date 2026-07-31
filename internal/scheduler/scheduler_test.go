package scheduler

import (
	"errors"
	"path/filepath"
	"sync"
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

	// Sem bloqueios: proteção esperada = false
	ps, err := sched.ProtectionStatus()
	if err != nil {
		t.Fatalf("ProtectionStatus() erro: %v", err)
	}
	if ps.ExpectedDoH {
		t.Error("ExpectedDoH deve ser false sem bloqueios ativos")
	}

	// Com bloqueio ativo: proteção esperada = true
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

	// Erro do enforcer deve ser propagado
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

	go sched.startPeriodicIPRefresh(20 * time.Millisecond)

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
