// Package metrics is a tiny stdlib-only latency registry per action (Fase 8 —
// C3 do refactor-plan): the daemon records each IPC dispatch and the web proxy
// records each proxied HTTP action, so a perf regression from a refactor shows
// up as a latency jump instead of a surprise. The registry is intentionally
// small (no external deps, no exporters): consumers read the snapshot through
// the IPC action "metrics" (CLI `focusguard metrics`) or the web endpoint
// GET /api/metrics.
//
// Count/total/avg/max accumulate for the process lifetime; p50/p95/p99 are
// computed over a bounded recent window (a ring of the last samples per
// action), so percentiles stay cheap and reflect current behavior.
package metrics

import (
	"sort"
	"sync"
	"time"
)

// ActionStat is the latency aggregate of one action (IPC or HTTP proxy).
// Durations serialize as nanoseconds (the IPC contract maps time.Duration to
// number) and are rendered as ms by the CLI.
type ActionStat struct {
	Action string        `json:"action"`
	Count  int64         `json:"count"`
	Total  time.Duration `json:"total"`
	Avg    time.Duration `json:"avg"`
	Max    time.Duration `json:"max"`
	P50    time.Duration `json:"p50"`
	P95    time.Duration `json:"p95"`
	P99    time.Duration `json:"p99"`
	Last   time.Duration `json:"last"`
}

type entry struct {
	samples []time.Duration // ring dos últimos cap tempos
	head    int
	count   int64 // total de chamadas (sem teto)
	total   time.Duration
	max     time.Duration
	last    time.Duration
}

// Registry holds per-action latency samples. Thread-safe; Record is cheap
// (mutex + ring write) and never blocks for long.
type Registry struct {
	mu      sync.Mutex
	actions map[string]*entry
	cap     int
}

// New returns a registry that keeps the last samplesPerAction durations per
// action for the percentile window.
func New(samplesPerAction int) *Registry {
	if samplesPerAction < 2 {
		samplesPerAction = 2
	}
	return &Registry{
		actions: make(map[string]*entry),
		cap:     samplesPerAction,
	}
}

// Record appends one duration for action.
func (r *Registry) Record(action string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.actions[action]
	if e == nil {
		e = &entry{samples: make([]time.Duration, r.cap)}
		r.actions[action] = e
	}
	e.samples[e.head] = d
	e.head = (e.head + 1) % r.cap
	e.count++
	e.total += d
	if d > e.max {
		e.max = d
	}
	e.last = d
}

// Reset clears every recorded action (the CLI `metrics --reset` marks the
// start of a measurement window).
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = make(map[string]*entry)
}

// Snapshot returns one ActionStat per recorded action, sorted by action name.
// Percentiles come from the recent ring; count/total/avg/max are lifetime.
func (r *Registry) Snapshot() []ActionStat {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ActionStat, 0, len(r.actions))
	for name, e := range r.actions {
		out = append(out, e.stat(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Action < out[j].Action })
	return out
}

// stat builds the aggregate for entry e. The caller holds r.mu.
func (e *entry) stat(name string) ActionStat {
	st := ActionStat{
		Action: name,
		Count:  e.count,
		Total:  e.total,
		Max:    e.max,
		Last:   e.last,
	}
	if e.count > 0 {
		st.Avg = time.Duration(int64(e.total) / e.count)
	}
	// Percentis sobre os últimos samples do ring (janela recente).
	n := int64(len(e.samples))
	if e.count < n {
		n = e.count
	}
	if n == 0 {
		return st
	}
	vals := make([]time.Duration, n)
	start := (e.head - int(n) + len(e.samples)) % len(e.samples)
	for i := int64(0); i < n; i++ {
		vals[i] = e.samples[(start+int(i))%len(e.samples)]
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	st.P50 = percentile(vals, 50)
	st.P95 = percentile(vals, 95)
	st.P99 = percentile(vals, 99)
	return st
}

// percentile returns the p-th percentile of the sorted slice (nearest rank).
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p*len(sorted) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}
