package scheduler

import (
	"fmt"
	"sync"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/policy"
	"focusguard/internal/store"
)

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
			return fmt.Errorf("scheduler: erro ao atualizar estado pós-boot: %w", err)
		}
	}

	return s.enforcer.Sync(activeIPs)
}

func (s *Scheduler) Block(domain string, duration time.Duration) (*policy.Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ips, err := enforcer.ResolveIPs(domain)
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
