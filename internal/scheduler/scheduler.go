package scheduler

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/policy"
	"focusguard/internal/store"
)

var resolveFunc = enforcer.ResolveIPs

// resolveFuncCtx resolves a domain honoring a context, so the periodic refresh
// can bound each individual DNS lookup with a timeout. Kept as a package var
// so tests can stub it.
var resolveFuncCtx = enforcer.ResolveIPsContext

// maxConcurrentDNSResolutions bounds how many DNS lookups the periodic refresh
// runs at the same time, avoiding a socket storm when many domains are blocked.
const maxConcurrentDNSResolutions = 8

// dnsRefreshTimeout bounds each individual DNS lookup in the periodic refresh,
// so one stalled resolver cannot hold the worker indefinitely.
const dnsRefreshTimeout = 3 * time.Second

// resolveBlockIPs resolves a domain and its www subdomain, merging both into a
// single de-duplicated list. This keeps the enforcer free of its own DNS
// lookups (G2) while preserving firewall coverage for the www addresses that
// the previous collectAllIPs provided. It uses resolveFunc (the non-ctx
// resolver) so benchmarks and tests that stub resolveFunc keep working.
func resolveBlockIPs(domain string) ([]string, error) {
	ips, err := resolveFunc(domain)
	if err != nil {
		return nil, err
	}

	wwwIPs, wwwErr := resolveFunc("www." + domain)
	if wwwErr != nil {
		return ips, nil
	}

	return mergeWWWIPs(ips, wwwIPs), nil
}

// resolveBlockIPsCtx is the context-aware counterpart of resolveBlockIPs used
// by the batched BlockDomains path: it resolves a domain and its www subdomain
// with a per-call context (so each DNS lookup in the batch can be bounded by a
// timeout) and merges both into a single de-duplicated list.
func resolveBlockIPsCtx(ctx context.Context, domain string) ([]string, error) {
	ips, err := resolveFuncCtx(ctx, domain)
	if err != nil {
		return nil, err
	}

	wwwIPs, wwwErr := resolveFuncCtx(ctx, "www."+domain)
	if wwwErr != nil {
		return ips, nil
	}

	return mergeWWWIPs(ips, wwwIPs), nil
}

// mergeWWWIPs appends the www subdomain's IPs to the base list, de-duplicating.
func mergeWWWIPs(ips, wwwIPs []string) []string {
	seen := make(map[string]bool, len(ips)+len(wwwIPs))
	for _, ip := range ips {
		seen[ip] = true
	}
	for _, ip := range wwwIPs {
		if !seen[ip] {
			ips = append(ips, ip)
			seen[ip] = true
		}
	}
	return ips
}

// dedupeIPs removes duplicate IPs while preserving order.
func dedupeIPs(ips []string) []string {
	seen := make(map[string]bool, len(ips))
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}

