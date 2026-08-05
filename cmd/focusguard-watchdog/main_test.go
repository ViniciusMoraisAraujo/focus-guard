package main

import (
	"encoding/json"
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

// TestVersionInfo_GoWinresFormat verifies cmd/focusguard-watchdog/versioninfo.json
// follows the go-winres schema: an application icon (RT_GROUP_ICON referencing
// the generated .ico) and version metadata (RT_VERSION) — and NO manifest, since
// the watchdog runs as a regular user / LocalSystem service and must never
// require admin (only the daemon has requireAdministrator). This is what
// go-winres make consumes to emit rsrc_windows_*.syso for the watchdog.
func TestVersionInfo_GoWinresFormat(t *testing.T) {
	data, err := os.ReadFile("versioninfo.json")
	if err != nil {
		t.Fatalf("leitura do versioninfo.json: %v", err)
	}

	// Os .syso são committed (o CI precisa deles): sem eles o go build no
	// Windows gera um .exe SEM ícone silenciosamente.
	for _, syso := range []string{"rsrc_windows_amd64.syso", "rsrc_windows_arm64.syso"} {
		if _, err := os.Stat(syso); err != nil {
			t.Errorf("resource %s ausente — rode `make winres` e commite-o: %v", syso, err)
		}
	}

	var v struct {
		GroupIcon map[string]map[string]json.RawMessage `json:"RT_GROUP_ICON"`
		Manifest  map[string]map[string]string          `json:"RT_MANIFEST"`
		Version   map[string]map[string]struct {
			Info map[string]map[string]string `json:"info"`
		} `json:"RT_VERSION"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("versioninfo.json não é JSON válido: %v", err)
	}

	if len(v.GroupIcon) == 0 {
		t.Error("RT_GROUP_ICON ausente — o .exe não terá ícone")
	}
	if len(v.Manifest) != 0 {
		t.Error("RT_MANIFEST presente — o watchdog não deve exigir admin")
	}

	info := v.Version["#1"]["0000"].Info["0409"]
	if info["ProductName"] == "" || info["OriginalFilename"] == "" {
		t.Errorf("RT_VERSION.info.0409 incompleto: %+v", info)
	}
	if info["FileDescription"] == "" {
		t.Error("FileDescription vazio — aba de detalhes do Explorer sem descrição")
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
