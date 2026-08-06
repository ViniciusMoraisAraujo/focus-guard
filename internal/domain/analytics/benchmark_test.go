package analytics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSessionsFile appends numSessions plausible sessions to a JSONL file,
// the shape a long-time FocusGuard user accumulates over months.
func writeSessionsFile(b *testing.B, path string, numSessions int) {
	b.Helper()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer f.Close()

	now := time.Now()
	for i := 0; i < numSessions; i++ {
		start := now.Add(-time.Duration(i) * 90 * time.Minute)
		s := Session{
			Start:   start,
			End:     start.Add(25 * time.Minute),
			Preset:  "social",
			Label:   fmt.Sprintf("mission-%d", i%5),
			Domains: []string{"youtube.com", "twitter.com", "instagram.com"},
			WorkMin: 25,
			RestMin: 5,
			Cycles:  4,
			Focus:   25 * time.Minute,
			Strict:  i%2 == 0,
		}
		data, err := json.Marshal(s)
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
}

func BenchmarkAnalytics_Sessions(b *testing.B) {
	for _, numSessions := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("sessions=%d", numSessions), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "analytics.jsonl")
			writeSessionsFile(b, path, numSessions)
			r := NewRecorder(path)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := r.Sessions(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAnalytics_Sessions_Cached measures repeated Sessions() over an
// unchanged file — the "repeated stats" scenario — where the incremental cache
// avoids re-reading and re-parsing the whole JSONL: only os.Stat + a defensive
// copy of the cached list remain.
func BenchmarkAnalytics_Sessions_Cached(b *testing.B) {
	for _, numSessions := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("sessions=%d", numSessions), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "analytics.jsonl")
			writeSessionsFile(b, path, numSessions)
			r := NewRecorder(path)

			// Primeia o cache uma vez; o timer mede só os hits subsequentes.
			if _, err := r.Sessions(); err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := r.Sessions(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAnalytics_Summarize measures the aggregation pass over an
// already-read session list (the CPU part of every stats IPC action).
func BenchmarkAnalytics_Summarize(b *testing.B) {
	r := NewRecorder("")
	now := time.Now()
	for i := 0; i < 10000; i++ {
		start := now.Add(-time.Duration(i) * 90 * time.Minute)
		r.Record(Session{
			Start:   start,
			End:     start.Add(25 * time.Minute),
			Preset:  "social",
			Label:   fmt.Sprintf("mission-%d", i%5),
			Domains: []string{"youtube.com", "twitter.com", "instagram.com"},
			Focus:   25 * time.Minute,
		})
	}
	sessions, err := r.Sessions()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Summarize(sessions, 7, now)
	}
}
