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
