package analytics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func testSession(start time.Time, end time.Time, preset string, focus time.Duration) Session {
	return Session{
		Start:   start,
		End:     end,
		Preset:  preset,
		Domains: []string{"twitter.com", "facebook.com"},
		WorkMin: 25,
		RestMin: 5,
		Cycles:  4,
		Focus:   focus,
		Strict:  false,
	}
}

// TestRecentSessions_NewestFirst verifies RecentSessions orders by End
// descending, regardless of the input order, and does not mutate the input.
func TestRecentSessions_NewestFirst(t *testing.T) {
	now := time.Now()
	old := testSession(now.Add(-3*time.Hour), now.Add(-2*time.Hour), "social", time.Hour)
	mid := testSession(now.Add(-2*time.Hour), now.Add(-time.Hour), "video", 30*time.Minute)
	newest := testSession(now.Add(-time.Hour), now, "news", 45*time.Minute)

	input := []Session{mid, old, newest}
	got := RecentSessions(input, 10)

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Preset != "news" || got[1].Preset != "video" || got[2].Preset != "social" {
		t.Errorf("order = %q,%q,%q, want news,video,social", got[0].Preset, got[1].Preset, got[2].Preset)
	}
	if len(input) != 3 {
		t.Error("input slice was mutated")
	}
}

// TestRecentSessions_RespectsLimit verifies the result is capped at limit.
func TestRecentSessions_RespectsLimit(t *testing.T) {
	now := time.Now()
	var sessions []Session
	for i := 0; i < 10; i++ {
		sessions = append(sessions, testSession(now.Add(-time.Duration(i)*time.Hour), now.Add(-time.Duration(i-1)*time.Hour), "social", time.Hour))
	}

	got := RecentSessions(sessions, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].End.Before(got[1].End) || got[1].End.Before(got[2].End) {
		t.Error("result should be ordered newest first")
	}
}

func TestRecentSessions_EmptyAndNonPositiveLimit(t *testing.T) {
	if got := RecentSessions(nil, 10); len(got) != 0 {
		t.Errorf("empty input → len %d, want 0", len(got))
	}
	now := time.Now()
	s := []Session{testSession(now, now, "social", time.Minute)}
	if got := RecentSessions(s, 0); len(got) != 0 {
		t.Errorf("limit 0 → len %d, want 0", len(got))
	}
}

// TestSummarize_Totals verifies the aggregate counters: number of sessions and
// the total focus time across them.
func TestSummarize_Totals(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		testSession(now.Add(-2*time.Hour), now.Add(-1*time.Hour), "social", time.Hour),
		testSession(now.Add(-3*time.Hour), now.Add(-2*time.Hour), "video", 30*time.Minute),
	}

	st := Summarize(sessions, 7, now)

	if st.TotalSessions != 2 {
		t.Errorf("TotalSessions = %d, want 2", st.TotalSessions)
	}
	if st.TotalFocus != 90*time.Minute {
		t.Errorf("TotalFocus = %v, want 90m", st.TotalFocus)
	}
}

// TestSummarize_PerDay verifies the per-day breakdown is zero-filled for days
// without sessions and ordered oldest-first.
func TestSummarize_PerDay(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		testSession(now.Add(-48*time.Hour), now.Add(-47*time.Hour), "social", time.Hour),
	}

	st := Summarize(sessions, 3, now)

	if len(st.PerDay) != 3 {
		t.Fatalf("len(PerDay) = %d, want 3", len(st.PerDay))
	}
	if st.PerDay[0].Day != "2026-08-01" || st.PerDay[0].Duration != time.Hour {
		t.Errorf("PerDay[0] = %+v, want 2026-08-01 with 1h", st.PerDay[0])
	}
	if st.PerDay[1].Day != "2026-08-02" || st.PerDay[1].Duration != 0 {
		t.Errorf("PerDay[1] = %+v, want 2026-08-02 zero-filled", st.PerDay[1])
	}
	if st.PerDay[2].Day != "2026-08-03" {
		t.Errorf("PerDay[2].Day = %q, want 2026-08-03", st.PerDay[2].Day)
	}
}

