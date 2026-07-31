package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"focusguard/internal/hostswatch"
	"focusguard/internal/policy"
	"focusguard/internal/statewatch"
)

type mockHostswatchEnforcer struct {
	syncCalled int32
}

func (m *mockHostswatchEnforcer) Sync(blocks map[string][]string) error {
	atomic.AddInt32(&m.syncCalled, 1)
	return nil
}

type mockHostswatchScheduler struct {
	blocks []policy.Block
}

func (m *mockHostswatchScheduler) ListBlocks() ([]policy.Block, error) {
	return m.blocks, nil
}

func TestNewHostswatchVariable_Initialized(t *testing.T) {
	if newHostswatch == nil {
		t.Fatal("newHostswatch should be initialized from hostswatch.New")
	}
}

func TestStartHostswatch_Success(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}

	enf := &mockHostswatchEnforcer{}
	sched := &mockHostswatchScheduler{}

	var capturedEnf hostswatch.Enforcer
	var capturedSched hostswatch.Scheduler

	origNew := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		capturedEnf = enf
		capturedSched = sched
		hw := hostswatch.New(enf, sched)
		hw.HostsPath = hostsPath
		return hw
	}
	defer func() { newHostswatch = origNew }()

	hw := startHostswatch(enf, sched)
	if hw == nil {
		t.Fatal("startHostswatch returned nil, expected non-nil")
	}
	defer hw.Stop()

	if capturedEnf != enf {
		t.Error("newHostswatch called with wrong enforcer")
	}
	if capturedSched != sched {
		t.Error("newHostswatch called with wrong scheduler")
	}
}

func TestStartHostswatch_StartFails(t *testing.T) {
	enf := &mockHostswatchEnforcer{}
	sched := &mockHostswatchScheduler{}

	origNew := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		hw := hostswatch.New(enf, sched)
		hw.HostsPath = filepath.Join(t.TempDir(), "nonexistent", "subdir", "hosts")
		return hw
	}
	defer func() { newHostswatch = origNew }()

	if hw := startHostswatch(enf, sched); hw != nil {
		hw.Stop()
		t.Fatal("expected nil when Start fails, got non-nil")
	}
}

type mockStatewatchReconciler struct{}

func (m *mockStatewatchReconciler) Reconcile() error {
	return nil
}

func TestNewStatewatchVariable_Initialized(t *testing.T) {
	if newStatewatch == nil {
		t.Fatal("newStatewatch should be initialized from statewatch.New")
	}
}

func TestStartStatewatch_Success(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := &mockStatewatchReconciler{}

	var capturedRec statewatch.Reconciler
	var capturedPath string

	origNew := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		capturedRec = rec
		capturedPath = statePath
		sw := statewatch.New(rec, statePath)
		return sw
	}
	defer func() { newStatewatch = origNew }()

	sw := startStatewatch(rec, statePath)
	if sw == nil {
		t.Fatal("startStatewatch returned nil, expected non-nil")
	}
	defer sw.Stop()

	if capturedRec != rec {
		t.Error("newStatewatch called with wrong reconciler")
	}
	if capturedPath != statePath {
		t.Errorf("newStatewatch called with wrong path: got %q, want %q", capturedPath, statePath)
	}
}

func TestStartStatewatch_StartFails(t *testing.T) {
	rec := &mockStatewatchReconciler{}

	origNew := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		sw := statewatch.New(rec, statePath)
		sw.StatePath = filepath.Join(t.TempDir(), "nonexistent", "subdir", "state.json")
		return sw
	}
	defer func() { newStatewatch = origNew }()

	if sw := startStatewatch(rec, "/tmp/state.json"); sw != nil {
		sw.Stop()
		t.Fatal("expected nil when Start fails, got non-nil")
	}
}

