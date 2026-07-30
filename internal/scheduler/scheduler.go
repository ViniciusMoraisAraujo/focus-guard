package scheduler

import (
	"fmt"
	"sync"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/policy"
	"focusguard/internal/store"
)

var resolveFunc = enforcer.ResolveIPs

type Scheduler struct {
	mu       sync.Mutex
	store    *store.Store
	enforcer enforcer.Enforcer
	timers   map[string]*time.Timer
}

func NewScheduler(st *store.Store, enf enforcer.Enforcer) *Scheduler {
	return &Scheduler{
		store:    st,
		enforcer: enf,
		timers:   make(map[string]*time.Timer),
	}
}

func (s *Scheduler) Start() error {
	if err := s.Reconcile(); err != nil {
		return err
	}

	go s.startPeriodicIPRefresh(15 * time.Minute)

	return nil
}

func (s *Scheduler) Reconcile() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.store.Load()
	if err != nil {
		return fmt.Errorf("scheduler: erro ao carregar estado: %w", err)
	}

	activeIPs := make(map[string][]string)
	updated := false

	for domain, block := range state.Blocks {
		if block.CanUnblock() {
			_ = s.enforcer.UnblockDomain(domain, block.ResolvedIPs)
			delete(state.Blocks, domain)
			updated = true
		} else {
			activeIPs[domain] = block.ResolvedIPs
			s.setupTimerLocked(block)
		}
	}

	if updated {
		if err := s.store.Save(state); err != nil {
			return fmt.Errorf("scheduler: erro ao atualizar estado pós-reconciliação: %w", err)
		}
	}

	if err := s.enforcer.Sync(activeIPs); err != nil {
		return err
	}

	return nil
}

func (s *Scheduler) startPeriodicIPRefresh(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		type refreshEntry struct {
			domain string
			ips    []string
		}

		// Step 1: collect active domains (quick lock, no DNS)
		var entries []refreshEntry
		s.mu.Lock()
		state, err := s.store.Load()
		if err != nil {
			s.mu.Unlock()
			continue
		}
		for domain, block := range state.Blocks {
			if block.IsActive() {
				entries = append(entries, refreshEntry{domain: domain, ips: block.ResolvedIPs})
			}
		}
		s.mu.Unlock()

		if len(entries) == 0 {
			continue
		}

		// Step 2: resolve DNS for each domain (outside lock)
		refreshed := make(map[string][]string)
		for _, entry := range entries {
			newIPs, err := resolveFunc(entry.domain)
			if err != nil || len(newIPs) == 0 {
				continue
			}

			ipMap := make(map[string]bool)
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

		// Step 3: persist updates (quick lock)
		s.mu.Lock()
		state, err = s.store.Load()
		if err != nil {
			s.mu.Unlock()
			continue
		}
		for domain, ips := range refreshed {
			if block, exists := state.Blocks[domain]; exists && block.IsActive() {
				block.ResolvedIPs = ips
				state.Blocks[domain] = block
				_ = s.enforcer.BlockDomain(domain, ips)
			}
		}
		_ = s.store.Save(state)
		s.mu.Unlock()
	}
}

func (s *Scheduler) Block(domain string, duration time.Duration) (*policy.Block, error) {
	ips, err := resolveFunc(domain)
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	block := policy.Block{
		Domain:      domain,
		StartedAt:   now,
		ExpiresAt:   now.Add(duration),
		ResolvedIPs: ips,
	}

	state, err := s.store.Load()
	if err != nil {
		return nil, fmt.Errorf("scheduler: erro ao carregar estado: %w", err)
	}

	state.Blocks[domain] = block
	if err := s.store.Save(state); err != nil {
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}

	if err := s.enforcer.BlockDomain(domain, ips); err != nil {
		return nil, fmt.Errorf("scheduler: erro ao aplicar bloqueio: %w", err)
	}

	s.setupTimerLocked(block)
	return &block, nil
}

func (s *Scheduler) ListBlocks() ([]policy.Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}

	var list []policy.Block
	for _, b := range state.Blocks {
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
	defer s.mu.Unlock()

	state, err := s.store.Load()
	if err != nil {
		return
	}

	block, exists := state.Blocks[domain]
	if !exists {
		return
	}

	if !block.CanUnblock() {
		return
	}

	_ = s.enforcer.UnblockDomain(domain, block.ResolvedIPs)
	delete(state.Blocks, domain)
	_ = s.store.Save(state)
	delete(s.timers, domain)
}

func (s *Scheduler) HasActiveBlocks() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.store.Load()
	if err != nil {
		return false
	}

	for _, block := range state.Blocks {
		if block.IsActive() {
			return true
		}
	}
	return false
}

func (s *Scheduler) Ping() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return nil
}
