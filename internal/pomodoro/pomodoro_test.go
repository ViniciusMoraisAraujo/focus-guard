package pomodoro

import (
	"errors"
	"sync"
	"testing"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/policy"
)

// mockBlocker records every BlockDomains call (mutex-protected — the run loop
// calls it from its own goroutine).
type mockBlocker struct {
	mu       sync.Mutex
	calls    int
	domains  []string
	duration time.Duration
	err      error
}

func (m *mockBlocker) BlockDomains(domains []string, duration time.Duration) ([]policy.Block, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.domains = append([]string(nil), domains...)
	m.duration = duration
	if m.err != nil {
		return nil, m.err
	}
	blocks := make([]policy.Block, 0, len(domains))
	now := time.Now()
	for _, d := range domains {
		blocks = append(blocks, policy.Block{Domain: d, StartedAt: now, ExpiresAt: now.Add(duration)})
	}
	return blocks, nil
}

func (m *mockBlocker) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestStart_ValidationErrors(t *testing.T) {
	c := New(&mockBlocker{})

	tests := []struct {
		name string
		s    Session
	}{
		{"no domains", Session{Preset: "social", Work: time.Minute, Rest: 0, Cycles: 1}},
		{"no work", Session{Preset: "social", Domains: []string{"x.com"}, Work: 0, Cycles: 1}},
		{"negative rest", Session{Preset: "social", Domains: []string{"x.com"}, Work: time.Minute, Rest: -time.Minute, Cycles: 1}},
		{"no cycles", Session{Preset: "social", Domains: []string{"x.com"}, Work: time.Minute, Cycles: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.Start(tt.s); err == nil {
				t.Errorf("Start(%+v) should error", tt.s)
			}
		})
	}
}

func TestStart_AlreadyActive_ReturnsError(t *testing.T) {
	blk := &mockBlocker{}
	c := New(blk)

	_, err := c.Start(Session{Preset: "social", Domains: []string{"x.com"}, Work: time.Hour, Rest: 0, Cycles: 1})
	if err != nil {
		t.Fatalf("Start erro: %v", err)
	}
	if _, err := c.Start(Session{Preset: "video", Domains: []string{"youtube.com"}, Work: time.Hour, Rest: 0, Cycles: 1}); err == nil {
		t.Error("expected error when a session is already active")
	}

	st, _ := c.Stop()
	if st.Active {
		t.Error("Stop should end the session")
	}
}

func TestStatus_InitiallyInactive(t *testing.T) {
	c := New(&mockBlocker{})
	st := c.Status()
	if st.Active {
		t.Error("expected inactive status before any session")
	}
}

// TestRun_BlocksDomainsEachWorkCycle verifies a full session: the blocker is
// invoked once per work cycle with the session domains and work duration, and
// the status transitions through work/rest phases before finishing.
func TestRun_BlocksDomainsEachWorkCycle(t *testing.T) {
	blk := &mockBlocker{}
	c := New(blk)

	s := Session{
		Preset:  "social",
		Domains: []string{"twitter.com", "facebook.com"},
		Work:    60 * time.Millisecond,
		Rest:    40 * time.Millisecond,
		Cycles:  2,
	}
	if _, err := c.Start(s); err != nil {
		t.Fatalf("Start erro: %v", err)
	}

	// O status deve entrar em work e depois descansar (rest) ao menos uma vez.
	if !waitFor(t, 2*time.Second, func() bool {
		st := c.Status()
		return st.Active && st.Phase == PhaseWork && st.Cycle == 1
	}) {
		t.Fatalf("expected active work phase, got %+v", c.Status())
	}
	if !waitFor(t, 2*time.Second, func() bool {
		return c.Status().Phase == PhaseRest
	}) {
		t.Fatalf("expected a rest phase between cycles, got %+v", c.Status())
	}

	// Aguarda o término da sessão (Active=false).
	if !waitFor(t, 3*time.Second, func() bool { return !c.Status().Active }) {
		t.Fatalf("session should finish, got %+v", c.Status())
	}

	if got := blk.callCount(); got != s.Cycles {
		t.Errorf("BlockDomains chamado %d vezes, want %d", got, s.Cycles)
	}
	if blk.duration != s.Work {
		t.Errorf("BlockDomains duration = %v, want %v", blk.duration, s.Work)
	}
}

