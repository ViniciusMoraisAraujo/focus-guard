package scheduler

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"focusguard/internal/domain/policy"
	"focusguard/internal/infrastructure/enforcer"
	"focusguard/internal/infrastructure/store"
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

// dnsCacheTTL bounds how long a resolved domain's IPs are reused by the block
// paths before the domain is re-resolved. Blocking the same domain twice within
// the TTL (unblock/re-block, a preset sharing active domains, deep-focus
// allowlists) skips the DNS round-trip that dominates block latency in the
// real daemon (2.4-10.2s with a slow resolver). DNS records rarely change
// faster than this, and the periodic refresh still re-resolves active blocks
// every 15 minutes — so the cache only smooths the hot path, never the upkeep.
const dnsCacheTTL = 60 * time.Second

type dnsEntry struct {
	ips     []string
	expires time.Time
}

// dnsCache is a small TTL cache for resolved domain IPs. It has its own mutex
// because BlockDomains/BlockAllInternet resolve in parallel goroutines.
type dnsCache struct {
	mu      sync.Mutex
	entries map[string]dnsEntry
}

func (c *dnsCache) get(domain string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[domain]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.ips, true
}

func (c *dnsCache) put(domain string, ips []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]dnsEntry)
	}
	c.entries[domain] = dnsEntry{ips: ips, expires: time.Now().Add(dnsCacheTTL)}
}

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

// resolveBlockIPsCached resolves a domain for the single-block path, reusing
// the DNS cache when a fresh entry exists. Caching the merged result (base +
// www) keeps repeated Block calls on the same domain from paying the
// 2.4-10.2s real-DNS round-trip measured on this machine.
func (s *Scheduler) resolveBlockIPsCached(domain string) ([]string, error) {
	if ips, ok := s.dns.get(domain); ok {
		return ips, nil
	}
	ips, err := resolveBlockIPs(domain)
	if err != nil {
		return nil, err
	}
	s.dns.put(domain, ips)
	return ips, nil
}

// resolveBlockIPsCachedCtx is the context-aware cached resolver used by the
// batched block paths, mirroring resolveBlockIPsCtx with the DNS cache in
// front so a batch sharing domains with a recent block reuses their IPs.
func (s *Scheduler) resolveBlockIPsCachedCtx(ctx context.Context, domain string) ([]string, error) {
	if ips, ok := s.dns.get(domain); ok {
		return ips, nil
	}
	ips, err := resolveBlockIPsCtx(ctx, domain)
	if err != nil {
		return nil, err
	}
	s.dns.put(domain, ips)
	return ips, nil
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
	// stopOnce garante que Stop é idempotente: fechar um canal já fechado
	// panicaria — o teardown do daemon pode chamá-lo mais de uma vez.
	stopOnce sync.Once
	dns      *dnsCache
	// dnsEnabled persists whether the DNS sinkhole server should run. It is a
	// setting, not a live state: starting/stopping the actual listener is the
	// daemon's job, which reads this after the bootstrap Reconcile.
	dnsEnabled bool
	// dnsUpstream persists the upstream resolver (host:port) the sinkhole
	// forwards allowed queries to. Empty means the daemon default
	// (dnsserver.DefaultUpstream). Like dnsEnabled, it is a setting mirrored
	// to disk, not live state.
	dnsUpstream string
	// snapshot is the immutable read cache for ListBlocks, rebuilt on demand so
	// query paths never iterate the source-of-truth map. Writers only mark it
	// stale (invalidateSnapshot); the first reader after a mutation rebuilds it
	// from the map and swaps the atomic pointer — reads stay O(n) slice copies
	// with minimal lock contention even while a writer is mid-Block.
	snapshot      atomic.Pointer[[]policy.Block]
	snapshotDirty atomic.Bool
	// onChange is a coarse "state changed" hook (Fase 7): the daemon wires it
	// to publish blocks-changed on the event hub, so the web UI can refresh
	// without polling. Called after every mutation (block/extend/batch/panic/
	// expiry/reconcile) WITHOUT holding s.mu — the callback must never call
	// back into the scheduler.
	onChange func()
}

