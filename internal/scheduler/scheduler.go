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

type unblockEntry struct {
	domain string
	ips    []string
}

type refreshEntry struct {
	domain string
	ips    []string
}

func (s *Scheduler) Reconcile() error {
	s.mu.Lock()

	state, err := s.store.Load()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("scheduler: erro ao carregar estado: %w", err)
	}

	var toUnblock []unblockEntry
	activeIPs := make(map[string][]string)
	updated := false

	for domain, block := range state.Blocks {
		if block.CanUnblock() {
			toUnblock = append(toUnblock, unblockEntry{domain, block.ResolvedIPs})
			delete(state.Blocks, domain)
			updated = true
		} else {
			activeIPs[domain] = block.ResolvedIPs
			s.setupTimerLocked(block)
		}
	}

	if updated {
		if err := s.store.Save(state); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("scheduler: erro ao atualizar estado pós-reconciliação: %w", err)
		}
	}
	s.mu.Unlock()

	// I/O pesado (netsh) FORA do mutex — não trava o scheduler
	for _, e := range toUnblock {
		_ = s.enforcer.UnblockDomain(e.domain, e.ips)
	}
	if len(activeIPs) > 0 {
		if err := s.enforcer.Sync(activeIPs); err != nil {
			return err
		}
		// Se há bloqueios ativos, garante proteção DoH/DoT
		_ = s.enforcer.BlockDoH()
	} else {
		// Sem bloqueios ativos, desativa proteção DoH/DoT
		_ = s.enforcer.UnblockDoH()
	}

	return nil
}

func (s *Scheduler) startPeriodicIPRefresh(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		// Step 1: collect active domains (lock rápido, sem I/O pesado)
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

		// Step 2: resolver DNS para cada domínio (FORA do lock)
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

		// Step 3: persist no store (lock rápido) + aplicar firewall (FORA do lock)
		s.mu.Lock()
		state, err = s.store.Load()
		if err != nil {
			s.mu.Unlock()
			continue
		}
		var toRefresh []refreshEntry
		for domain, ips := range refreshed {
			if block, exists := state.Blocks[domain]; exists && block.IsActive() {
				block.ResolvedIPs = ips
				state.Blocks[domain] = block
				toRefresh = append(toRefresh, refreshEntry{domain, ips})
			}
		}
		_ = s.store.Save(state)
		s.mu.Unlock()

		// I/O pesado FORA do mutex
		for _, e := range toRefresh {
			_ = s.enforcer.BlockDomain(e.domain, e.ips)
		}
	}
}

func (s *Scheduler) Block(domain string, duration time.Duration) (*policy.Block, error) {
	ips, err := resolveFunc(domain)
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

	// Lock rápido: só para operações de store/memória
	s.mu.Lock()
	state, err := s.store.Load()
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao carregar estado: %w", err)
	}
	wasEmpty := len(state.Blocks) == 0
	state.Blocks[domain] = block
	if err := s.store.Save(state); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("scheduler: erro ao salvar estado: %w", err)
	}
	s.mu.Unlock()

	// I/O pesado (netsh firewall) FORA do mutex — não bloqueia o scheduler
	if err := s.enforcer.BlockDomain(domain, ips); err != nil {
		return nil, fmt.Errorf("scheduler: erro ao aplicar bloqueio: %w", err)
	}

	// Primeiro bloqueio? Ativa proteção DoH/DoT
	if wasEmpty {
		_ = s.enforcer.BlockDoH()
	}

	// Lock rápido para configurar o timer de expiração
	s.mu.Lock()
	s.setupTimerLocked(block)
	s.mu.Unlock()

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
	// Lock rápido: só para ler/atualizar o store
	s.mu.Lock()
	state, err := s.store.Load()
	if err != nil {
		s.mu.Unlock()
		return
	}

	block, exists := state.Blocks[domain]
	if !exists || !block.CanUnblock() {
		s.mu.Unlock()
		return
	}

	ips := block.ResolvedIPs
	delete(state.Blocks, domain)
	remaining := len(state.Blocks)
	_ = s.store.Save(state)
	delete(s.timers, domain)
	s.mu.Unlock()

	// I/O pesado (netsh) FORA do mutex
	_ = s.enforcer.UnblockDomain(domain, ips)

	// Último bloqueio removido? Desativa proteção DoH/DoT
	if remaining == 0 {
		_ = s.enforcer.UnblockDoH()
	}
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
