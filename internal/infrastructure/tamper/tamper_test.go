package tamper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecorder_LogAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tamper.jsonl")
	r := NewRecorder(path)

	now := time.Now()
	r.Log(Event{At: now, Source: "hosts", Action: "restore", Detail: "twitter.com"})
	r.Log(Event{At: now.Add(-time.Minute), Source: "state", Action: "reconcile"})

	events, err := r.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Source != "hosts" || events[0].Action != "restore" || events[0].Detail != "twitter.com" {
		t.Errorf("evento inesperado: %+v", events[0])
	}
	if events[1].Source != "state" {
		t.Errorf("evento inesperado: %+v", events[1])
	}
}

func TestRecorder_ReopenLoadsPrior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tamper.jsonl")
	r := NewRecorder(path)
	r.Log(Event{At: time.Now(), Source: "hosts", Action: "restore"})

	r2 := NewRecorder(path)
	events, err := r2.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 || events[0].Source != "hosts" {
		t.Errorf("expected prior events after reopen, got %+v", events)
	}
}

func TestRecorder_SkipsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tamper.jsonl")
	r := NewRecorder(path)
	r.Log(Event{At: time.Now(), Source: "hosts", Action: "restore"})

	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append([]byte("{corrupt}\n"), data...), 0644); err != nil {
		t.Fatal(err)
	}

	events, err := r.Events()
	if err != nil {
		t.Fatalf("Events should not fail on corrupt lines: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("len(events) = %d, want 1 (corrupt skipped)", len(events))
	}
}

func TestRecorder_MissingFile_Empty(t *testing.T) {
	r := NewRecorder(filepath.Join(t.TempDir(), "nope.jsonl"))
	events, err := r.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected no events, got %d", len(events))
	}
}

func TestRecorder_MemoryMode(t *testing.T) {
	r := NewRecorder("")
	r.Log(Event{At: time.Now(), Source: "state", Action: "reconcile"})
	events, err := r.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 || events[0].Action != "reconcile" {
		t.Errorf("expected 1 in-memory event, got %+v", events)
	}
}

func TestFormatEvent_HumanReadable(t *testing.T) {
	ev := Event{At: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC), Source: "hosts", Action: "restore", Detail: "twitter.com"}
	out := FormatEvent(ev)
	for _, want := range []string{"hosts", "restore", "twitter.com", "2026-08-03"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted event should contain %q, got: %q", want, out)
		}
	}
}