// SetOnChange registers a callback invoked (without the scheduler lock) after
// every block-state mutation. Nil disables it. The callback must not call back
// into the scheduler (it runs on the mutating goroutine).
func (s *Scheduler) SetOnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// notifyChange fires the change hook outside the lock (safe to call anywhere
// the caller does not hold s.mu).
func (s *Scheduler) notifyChange() {
	s.mu.RLock()
	fn := s.onChange
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func NewScheduler(st stateStore, enf enforcer.Enforcer) *Scheduler {
	s := &Scheduler{
		store:       st,
		enforcer:    enf,
		blocks:      make(map[string]policy.Block),
		timers:      make(map[string]*time.Timer),
		refreshStop: make(chan struct{}),
		dns:         &dnsCache{entries: make(map[string]dnsEntry)},
	}
	empty := make([]policy.Block, 0)
	s.snapshot.Store(&empty)
	return s
}

func (s *Scheduler) Start() error {
	if err := s.Reconcile(); err != nil {
		return err
	}

	go s.startPeriodicIPRefresh(15 * time.Minute)

	return nil
}

// Stop encerra o refresh periódico de IPs: a goroutine do
// startPeriodicIPRefresh sai no próximo select. Idempotente — o teardown do
// daemon pode chamá-lo mais de uma vez sem panic. Antes deste método a
// goroutine vazava no shutdown do daemon (bug-hunt Etapa 4).
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.refreshStop) })
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
// difference (missing/extra domain, altered timestamps or IPs, a flipped
// DNS-enabled flag) means the disk copy no longer mirrors the in-memory state
// and must be restored.
func statesEqual(a, b *store.State) bool {
	if a.DNSEnabled != b.DNSEnabled || a.DNSUpstream != b.DNSUpstream {
		return false
	}
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
		if !slices.Equal(ab.ResolvedIPs, bb.ResolvedIPs) || !slices.Equal(ab.Allowlist, bb.Allowlist) {
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
	return &store.State{Version: 1, Blocks: blocks, DNSEnabled: s.dnsEnabled, DNSUpstream: s.dnsUpstream}
}

// invalidateSnapshot marks the ListBlocks snapshot stale so the next read
// rebuilds it from the source-of-truth map. Call after every mutation of
// s.blocks while holding s.mu (a plain bool is safe here: the readers that
// clear it run under RLock, which excludes the writer holding s.mu).
func (s *Scheduler) invalidateSnapshot() {
	s.snapshotDirty.Store(true)
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
		s.dnsEnabled = state.DNSEnabled
		s.dnsUpstream = state.DNSUpstream
		s.invalidateSnapshot()
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
		// Sem blocos ativos: varre regras de domínio órfãs antes de desligar o
		// DoH. O Sync com conjunto vazio deixa o firewall exatamente igual ao
		// estado (nenhuma regra) — remove regras que um refresh periódico
		// aplicou na janela da expiração (raça real) ou restos de crash, em vez
		// de deixar um "estado limpo" com regras órfãs no SO.
		_ = s.enforcer.Sync(nil)
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
		s.invalidateSnapshot()
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

	// Boot/tamper pode ter adicionado/removido blocos — avisa o hub (Fase 7)
	// para a UI refrescar (o SSE conecta depois do boot e o since=0 já pega o
	// ring; aqui cobre reconciliações por tamper em runtime).
	if changed {
		s.notifyChange()
	}

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
			s.invalidateSnapshot()
			_ = s.store.Save(s.ramState())
		}
		s.mu.Unlock()

		for _, e := range toRefresh {
			_ = s.enforcer.BlockDomain(e.domain, e.ips)
		}
	}
}