// dedupeDomains removes duplicate domains while preserving order.
func dedupeDomains(domains []string) []string {
	seen := make(map[string]bool, len(domains))
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// stateStore is the disk persistence contract the scheduler needs. It is an
// interface so tests can substitute a spy that counts Load calls and prove
// query methods are served 100% from RAM (no disk I/O under the lock).
type stateStore interface {
	Load() (*store.State, error)
	Save(*store.State) error
}

// Scheduler treats the in-memory blocks map as the source of truth. The
// state.json file on disk is only a mirror: it is read once at bootstrap to
// restore the RAM after a daemon restart, and afterwards any divergence from
// the in-memory state is treated as external tampering and the disk copy is
// overwritten to match the RAM.
//
// The mutex is an RWMutex so query methods (ListBlocks, HasActiveBlocks, Ping)
// run concurrently with each other and never block on disk I/O — they only
// read the RAM map.
type Scheduler struct {
	mu           sync.RWMutex
	store        stateStore
	enforcer     enforcer.Enforcer
	blocks       map[string]policy.Block
	timers       map[string]*time.Timer
	bootstrapped bool
	refreshStop  chan struct{}
}

func NewScheduler(st stateStore, enf enforcer.Enforcer) *Scheduler {
	return &Scheduler{
		store:       st,
		enforcer:    enf,
		blocks:      make(map[string]policy.Block),
		timers:      make(map[string]*time.Timer),
		refreshStop: make(chan struct{}),
	}
}

func (s *Scheduler) Start() error {
	if err := s.Reconcile(); err != nil {
		return err
	}

	go s.startPeriodicIPRefresh(15 * time.Minute)

	return nil
}

type unblockEntry struct {
	domain string
	ips    []string
}

type refreshEntry struct {
	domain string
	ips    []string
}

// mergeResolvedIPs merges the newly resolved IPs into the existing list,
// de-duplicating, and reports whether anything new was added.
func mergeResolvedIPs(existing, newIPs []string) ([]string, bool) {
	ipMap := make(map[string]bool, len(existing)+len(newIPs))
	merged := make([]string, 0, len(existing)+len(newIPs))
	for _, ip := range existing {
		ipMap[ip] = true
		merged = append(merged, ip)
	}
	hasNew := false
	for _, ip := range newIPs {
		if !ipMap[ip] {
			ipMap[ip] = true
			merged = append(merged, ip)
			hasNew = true
		}
	}
	return merged, hasNew
}

// refreshResolvedIPs resolves every entry's IPs in parallel with an individual
// per-domain timeout, returning the domains that gained at least one new
// address. A bounded worker pool (maxConcurrentDNSResolutions) avoids opening
// one DNS socket per blocked domain at once, while the timeout ensures one
// stalled resolver cannot hold the whole refresh cycle.
func refreshResolvedIPs(entries []refreshEntry, timeout time.Duration, resolve func(ctx context.Context, domain string) ([]string, error)) map[string][]string {
	refreshed := make(map[string][]string)
	if len(entries) == 0 {
		return refreshed
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		sem = make(chan struct{}, maxConcurrentDNSResolutions)
	)

	for _, entry := range entries {
		wg.Add(1)
		go func(e refreshEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			newIPs, err := resolve(ctx, e.domain)
			if err != nil || len(newIPs) == 0 {
				return
			}

			merged, hasNew := mergeResolvedIPs(e.ips, newIPs)
			if hasNew {
				mu.Lock()
				refreshed[e.domain] = merged
				mu.Unlock()
			}
		}(entry)
	}

	wg.Wait()
	return refreshed
}

// statesEqual reports whether two states are semantically identical. Any
// difference (missing/extra domain, altered timestamps or IPs) means the disk
// copy no longer mirrors the in-memory state and must be restored.
func statesEqual(a, b *store.State) bool {
	if len(a.Blocks) != len(b.Blocks) {
		return false
	}
	for domain, ab := range a.Blocks {
		bb, ok := b.Blocks[domain]
		if !ok {
			return false
		}
		if ab.Domain != bb.Domain || !ab.StartedAt.Equal(bb.StartedAt) || !ab.ExpiresAt.Equal(bb.ExpiresAt) {
			return false
		}
		if !slices.Equal(ab.ResolvedIPs, bb.ResolvedIPs) {
			return false
		}
	}
	return true
}

func (s *Scheduler) ramState() *store.State {
	blocks := make(map[string]policy.Block, len(s.blocks))
	for domain, block := range s.blocks {
		blocks[domain] = block
	}
	return &store.State{Version: 1, Blocks: blocks}
}

// Reconcile loads the persisted state into RAM on the first call (bootstrap)
// and, on every subsequent call, restores any disk tampering from the in-memory
// state. Expired blocks are cleaned up and the enforcer is re-applied only when
// something actually changed.
//
// The expiry cleanup is committed only AFTER the enforcer confirms the OS rules
// (hosts + firewall) were actually removed: a block is dropped from RAM and the
// cleaned state is persisted only on success. If the removal fails (e.g. the
// firewall service is still coming up right after boot, a transient netsh/
// iptables error), the block stays in RAM and on disk and a retry timer is
// armed — the state mirror never claims "clean" while stale rules remain.
func (s *Scheduler) Reconcile() error {
	s.mu.Lock()

	changed := false

	if !s.bootstrapped {
		state, err := s.store.Load()
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("scheduler: erro ao carregar estado: %w", err)
		}
		for domain, block := range state.Blocks {
			s.blocks[domain] = block
		}
		s.bootstrapped = true
		changed = true
	} else {
		// The disk is only a mirror: if it diverges from RAM (edited, emptied,
		// deleted or corrupted), it is overwritten with the in-memory state.
		// This comparison only runs after bootstrap — the boot path already
		// loaded the disk into RAM, so reading it a second time would be a
		// needless disk access under the exclusive lock.
		if disk, err := s.store.Load(); err != nil || !statesEqual(disk, s.ramState()) {
			changed = true
		}
	}

	// Coleta os bloqueios expirados SEM removê-los ainda: a remoção do RAM e a
	// gravação do state.json só acontecem depois que o enforcer confirmar a
	// limpeza das regras do SO (hosts + firewall).
	var toUnblock []unblockEntry
	activeIPs := make(map[string][]string, len(s.blocks))
	sentinelActive := false
	var sentinelIPs []string
	sentinelExpired := false
	for domain, block := range s.blocks {
		if isAllInternetBlock(block) {
			if block.CanUnblock() {
				sentinelExpired = true
			} else {
				sentinelActive = true
				sentinelIPs = block.ResolvedIPs
				s.setupTimerLocked(block)
			}
			continue
		}
		if block.CanUnblock() {
			toUnblock = append(toUnblock, unblockEntry{domain, block.ResolvedIPs})
		} else {
			activeIPs[domain] = block.ResolvedIPs
			s.setupTimerLocked(block)
		}
	}
	hasExpired := len(toUnblock) > 0 || sentinelExpired

	if !changed && !hasExpired {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	// Limpeza do enforcer ANTES de commitar a remoção: as regras do SO só
	// saem quando o enforcer confirmar. Na falha (ex.: serviço de firewall
	// ainda subindo no boot), o bloqueio permanece no estado para retry —
	// nunca um state.json "limpo" com regras órfãs no sistema.
	expiryClean := true
	for _, e := range toUnblock {
		if err := s.enforcer.UnblockDomain(e.domain, e.ips); err != nil {
			expiryClean = false
			log.Printf("[Scheduler] Falha ao remover regras de %s (será re-tentado): %v", e.domain, err)
		}
	}
	if sentinelExpired {
		if err := s.enforcer.UnblockAll(); err != nil {
			expiryClean = false
			log.Printf("[Scheduler] Falha ao remover o bloqueio de internet (será re-tentado): %v", err)
		}
	}
	if sentinelActive {
		// Re-aplica o sentinela após restart (bootstrap) — o enforcer é
		// idempotente e o allowlist precisa ser reestabelecido.
		if err := s.enforcer.BlockAll(sentinelIPs); err != nil {
			return err
		}
		// Domínios persistidos continuam precisando do Sync (hosts + regras
		// por IP): o sentinela bloqueia tudo, mas ao expirar a proteção dos
		// domínios precisa já estar no ar. Sem este Sync, um restart com
		// pânico + domínios deixaria os domínios sem proteção após a
		// expiração do sentinela.
		if len(activeIPs) > 0 {
			if err := s.enforcer.Sync(activeIPs); err != nil {
				return err
			}
		}
		_ = s.enforcer.BlockDoH()
	} else if len(activeIPs) > 0 {
		if err := s.enforcer.Sync(activeIPs); err != nil {
			return err
		}
		_ = s.enforcer.BlockDoH()
	} else {
		_ = s.enforcer.UnblockDoH()
	}

	// Commit: remove os expirados do RAM e grava o estado limpo só quando o
	// enforcer confirmou a remoção. Na falha, os bloqueios ficam no RAM/estado
	// e um timer de retry re-tenta a limpeza (também no próximo Reconcile —
	// próximo boot, tamper ou mudança externa de state.json).
	s.mu.Lock()
	if expiryClean && hasExpired {
		for _, e := range toUnblock {
			if _, exists := s.blocks[e.domain]; !exists {
				continue // outro caminho (onExpire) já cuidou
			}
			delete(s.blocks, e.domain)
			if t, ok := s.timers[e.domain]; ok {
				t.Stop()
				delete(s.timers, e.domain)
			}
		}
		if sentinelExpired {
			delete(s.blocks, enforcer.AllInternetDomain)
			if t, ok := s.timers[enforcer.AllInternetDomain]; ok {
				t.Stop()
				delete(s.timers, enforcer.AllInternetDomain)
			}
		}
		changed = true
	}
	if changed {
		if err := s.store.Save(s.ramState()); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("scheduler: erro ao restaurar/salvar estado: %w", err)
		}
	}
	if !expiryClean && hasExpired {
		for _, e := range toUnblock {
			if _, exists := s.blocks[e.domain]; exists {
				s.armExpiryRetryLocked(e.domain)
			}
		}
		if sentinelExpired {
			if _, exists := s.blocks[enforcer.AllInternetDomain]; exists {
				s.armExpiryRetryLocked(enforcer.AllInternetDomain)
			}
		}
	}
	s.mu.Unlock()

	return nil
}

