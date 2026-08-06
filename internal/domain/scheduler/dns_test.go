package scheduler

import (
	"testing"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/domain/policy"
	"focusguard/internal/store"
)

// seedBlock inserts a block directly into the RAM map (the established test
// pattern) without touching the enforcer or the disk.
func seedBlock(s *Scheduler, domain string, active bool, allowlist []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	b := policy.Block{Domain: domain, Allowlist: allowlist}
	if active {
		b.StartedAt = now.Add(-time.Minute)
		b.ExpiresAt = now.Add(time.Hour)
	} else {
		b.StartedAt = now.Add(-2 * time.Hour)
		b.ExpiresAt = now.Add(-time.Hour)
	}
	s.blocks[domain] = b
}

func TestScheduler_IsBlockedExactAndSubdomain(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	seedBlock(sched, "youtube.com", true, nil)

	cases := []struct {
		domain string
		want   bool
	}{
		{"youtube.com", true},          // match exato
		{"www.youtube.com", true},      // subdomínio de 1 nível
		{"a.b.youtube.com", true},      // subdomínio profundo
		{"notyoutube.com", false},      // prefixo não é sufixo
		{"youtube.com.evil.com", false}, // não é subdomínio
	}
	for _, c := range cases {
		if got := sched.IsBlocked(c.domain); got != c.want {
			t.Errorf("IsBlocked(%q) = %v, want %v", c.domain, got, c.want)
		}
	}
}

func TestScheduler_IsBlockedIgnoresExpired(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	seedBlock(sched, "youtube.com", false, nil)
	seedBlock(sched, "twitter.com", true, nil)

	if sched.IsBlocked("youtube.com") {
		t.Error("IsBlocked(youtube.com) = true para bloqueio expirado")
	}
	if !sched.IsBlocked("twitter.com") {
		t.Error("IsBlocked(twitter.com) = false para bloqueio ativo")
	}
}

func TestScheduler_IsBlockedAllInternetRespectsAllowlist(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	seedBlock(sched, enforcer.AllInternetDomain, true, []string{"docs.com", "mail.example.com"})

	cases := []struct {
		domain string
		want   bool
	}{
		{"anywhere.com", true},
		{"instagram.com", true},
		{"docs.com", false},                  // allowlist exato
		{"sub.docs.com", false},              // subdomínio de allowlist
		{"x.mail.example.com", false},        // subdomínio profundo de allowlist
		{"example.com", true},   // pai não cobre o filho permitido
		{"notdocs.com", true},   // prefixo não é sufixo
	}
	for _, c := range cases {
		if got := sched.IsBlocked(c.domain); got != c.want {
			t.Errorf("IsBlocked(%q) = %v, want %v", c.domain, got, c.want)
		}
	}
}

func TestScheduler_IsBlockedAllInternetExpiredFallsBackToDomainRules(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	seedBlock(sched, enforcer.AllInternetDomain, false, []string{"docs.com"})
	seedBlock(sched, "youtube.com", true, nil)

	if !sched.IsBlocked("youtube.com") {
		t.Error("IsBlocked(youtube.com) = false com sentinela expirado")
	}
	if sched.IsBlocked("anything.com") {
		t.Error("IsBlocked(anything.com) = true com sentinela expirado")
	}
}

func TestScheduler_IsBlockedCaseInsensitive(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	seedBlock(sched, "youtube.com", true, nil)

	if !sched.IsBlocked("WWW.YouTube.Com") {
		t.Error("IsBlocked(WWW.YouTube.Com) = false; matching deve ser case-insensitive")
	}
}

func TestScheduler_SetDNSEnabledPersistsAndBootstraps(t *testing.T) {
	sched, _, st := setupTestScheduler(t)

	if sched.DNSEnabled() {
		t.Error("DNSEnabled = true no estado inicial")
	}
	if err := sched.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled(true): %v", err)
	}
	if !sched.DNSEnabled() {
		t.Error("DNSEnabled = false após SetDNSEnabled(true)")
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if !state.DNSEnabled {
		t.Error("store não persistiu DNSEnabled=true")
	}

	// Um novo scheduler bootstrapped do mesmo store deve restaurar a flag.
	sched2 := NewScheduler(st, newMockEnforcer())
	if err := sched2.Reconcile(); err != nil {
		t.Fatalf("Reconcile bootstrap: %v", err)
	}
	if !sched2.DNSEnabled() {
		t.Error("DNSEnabled perdido após bootstrap a partir do disco")
	}
}