func (s *Scheduler) Block(domain string, duration time.Duration) (*policy.Block, error) {
	ips, err := s.resolveBlockIPsCached(domain)
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
	s.invalidateSnapshot()
	if err := s.store.Save(s.ramState()); err != nil {
		// Reverte a RAM: sem o disco persistido, o domínio não pode ficar
		// ativo sem timer e sem regra aplicada (estado zumbi) — o bloqueio
		// é descartado, como BlockDomains/BlockAllInternet já fazem.
		delete(s.blocks, domain)
		s.invalidateSnapshot()
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}
	s.mu.Unlock()

	if err := s.enforcer.BlockDomain(domain, ips); err != nil {
		// Reverte a RAM e o disco: um bloqueio que falhou não pode deixar o
		// domínio ativo sem timer (estado zumbi).
		s.mu.Lock()
		delete(s.blocks, domain)
		s.invalidateSnapshot()
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

	s.notifyChange()
	return &block, nil
}

// ActiveBlock returns a copy of the block for domain only when it is still
// active (expiry in the future); nil when the domain is not blocked or its
// window already ended. It backs the conflict detection in the user-driven
// block paths (IPC/CLI/Web): an already-active block is a question to the user
// (somar/substituir), not a silent overwrite. Schedule windows and pomodoro
// keep their upsert semantics and never consult it.
func (s *Scheduler) ActiveBlock(domain string) *policy.Block {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blocks[domain]
	if !ok || !b.IsActive() {
		return nil
	}
	cp := b
	return &cp
}

// ExtendBlock prolongs an active block by duration — soma: it adds to the
// current expiry (for an active block max(now, ExpiresAt) is the same thing),
// never restarting or shortening the window. The existing block keeps its IPs
// and firewall rules, so the hot path skips the DNS round-trip and the netsh
// re-apply entirely. When no active block exists it falls back to a fresh
// Block, making "extend" idempotent.
func (s *Scheduler) ExtendBlock(domain string, duration time.Duration) (*policy.Block, error) {
	s.mu.Lock()
	existing, ok := s.blocks[domain]
	if ok && existing.IsActive() {
		original := existing
		existing.Extend(duration)
		s.blocks[domain] = existing
		s.invalidateSnapshot()
		if err := s.store.Save(s.ramState()); err != nil {
			// Reverte a RAM: sem o disco persistido, a extensão não pode
			// ficar vigente — o timer antigo (expiração original) continuaria
			// armado e, ao disparar, onExpire veria o bloco ativo na RAM e
			// retornaria sem re-armar (zumbi que nunca expira). Restaura o
			// bloco original, consistente com o timer e o disco.
			s.blocks[domain] = original
			s.invalidateSnapshot()
			s.mu.Unlock()
			return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
		}
		s.setupTimerLocked(existing)
		s.mu.Unlock()
		s.notifyChange()
		return &existing, nil
	}
	s.mu.Unlock()
	return s.Block(domain, duration)
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

	// Resolve all domains in parallel with an individual timeout each, reusing
	// the DNS cache for domains resolved within the last dnsCacheTTL.
	resolved := refreshResolvedIPs(resolveEntries(unique), dnsRefreshTimeout, s.resolveBlockIPsCachedCtx)
	if len(resolved) != len(unique) {
		for _, d := range unique {
			if _, ok := resolved[d]; !ok {
				return nil, fmt.Errorf("scheduler: falha ao resolver %s", d)
			}
		}
	}

	now := time.Now()
	blocks := make([]policy.Block, 0, len(unique))
	for _, d := range unique {
		b := policy.Block{
			Domain:      d,
			StartedAt:   now,
			ExpiresAt:   now.Add(duration),
			ResolvedIPs: resolved[d],
		}
		blocks = append(blocks, b)
	}

	s.mu.Lock()
	wasEmpty := len(s.blocks) == 0
	for _, b := range blocks {
		s.blocks[b.Domain] = b
	}
	// O Sync do enforcer recebe TODOS os blocos ativos (pré-existentes + lote),
	// não só o lote: o Sync reescreve o hosts e varre regras órfãs com base no
	// conjunto que recebe — passar só o lote removeria a proteção (hosts +
	// firewall) dos domínios já bloqueados (ex.: preset/pomodoro/schedule
	// rodando por cima de um bloqueio manual) mesmo com eles ativos na RAM.
	allActive := make(map[string][]string, len(s.blocks))
	for domain, b := range s.blocks {
		if !b.CanUnblock() {
			allActive[domain] = b.ResolvedIPs
		}
	}
	s.invalidateSnapshot()
	if err := s.store.Save(s.ramState()); err != nil {
		// Reverte a RAM: sem o disco persistido, os domínios não podem ficar
		// ativos sem timer (estado zumbi) — o lote inteiro é descartado.
		for _, b := range blocks {
			delete(s.blocks, b.Domain)
		}
		s.invalidateSnapshot()
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}
	s.mu.Unlock()

	// A single batched Sync writes the hosts file once for the whole batch.
	if err := s.enforcer.Sync(allActive); err != nil {
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
		s.invalidateSnapshot()
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

	s.notifyChange()
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
	// dropped so one bad domain never kills the whole panic block). Reuses the
	// DNS cache like the single/batch block paths.
	var allowlistIPs []string
	if len(allowlistDomains) > 0 {
		resolved := refreshResolvedIPs(resolveEntries(dedupeDomains(allowlistDomains)), dnsRefreshTimeout, s.resolveBlockIPsCachedCtx)
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
		Allowlist:   dedupeDomains(allowlistDomains),
	}

	s.mu.Lock()
	wasEmpty := len(s.blocks) == 0
	s.blocks[enforcer.AllInternetDomain] = block
	s.invalidateSnapshot()
	if err := s.store.Save(s.ramState()); err != nil {
		delete(s.blocks, enforcer.AllInternetDomain)
		s.invalidateSnapshot()
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}
	s.mu.Unlock()

	if err := s.enforcer.BlockAll(block.ResolvedIPs); err != nil {
		// Reverte a RAM e o disco: um bloqueio que falhou não pode deixar o
		// sentinela ativo sem timer (estado zumbi).
		s.mu.Lock()
		delete(s.blocks, enforcer.AllInternetDomain)
		s.invalidateSnapshot()
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

	s.notifyChange()
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

	// Lê do snapshot imutável quando ele reflete o map (sem mutação pendente
	// e mesmo tamanho) — cópia O(n) da fatia, sem iterar o map. Se algo
	// divergiu (mutação não-publicada ou seed direto do map em testes),
	// reconstrói sob o RLock e publica; writers não intercalam aqui.
	sp := s.snapshot.Load()
	if sp != nil && !s.snapshotDirty.Load() && len(*sp) == len(s.blocks) {
		return append([]policy.Block(nil), (*sp)...), nil
	}

	list := make([]policy.Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		list = append(list, b)
	}
	s.snapshot.Store(&list)
	s.snapshotDirty.Store(false)
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
	s.invalidateSnapshot()
	delete(s.timers, domain)
	remaining := len(s.blocks)
	_ = s.store.Save(s.ramState())
	s.mu.Unlock()

	if remaining == 0 {
		// Varredura final (fix bug-hunt): o refresh periódico pode aplicar um
		// IP novo ao firewall na janela entre a checagem de atividade e o
		// apply — o UnblockDomain acima só remove os IPs conhecidos do bloco.
		// O Sync com conjunto vazio remove QUALQUER regra de domínio órfã que
		// tenha sobrado (IP novo do refresh) e reescreve o hosts limpo, antes
		// de desligar o DoH. Idempotente e best-effort (como o UnblockDoH).
		_ = s.enforcer.Sync(nil)
		_ = s.enforcer.UnblockDoH()
	}
	s.notifyChange()
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

// IsBlocked reports whether the DNS sinkhole server should answer a query for
// domain with a dead address. It walks up the parent domains (a block on
// example.com also covers www.example.com and a.b.example.com), ignores
// expired blocks, and treats an active all-internet sentinel as "block
// everything except the allowlisted domains". Matching is case-insensitive and
// the trailing dot is tolerated, so callers (the DNS server) may pass either
// the raw wire name or the already-normalized one.
func (s *Scheduler) IsBlocked(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	s.mu.RLock()
	defer s.mu.RUnlock()

	if sentinel, ok := s.blocks[enforcer.AllInternetDomain]; ok && sentinel.IsActive() {
		return !domainAllowlisted(sentinel.Allowlist, domain)
	}

	for labels := domain; labels != ""; {
		if b, ok := s.blocks[labels]; ok && b.IsActive() {
			return true
		}
		i := strings.IndexByte(labels, '.')
		if i < 0 {
			break
		}
		labels = labels[i+1:]
	}
	return false
}

// domainAllowlisted reports whether domain equals an allowlist entry or sits
// under one (allowlist "example.com" covers "api.example.com").
func domainAllowlisted(allowlist []string, domain string) bool {
	for _, a := range allowlist {
		a = strings.ToLower(a)
		if domain == a || strings.HasSuffix(domain, "."+a) {
			return true
		}
	}
	return false
}

// SetDNSEnabled persists whether the DNS sinkhole server should run. It only
// touches the state mirror — starting/stopping the actual listener is the
// daemon's job. Setting the same value is a no-op.
func (s *Scheduler) SetDNSEnabled(enabled bool) error {
	s.mu.Lock()
	if s.dnsEnabled == enabled {
		s.mu.Unlock()
		return nil
	}
	s.dnsEnabled = enabled
	if err := s.store.Save(s.ramState()); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	// Ajuste de configuração visível no status (DNSEnabled) — avisa o hub
	// (Fase 7) para a UI refrescar sem polling.
	s.notifyChange()
	return nil
}

// DNSEnabled reports whether the DNS sinkhole server should be running
// (persisted setting, not the live listener state).
func (s *Scheduler) DNSEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dnsEnabled
}

// SetDNSUpstream persists the upstream resolver (host:port) the DNS sinkhole
// forwards allowed queries to. It only touches the state mirror — applying the
// change to the live listener is the daemon's job (Controller.SetUpstream).
// Setting the same value is a no-op.
func (s *Scheduler) SetDNSUpstream(upstream string) error {
	s.mu.Lock()
	if s.dnsUpstream == upstream {
		s.mu.Unlock()
		return nil
	}
	s.dnsUpstream = upstream
	if err := s.store.Save(s.ramState()); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	// Ajuste de configuração visível no status (DNSUpstream) — avisa o hub
	// (Fase 7) para a UI refrescar sem polling.
	s.notifyChange()
	return nil
}

// DNSUpstream reports the persisted upstream resolver ("" = daemon default).
func (s *Scheduler) DNSUpstream() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dnsUpstream
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
