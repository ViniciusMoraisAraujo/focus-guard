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

	"focusguard/internal/domain/policy"
)

// Rule is a recurring block window. Days are time.Weekday (0=Sunday).
// Start/End are "HH:MM" 24h. End <= Start means the window crosses midnight
// (e.g. 22:00 → 02:00). Enabled rules are evaluated by ActiveRules.
type Rule struct {
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
	Preset string `json:"preset"`
	Days   []int  `json:"days"`
	Start  string `json:"start"`
	End    string `json:"end"`
	// Windows lists optional "HH:MM-HH:MM" time windows (multiple per rule,
	// e.g. "08:00-12:00,14:00-18:00"). When empty the rule falls back to the
	// legacy single Start/End window.
	Windows []string `json:"windows,omitempty"`
	Enabled bool     `json:"enabled"`
}

// Manager owns the rule catalog with file persistence.
type Manager struct {
	mu    sync.Mutex
	path  string
	rules []Rule
	// onChange is a coarse "catalog changed" hook (Fase 7): the daemon wires it
	// to publish schedule-changed on the event hub, so the web UI refreshes the
	// agenda without polling. Called after every successful mutation
	// (Add/Remove/Import) WITHOUT holding m.mu.
	onChange func()
}

// SetOnChange registers a callback invoked (without the manager lock) after
// every successful catalog mutation: add, remove and import. Nil disables it.
// The callback must not call back into the manager.
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// notifyChange fires the change hook outside the lock (safe to call anywhere
// the caller does not hold m.mu).
func (m *Manager) notifyChange() {
	m.mu.Lock()
	fn := m.onChange
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
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
	if err := validateRule(r); err != nil {
		m.mu.Unlock()
		return Rule{}, err
	}

	r.ID = newID()
	r.Label = strings.TrimSpace(r.Label)
	m.rules = append(m.rules, cloneRule(r))
	if err := m.save(); err != nil {
		m.mu.Unlock()
		return Rule{}, err
	}
	m.mu.Unlock()

	m.notifyChange()
	return cloneRule(r), nil
}

// validateRule checks the fields every persisted rule must satisfy: a preset,
// at least one weekday and a valid time window (start != end; an always-on
// window would re-block forever). Shared by Add and ImportICS.
func validateRule(r Rule) error {
	if strings.TrimSpace(r.Preset) == "" {
		return fmt.Errorf("schedule: informe um preset")
	}
	if len(r.Days) == 0 {
		return fmt.Errorf("schedule: informe ao menos um dia da semana")
	}
	for _, d := range r.Days {
		if d < 0 || d > 6 {
			return fmt.Errorf("schedule: dia inválido %d (use 0-6, 0=domingo)", d)
		}
	}
	if len(r.Windows) > 0 {
		if _, err := windowsPairs(r); err != nil {
			return err
		}
	} else {
		start, err := parseClock(r.Start)
		if err != nil {
			return fmt.Errorf("schedule: start inválido %q", r.Start)
		}
		end, err := parseClock(r.End)
		if err != nil {
			return fmt.Errorf("schedule: end inválido %q", r.End)
		}
		if start == end {
			return fmt.Errorf("schedule: end deve ser diferente do start")
		}
	}
	return nil
}

// Remove deletes the rule with the given ID.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	for i, r := range m.rules {
		if r.ID == id {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			if err := m.save(); err != nil {
				m.mu.Unlock()
				return err
			}
			m.mu.Unlock()
			m.notifyChange()
			return nil
		}
	}
	m.mu.Unlock()
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

// windowsPairs returns the rule's time windows as (start, end) minutes-since-
// midnight pairs. When Windows is empty it falls back to the legacy Start/End
// window (backward compatibility with schedules created before --windows).
func windowsPairs(r Rule) ([][2]int, error) {
	if len(r.Windows) == 0 {
		start, err1 := parseClock(r.Start)
		end, err2 := parseClock(r.End)
		if err1 != nil || err2 != nil || start == end {
			return nil, fmt.Errorf("schedule: janela inválida %q-%q", r.Start, r.End)
		}
		return [][2]int{{start, end}}, nil
	}
	pairs := make([][2]int, 0, len(r.Windows))
	for _, w := range r.Windows {
		parts := strings.SplitN(strings.TrimSpace(w), "-", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("schedule: janela inválida %q (use HH:MM-HH:MM)", w)
		}
		start, err := parseClock(parts[0])
		if err != nil {
			return nil, fmt.Errorf("schedule: janela inválida %q", w)
		}
		end, err := parseClock(parts[1])
		if err != nil {
			return nil, fmt.Errorf("schedule: janela inválida %q", w)
		}
		if start == end {
			return nil, fmt.Errorf("schedule: end deve ser diferente do start na janela %q", w)
		}
		pairs = append(pairs, [2]int{start, end})
	}
	return pairs, nil
}

// ruleActive reports whether any of the rule's windows is active at now.
func ruleActive(r Rule, now time.Time) bool {
	if !r.Enabled {
		return false
	}
	pairs, err := windowsPairs(r)
	if err != nil {
		return false
	}
	cur := minutesOf(now)
	for _, p := range pairs {
		if windowActive(p[0], p[1], cur, r.Days, now) {
			return true
		}
	}
	return false
}

// windowActive reports whether now is inside the (start,end) window, honoring
// the rule's days — including overnight windows (start > end).
func windowActive(start, end, cur int, days []int, now time.Time) bool {
	switch {
	case start < end: // janela no mesmo dia
		return hasDay(days, int(now.Weekday())) && cur >= start && cur < end
	case cur >= start: // virada de noite: daqui até a meia-noite (hoje é dia da regra)
		return hasDay(days, int(now.Weekday()))
	default: // virada de noite: 00:00 → end pertence ao dia seguinte
		prev := now.AddDate(0, 0, -1)
		return hasDay(days, int(prev.Weekday())) && cur < end
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
	r.Windows = append([]string(nil), r.Windows...)
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
// the fresh block): the remaining time of the current window. For overnight
// windows it spans until end on the next day. With multiple windows it returns
// the longest remaining among the active ones (normally just one).
func remainingUntil(r Rule, now time.Time) time.Duration {
	pairs, err := windowsPairs(r)
	if err != nil {
		return 0
	}
	cur := minutesOf(now)
	best := 0
	for _, p := range pairs {
		if !windowActive(p[0], p[1], cur, r.Days, now) {
			continue
		}
		var rem int
		if p[0] < p[1] {
			rem = p[1] - cur
		} else if cur >= p[0] {
			rem = (24*60 - cur) + p[1]
		} else {
			rem = p[1] - cur
		}
		if rem < 0 {
			rem = 0
		}
		if rem > best {
			best = rem
		}
	}
	return time.Duration(best) * time.Minute
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