func TestRun_NoRestPhase(t *testing.T) {
	blk := &mockBlocker{}
	c := New(blk)

	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    40 * time.Millisecond,
		Rest:    0,
		Cycles:  3,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}

	if !waitFor(t, 3*time.Second, func() bool { return !c.Status().Active }) {
		t.Fatalf("session should finish, got %+v", c.Status())
	}
	if got := blk.callCount(); got != 3 {
		t.Errorf("BlockDomains chamado %d vezes, want 3", got)
	}
}

func TestStop_EndsSessionImmediately(t *testing.T) {
	blk := &mockBlocker{}
	c := New(blk)

	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    10 * time.Minute,
		Rest:    0,
		Cycles:  10,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	callsBefore := blk.callCount()

	st, _ := c.Stop()
	if st.Active {
		t.Error("Stop deve encerrar a sessão")
	}

	// Nenhum ciclo adicional pode ser bloqueado após o Stop.
	time.Sleep(80 * time.Millisecond)
	if got := blk.callCount(); got != callsBefore {
		t.Errorf("BlockDomains chamado após Stop: %d -> %d", callsBefore, got)
	}
}

func TestStop_WhenInactive_IsNoOp(t *testing.T) {
	c := New(&mockBlocker{})
	st, err := c.Stop()
	if err != nil {
		t.Fatalf("Stop inativo não deve errar: %v", err)
	}
	if st.Active {
		t.Error("expected inactive state")
	}
}

// TestStop_ConcurrentDoubleStop_NoPanic verifies that two concurrent Stop
// calls on the same active session do not panic with "close of closed channel"
// — the stop channel must be claimed by exactly one caller.
func TestStop_ConcurrentDoubleStop_NoPanic(t *testing.T) {
	c := New(&mockBlocker{})
	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    10 * time.Minute,
		Rest:    0,
		Cycles:  5,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.Stop()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Stop %d erro: %v", i, err)
		}
	}
	if c.Status().Active {
		t.Error("session should be inactive after concurrent stops")
	}
}

// TestRun_BlockFailureIsBestEffort verifies a BlockDomains failure does not
// abort the session: the cycles continue and the session still completes.
func TestRun_BlockFailureIsBestEffort(t *testing.T) {
	blk := &mockBlocker{err: errors.New("dns failure")}
	c := New(blk)

	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    30 * time.Millisecond,
		Rest:    0,
		Cycles:  2,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}

	if !waitFor(t, 3*time.Second, func() bool { return !c.Status().Active }) {
		t.Fatalf("session should finish despite block failures, got %+v", c.Status())
	}
	if got := blk.callCount(); got != 2 {
		t.Errorf("expected 2 cycles despite failures, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Strict Mode & Analytics recording
// ---------------------------------------------------------------------------

// fakeRecorder records every analytics.Session pushed by the Controller
// (mutex-protected — run() records from its own goroutine).
type fakeRecorder struct {
	mu       sync.Mutex
	sessions []analytics.Session
}

func (f *fakeRecorder) Record(s analytics.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, s)
}

func (f *fakeRecorder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

func (f *fakeRecorder) last() analytics.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sessions) == 0 {
		return analytics.Session{}
	}
	return f.sessions[len(f.sessions)-1]
}

// TestStop_Strict_RefusedWhileActive verifies a strict session refuses Stop
// while active (the session is inviolable), then ends naturally.
func TestStop_Strict_RefusedWhileActive(t *testing.T) {
	c := New(&mockBlocker{})
	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    300 * time.Millisecond,
		Rest:    0,
		Cycles:  1,
		Strict:  true,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}

	// Janela larga o suficiente para o Stop ser recusado mesmo sob carga de CI
	// (um stall de 80ms entre Start e Stop deixaria a sessão terminar sozinha).
	if _, err := c.Stop(); err == nil {
		t.Error("Stop should be refused on an active strict session")
	}
	if !c.Status().Active {
		t.Error("strict session should remain active after refused Stop")
	}

	// A sessão termina naturalmente; Stop pós-término é no-op.
	if !waitFor(t, 3*time.Second, func() bool { return !c.Status().Active }) {
		t.Fatalf("strict session should finish on its own, got %+v", c.Status())
	}
	if st, err := c.Stop(); err != nil || st.Active {
		t.Errorf("Stop after natural end should be a no-op, got %+v err=%v", st, err)
	}
}

