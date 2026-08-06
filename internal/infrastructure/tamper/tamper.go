// Package tamper records detected tampering attempts (external edits to the
// hosts file or the state.json) as an append-only JSONL log next to the state.
// The watchers log an event when they detect a violation and restore it; the
// "focusguard tamper-log" command surfaces the history for accountability.
package tamper

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Event is one detected tamper attempt.
type Event struct {
	At     time.Time `json:"at"`
	Source string    `json:"source"` // "hosts" | "state"
	Action string    `json:"action"` // "restore" | "reconcile"
	Detail string    `json:"detail,omitempty"`
}

// Recorder appends events to a JSONL file. An empty path keeps the recorder in
// memory (tests and daemon fallback).
type Recorder struct {
	mu     sync.Mutex
	path   string
	memory []Event
}

// NewRecorder returns a Recorder writing to path, or an in-memory recorder
// when path is empty.
func NewRecorder(path string) *Recorder {
	return &Recorder{path: path}
}

// Log appends one event to the file (or memory). Best-effort: a failed append
// is silently dropped — the protection keeps working either way.
func (r *Recorder) Log(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.path == "" {
		r.memory = append(r.memory, e)
		return
	}
	data, err := json.Marshal(e)
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

// Events returns every event read from the file (or memory), skipping corrupt
// lines. A missing file yields an empty list without error.
func (r *Recorder) Events() ([]Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.path == "" {
		return append([]Event(nil), r.memory...), nil
	}
	f, err := os.Open(r.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // linha corrompida — nunca aborta a leitura
		}
		events = append(events, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

// FormatEvent renders an event as a human-readable line for the tamper-log
// command: "2026-08-03 10:00:00  hosts  restore  twitter.com".
func FormatEvent(e Event) string {
	line := fmt.Sprintf("%s  %s  %s", e.At.Local().Format("2006-01-02 15:04:05"), e.Source, e.Action)
	if e.Detail != "" {
		line += "  " + e.Detail
	}
	return line
}