func (s *Scheduler) startPeriodicIPRefresh(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.refreshStop:
			return
		case <-ticker.C:
		}

		s.mu.Lock()
		if len(s.blocks) == 0 {
			s.mu.Unlock()
			continue
		}

		var entries []refreshEntry
		for domain, block := range s.blocks {
			if isAllInternetBlock(block) {
				continue // sentinela não tem domínio para re-resolver
			}
			if block.IsActive() {
				entries = append(entries, refreshEntry{domain: domain, ips: block.ResolvedIPs})
			}
		}
		s.mu.Unlock()

		if len(entries) == 0 {
			continue
		}

		refreshed := refreshResolvedIPs(entries, dnsRefreshTimeout, resolveFuncCtx)

		if len(refreshed) == 0 {
			continue
		}

		var toRefresh []refreshEntry
		s.mu.Lock()
		for domain, ips := range refreshed {
			if block, exists := s.blocks[domain]; exists && block.IsActive() {
				block.ResolvedIPs = ips
				s.blocks[domain] = block
				toRefresh = append(toRefresh, refreshEntry{domain, ips})
			}
		}
		if len(toRefresh) > 0 {
			_ = s.store.Save(s.ramState())
		}
		s.mu.Unlock()

		for _, e := range toRefresh {
			_ = s.enforcer.BlockDomain(e.domain, e.ips)
		}
	}
}