func TestScheduler_SetDNSUpstreamPersistsAndBootstraps(t *testing.T) {
	sched, _, st := setupTestScheduler(t)

	if sched.DNSUpstream() != "" {
		t.Errorf("DNSUpstream = %q no estado inicial, esperava vazio", sched.DNSUpstream())
	}
	if err := sched.SetDNSUpstream("9.9.9.9:53"); err != nil {
		t.Fatalf("SetDNSUpstream: %v", err)
	}
	if got := sched.DNSUpstream(); got != "9.9.9.9:53" {
		t.Errorf("DNSUpstream = %q após set, esperava 9.9.9.9:53", got)
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if state.DNSUpstream != "9.9.9.9:53" {
		t.Errorf("store não persistiu DNSUpstream, got %q", state.DNSUpstream)
	}

	// Um novo scheduler bootstrapped do mesmo store deve restaurar o upstream.
	sched2 := NewScheduler(st, newMockEnforcer())
	if err := sched2.Reconcile(); err != nil {
		t.Fatalf("Reconcile bootstrap: %v", err)
	}
	if got := sched2.DNSUpstream(); got != "9.9.9.9:53" {
		t.Errorf("DNSUpstream perdido após bootstrap, got %q", got)
	}
}

func TestScheduler_SetDNSUpstreamSameValueIsNoOp(t *testing.T) {
	sched, _, st := setupTestScheduler(t)
	if err := sched.SetDNSUpstream("9.9.9.9:53"); err != nil {
		t.Fatalf("SetDNSUpstream: %v", err)
	}
	if err := sched.SetDNSUpstream("9.9.9.9:53"); err != nil {
		t.Fatalf("SetDNSUpstream mesmo valor: %v", err)
	}
	// No-op: estado em disco continua com o mesmo valor e nada quebrou.
	state, err := st.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if state.DNSUpstream != "9.9.9.9:53" {
		t.Errorf("DNSUpstream = %q após no-op, esperava 9.9.9.9:53", state.DNSUpstream)
	}
}

func TestScheduler_ReconcileRestoresTamperedDNSUpstream(t *testing.T) {
	sched, _, st := setupTestScheduler(t)

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile bootstrap: %v", err)
	}
	if err := sched.SetDNSUpstream("9.9.9.9:53"); err != nil {
		t.Fatalf("SetDNSUpstream: %v", err)
	}

	// Tamper: um editor externo zera o dns_upstream no disco.
	if err := st.Save(&store.State{Version: 1, Blocks: map[string]policy.Block{}, DNSEnabled: false, DNSUpstream: ""}); err != nil {
		t.Fatalf("Save tampered: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if state.DNSUpstream != "9.9.9.9:53" {
		t.Errorf("Reconcile não restaurou DNSUpstream após tamper, got %q", state.DNSUpstream)
	}
	if sched.DNSUpstream() != "9.9.9.9:53" {
		t.Error("Reconcile derrubou o upstream em RAM")
	}
}

func TestScheduler_SetDNSEnabledFalsePersists(t *testing.T) {
	sched, _, st := setupTestScheduler(t)
	if err := sched.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled(true): %v", err)
	}
	if err := sched.SetDNSEnabled(false); err != nil {
		t.Fatalf("SetDNSEnabled(false): %v", err)
	}
	if sched.DNSEnabled() {
		t.Error("DNSEnabled = true após SetDNSEnabled(false)")
	}
	state, err := st.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if state.DNSEnabled {
		t.Error("store manteve DNSEnabled=true após desligar")
	}
}

func TestScheduler_ReconcileRestoresTamperedDNSSetting(t *testing.T) {
	sched, _, st := setupTestScheduler(t)

	// Bootstrap + liga o DNS em RAM (e em disco via SetDNSEnabled).
	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile bootstrap: %v", err)
	}
	if err := sched.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled(true): %v", err)
	}

	// Tamper: um editor externo grava dns_enabled=false no disco.
	if err := st.Save(&store.State{Version: 1, Blocks: map[string]policy.Block{}, DNSEnabled: false}); err != nil {
		t.Fatalf("Save tampered: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	state, err := st.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if !state.DNSEnabled {
		t.Error("Reconcile não restaurou DNSEnabled=true após tamper no disco")
	}
	if !sched.DNSEnabled() {
		t.Error("Reconcile derrubou o flag em RAM")
	}
}

func TestScheduler_BlockAllInternetCarriesAllowlist(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)
	stubResolveFuncCtx(t, map[string][]string{
		"docs.com":     {"1.2.3.4"},
		"www.docs.com": {"1.2.3.4"},
	})

	if _, err := sched.BlockAllInternet([]string{"docs.com"}, time.Hour); err != nil {
		t.Fatalf("BlockAllInternet: %v", err)
	}

	sched.mu.RLock()
	sentinel := sched.blocks[enforcer.AllInternetDomain]
	sched.mu.RUnlock()
	if len(sentinel.Allowlist) != 1 || sentinel.Allowlist[0] != "docs.com" {
		t.Errorf("Allowlist = %v, want [docs.com]", sentinel.Allowlist)
	}
	if !sched.IsBlocked("instagram.com") {
		t.Error("IsBlocked(instagram.com) = false em modo deep-focus")
	}
	if sched.IsBlocked("docs.com") {
		t.Error("IsBlocked(docs.com) = true; allowlist do deep-focus deve furar o sinkhole")
	}
}