// TestStop_NonStrict_StillStoppable verifies non-strict sessions keep the
// existing immediate-stop behavior (regression guard for the strict flag).
func TestStop_NonStrict_StillStoppable(t *testing.T) {
	c := New(&mockBlocker{})
	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    10 * time.Minute,
		Rest:    0,
		Cycles:  5,
		Strict:  false,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}
	if st, err := c.Stop(); err != nil {
		t.Errorf("non-strict Stop should succeed, got %v", err)
	} else if st.Active {
		t.Error("session should be inactive after Stop")
	}
}

// TestRun_RecordsCompletedSession verifies the Controller pushes exactly one
// analytics.Session at the end, with focus = cycles × work and the session
// metadata preserved.
func TestRun_RecordsCompletedSession(t *testing.T) {
	rec := &fakeRecorder{}
	c := New(&mockBlocker{})
	c.SetRecorder(rec)

	s := Session{
		Preset:  "social",
		Domains: []string{"x.com", "instagram.com"},
		Work:    40 * time.Millisecond,
		Rest:    0,
		Cycles:  2,
	}
	if _, err := c.Start(s); err != nil {
		t.Fatalf("Start erro: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return !c.Status().Active }) {
		t.Fatalf("session should finish, got %+v", c.Status())
	}

	if got := rec.count(); got != 1 {
		t.Fatalf("recorder got %d sessions, want 1", got)
	}
	got := rec.last()
	if got.Preset != "social" {
		t.Errorf("Preset = %q, want social", got.Preset)
	}
	if len(got.Domains) != 2 || got.Domains[0] != "x.com" {
		t.Errorf("Domains = %v, want [x.com instagram.com]", got.Domains)
	}
	if got.Focus != 2*40*time.Millisecond {
		t.Errorf("Focus = %v, want 80ms (2 cycles × 40ms)", got.Focus)
	}
	if got.Strict {
		t.Error("Strict should be false for a non-strict session")
	}
	if got.Cycles != 2 || got.WorkMin != 0 {
		t.Errorf("unexpected session metadata: %+v", got)
	}
	if got.End.Before(got.Start) {
		t.Errorf("End should be after Start: %+v", got)
	}
}

// TestRun_Strict_RecordsStrictTrue verifies the Strict flag is recorded in the
// analytics session.
func TestRun_Strict_RecordsStrictTrue(t *testing.T) {
	rec := &fakeRecorder{}
	c := New(&mockBlocker{})
	c.SetRecorder(rec)

	if _, err := c.Start(Session{
		Preset:  "video",
		Domains: []string{"youtube.com"},
		Work:    40 * time.Millisecond,
		Rest:    0,
		Cycles:  1,
		Strict:  true,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return !c.Status().Active }) {
		t.Fatalf("session should finish, got %+v", c.Status())
	}

	if got := rec.last(); !got.Strict {
		t.Error("Strict should be recorded as true")
	} else if got.Focus != 40*time.Millisecond {
		t.Errorf("Focus = %v, want 40ms", got.Focus)
	}
}

// TestStop_RecordsStoppedSession verifies stopping a non-strict session mid-
// way records the session with the focus accumulated so far (work phases that
// already ran), exactly once.
func TestStop_RecordsStoppedSession(t *testing.T) {
	rec := &fakeRecorder{}
	c := New(&mockBlocker{})
	c.SetRecorder(rec)

	if _, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"x.com"},
		Work:    10 * time.Minute,
		Rest:    0,
		Cycles:  5,
	}); err != nil {
		t.Fatalf("Start erro: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	if _, err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := rec.count(); got != 1 {
		t.Fatalf("recorder got %d sessions, want 1", got)
	}
	if got := rec.last(); got.Focus != 10*time.Minute {
		t.Errorf("Focus = %v, want 10m (first work phase already applied)", got.Focus)
	}
}