// TestSummarize_PerDomain verifies per-domain focus is aggregated across
// sessions and sorted by duration descending.
func TestSummarize_PerDomain(t *testing.T) {
	now := time.Now()
	social := Session{
		Start:   now.Add(-2 * time.Hour),
		End:     now.Add(-1 * time.Hour),
		Preset:  "social",
		Domains: []string{"twitter.com"},
		WorkMin: 25,
		RestMin: 5,
		Cycles:  4,
		Focus:   time.Hour,
	}
	video := Session{
		Start:   now.Add(-3 * time.Hour),
		End:     now.Add(-2 * time.Hour),
		Preset:  "video",
		Domains: []string{"youtube.com", "twitter.com"},
		WorkMin: 25,
		RestMin: 5,
		Cycles:  4,
		Focus:   30 * time.Minute,
	}

	st := Summarize([]Session{social, video}, 7, now)

	if len(st.PerDomain) != 2 {
		t.Fatalf("len(PerDomain) = %d, want 2", len(st.PerDomain))
	}
	if st.PerDomain[0].Domain != "twitter.com" {
		t.Errorf("PerDomain[0].Domain = %q, want twitter.com (mais foco)", st.PerDomain[0].Domain)
	}
	if st.PerDomain[0].Duration != 90*time.Minute {
		t.Errorf("PerDomain[0].Duration = %v, want 90m", st.PerDomain[0].Duration)
	}
	if st.PerDomain[1].Domain != "youtube.com" || st.PerDomain[1].Duration != 30*time.Minute {
		t.Errorf("PerDomain[1] = %+v, want youtube.com 30m", st.PerDomain[1])
	}
}

// TestSummarize_Empty verifies an empty session list produces zeroed stats
// with a fully zero-filled PerDay window.
// ---------------------------------------------------------------------------
// Streak (raia de dias consecutivos) & exportação
// ---------------------------------------------------------------------------

func TestComputeStreak_Empty(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) // segunda
	if got := ComputeStreak(nil, now); got != 0 {
		t.Errorf("streak sem sessões = %d, want 0", got)
	}
}

func TestComputeStreak_ConsecutiveDays(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) // segunda
	sessions := []Session{
		testSession(now.AddDate(0, 0, -2), now.AddDate(0, 0, -2).Add(time.Hour), "social", time.Hour), // sábado
		testSession(now.AddDate(0, 0, -1), now.AddDate(0, 0, -1).Add(time.Hour), "social", time.Hour), // domingo
		testSession(now.Add(-time.Hour), now, "social", time.Hour),                                    // hoje (segunda)
	}
	if got := ComputeStreak(sessions, now); got != 3 {
		t.Errorf("streak = %d, want 3", got)
	}
}

func TestComputeStreak_BrokenStreak(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) // segunda
	// sessões em seg (dia -3) e hoje (seg) — falta domingo (-1) → streak = 1 (hoje)
	sessions := []Session{
		testSession(now.AddDate(0, 0, -3), now.AddDate(0, 0, -3).Add(time.Hour), "social", time.Hour),
		testSession(now.Add(-time.Hour), now, "social", time.Hour),
	}
	if got := ComputeStreak(sessions, now); got != 1 {
		t.Errorf("streak = %d, want 1 (só hoje; sexta quebrou a raia)", got)
	}
}

func TestComputeStreak_TodayNotYetCounted(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) // segunda
	// sessão só ontem (domingo) → streak = 1 (ontem conta enquanto hoje está vazio)
	sessions := []Session{
		testSession(now.AddDate(0, 0, -1), now.AddDate(0, 0, -1).Add(time.Hour), "social", time.Hour),
	}
	if got := ComputeStreak(sessions, now); got != 1 {
		t.Errorf("streak = %d, want 1 (ontem ainda conta)", got)
	}
}

func TestExportCSV(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	st := Summarize([]Session{
		testSession(now.Add(-time.Hour), now, "social", time.Hour),
	}, 7, now)

	csv := ExportCSV(st)
	for _, c := range []string{"day", "focus_minutes", "sessions", "2026-08-03"} {
		if !strings.Contains(csv, c) {
			t.Errorf("CSV deveria conter %q:\n%s", c, csv)
		}
	}
}

func TestExportJSON(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	st := Summarize([]Session{
		testSession(now.Add(-time.Hour), now, "social", time.Hour),
	}, 7, now)

	js, err := ExportJSON(st)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	if !strings.Contains(js, "total_focus") || !strings.Contains(js, "per_day") {
		t.Errorf("JSON deveria conter campos do Stats:\n%s", js)
	}
}

