package scheduler

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"focusguard/internal/infrastructure/store"
)

// saveFailingStore wraps *store.Store and makes Save fail while fail is true,
// simulating a full disk or a read-only state dir. Load delegates to the real
// store so bootstrap works normally.
type saveFailingStore struct {
	*store.Store
	fail atomic.Bool
}

func (s *saveFailingStore) Save(st *store.State) error {
	if s.fail.Load() {
		return errors.New("state: disk full")
	}
	return s.Store.Save(st)
}

// TestScheduler_Block_SaveErrorRollsBackRAM reproduces the zombie-state bug:
// when store.Save fails, Block must NOT leave the domain in RAM — no block,
// no timer, nothing persisted (the enforcer is never touched because the
// failure happens before the apply). A zombie block would show in status
// forever with no timer to expire it. BlockDomains/BlockAllInternet already
// roll back on Save failure; Block must behave the same.
func TestScheduler_Block_SaveErrorRollsBackRAM(t *testing.T) {
	st, err := store.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	failing := &saveFailingStore{Store: st}

	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) { return []string{"1.2.3.4"}, nil }
	t.Cleanup(func() { resolveFunc = origResolve })

	enf := newMockEnforcer()
	sched := NewScheduler(failing, enf)

	failing.fail.Store(true)
	if _, err := sched.Block("zombie.com", time.Hour); err == nil {
		t.Fatal("expected Block() to fail when store.Save fails")
	}

	// RAM deve estar limpa: sem bloco, sem timer, sem status ativo.
	blocks, _ := sched.ListBlocks()
	if len(blocks) != 0 {
		t.Errorf("expected RAM blocks to be rolled back after failed Save, got %v", blocks)
	}
	if sched.HasActiveBlocks() {
		t.Error("expected HasActiveBlocks to be false after failed Save")
	}
	if sched.IsBlocked("zombie.com") {
		t.Error("expected IsBlocked to be false after failed Save")
	}
	if b := sched.ActiveBlock("zombie.com"); b != nil {
		t.Errorf("expected ActiveBlock to be nil after failed Save, got %+v", b)
	}
	sched.mu.RLock()
	_, hasTimer := sched.timers["zombie.com"]
	sched.mu.RUnlock()
	if hasTimer {
		t.Error("expected no timer after failed Save")
	}

	// O enforcer não pode ter sido tocado (o Save falha antes do apply).
	enf.mu.Lock()
	if len(enf.blockedDomains) != 0 {
		t.Errorf("expected enforcer untouched after failed Save, got %v", enf.blockedDomains)
	}
	if enf.syncCalls != 0 {
		t.Errorf("expected no Sync after failed Save, got %d", enf.syncCalls)
	}
	enf.mu.Unlock()

	// Recuperação: com o disco de volta, o mesmo scheduler bloqueia
	// normalmente — nenhum resíduo do bloqueio zumbi interfere.
	failing.fail.Store(false)
	if _, err := sched.Block("zombie.com", time.Hour); err != nil {
		t.Fatalf("Block after recovery: %v", err)
	}
	blocks, _ = sched.ListBlocks()
	if len(blocks) != 1 || blocks[0].Domain != "zombie.com" {
		t.Errorf("expected zombie.com blocked after recovery, got %v", blocks)
	}
}

