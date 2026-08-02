package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonTracker_MarkHealthyRecordsTime(t *testing.T) {
	tracker := &daemonTracker{}
	if !tracker.lastHealthy().IsZero() {
		t.Error("expected zero lastHealthy before the first healthy ping")
	}

	before := time.Now()
	tracker.markHealthy()
	after := tracker.lastHealthy()

	if after.Before(before) {
		t.Errorf("lastHealthy %v should be >= %v", after, before)
	}
}

// TestCheckDaemon_MarksHealthyWhenResponding verifies a responding daemon is
// recorded as healthy so the crash window resets.
func TestCheckDaemon_MarksHealthyWhenResponding(t *testing.T) {
	origResponds := daemonResponds
	daemonResponds = func() bool { return true }
	defer func() { daemonResponds = origResponds }()

	origKill := killDaemon
	killDaemon = func() {}
	defer func() { killDaemon = origKill }()

	tracker := &daemonTracker{}
	checkDaemon(tracker)

	if tracker.lastHealthy().IsZero() {
		t.Error("expected lastHealthy to be recorded when the daemon responds")
	}
}

// TestMaybeRollback_RestoresAfterCrashLoop is an end-to-end test of the Smart
// Recovery path the watchdog runs when the daemon stops responding: with a
// recent .bak created AFTER the last confirmed health (the update replaced the
// binary) and the daemon down past the restart grace, the binary must be
// restored to the previous version.
func TestMaybeRollback_RestoresAfterCrashLoop(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	if err := os.WriteFile(binary, []byte("broken-new"), 0755); err != nil {
		t.Fatal(err)
	}
	backup := binary + ".bak.20260802100000"
	if err := os.WriteFile(backup, []byte("good-old"), 0755); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-90 * time.Second)
	_ = os.Chtimes(backup, mt, mt)

	origPath := daemonBinaryPath
	daemonBinaryPath = func() string { return binary }
	defer func() { daemonBinaryPath = origPath }()

	restored, err := maybeRollback(time.Now().Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("maybeRollback erro: %v", err)
	}
	if !restored {
		t.Fatal("expected rollback to happen")
	}

	data, _ := os.ReadFile(binary)
	if string(data) != "good-old" {
		t.Errorf("binary deveria ter sido restaurado, got %q", string(data))
	}
}

// TestMaybeRollback_NoRollbackDuringPostUpdateRestart is the anti-regression
// guard: right after a successful update the daemon exits on purpose and the
// service manager restarts it within seconds. With the backup just created and
// the daemon down for less than minDowntime, the fresh (good) binary must NOT
// be reverted to the previous version.
func TestMaybeRollback_NoRollbackDuringPostUpdateRestart(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	if err := os.WriteFile(binary, []byte("good-new"), 0755); err != nil {
		t.Fatal(err)
	}
	backup := binary + ".bak.20260802100000"
	if err := os.WriteFile(backup, []byte("old-version"), 0755); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-40 * time.Second)
	_ = os.Chtimes(backup, mt, mt)

	origPath := daemonBinaryPath
	daemonBinaryPath = func() string { return binary }
	defer func() { daemonBinaryPath = origPath }()

	// Saudável até 50s atrás — dentro da graça de 60s, o restart ainda pode
	// estar em andamento.
	restored, err := maybeRollback(time.Now().Add(-50 * time.Second))
	if err != nil {
		t.Fatalf("maybeRollback erro: %v", err)
	}
	if restored {
		t.Fatal("no rollback during the post-update restart grace period")
	}

	data, _ := os.ReadFile(binary)
	if string(data) != "good-new" {
		t.Errorf("binary novo não deveria ter sido revertido, got %q", string(data))
	}
}

// TestMaybeRollback_NoDecisionWithoutHealthyHistory verifies the watchdog does
// not roll back when it never saw the daemon healthy (e.g. first boot) — a
// fresh install must not be reverted.
func TestMaybeRollback_NoDecisionWithoutHealthyHistory(t *testing.T) {
	restored, err := maybeRollback(time.Time{})
	if err != nil {
		t.Fatalf("maybeRollback erro: %v", err)
	}
	if restored {
		t.Error("no rollback should happen without healthy history")
	}
}

// TestMaybeRollback_NoRollbackForStableDaemon verifies a daemon that ran long
// enough (outside the crash window) is not rolled back on a routine death.
func TestMaybeRollback_NoRollbackForStableDaemon(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	if err := os.WriteFile(binary, []byte("working"), 0755); err != nil {
		t.Fatal(err)
	}
	backup := binary + ".bak.20260802100000"
	if err := os.WriteFile(backup, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-30 * time.Minute)
	_ = os.Chtimes(backup, mt, mt)

	origPath := daemonBinaryPath
	daemonBinaryPath = func() string { return binary }
	defer func() { daemonBinaryPath = origPath }()

	restored, err := maybeRollback(time.Now().Add(-10 * time.Minute))
	if err != nil {
		t.Fatalf("maybeRollback erro: %v", err)
	}
	if restored {
		t.Error("no rollback for a stable daemon death")
	}

	data, _ := os.ReadFile(binary)
	if string(data) != "working" {
		t.Errorf("binary deveria permanecer intacto, got %q", string(data))
	}
}