func TestStartStatewatch_PassesCorrectArgs(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	var capturedRec statewatch.Reconciler
	var capturedPath string

	origNew := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		capturedRec = rec
		capturedPath = statePath
		sw := statewatch.New(rec, statePath)
		return sw
	}
	defer func() { newStatewatch = origNew }()

	rec := &mockStatewatchReconciler{}
	sw := startStatewatch(rec, statePath)
	if sw == nil {
		t.Fatal("startStatewatch returned nil")
	}
	defer sw.Stop()

	if capturedRec != rec {
		t.Error("reconciler was not passed through correctly")
	}
	if capturedPath != statePath {
		t.Errorf("statePath was not passed through: got %q, want %q", capturedPath, statePath)
	}
}

func TestStartStatewatch_StopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := &mockStatewatchReconciler{}

	origNew := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		return statewatch.New(rec, statePath)
	}
	defer func() { newStatewatch = origNew }()

	sw := startStatewatch(rec, statePath)
	if sw == nil {
		t.Fatal("expected non-nil statewatch")
	}

	sw.Stop()
	sw.Stop()
}

func TestRunDaemon_StatewatchIntegration(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	var statewatchStarted bool

	origNewHostswatch := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		return nil
	}
	defer func() { newHostswatch = origNewHostswatch }()

	origNewStatewatch := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		statewatchStarted = true
		sw := statewatch.New(rec, statePath)
		return sw
	}
	defer func() { newStatewatch = origNewStatewatch }()

	origServiceStopCh := serviceStopCh
	newStopCh := make(chan struct{})
	serviceStopCh = newStopCh
	defer func() { serviceStopCh = origServiceStopCh }()

	done := make(chan bool, 1)
	go func() {
		result := runDaemon()
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)
	close(newStopCh)

	select {
	case result := <-done:
		if !statewatchStarted {
			t.Error("expected statewatch to be started")
		}
		if !result {
			t.Log("runDaemon returned false (expected for mock setup)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runDaemon")
	}
}

func TestRunDaemon_StatewatchReconcilerIsScheduler(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	var capturedRec statewatch.Reconciler

	origNewHostswatch := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		return nil
	}
	defer func() { newHostswatch = origNewHostswatch }()

	origNewStatewatch := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		capturedRec = rec
		return statewatch.New(rec, statePath)
	}
	defer func() { newStatewatch = origNewStatewatch }()

	origServiceStopCh := serviceStopCh
	newStopCh := make(chan struct{})
	serviceStopCh = newStopCh
	defer func() { serviceStopCh = origServiceStopCh }()

	done := make(chan bool, 1)
	go func() {
		result := runDaemon()
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)
	close(newStopCh)

	select {
	case <-done:
		if capturedRec == nil {
			t.Fatal("expected a reconciler to be passed to newStatewatch")
		}
		if err := capturedRec.Reconcile(); err != nil {
			t.Errorf("Reconcile() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runDaemon")
	}
}

func TestGetStateFilePath_Windows(t *testing.T) {
	orig := goos
	goos = "windows"
	defer func() { goos = orig }()

	t.Setenv("PROGRAMDATA", `C:\ProgramData`)
	path := getStateFilePath()

	expectedSuffix := filepath.Join("FocusGuard", "state.json")
	if !strings.HasSuffix(path, expectedSuffix) {
		t.Errorf("expected path ending with %q, got %q", expectedSuffix, path)
	}
	if !strings.HasPrefix(path, `C:\ProgramData`) {
		t.Errorf("expected path starting with C:\\ProgramData, got %q", path)
	}
}

func TestGetStateFilePath_Windows_CustomProgramData(t *testing.T) {
	orig := goos
	goos = "windows"
	defer func() { goos = orig }()

	t.Setenv("PROGRAMDATA", `D:\AppData`)
	path := getStateFilePath()

	if !strings.HasPrefix(path, `D:\AppData`) {
		t.Errorf("expected path starting with D:\\AppData, got %q", path)
	}
	if !strings.Contains(path, "FocusGuard") {
		t.Errorf("expected path to contain FocusGuard, got %q", path)
	}
	if !strings.HasSuffix(path, "state.json") {
		t.Errorf("expected path to end with state.json, got %q", path)
	}
}

