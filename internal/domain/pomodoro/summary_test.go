package pomodoro

import (
	"testing"
	"time"
)

// TestWatchCompletion_DeliversSummary verifies a completed session produces a
// summary on the channel returned by WatchCompletion (used by the daemon to
// save prefs and log a post-session summary).
func TestWatchCompletion_DeliversSummary(t *testing.T) {
	c := New(&stubBlocker{})
	c.SetRecorder(nil)
	ch := c.WatchCompletion()

	_, err := c.Start(Session{
		Preset:  "social",
		Domains: []string{"twitter.com"},
		Work:    15 * time.Millisecond,
		Rest:    5 * time.Millisecond,
		Cycles:  2,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case s := <-ch:
		if s.Preset != "social" {
			t.Errorf("summary preset = %q, want social", s.Preset)
		}
		if s.Cycles != 2 {
			t.Errorf("summary cycles = %d, want 2", s.Cycles)
		}
		if s.Work != 15*time.Millisecond {
			t.Errorf("summary work = %v, want 15ms", s.Work)
		}
		if s.Focus <= 0 {
			t.Errorf("summary focus should be positive, got %v", s.Focus)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no summary delivered before timeout")
	}
}

// TestWatchCompletion_NoSummaryWhenIdle verifies the channel stays open (not
// closed) while no session ever runs — the daemon's watcher must not spin.
func TestWatchCompletion_NoSummaryWhenIdle(t *testing.T) {
	c := New(&stubBlocker{})
	ch := c.WatchCompletion()

	select {
	case s := <-ch:
		t.Fatalf("unexpected summary while idle: %+v", s)
	case <-time.After(100 * time.Millisecond):
		// OK — nothing delivered while idle.
	}
}