func (s *Scheduler) Block(domain string, duration time.Duration) (*policy.Block, error) {
	ips, err := resolveBlockIPs(domain)
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}

	now := time.Now()
	block := policy.Block{
		Domain:      domain,
		StartedAt:   now,
		ExpiresAt:   now.Add(duration),
		ResolvedIPs: ips,
	}

	s.mu.Lock()
	wasEmpty := len(s.blocks) == 0
	s.blocks[domain] = block
	if err := s.store.Save(s.ramState()); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}
	s.mu.Unlock()

	if err := s.enforcer.BlockDomain(domain, ips); err != nil {
		// Reverte a RAM e o disco: um bloqueio que falhou não pode deixar o
		// domínio ativo sem timer (estado zumbi).
		s.mu.Lock()
		delete(s.blocks, domain)
		_ = s.store.Save(s.ramState())
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao aplicar bloqueio: %w", err)
	}

	if wasEmpty {
		_ = s.enforcer.BlockDoH()
	}

	s.mu.Lock()
	s.setupTimerLocked(block)
	s.mu.Unlock()

	return &block, nil
}

// BlockDomains blocks several domains at once, resolving all of them in
// parallel (with a per-domain timeout) and then persisting the whole batch
// with a single store.Save and applying it with a single enforcer.Sync — one
// hosts-file rewrite for N sites instead of N+1. RAM remains the source of
// truth; the disk is written once for the entire batch.
func (s *Scheduler) BlockDomains(domains []string, duration time.Duration) ([]policy.Block, error) {
	unique := dedupeDomains(domains)
	if len(unique) == 0 {
		return nil, fmt.Errorf("scheduler: nenhum domínio para bloquear")
	}

	// Resolve all domains in parallel with an individual timeout each.
	resolved := refreshResolvedIPs(resolveEntries(unique), dnsRefreshTimeout, resolveBlockIPsCtx)
	if len(resolved) != len(unique) {
		for _, d := range unique {
			if _, ok := resolved[d]; !ok {
				return nil, fmt.Errorf("scheduler: falha ao resolver %s", d)
			}
		}
	}

	now := time.Now()
	blocks := make([]policy.Block, 0, len(unique))
	activeIPs := make(map[string][]string, len(unique))
	for _, d := range unique {
		b := policy.Block{
			Domain:      d,
			StartedAt:   now,
			ExpiresAt:   now.Add(duration),
			ResolvedIPs: resolved[d],
		}
		blocks = append(blocks, b)
		activeIPs[d] = resolved[d]
	}

	s.mu.Lock()
	wasEmpty := len(s.blocks) == 0
	for _, b := range blocks {
		s.blocks[b.Domain] = b
	}
	if err := s.store.Save(s.ramState()); err != nil {
		// Reverte a RAM: sem o disco persistido, os domínios não podem ficar
		// ativos sem timer (estado zumbi) — o lote inteiro é descartado.
		for _, b := range blocks {
			delete(s.blocks, b.Domain)
		}
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}
	s.mu.Unlock()

	// A single batched Sync writes the hosts file once for the whole batch.
	if err := s.enforcer.Sync(activeIPs); err != nil {
		// Reverte a RAM e o disco: um lote que falhou não pode deixar os
		// domínios ativos sem timer (estado zumbi).
		s.mu.Lock()
		for _, b := range blocks {
			delete(s.blocks, b.Domain)
			if t, ok := s.timers[b.Domain]; ok {
				t.Stop()
				delete(s.timers, b.Domain)
			}
		}
		_ = s.store.Save(s.ramState())
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao aplicar bloqueios: %w", err)
	}

	if wasEmpty {
		_ = s.enforcer.BlockDoH()
	}

	s.mu.Lock()
	for _, b := range blocks {
		s.setupTimerLocked(b)
	}
	s.mu.Unlock()

	return blocks, nil
}