func TestSummarize_Empty(t *testing.T) {
	now := time.Now()
	st := Summarize(nil, 7, now)

	if st.TotalSessions != 0 || st.TotalFocus != 0 {
		t.Errorf("expected zero totals, got %+v", st)
	}
	if len(st.PerDay) != 7 {
		t.Errorf("len(PerDay) = %d, want 7", len(st.PerDay))
	}
	for _, d := range st.PerDay {
		if d.Duration != 0 {
			t.Errorf("expected zero-filled day, got %+v", d)
		}
	}
}

// TestRenderStats_ContainsDaysAndTotals verifies the ASCII chart renders the
// totals header, each day with a bar, and per-domain rows.
func TestRenderStats_ContainsDaysAndTotals(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		testSession(now.Add(-24*time.Hour), now.Add(-23*time.Hour), "social", time.Hour),
	}
	st := Summarize(sessions, 3, now)

	out := RenderStats(st, 10)

	for _, want := range []string{"FocusGuard", "Sessões", "1", "2026-08-02", "2026-08-03"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "twitter.com") {
		t.Errorf("output should list the blocked domain, got:\n%s", out)
	}
}

// TestRenderStats_BarWidth verifies the bar length for the max day equals the
// configured width and a zero day renders no bar.
func TestRenderStats_BarWidth(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		testSession(now.Add(-24*time.Hour), now.Add(-23*time.Hour), "social", time.Hour),
	}
	st := Summarize(sessions, 3, now)

	out := RenderStats(st, 10)
	lines := strings.Split(out, "\n")

	var maxLine, zeroLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "2026-08-02") {
			maxLine = l
		}
		if strings.HasPrefix(l, "2026-08-01") {
			zeroLine = l
		}
	}

	if maxLine == "" {
		t.Fatal("missing max day line")
	}
	bar := strings.Split(maxLine, "  ")[1]
	if got := utf8.RuneCountInString(bar); got != 10 {
		t.Errorf("max bar length = %d runes, want 10", got)
	}
	if zeroLine != "" && strings.Contains(zeroLine, "█") {
		t.Errorf("zero day should have no bar, got %q", zeroLine)
	}
}

// ---------------------------------------------------------------------------
// Relatório HTML & resumo semanal
// ---------------------------------------------------------------------------

func TestExportHTML_ContainsSections(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	st := Summarize([]Session{
		testSession(now.Add(-24*time.Hour), now.Add(-23*time.Hour), "social", time.Hour),
		testSession(now.Add(-time.Hour), now, "social", time.Hour),
	}, 7, now)

	html := ExportHTML(st)
	for _, want := range []string{
		"<!DOCTYPE html>", "FocusGuard", "Sessões", "2026-08-02", "2026-08-03",
		"twitter.com", "Raia",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML deveria conter %q", want)
		}
	}
	if !strings.Contains(strings.ToLower(html), "<style") {
		t.Error("HTML deveria ser self-contained com CSS inline")
	}
}

func TestExportHTML_EscapesDomains(t *testing.T) {
	now := time.Now()
	st := Summarize([]Session{{Start: now.Add(-time.Hour), End: now, Focus: time.Hour, Domains: []string{"<script>alert(1)</script>"}}}, 7, now)
	html := ExportHTML(st)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("HTML deveria escapar conteúdo injetável nos domínios")
	}
}

func TestRenderWeeklySummary_Totals(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) // segunda
	sessions := []Session{
		testSession(now.Add(-48*time.Hour), now.Add(-47*time.Hour), "social", time.Hour), // sábado
		testSession(now.Add(-24*time.Hour), now.Add(-23*time.Hour), "video", time.Hour),  // domingo
		testSession(now.Add(-time.Hour), now, "social", time.Hour),                       // hoje
	}
	st := Summarize(sessions, 7, now)

	out := RenderWeeklySummary(st)
	for _, want := range []string{"Resumo semanal", "3", "3h", "twitter.com", "Raia de foco: 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("resumo deveria conter %q:\n%s", want, out)
		}
	}
}

func TestRenderWeeklySummary_Empty(t *testing.T) {
	st := Summarize(nil, 7, time.Now())
	out := RenderWeeklySummary(st)
	if !strings.Contains(out, "Nenhuma sessão") {
		t.Errorf("resumo vazio deveria avisar, got:\n%s", out)
	}
}

