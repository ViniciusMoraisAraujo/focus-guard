// Package analytics records completed focus sessions and renders focus
// statistics (ASCII bar charts) for the "focusguard stats" command. Sessions
// are appended to a JSONL file (one JSON object per line) and aggregated on
// demand — the file is the source of truth for analytics (unlike state.json,
// which is a RAM mirror). Corrupt or partial lines are skipped on read, so a
// torn write never aborts the report.
package analytics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Session is one completed focus session, recorded by the pomodoro controller
// at the end (natural completion or stop).
type Session struct {
	Start   time.Time     `json:"start"`
	End     time.Time     `json:"end"`
	Preset  string        `json:"preset"`
	Domains []string      `json:"domains"`
	WorkMin int           `json:"work_min"`
	RestMin int           `json:"rest_min"`
	Cycles  int           `json:"cycles"`
	Focus   time.Duration `json:"focus"`
	Strict  bool          `json:"strict"`
}

// DayStat aggregates focus for a single calendar day.
type DayStat struct {
	Day      string        `json:"day"`
	Duration time.Duration `json:"duration"`
	Sessions int           `json:"sessions"`
}

// DomainStat aggregates focus attributed to a single blocked domain (each
// session's focus is attributed to every domain it blocked).
type DomainStat struct {
	Domain   string        `json:"domain"`
	Duration time.Duration `json:"duration"`
}

// Stats is the aggregated report returned by the "stats" IPC action.
type Stats struct {
	TotalSessions int           `json:"total_sessions"`
	TotalFocus    time.Duration `json:"total_focus"`
	PerDay        []DayStat     `json:"per_day"`
	PerDomain     []DomainStat  `json:"per_domain"`
	// Streak is the current run of consecutive days with focus.
	Streak int `json:"streak"`
}

// Recorder appends sessions to a JSONL file. An empty path keeps the recorder
// in memory (tests and daemon fallback).
type Recorder struct {
	mu     sync.Mutex
	path   string
	memory []Session
}

// NewRecorder returns a Recorder writing to path, or an in-memory recorder
// when path is empty.
func NewRecorder(path string) *Recorder {
	return &Recorder{path: path}
}

// Record appends one session to the file (or memory).
func (r *Recorder) Record(s Session) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.path == "" {
		r.memory = append(r.memory, s)
		return
	}

	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// Sessions returns every session read from the file (or memory), skipping
// corrupt lines. A missing file yields an empty list without error.
func (r *Recorder) Sessions() ([]Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.path == "" {
		return append([]Session(nil), r.memory...), nil
	}

	f, err := os.Open(r.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sessions []Session
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s Session
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue // linha corrompida/parcial — nunca aborta a leitura
		}
		sessions = append(sessions, s)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

// Summarize aggregates sessions into a Stats report: totals, a per-day window
// of the last `days` days (zero-filled, oldest first) and per-domain focus
// sorted by duration descending.
func Summarize(sessions []Session, days int, now time.Time) *Stats {
	st := &Stats{
		PerDay:    make([]DayStat, 0, days),
		PerDomain: []DomainStat{},
	}

	dayTotals := make(map[string]time.Duration)
	dayCounts := make(map[string]int)
	domainTotals := make(map[string]time.Duration)

	for _, s := range sessions {
		st.TotalSessions++
		st.TotalFocus += s.Focus

		day := s.Start.Format("2006-01-02")
		dayTotals[day] += s.Focus
		dayCounts[day]++

		for _, d := range s.Domains {
			domainTotals[d] += s.Focus
		}
	}

	for i := days - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		st.PerDay = append(st.PerDay, DayStat{
			Day:      day,
			Duration: dayTotals[day],
			Sessions: dayCounts[day],
		})
	}

	for d, dur := range domainTotals {
		st.PerDomain = append(st.PerDomain, DomainStat{Domain: d, Duration: dur})
	}
	sort.Slice(st.PerDomain, func(i, j int) bool {
		if st.PerDomain[i].Duration != st.PerDomain[j].Duration {
			return st.PerDomain[i].Duration > st.PerDomain[j].Duration
		}
		return st.PerDomain[i].Domain < st.PerDomain[j].Domain
	})

	st.Streak = ComputeStreak(sessions, now)
	return st
}

// ComputeStreak returns the number of consecutive days (ending today, or
// yesterday if today has no focus yet — a fresh day must not kill the streak
// before the user starts) with at least one recorded session.
func ComputeStreak(sessions []Session, now time.Time) int {
	focused := make(map[string]bool)
	for _, s := range sessions {
		focused[s.Start.Format("2006-01-02")] = true
	}

	streak := 0
	cursor := now
	if !focused[now.Format("2006-01-02")] {
		cursor = cursor.AddDate(0, 0, -1) // hoje ainda não conta — começa de ontem
	}
	for focused[cursor.Format("2006-01-02")] {
		streak++
		cursor = cursor.AddDate(0, 0, -1)
	}
	return streak
}

// ExportCSV renders the per-day window as comma-separated values with a
// header row, minutes as integers (machine-readable, spreadsheet-friendly).
func ExportCSV(st *Stats) string {
	var b strings.Builder
	b.WriteString("day,focus_minutes,sessions\n")
	for _, d := range st.PerDay {
		fmt.Fprintf(&b, "%s,%d,%d\n", d.Day, int(d.Duration/time.Minute), d.Sessions)
	}
	return b.String()
}

// ExportJSON marshals the whole report as indented JSON.
func ExportJSON(st *Stats) (string, error) {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RenderStats renders the report as an ASCII bar chart. The day with the most
// focus fills exactly barWidth block characters; days with no focus render no
// bar. Durations are rounded to the minute.
func RenderStats(st *Stats, barWidth int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "FocusGuard — Estatísticas de foco\n\n")
	fmt.Fprintf(&b, "Sessões registradas: %d\n", st.TotalSessions)
	fmt.Fprintf(&b, "Tempo total de foco: %s\n", st.TotalFocus.Round(time.Minute))
	if st.Streak > 0 {
		fmt.Fprintf(&b, "🔥 Raia de foco: %d dia(s) consecutivo(s)\n", st.Streak)
	}
	fmt.Fprintln(&b)

	fmt.Fprintf(&b, "Foco por dia (últimos %d dias):\n", len(st.PerDay))
	max := time.Duration(0)
	for _, d := range st.PerDay {
		if d.Duration > max {
			max = d.Duration
		}
	}
	for _, d := range st.PerDay {
		barLen := 0
		if max > 0 && d.Duration > 0 {
			barLen = int(float64(d.Duration)/float64(max)*float64(barWidth) + 0.5)
			if barLen < 1 {
				barLen = 1
			}
		}
		fmt.Fprintf(&b, "%s  %s  %s\n", d.Day, strings.Repeat("█", barLen), d.Duration.Round(time.Minute))
	}

	if len(st.PerDomain) > 0 {
		fmt.Fprintf(&b, "\nDomínios mais bloqueados:\n")
		for _, d := range st.PerDomain {
			fmt.Fprintf(&b, "%s  %s\n", d.Domain, d.Duration.Round(time.Minute))
		}
	}

	return b.String()
}