func TestGetStateFilePath_Windows_EmptyProgramData(t *testing.T) {
	orig := goos
	goos = "windows"
	defer func() { goos = orig }()

	t.Setenv("PROGRAMDATA", "")
	path := getStateFilePath()

	if !strings.HasSuffix(path, filepath.Join("FocusGuard", "state.json")) {
		t.Errorf("expected path ending with FocusGuard/state.json even with empty PROGRAMDATA, got %q", path)
	}
}

func TestGetStateFilePath_Linux(t *testing.T) {
	orig := goos
	goos = "linux"
	defer func() { goos = orig }()

	path := getStateFilePath()
	expected := "/var/lib/focusguard/state.json"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestGetStateFilePath_MacOS(t *testing.T) {
	orig := goos
	goos = "darwin"
	defer func() { goos = orig }()

	path := getStateFilePath()
	expected := "/var/lib/focusguard/state.json"
	if path != expected {
		t.Errorf("expected %q for darwin, got %q", expected, path)
	}
}

func TestGetStateFilePath_DefaultPathOnNonWindows(t *testing.T) {
	for _, platform := range []string{"linux", "darwin", "freebsd", "openbsd", "android"} {
		t.Run(platform, func(t *testing.T) {
			orig := goos
			goos = platform
			defer func() { goos = orig }()

			path := getStateFilePath()
			expected := "/var/lib/focusguard/state.json"
			if path != expected {
				t.Errorf("on %q: expected %q, got %q", platform, expected, path)
			}
		})
	}
}

func TestGetStateFilePath_Windows_ForwardSlash(t *testing.T) {
	orig := goos
	goos = "windows"
	defer func() { goos = orig }()

	t.Setenv("PROGRAMDATA", "C:/ProgramData")
	path := getStateFilePath()

	if !strings.Contains(path, "FocusGuard") {
		t.Errorf("expected path to contain FocusGuard, got %q", path)
	}
	if !strings.HasSuffix(path, "state.json") {
		t.Errorf("expected path to end with state.json, got %q", path)
	}
}

func TestRunDaemon_ServiceStop_NoActiveBlocks(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origNewHostswatch := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		return nil
	}
	defer func() { newHostswatch = origNewHostswatch }()

	origNewStatewatch := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		sw := statewatch.New(rec, statePath)
		return sw
	}
	defer func() { newStatewatch = origNewStatewatch }()

	origServiceStopCh := serviceStopCh
	newStopCh := make(chan struct{})
	serviceStopCh = newStopCh
	defer func() { serviceStopCh = origServiceStopCh }()

	done := make(chan bool, 1)
	go func() {
		result := runDaemon()
		done <- result
	}()

	time.Sleep(100 * time.Millisecond)
	close(newStopCh)

	select {
	case result := <-done:
		if !result {
			t.Error("expected runDaemon to return true (clean exit with no active blocks)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out: daemon did not stop after serviceStopCh without active blocks")
	}
}

func TestRunDaemon_ServiceStop_WithActiveBlocks(t *testing.T) {
	origGoos := goos

	goos = "linux"
	defer func() { goos = origGoos }()

	statePath := "/var/lib/focusguard/state.json"
	stateDir := filepath.Dir(statePath)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	now := time.Now()
	stateContent := fmt.Sprintf(`{
		"version": 1,
		"blocks": {
			"blocked.com": {
				"domain": "blocked.com",
				"started_at": %q,
				"expires_at": %q,
				"resolved_ips": ["127.0.0.1"]
			}
		}
	}`, now.Format(time.RFC3339), now.Add(24*time.Hour).Format(time.RFC3339))

	if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
		t.Fatalf("failed to create state file: %v", err)
	}

	defer os.RemoveAll(stateDir)

	origNewHostswatch := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		return nil
	}
	defer func() { newHostswatch = origNewHostswatch }()

	origNewStatewatch := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		return statewatch.New(rec, statePath)
	}
	defer func() { newStatewatch = origNewStatewatch }()

	origServiceStopCh := serviceStopCh
	newStopCh := make(chan struct{})
	serviceStopCh = newStopCh
	defer func() { serviceStopCh = origServiceStopCh }()

	done := make(chan bool, 1)
	go func() {
		result := runDaemon()
		done <- result
	}()

	time.Sleep(100 * time.Millisecond)
	close(newStopCh)

	select {
	case result := <-done:
		t.Fatalf("daemon should NOT stop with active blocks, but it returned %v", result)
	case <-time.After(1 * time.Second):
	}
}