// TestRecorder_AppendAndLoad verifies sessions persist to a JSONL file and
// load back in order.
func TestRecorder_AppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.jsonl")
	rec := NewRecorder(path)

	now := time.Now()
	rec.Record(testSession(now.Add(-time.Hour), now, "social", time.Hour))
	rec.Record(testSession(now.Add(-2*time.Hour), now.Add(-time.Hour), "video", 30*time.Minute))

	sessions, err := rec.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}
	if sessions[0].Preset != "social" || sessions[1].Preset != "video" {
		t.Errorf("order lost: %+v", sessions)
	}
	if sessions[0].Focus != time.Hour {
		t.Errorf("Focus = %v, want 1h", sessions[0].Focus)
	}
}

// TestRecorder_ReopenLoadsPriorData verifies a new Recorder instance over the
// same file sees the previously appended sessions (daemon restart survival).
func TestRecorder_ReopenLoadsPriorData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.jsonl")
	rec := NewRecorder(path)
	now := time.Now()
	rec.Record(testSession(now.Add(-time.Hour), now, "social", time.Hour))

	rec2 := NewRecorder(path)
	sessions, err := rec2.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Preset != "social" {
		t.Errorf("expected prior data after reopen, got %+v", sessions)
	}
}

// TestRecorder_SkipsCorruptLines verifies a corrupt JSON line does not abort
// the load of the remaining valid sessions.
func TestRecorder_SkipsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.jsonl")
	rec := NewRecorder(path)
	now := time.Now()
	rec.Record(testSession(now.Add(-time.Hour), now, "social", time.Hour))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	corrupt := append([]byte("{corrupt-json}\n"), data...)
	if err := os.WriteFile(path, corrupt, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sessions, err := rec.Sessions()
	if err != nil {
		t.Fatalf("Sessions should not fail on corrupt lines: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("len(sessions) = %d, want 1 (corrupt skipped)", len(sessions))
	}
}

// TestRecorder_MissingFile_Empty verifies a nonexistent file yields zero
// sessions without error.
func TestRecorder_MissingFile_Empty(t *testing.T) {
	rec := NewRecorder(filepath.Join(t.TempDir(), "nope.jsonl"))
	sessions, err := rec.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected no sessions, got %d", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// Sessões nomeadas (label) e relatório por missão
// ---------------------------------------------------------------------------

func TestSummarizeByLabel_FiltersSessions(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		testSession(now.Add(-time.Hour), now, "social", time.Hour),
		{Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour), Preset: "video", Label: "ENEM", Domains: []string{"youtube.com"}, Focus: 2 * time.Hour},
	}

	st := SummarizeByLabel(sessions, "ENEM", 7, now)
	if st.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", st.TotalSessions)
	}
	if st.TotalFocus != 2*time.Hour {
		t.Errorf("TotalFocus = %v, want 2h", st.TotalFocus)
	}
	if st.PerDomain[0].Domain != "youtube.com" {
		t.Errorf("PerDomain should only contain the filtered session's domains, got %+v", st.PerDomain)
	}
}

func TestSummarizeLabels_AggregatesByMission(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{Start: now.Add(-time.Hour), End: now, Preset: "social", Label: "ENEM", Domains: []string{"twitter.com"}, Focus: time.Hour},
		{Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour), Preset: "video", Label: "ENEM", Domains: []string{"youtube.com"}, Focus: 2 * time.Hour},
		{Start: now.Add(-3 * time.Hour), End: now.Add(-2 * time.Hour), Preset: "social", Domains: []string{"twitter.com"}, Focus: time.Hour}, // sem label
	}

	labels := SummarizeLabels(sessions)
	if len(labels) != 1 {
		t.Fatalf("len(labels) = %d, want 1 (só a missão ENEM)", len(labels))
	}
	if labels[0].Label != "ENEM" || labels[0].Duration != 3*time.Hour || labels[0].Sessions != 2 {
		t.Errorf("label stat = %+v, want ENEM/3h/2", labels[0])
	}
}

// TestRecorder_MemoryMode verifies an empty path keeps the recorder
// in-memory (no file writes) — used by tests and the daemon fallback.
func TestRecorder_MemoryMode(t *testing.T) {
	rec := NewRecorder("")
	now := time.Now()
	rec.Record(testSession(now.Add(-time.Hour), now, "social", time.Hour))

	sessions, err := rec.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 in-memory session, got %d", len(sessions))
	}
	if sessions[0].Preset != "social" {
		t.Errorf("Preset = %q, want social", sessions[0].Preset)
	}
}
