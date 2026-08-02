// Package schedule provides recurring block rules ("block social Mon-Fri
// 08:00-12:00") evaluated against the clock and applied through the scheduler.
// Rules are persisted in a JSON file next to the state; the built-in policy
// stays untouched (blocks still expire on their own — schedules simply create
// fresh blocks whenever a rule window is active and the domains are free).
package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"focusguard/internal/policy"
)

// Rule is a recurring block window. Days are time.Weekday (0=Sunday).
// Start/End are "HH:MM" 24h. End <= Start means the window crosses midnight
// (e.g. 22:00 → 02:00). Enabled rules are evaluated by ActiveRules.
type Rule struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	Preset  string `json:"preset"`
	Days    []int  `json:"days"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Enabled bool   `json:"enabled"`
}

// Manager owns the rule catalog with file persistence.
type Manager struct {
	mu    sync.Mutex
	path  string
	rules []Rule
}

// NewManager loads (or initializes) a Manager backed by path. A missing or
// corrupt file yields an empty catalog — never an error at startup.
func NewManager(path string) *Manager {
	m := &Manager{path: path}
	m.load()
	return m
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return
	}
	m.rules = rules
}

func (m *Manager) save() error {
	if m.path == "" {
		return nil // em memória (testes)
	}
	data, err := json.MarshalIndent(m.rules, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), "schedules-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), m.path)
}

// List returns a copy of the catalog.
func (m *Manager) List() []Rule {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Rule, len(m.rules))
	for i, r := range m.rules {
		out[i] = cloneRule(r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Add validates and persists a new rule, generating a short ID. Validation
// covers preset, days (0-6), "HH:MM" bounds and end != start (an always-on
// window would re-block forever).
func (m *Manager) Add(r Rule) (Rule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(r.Preset) == "" {
		return Rule{}, fmt.Errorf("schedule: informe um preset")
	}
	if len(r.Days) == 0 {
		return Rule{}, fmt.Errorf("schedule: informe ao menos um dia da semana")
	}
	for _, d := range r.Days {
		if d < 0 || d > 6 {
			return Rule{}, fmt.Errorf("schedule: dia inválido %d (use 0-6, 0=domingo)", d)
		}
	}
	start, err := parseClock(r.Start)
	if err != nil {
		return Rule{}, fmt.Errorf("schedule: start inválido %q", r.Start)
	}
	end, err := parseClock(r.End)
	if err != nil {
		return Rule{}, fmt.Errorf("schedule: end inválido %q", r.End)
	}
	if start == end {
		return Rule{}, fmt.Errorf("schedule: end deve ser diferente do start")
	}

	r.ID = newID()
	r.Label = strings.TrimSpace(r.Label)
	m.rules = append(m.rules, cloneRule(r))
	if err := m.save(); err != nil {
		return Rule{}, err
	}
	return cloneRule(r), nil
}

// Remove deletes the rule with the given ID.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, r := range m.rules {
		if r.ID == id {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return m.save()
		}
	}
	return fmt.Errorf("schedule: regra %q não encontrada", id)
}

// ActiveRules returns the enabled rules whose window covers now (including
// overnight windows that cross midnight).
func (m *Manager) ActiveRules(now time.Time) []Rule {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []Rule
	for _, r := range m.rules {
		if ruleActive(r, now) {
			out = append(out, cloneRule(r))
		}
	}
	return out
}

// ruleActive reports whether the rule window is active at now.
func ruleActive(r Rule, now time.Time) bool {
	if !r.Enabled {
		return false
	}
	start, err1 := parseClock(r.Start)
	end, err2 := parseClock(r.End)
	if err1 != nil || err2 != nil || start == end {
		return false
	}

	cur := minutesOf(now)
	switch {
	case start < end: // janela no mesmo dia
		return hasDay(r.Days, int(now.Weekday())) && cur >= start && cur < end
	case cur >= start: // virada de noite: daqui até a meia-noite (hoje é dia da regra)
		return hasDay(r.Days, int(now.Weekday()))
	default: // virada de noite: 00:00 → end pertence ao dia seguinte
		prev := now.AddDate(0, 0, -1)
		return hasDay(r.Days, int(prev.Weekday())) && cur < end
	}
}

func hasDay(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

func cloneRule(r Rule) Rule {
	r.Days = append([]int(nil), r.Days...)
	return r
}

// parseClock parses "HH:MM" into minutes since midnight.
func parseClock(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("formato deve ser HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("hora inválida %q", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("minuto inválido %q", parts[1])
	}
	return h*60 + m, nil
}

func minutesOf(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// remainingUntil returns how long the rule stays active from now (used to size
// the fresh block). For overnight windows it spans until end on the next day.
func remainingUntil(r Rule, now time.Time) time.Duration {
	start, err1 := parseClock(r.Start)
	end, err2 := parseClock(r.End)
	if err1 != nil || err2 != nil || start == end {
		return 0
	}
	cur := minutesOf(now)
	var rem int
	if start < end {
		rem = end - cur
	} else if cur >= start {
		rem = (24*60 - cur) + end
	} else {
		rem = end - cur
	}
	if rem < 0 {
		rem = 0
	}
	return time.Duration(rem) * time.Minute
}

func newID() string {
	// 8 caracteres hex aleatórios baseados em nanosegundos (suficiente para
	// o volume doméstico do produto; sem dependências novas).
	const hex = "0123456789abcdef"
	var sb strings.Builder
	n := time.Now().UnixNano()
	for i := 0; i < 8; i++ {
		sb.WriteByte(hex[n&0xf])
		n >>= 4
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Worker — aplica as regras vencidas através do scheduler
// ---------------------------------------------------------------------------

// Resolver maps a preset name to its domains (satisfied by *preset.Store).
type Resolver interface {
	Resolve(name string) ([]string, error)
}

// Blocker is the scheduler surface the worker needs. The daemon's scheduler
// satisfies it via ListBlocks + BlockDomains.
type Blocker interface {
	ListBlocks() ([]policy.Block, error)
	BlockDomains(domains []string, duration time.Duration) ([]policy.Block, error)
}

// ApplyActiveRules blocks the domains of every rule active at now whose preset
// domains are not already blocked. Deduplicates across rules (a preset covered
// by two rules is applied once). Returns the number of rules applied.
func ApplyActiveRules(m *Manager, resolver Resolver, b Blocker, now time.Time) (int, error) {
	active := m.ActiveRules(now)
	if len(active) == 0 {
		return 0, nil
	}

	existing, err := b.ListBlocks()
	if err != nil {
		return 0, fmt.Errorf("schedule: listar bloqueios: %w", err)
	}
	already := make(map[string]bool, len(existing))
	for _, blk := range existing {
		already[blk.Domain] = true
	}

	// agrega domínios por regra; regras com mesmo preset compartilham domínios
	seenRules := make(map[string]bool) // presets já contabilizados nesta rodada
	applied := 0
	for _, r := range active {
		if seenRules[r.Preset] {
			continue
		}
		seenRules[r.Preset] = true

		domains, err := resolver.Resolve(r.Preset)
		if err != nil {
			continue // preset removido entre a criação e a execução
		}
		var fresh []string
		for _, d := range domains {
			if !already[d] {
				fresh = append(fresh, d)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		rem := remainingUntil(r, now)
		if rem <= 0 {
			continue
		}
		if _, err := b.BlockDomains(fresh, rem); err != nil {
			return applied, fmt.Errorf("schedule: aplicar bloqueio: %w", err)
		}
		for _, d := range fresh {
			already[d] = true
		}
		applied++
	}
	return applied, nil
}
