package scheduler

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/policy"
	"focusguard/internal/store"
)

var resolveFunc = enforcer.ResolveIPs

// resolveBlockIPs resolves a domain and its www subdomain, merging both into a
// single de-duplicated list. This keeps the enforcer free of its own DNS
// lookups (G2) while preserving firewall coverage for the www addresses that
// the previous collectAllIPs provided.
func resolveBlockIPs(domain string) ([]string, error) {
	ips, err := resolveFunc(domain)
	if err != nil {
		return nil, err
	}

	wwwIPs, wwwErr := resolveFunc("www." + domain)
	if wwwErr != nil {
		return ips, nil
	}

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
	return ips, nil
}

// Scheduler treats the in-memory blocks map as the source of truth. The
// state.json file on disk is only a mirror: it is read once at bootstrap to
// restore the RAM after a daemon restart, and afterwards any divergence from
// the in-memory state is treated as external tampering and the disk copy is
// overwritten to match the RAM.
type Scheduler struct {
	mu           sync.Mutex
	store        *store.Store
	enforcer     enforcer.Enforcer
	blocks       map[string]policy.Block
	timers       map[string]*time.Timer
	bootstrapped bool
	refreshStop  chan struct{}
}

func NewScheduler(st *store.Store, enf enforcer.Enforcer) *Scheduler {
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
	}

	// The disk is only a mirror: if it diverges from RAM (edited, emptied,
	// deleted or corrupted), it is overwritten with the in-memory state.
	if disk, err := s.store.Load(); err != nil || !statesEqual(disk, s.ramState()) {
		changed = true
	}

	var toUnblock []unblockEntry
	activeIPs := make(map[string][]string, len(s.blocks))
	for domain, block := range s.blocks {
		if block.CanUnblock() {
			toUnblock = append(toUnblock, unblockEntry{domain, block.ResolvedIPs})
			delete(s.blocks, domain)
			if t, ok := s.timers[domain]; ok {
				t.Stop()
				delete(s.timers, domain)
			}
			changed = true
		} else {
			activeIPs[domain] = block.ResolvedIPs
			s.setupTimerLocked(block)
		}
	}

	if changed {
		if err := s.store.Save(s.ramState()); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("scheduler: erro ao restaurar/salvar estado: %w", err)
		}
	}
	s.mu.Unlock()

	if !changed {
		return nil
	}

	for _, e := range toUnblock {
		_ = s.enforcer.UnblockDomain(e.domain, e.ips)
	}
	if len(activeIPs) > 0 {
		if err := s.enforcer.Sync(activeIPs); err != nil {
			return err
		}
		_ = s.enforcer.BlockDoH()
	} else {
		_ = s.enforcer.UnblockDoH()
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
			if block.IsActive() {
				entries = append(entries, refreshEntry{domain: domain, ips: block.ResolvedIPs})
			}
		}
		s.mu.Unlock()

		if len(entries) == 0 {
			continue
		}

		refreshed := make(map[string][]string)
		for _, entry := range entries {
			newIPs, err := resolveFunc(entry.domain)
			if err != nil || len(newIPs) == 0 {
				continue
			}

			ipMap := make(map[string]bool, len(entry.ips)+len(newIPs))
			for _, ip := range entry.ips {
				ipMap[ip] = true
			}

			var hasNewIP bool
			for _, ip := range newIPs {
				if !ipMap[ip] {
					entry.ips = append(entry.ips, ip)
					ipMap[ip] = true
					hasNewIP = true
				}
			}

			if hasNewIP {
				refreshed[entry.domain] = entry.ips
			}
		}

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

func (s *Scheduler) ListBlocks() ([]policy.Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *Scheduler) onExpire(domain string) {
	s.mu.Lock()
	block, exists := s.blocks[domain]
	if !exists || !block.CanUnblock() {
		s.mu.Unlock()
		return
	}

	ips := block.ResolvedIPs
	delete(s.blocks, domain)
	delete(s.timers, domain)
	remaining := len(s.blocks)
	_ = s.store.Save(s.ramState())
	s.mu.Unlock()

	_ = s.enforcer.UnblockDomain(domain, ips)

	if remaining == 0 {
		_ = s.enforcer.UnblockDoH()
	}
}

func (s *Scheduler) HasActiveBlocks() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	s.mu.Lock()
	defer s.mu.Unlock()

	return nil
}