// resolveEntries converts a list of domains into refresh entries with no
// existing IPs, so refreshResolvedIPs returns every successfully resolved
// domain (empty merges are skipped, failures are absent from the result).
func resolveEntries(domains []string) []refreshEntry {
	entries := make([]refreshEntry, 0, len(domains))
	for _, d := range domains {
		entries = append(entries, refreshEntry{domain: d})
	}
	return entries
}

// BlockAllInternet cuts off ALL outbound internet (panic mode) or blocks
// everything except the allowlist domains (deep-focus mode), for the given
// duration. The block is tracked under the enforcer.AllInternetDomain sentinel
// key (never a real hostname, so no hosts-file entry and no per-domain IP
// rules): it persists to the state mirror like any block and expires through
// the same timer machinery, invoking enforcer.UnblockAll on expiry.
func (s *Scheduler) BlockAllInternet(allowlistDomains []string, duration time.Duration) (*policy.Block, error) {
	// Resolve allowlist domains to IPs (best-effort per domain; failures are
	// dropped so one bad domain never kills the whole panic block).
	var allowlistIPs []string
	if len(allowlistDomains) > 0 {
		resolved := refreshResolvedIPs(resolveEntries(dedupeDomains(allowlistDomains)), dnsRefreshTimeout, resolveBlockIPsCtx)
		for _, d := range dedupeDomains(allowlistDomains) {
			if ips, ok := resolved[d]; ok {
				allowlistIPs = append(allowlistIPs, ips...)
			}
		}
	}

	now := time.Now()
	block := policy.Block{
		Domain:      enforcer.AllInternetDomain,
		StartedAt:   now,
		ExpiresAt:   now.Add(duration),
		ResolvedIPs: dedupeIPs(allowlistIPs),
	}

	s.mu.Lock()
	wasEmpty := len(s.blocks) == 0
	s.blocks[enforcer.AllInternetDomain] = block
	if err := s.store.Save(s.ramState()); err != nil {
		delete(s.blocks, enforcer.AllInternetDomain)
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}
	s.mu.Unlock()

	if err := s.enforcer.BlockAll(block.ResolvedIPs); err != nil {
		// Reverte a RAM e o disco: um bloqueio que falhou não pode deixar o
		// sentinela ativo sem timer (estado zumbi).
		s.mu.Lock()
		delete(s.blocks, enforcer.AllInternetDomain)
		_ = s.store.Save(s.ramState())
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao aplicar bloqueio de internet: %w", err)
	}

	if wasEmpty {
		_ = s.enforcer.BlockDoH()
	}

	s.mu.Lock()
	s.setupTimerLocked(block)
	s.mu.Unlock()

	return &block, nil
}