// TestScheduler_ExtendBlock_SaveErrorKeepsOriginalExpiry covers the same
// zombie family in ExtendBlock: when store.Save fails, the extension must NOT
// stay in RAM. With the extended expiry in RAM but the OLD timer still armed
// (original expiry), the timer would fire, onExpire would see the block still
// active and return early without re-arming — the block would never expire in
// RAM. The RAM must keep the ORIGINAL block (consistent with the armed timer
// and the disk mirror).
func TestScheduler_ExtendBlock_SaveErrorKeepsOriginalExpiry(t *testing.T) {
	st, err := store.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	failing := &saveFailingStore{Store: st}

	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) { return []string{"1.2.3.4"}, nil }
	t.Cleanup(func() { resolveFunc = origResolve })

	sched := NewScheduler(failing, newMockEnforcer())

	// Bloqueio inicial grava o disco com sucesso.
	block, err := sched.Block("x.com", time.Hour)
	if err != nil {
		t.Fatalf("Block: %v", err)
	}
	originalExpiry := block.ExpiresAt

	// Extensão falha no Save: a RAM deve manter a expiração ORIGINAL.
	failing.fail.Store(true)
	if _, err := sched.ExtendBlock("x.com", 30*time.Minute); err == nil {
		t.Fatal("expected ExtendBlock() to fail when store.Save fails")
	}

	sched.mu.RLock()
	ramBlock, ok := sched.blocks["x.com"]
	sched.mu.RUnlock()
	if !ok {
		t.Fatal("x.com should still be blocked after failed extend")
	}
	if !ramBlock.ExpiresAt.Equal(originalExpiry) {
		t.Errorf("RAM expiry after failed extend = %v, want original %v", ramBlock.ExpiresAt, originalExpiry)
	}

	// O timer continua armado com a expiração original (consistente).
	sched.mu.RLock()
	_, hasTimer := sched.timers["x.com"]
	sched.mu.RUnlock()
	if !hasTimer {
		t.Error("expected the block's timer to remain armed after failed extend")
	}

	// Recuperação: com o disco de volta, a extensão funciona normalmente.
	failing.fail.Store(false)
	ext, err := sched.ExtendBlock("x.com", 30*time.Minute)
	if err != nil {
		t.Fatalf("ExtendBlock after recovery: %v", err)
	}
	if !ext.ExpiresAt.Equal(originalExpiry.Add(30 * time.Minute)) {
		t.Errorf("extended expiry = %v, want %v", ext.ExpiresAt, originalExpiry.Add(30*time.Minute))
	}
}

// TestScheduler_SetDNSEnabled_SaveErrorRevertsRAM covers the same rollback
// family for the DNS sinkhole setting: when store.Save fails, the RAM value
// must revert to the persisted one instead of diverging from the disk until
// the next boot (the daemon reads DNSEnabled() after boot).
func TestScheduler_SetDNSEnabled_SaveErrorRevertsRAM(t *testing.T) {
	st, err := store.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	failing := &saveFailingStore{Store: st}
	sched := NewScheduler(failing, newMockEnforcer())

	// Estado persistido com sucesso: DNS ligado.
	if err := sched.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled(true): %v", err)
	}
	if !sched.DNSEnabled() {
		t.Fatal("DNSEnabled should be true after successful set")
	}

	// Desligar falha no Save: a RAM deve reverter para true (valor persistido).
	failing.fail.Store(true)
	if err := sched.SetDNSEnabled(false); err == nil {
		t.Fatal("expected SetDNSEnabled to fail when store.Save fails")
	}
	if !sched.DNSEnabled() {
		t.Error("DNSEnabled should revert to the persisted value (true) after failed Save")
	}

	// Recuperação: com o disco de volta, a mudança funciona.
	failing.fail.Store(false)
	if err := sched.SetDNSEnabled(false); err != nil {
		t.Fatalf("SetDNSEnabled after recovery: %v", err)
	}
	if sched.DNSEnabled() {
		t.Error("DNSEnabled should be false after successful recovery set")
	}
}

// TestScheduler_SetDNSUpstream_SaveErrorRevertsRAM is the same guarantee for
// the upstream resolver setting.
func TestScheduler_SetDNSUpstream_SaveErrorRevertsRAM(t *testing.T) {
	st, err := store.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	failing := &saveFailingStore{Store: st}
	sched := NewScheduler(failing, newMockEnforcer())

	if err := sched.SetDNSUpstream("1.1.1.2:53"); err != nil {
		t.Fatalf("SetDNSUpstream: %v", err)
	}

	// Troca falha no Save: a RAM deve reverter para o upstream persistido.
	failing.fail.Store(true)
	if err := sched.SetDNSUpstream("8.8.8.8:53"); err == nil {
		t.Fatal("expected SetDNSUpstream to fail when store.Save fails")
	}
	if got := sched.DNSUpstream(); got != "1.1.1.2:53" {
		t.Errorf("DNSUpstream should revert to the persisted value after failed Save, got %q", got)
	}

	// Recuperação: com o disco de volta, a troca funciona.
	failing.fail.Store(false)
	if err := sched.SetDNSUpstream("8.8.8.8:53"); err != nil {
		t.Fatalf("SetDNSUpstream after recovery: %v", err)
	}
	if got := sched.DNSUpstream(); got != "8.8.8.8:53" {
		t.Errorf("DNSUpstream should be updated after successful recovery, got %q", got)
	}
}
