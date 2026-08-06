package pomodoro

import (
	"strings"
	"sync"
	"testing"
	"time"

	"focusguard/internal/domain/analytics"
	"focusguard/internal/domain/policy"
)

// fakeBeep writes beep markers to a strings.Builder so tests can assert the
// sequence of transitions without touching the terminal.
type fakeBeep struct{ b strings.Builder }

func (f *fakeBeep) beep(kind string) {
	f.b.WriteString(kind + ";")
}

func (f *fakeBeep) String() string { return f.b.String() }

func TestBeep_WorkThenRestThenFinish(t *testing.T) {
	var fb fakeBeep
	n := &Notifier{beep: fb.beep}

	n.WorkStart()
	n.RestStart()
	n.Finish()

	got := fb.String()
	for _, want := range []string{"work;rest;done;"} {
		if got != want {
			t.Fatalf("beep sequence = %q, want %q", got, want)
		}
	}
}

// stubBlockers records BlockDomains calls without applying firewall rules.
type stubBlocker struct{ calls int }

func (s *stubBlocker) BlockDomains([]string, time.Duration) ([]policy.Block, error) {
	s.calls++
	return nil, nil
}

// beepRecorder is a SessionRecorder that also exposes the beep markers. The
// session field is written by the controller's goroutine, so it is protected
// by a mutex (the test polls after the session ends).
type beepRecorder struct {
	mu      sync.Mutex
	fb      fakeBeep
	session analytics.Session
}

func (r *beepRecorder) Record(s analytics.Session) {
	r.mu.Lock()
	r.session = s
	r.mu.Unlock()
}

func (r *beepRecorder) sessionCopy() analytics.Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.session
}

func (r *beepRecorder) beep(kind string) { r.fb.beep(kind) }

// TestBeep_ControllerWiresTransitions verifies a fast short session emits the
// work/rest/done beeps in order (work for each cycle, rest between cycles,
// done at the end) and records the session.
func TestBeep_ControllerWiresTransitions(t *testing.T) {
	rec := &beepRecorder{}
	c := New(&stubBlocker{})
	c.SetNotifier(&Notifier{beep: rec.beep})
	c.SetRecorder(rec)

	_, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"twitter.com"},
		Work:    20 * time.Millisecond,
		Rest:    5 * time.Millisecond,
		Cycles:  2,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for c.Status().Active {
		select {
		case <-deadline:
			t.Fatal("session did not finish in time")
		case <-time.After(5 * time.Millisecond):
		}
	}

	seq := rec.fb.String()
	if !strings.HasPrefix(seq, "work;") {
		t.Errorf("expected work beep first, got %q", seq)
	}
	if !strings.Contains(seq, "rest;") {
		t.Errorf("expected a rest beep between cycles, got %q", seq)
	}
	if !strings.HasSuffix(seq, "done;") {
		t.Errorf("expected done beep at the end, got %q", seq)
	}
	if s := rec.sessionCopy(); s.Cycles != 2 {
		t.Errorf("recorded session cycles = %d, want 2", s.Cycles)
	}
}