// isAllInternetBlock reports whether a scheduler block entry is the sentinel
// all-internet block (panic/deep-focus), which the enforcer handles through
// BlockAll/UnblockAll instead of per-domain hosts/firewall rules.
func isAllInternetBlock(b policy.Block) bool {
	return b.Domain == enforcer.AllInternetDomain
}

func (s *Scheduler) ListBlocks() ([]policy.Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]policy.Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		list = append(list, b)
	}
	return list, nil
}

func (s *Scheduler) setupTimerLocked(block policy.Block) {
	if timer, exists := s.timers[block.Domain]; exists {
		timer.Stop()
	}

	duration := block.RemainingTime()
	s.timers[block.Domain] = time.AfterFunc(duration, func() {
		s.onExpire(block.Domain)
	})
}

// retryExpiryDelay is how long the scheduler waits before re-attempting the
// removal of a block whose OS rules could not be cleared. A transient failure
// at boot (firewall service still coming up) or during expiry must self-heal
// instead of leaving a clean state.json with stale rules in the OS.
const retryExpiryDelay = 30 * time.Second

// armExpiryRetryLocked schedules a retry of the expiry cleanup for domain.
// Unlike setupTimerLocked (which fires at RemainingTime — 0 for an already
// expired block, an instant busy loop), the retry always waits a fixed delay.
// The caller must hold s.mu.
func (s *Scheduler) armExpiryRetryLocked(domain string) {
	if t, ok := s.timers[domain]; ok {
		t.Stop()
	}
	s.timers[domain] = time.AfterFunc(retryExpiryDelay, func() {
		s.onExpire(domain)
	})
}

func (s *Scheduler) onExpire(domain string) {
	s.mu.Lock()
	block, exists := s.blocks[domain]
	if !exists || !block.CanUnblock() {
		s.mu.Unlock()
		return
	}
	ips := block.ResolvedIPs
	s.mu.Unlock()

	// O enforcer limpa as regras ANTES de o bloqueio sair do RAM/estado: só
	// removemos e gravamos o estado limpo depois que a remoção no SO
	// confirmou. Na falha, o bloqueio permanece e o timer de retry re-tenta.
	unblockOK := true
	if isAllInternetBlock(block) {
		if err := s.enforcer.UnblockAll(); err != nil {
			unblockOK = false
			log.Printf("[Scheduler] Falha ao remover o bloqueio de internet (será re-tentado): %v", err)
		}
	} else if err := s.enforcer.UnblockDomain(domain, ips); err != nil {
		unblockOK = false
		log.Printf("[Scheduler] Falha ao remover regras de %s (será re-tentado): %v", domain, err)
	}

	s.mu.Lock()
	if !unblockOK {
		if _, exists := s.blocks[domain]; exists {
			s.armExpiryRetryLocked(domain)
		}
		s.mu.Unlock()
		return
	}
	if _, exists := s.blocks[domain]; !exists {
		s.mu.Unlock()
		return // outro caminho (Reconcile) já removeu
	}
	delete(s.blocks, domain)
	delete(s.timers, domain)
	remaining := len(s.blocks)
	_ = s.store.Save(s.ramState())
	s.mu.Unlock()

	if remaining == 0 {
		_ = s.enforcer.UnblockDoH()
	}
}

func (s *Scheduler) HasActiveBlocks() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, block := range s.blocks {
		if block.IsActive() {
			return true
		}
	}
	return false
}

type ProtectionStatus struct {
	ExpectedDoH   bool
	DoHActive     bool
	FirewallRules int
}

func (s *Scheduler) ProtectionStatus() (ProtectionStatus, error) {
	expected := s.HasActiveBlocks()

	st, err := s.enforcer.Status()
	if err != nil {
		return ProtectionStatus{}, err
	}

	return ProtectionStatus{
		ExpectedDoH:   expected,
		DoHActive:     st.DoHActive,
		FirewallRules: st.FirewallRules,
	}, nil
}

func (s *Scheduler) Ping() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return nil
}