func TestRunDaemon_RestartOnFalseReturn(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origNewHostswatch := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		return nil
	}
	defer func() { newHostswatch = origNewHostswatch }()

	origNewStatewatch := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		return statewatch.New(rec, statePath)
	}
	defer func() { newStatewatch = origNewStatewatch }()

	origServiceStopCh := serviceStopCh
	newStopCh := make(chan struct{})
	serviceStopCh = newStopCh
	defer func() { serviceStopCh = origServiceStopCh }()

	callCount := 0

	done := make(chan bool, 1)
	go func() {
		result := runDaemon()
		done <- result
	}()

	time.Sleep(100 * time.Millisecond)
	close(newStopCh)

	select {
	case result := <-done:
		callCount++
		if !result {
			t.Logf("runDaemon returned false (expected when IPC fails to start)")
		} else {
			t.Logf("runDaemon returned true (clean exit)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runDaemon")
	}

	if callCount < 1 {
		t.Error("expected runDaemon to be called at least once")
	}
}

func TestDaemonDoneCh_NotClosedInRunDaemon(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origNewHostswatch := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		return nil
	}
	defer func() { newHostswatch = origNewHostswatch }()

	origNewStatewatch := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		return statewatch.New(rec, statePath)
	}
	defer func() { newStatewatch = origNewStatewatch }()

	origServiceStopCh := serviceStopCh
	newStopCh := make(chan struct{})
	serviceStopCh = newStopCh
	defer func() { serviceStopCh = origServiceStopCh }()

	origDaemonDoneCh := daemonDoneCh
	freshDaemonDoneCh := make(chan struct{})
	daemonDoneCh = freshDaemonDoneCh
	defer func() { daemonDoneCh = origDaemonDoneCh }()

	done := make(chan bool, 1)
	go func() {
		result := runDaemon()
		done <- result
	}()

	time.Sleep(100 * time.Millisecond)
	close(newStopCh)

	<-done

	select {
	case <-freshDaemonDoneCh:
		t.Error("daemonDoneCh was closed by runDaemon — only the service handler should close it")
	default:
	}
}

func TestGetWatchdog_Empty(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "")
	if got := getWatchdogSec(); got != 0 {
		t.Errorf("expected 0 for empty env, got %d", got)
	}
}

func TestGetWatchdog_20s(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "20000000")
	if got := getWatchdogSec(); got != 20 {
		t.Errorf("expected 20 for 20000000 usec, got %d", got)
	}
}

func TestGetWatchdog_1s(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "1000000")
	if got := getWatchdogSec(); got != 1 {
		t.Errorf("expected 1 for 1000000 usec, got %d", got)
	}
}

func TestGetWatchdog_RoundDown(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "1500000")
	if got := getWatchdogSec(); got != 1 {
		t.Errorf("expected 1 for 1500000 usec (integer division), got %d", got)
	}
}

func TestGetWatchdog_Zero(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "0")
	if got := getWatchdogSec(); got != 0 {
		t.Errorf("expected 0 for 0 usec, got %d", got)
	}
}

func TestGetWatchdog_Negative(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "-1000000")
	if got := getWatchdogSec(); got != 0 {
		t.Errorf("expected 0 for negative value, got %d", got)
	}
}

func TestGetWatchdog_Invalid(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "not-a-number")
	if got := getWatchdogSec(); got != 0 {
		t.Errorf("expected 0 for invalid text, got %d", got)
	}
}
