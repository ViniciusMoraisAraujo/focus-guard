package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"focusguard/internal/apps"
	"focusguard/internal/enforcer"
	"focusguard/internal/hostswatch"
	"focusguard/internal/ipc"
	"focusguard/internal/policy"
	"focusguard/internal/pomodoro"
	"focusguard/internal/schedule"
	"focusguard/internal/scheduler"
	"focusguard/internal/statewatch"
	"focusguard/internal/store"
	"focusguard/internal/update"
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

// TestStartHostswatch_NilConstructorIsNoOp verifies startHostswatch returns nil
// without panicking when the watcher constructor yields nil — regression for
// the nil pointer panic that crashed TestRunDaemon_RestartOnFalseReturn (a
// stubbed nil constructor used to reach hw.Start() on a nil receiver).
func TestStartHostswatch_NilConstructorIsNoOp(t *testing.T) {
	origNew := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		return nil
	}
	defer func() { newHostswatch = origNew }()

	if hw := startHostswatch(nil, nil); hw != nil {
		t.Fatal("expected nil when the constructor returns nil")
	}
}

// TestStartStatewatch_NilConstructorIsNoOp verifies startStatewatch returns nil
// without panicking when the watcher constructor yields nil (same defensive
// contract as startHostswatch).
func TestStartStatewatch_NilConstructorIsNoOp(t *testing.T) {
	origNew := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		return nil
	}
	defer func() { newStatewatch = origNew }()

	if sw := startStatewatch(nil, ""); sw != nil {
		t.Fatal("expected nil when the constructor returns nil")
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
	requireRoot(t)
	stubProbeDaemonAlive(t, false)

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

	// Process Guard desativado nos testes de integração: um guard ativo
	// encerraria processos reais (steam/discord) durante a suíte.
	origNewProcessGuard := newProcessGuard
	newProcessGuard = func(denylist []string) processGuardStarter { return nil }
	defer func() { newProcessGuard = origNewProcessGuard }()

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
	requireRoot(t)
	stubProbeDaemonAlive(t, false)

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

	// Process Guard desativado nos testes de integração: um guard ativo
	// encerraria processos reais (steam/discord) durante a suíte.
	origNewProcessGuard := newProcessGuard
	newProcessGuard = func(denylist []string) processGuardStarter { return nil }
	defer func() { newProcessGuard = origNewProcessGuard }()

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

// TestVersionInfo_GoWinresFormat verifies the root versioninfo.json follows
// the go-winres schema: an application icon (RT_GROUP_ICON referencing the
// generated .ico), the version metadata (RT_VERSION with product/description)
// and the admin manifest (RT_MANIFEST). This is what go-winres make consumes
// to emit rsrc_windows_*.syso for the daemon and CLI executables.
func TestVersionInfo_GoWinresFormat(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "versioninfo-daemon.json"))
	if err != nil {
		t.Fatalf("leitura do versioninfo.json: %v", err)
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
	if len(v.Manifest) == 0 {
		t.Error("RT_MANIFEST ausente — o daemon perde o manifest requireAdministrator")
	}

	info := v.Version["#1"]["0000"].Info["0409"]
	if info["ProductName"] == "" || info["OriginalFilename"] == "" {
		t.Errorf("RT_VERSION.info.0409 incompleto: %+v", info)
	}
	if info["FileDescription"] == "" {
		t.Error("FileDescription vazio — aba de detalhes do Explorer sem descrição")
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
	if path != "/var/lib/focusguard/state.json" {
		t.Errorf("expected /var/lib/focusguard/state.json, got %q", path)
	}
}

func TestIsServerEditionFor_NoMarker(t *testing.T) {
	dir := t.TempDir()
	if isServerEditionFor(dir) {
		t.Errorf("expected server edition to be false without marker file")
	}
}

func TestIsServerEditionFor_MarkerPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, serverRoleFileName), nil, 0644); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}
	if !isServerEditionFor(dir) {
		t.Errorf("expected server edition to be true with marker file")
	}
}

func TestIsServerEditionFor_NonExistentDir(t *testing.T) {
	if isServerEditionFor(filepath.Join(t.TempDir(), "missing")) {
		t.Errorf("expected server edition to be false for missing directory")
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
	requireRoot(t)
	stubProbeDaemonAlive(t, false)

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

	// Process Guard desativado nos testes de integração: um guard ativo
	// encerraria processos reais (steam/discord) durante a suíte.
	origNewProcessGuard := newProcessGuard
	newProcessGuard = func(denylist []string) processGuardStarter { return nil }
	defer func() { newProcessGuard = origNewProcessGuard }()

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
	requireRoot(t)
	stubProbeDaemonAlive(t, false)

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

	// Process Guard desativado nos testes de integração: um guard ativo
	// encerraria processos reais (steam/discord) durante a suíte.
	origNewProcessGuard := newProcessGuard
	newProcessGuard = func(denylist []string) processGuardStarter { return nil }
	defer func() { newProcessGuard = origNewProcessGuard }()

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
	stubProbeDaemonAlive(t, false)
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

	// Process Guard desativado nos testes de integração: um guard ativo
	// encerraria processos reais (steam/discord) durante a suíte.
	origNewProcessGuard := newProcessGuard
	newProcessGuard = func(denylist []string) processGuardStarter { return nil }
	defer func() { newProcessGuard = origNewProcessGuard }()

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
	stubProbeDaemonAlive(t, false)
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

	// Process Guard desativado nos testes de integração: um guard ativo
	// encerraria processos reais (steam/discord) durante a suíte.
	origNewProcessGuard := newProcessGuard
	newProcessGuard = func(denylist []string) processGuardStarter { return nil }
	defer func() { newProcessGuard = origNewProcessGuard }()

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

type fakeUpdaterAPI struct {
	result       *update.UpdateResult
	checkErr     error
	applyErr     error
	applyCalls   int32
	appliedTo    []string
	channel      string
	cleanupCalls int32
}

func (f *fakeUpdaterAPI) CheckForUpdate(_ context.Context) (*update.UpdateResult, error) {
	return f.result, f.checkErr
}

func (f *fakeUpdaterAPI) SetChannel(channel string) { f.channel = channel }

func (f *fakeUpdaterAPI) CleanupStale(_ string) { atomic.AddInt32(&f.cleanupCalls, 1) }

func (f *fakeUpdaterAPI) UpdateToAll(_ context.Context, _ *update.UpdateResult, binaries []string) ([]string, error) {
	atomic.AddInt32(&f.applyCalls, 1)
	f.appliedTo = append([]string(nil), binaries...)
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	backups := make([]string, 0, len(binaries))
	for _, b := range binaries {
		backups = append(backups, b+".bak")
	}
	return backups, nil
}

// TestUpdateInProgressFlag_Lifecycle verifies the Bug 2 flag helpers: the flag
// is written before the update, must survive the daemon restart (so it is NOT
// removed on success — only the healthy boot does that) and is removed on the
// error path.
func TestUpdateInProgressFlag_Lifecycle(t *testing.T) {
	dir := t.TempDir()

	markUpdateInProgress(dir)
	if _, err := os.Stat(filepath.Join(dir, updateInProgressFile)); err != nil {
		t.Fatalf("flag deve existir após markUpdateInProgress: %v", err)
	}

	// remover flag inexistente é no-op, sem erro
	clearUpdateInProgress(dir)
	clearUpdateInProgress(dir)
	if _, err := os.Stat(filepath.Join(dir, updateInProgressFile)); !os.IsNotExist(err) {
		t.Error("flag deve ser removida por clearUpdateInProgress")
	}
}

func TestDaemonUpdater_Check_NoUpdate(t *testing.T) {
	d := &daemonUpdater{u: &fakeUpdaterAPI{}, binaries: []string{"/tmp/focusguard-daemon"}}

	st, err := d.Check(context.Background(), false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Available {
		t.Error("expected no update available")
	}
	if st.CurrentVersion != daemonVersion {
		t.Errorf("expected CurrentVersion %q, got %q", daemonVersion, st.CurrentVersion)
	}
}

func TestDaemonUpdater_Check_UpdateAvailable(t *testing.T) {
	d := &daemonUpdater{
		u:        &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}},
		binaries: []string{"/tmp/focusguard-daemon"},
	}

	st, err := d.Check(context.Background(), false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Available {
		t.Error("expected update available")
	}
	if st.NewVersion != "1.1.0" {
		t.Errorf("expected NewVersion 1.1.0, got %q", st.NewVersion)
	}
	if st.Applied {
		t.Error("expected Applied=false when apply is false")
	}
}

func TestDaemonUpdater_Check_Error(t *testing.T) {
	d := &daemonUpdater{
		u:        &fakeUpdaterAPI{checkErr: errors.New("network down")},
		binaries: []string{"/tmp/focusguard-daemon"},
	}

	_, err := d.Check(context.Background(), false, "")
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected network error, got %v", err)
	}
}

// stubStopForBinarySwap troca stopForBinarySwap por um no-op que registra a
// chamada — os testes de Check nunca tocam em serviços/processos reais (o
// default Windows mataria o tray de verdade durante a suíte). Devolve um
// ponteiro para o contador de chamadas.
func stubStopForBinarySwap(t *testing.T) *int32 {
	t.Helper()
	var calls int32
	orig := stopForBinarySwap
	stopForBinarySwap = func(_ []string) func() {
		atomic.AddInt32(&calls, 1)
		return func() {}
	}
	t.Cleanup(func() { stopForBinarySwap = orig })
	return &calls
}

func TestDaemonUpdater_Check_Apply(t *testing.T) {
	stubStopForBinarySwap(t)
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon")
	fake := &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}}
	d := &daemonUpdater{u: fake, binaries: []string{daemon}}

	st, err := d.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Applied {
		t.Error("expected Applied=true when apply is true")
	}
	if atomic.LoadInt32(&fake.applyCalls) != 1 {
		t.Errorf("expected 1 UpdateToAll call, got %d", fake.applyCalls)
	}
	// Bug 1: após o update com sucesso, os backups precisam ser varridos —
	// sem isso os .bak.<timestamp> se acumulam para sempre.
	if atomic.LoadInt32(&fake.cleanupCalls) != 1 {
		t.Errorf("expected 1 CleanupStale call after successful apply, got %d", fake.cleanupCalls)
	}
	// Bug 2: a flag de update permanece até o boot saudável da nova versão —
	// ela precisa sobreviver ao restart para manter o watchdog de fora.
	if _, err := os.Stat(filepath.Join(dir, updateInProgressFile)); err != nil {
		t.Error("update.inprogress deve existir após aplicar o update (só o boot saudável a remove)")
	}
}

func TestDaemonUpdater_Check_ApplyError(t *testing.T) {
	stubStopForBinarySwap(t)
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon")
	fake := &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}, applyErr: errors.New("file locked")}
	d := &daemonUpdater{u: fake, binaries: []string{daemon}}

	_, err := d.Check(context.Background(), true, "")
	if err == nil || !strings.Contains(err.Error(), "file locked") {
		t.Fatalf("expected apply error, got %v", err)
	}
	// Sem aplicação → sem varredura: o update falhou e o rollback manteve os
	// backups para o smart recovery decidir.
	if atomic.LoadInt32(&fake.cleanupCalls) != 0 {
		t.Errorf("expected no CleanupStale on failed apply, got %d", fake.cleanupCalls)
	}
	// Update falhou → o daemon segue rodando e a flag não pode ficar para
	// trás (senão o watchdog ficaria mudo à toa).
	if _, err := os.Stat(filepath.Join(dir, updateInProgressFile)); !os.IsNotExist(err) {
		t.Error("update.inprogress deve ser removida quando o apply falha")
	}
}

func TestDaemonUpdater_Check_NilUpdater(t *testing.T) {
	d := &daemonUpdater{u: nil, binaries: []string{"/tmp/focusguard-daemon"}}

	st, err := d.Check(context.Background(), false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Available {
		t.Error("expected no update when updater is nil")
	}
}

// ---------------------------------------------------------------------------
// Auto-update: multi-binário (Bug 1) e restart pós-update (Bug 2)
// ---------------------------------------------------------------------------

// TestSiblingBinaries_ReturnsAllFocusGuardBinaries verifies the daemon derives
// the sibling binary paths (CLI, tray, watchdog) next to its own executable,
// so `focusguard update` replaces the whole suite — not just the daemon.
func TestSiblingBinaries_ReturnsAllFocusGuardBinaries(t *testing.T) {
	got := siblingBinaries("/usr/local/bin/focusguard-daemon")
	want := []string{
		// O daemon vem primeiro de propósito: é o binário decisivo do update
		// (o único que não pode ser parado antes do swap) — se a troca dele
		// falhar, nada mais foi trocado e o fallback move-on-reboot dispara.
		"/usr/local/bin/focusguard-daemon",
		"/usr/local/bin/focusguard",
		"/usr/local/bin/focusguard-tray",
		"/usr/local/bin/focusguard-watchdog",
		"/usr/local/bin/focusguard-web",
	}
	if len(got) != len(want) {
		t.Fatalf("siblingBinaries = %v, want %v", got, want)
	}
	for i := range want {
		// separadores de path dependem do SO (POSIX no Linux, barra invertida no
		// Windows) — normalize para comparar em qualquer plataforma
		if got[i] != filepath.FromSlash(want[i]) {
			t.Errorf("siblingBinaries[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSiblingBinaries_WindowsExt adds the .exe suffix on Windows so each
// binary is found next to the daemon.
func TestSiblingBinaries_WindowsExt(t *testing.T) {
	got := siblingBinaries(`C:\Program Files\FocusGuard\focusguard-daemon.exe`)
	for _, p := range got {
		if !strings.HasSuffix(p, ".exe") {
			t.Errorf("expected .exe suffix on windows path, got %q", p)
		}
	}
}

// TestDaemonUpdater_Check_Apply_AllBinaries verifies the updater applies to
// every sibling binary (daemon + CLI + tray + watchdog), not just the daemon.
func TestDaemonUpdater_Check_Apply_AllBinaries(t *testing.T) {
	stubStopForBinarySwap(t)
	dir := t.TempDir()
	fake := &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}}
	d := &daemonUpdater{
		u: fake,
		binaries: []string{
			filepath.Join(dir, "focusguard"),
			filepath.Join(dir, "focusguard-daemon"),
			filepath.Join(dir, "focusguard-tray"),
			filepath.Join(dir, "focusguard-watchdog"),
		},
	}

	st, err := d.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Applied {
		t.Error("expected Applied=true")
	}
	if atomic.LoadInt32(&fake.applyCalls) != 1 {
		t.Fatalf("expected 1 UpdateToAll call, got %d", fake.applyCalls)
	}
}

// TestDaemonUpdater_Check_Apply_PartialFailureNoRestart registers that a
// partial failure must abort the update (Applied=false) — the daemon must not
// restart into a half-updated state.
func TestDaemonUpdater_Check_Apply_PartialFailureNoRestart(t *testing.T) {
	stubStopForBinarySwap(t)
	dir := t.TempDir()
	d := &daemonUpdater{
		u: &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}, applyErr: errors.New("access denied")},
		binaries: []string{
			filepath.Join(dir, "focusguard"),
			filepath.Join(dir, "focusguard-daemon"),
		},
	}

	st, err := d.Check(context.Background(), true, "")
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("expected apply error, got %v", err)
	}
	if st.Applied {
		t.Error("expected Applied=false on failure — no restart into partial state")
	}
}

// TestDaemonUpdater_Check_Apply_PendingReboot verifica o fallback
// move-on-reboot: quando o UpdateToAll retorna ErrScheduledOnReboot (o exe do
// próprio daemon ficou travado para rename), o Check NÃO marca Applied, NÃO
// deixa a flag de update para trás (o daemon segue rodando a versão antiga) e
// reporta PendingReboot para o CLI/UI.
func TestDaemonUpdater_Check_Apply_PendingReboot(t *testing.T) {
	calls := stubStopForBinarySwap(t)
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon")
	fake := &fakeUpdaterAPI{
		result:   &update.UpdateResult{Version: "1.1.0"},
		applyErr: update.ErrScheduledOnReboot,
	}
	d := &daemonUpdater{u: fake, binaries: []string{daemon}}

	st, err := d.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("ErrScheduledOnReboot não é erro para o caller: %v", err)
	}
	if st.Applied {
		t.Error("Applied deve ser false no fallback move-on-reboot (daemon não reinicia)")
	}
	if !st.PendingReboot {
		t.Error("PendingReboot deve ser true quando a troca foi agendada para o boot")
	}
	// A flag não pode ficar: o daemon segue rodando e o watchdog voltaria mudo.
	if _, err := os.Stat(filepath.Join(dir, updateInProgressFile)); !os.IsNotExist(err) {
		t.Error("update.inprogress deve ser removida no caminho PendingReboot")
	}
	// Sem aplicação → sem varredura de backups.
	if atomic.LoadInt32(&fake.cleanupCalls) != 0 {
		t.Errorf("CleanupStale não deve rodar no caminho PendingReboot, got %d", fake.cleanupCalls)
	}
	// O prepare (parar watchdog + tray) ainda roda antes do swap.
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("stopForBinarySwap deveria ser chamado, got %d", atomic.LoadInt32(calls))
	}
}

// TestDaemonUpdater_Check_Apply_StopsGuards verifica que o apply chama o
// stopForBinarySwap (parar serviço do watchdog + processo do tray) antes de
// trocar os binários — o fix do "Acesso negado" da task.md: sem isso o exe do
// tray (GUI em execução) fica travado para rename.
func TestDaemonUpdater_Check_Apply_StopsGuards(t *testing.T) {
	calls := stubStopForBinarySwap(t)
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon")
	fake := &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}}
	d := &daemonUpdater{u: fake, binaries: []string{daemon}}

	st, err := d.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Applied {
		t.Error("expected Applied=true")
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("stopForBinarySwap deveria rodar antes do swap, got %d chamadas", atomic.LoadInt32(calls))
	}
}

// TestNewDaemonUpdater_WiresExistingSiblings verifies newDaemonUpdater builds
// the binary list from the daemon path (Bug 1): every installed sibling
// (CLI, watchdog) is included, a missing one (tray) is skipped, and the daemon
// itself is always present.
func TestNewDaemonUpdater_WiresExistingSiblings(t *testing.T) {
	dir := t.TempDir()
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	for _, n := range []string{"focusguard", "focusguard-daemon", "focusguard-watchdog"} {
		if err := os.WriteFile(filepath.Join(dir, n+ext), []byte("bin"), 0755); err != nil {
			t.Fatalf("write fake binary: %v", err)
		}
	}
	// tray não é criado de propósito — deve ser descartado.

	fake := &fakeUpdaterAPI{}
	d := newDaemonUpdater(filepath.Join(dir, "focusguard-daemon"+ext), fake)

	expected := []string{
		// Ordem do siblingBinaries: daemon primeiro (binário decisivo do swap).
		filepath.Join(dir, "focusguard-daemon"+ext),
		filepath.Join(dir, "focusguard"+ext),
		filepath.Join(dir, "focusguard-watchdog"+ext),
	}
	if len(d.binaries) != len(expected) {
		t.Fatalf("binaries = %v, want %v", d.binaries, expected)
	}
	for i := range expected {
		if d.binaries[i] != expected[i] {
			t.Errorf("binaries[%d] = %q, want %q", i, d.binaries[i], expected[i])
		}
	}
	if d.u != fake {
		t.Error("updater não foi repassado ao daemonUpdater")
	}
}

func TestSetupUpdateIntegration_DevVersionDisabled(t *testing.T) {
	orig := daemonVersion
	daemonVersion = "0.0.0-dev"
	defer func() { daemonVersion = orig }()

	server := ipc.NewServer(nil)
	stop := setupUpdateIntegration(server)
	if stop != nil {
		stop()
		t.Error("expected nil stop func for dev version (integration disabled)")
	}

	st, err := server.RefreshUpdateStatus(context.Background())
	if err != nil {
		t.Fatalf("RefreshUpdateStatus: %v", err)
	}
	if st.Available {
		t.Error("expected no update status when integration is disabled")
	}
}

func TestSetupUpdateIntegration_Enabled(t *testing.T) {
	origVersion := daemonVersion
	daemonVersion = "1.0.0"
	defer func() { daemonVersion = origVersion }()

	origUpdater := newUpdater
	fake := &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}}
	newUpdater = func(owner, repo string) updaterAPI {
		return fake
	}
	defer func() { newUpdater = origUpdater }()

	origInterval := updateCheckInterval
	updateCheckInterval = 0
	defer func() { updateCheckInterval = origInterval }()

	server := ipc.NewServer(nil)
	stop := setupUpdateIntegration(server)
	if stop == nil {
		t.Fatal("expected non-nil stop func for release version")
	}
	defer stop()

	st, err := server.RefreshUpdateStatus(context.Background())
	if err != nil {
		t.Fatalf("RefreshUpdateStatus: %v", err)
	}
	if !st.Available || st.NewVersion != "1.1.0" {
		t.Errorf("expected update 1.1.0 available, got %+v", st)
	}
}

// ---------------------------------------------------------------------------
// Restart pós-update (Bug 2)
// ---------------------------------------------------------------------------

// fakeRestartScheduler reports active blocks through HasActiveBlocks, letting
// the restart decision be exercised without a real scheduler.
type fakeRestartScheduler struct{ active bool }

func (f *fakeRestartScheduler) HasActiveBlocks() bool { return f.active }

// TestRestartAfterUpdate_ExitsWhenIdle verifies that after a successful update
// the daemon calls os.Exit(1) when there are no active blocks and no pomodoro
// session — so systemd (Restart=always) / SCM recovery respawn it with the new
// binary (no zombie). Exit code 1 (not 0) on purpose: the Windows SCM only
// applies the recovery actions on a failure exit, 0 would leave the service
// dead until reboot.
func TestRestartAfterUpdate_ExitsWhenIdle(t *testing.T) {
	origExit := osExit
	defer func() { osExit = origExit }()
	exited := make(chan int, 1)
	osExit = func(code int) { exited <- code }

	sched := &fakeRestartScheduler{active: false}

	go restartAfterUpdate(sched, nil)

	select {
	case code := <-exited:
		if code != 1 {
			t.Errorf("expected exit code 1 (falha p/ SCM recovery), got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("os.Exit não foi chamado — daemon ficaria zumbi após o update")
	}
}

// TestRestartAfterUpdate_ExitsEvenWithActiveBlocks verifies that applying a
// version update restarts the daemon immediately even with blocks active: the
// daemon stops the pomodoro session and exits so the supervisor brings up the
// new version — the blocks stay in the state mirror and the boot restores them.
func TestRestartAfterUpdate_ExitsEvenWithActiveBlocks(t *testing.T) {
	origExit := osExit
	defer func() { osExit = origExit }()
	exited := make(chan int, 1)
	osExit = func(code int) { exited <- code }

	sched := &fakeRestartScheduler{active: true}
	var sessionStopped atomic.Int32
	// stopSession retorna erro de propósito (simula uma sessão estrita):
	// o update nunca fica pendente — o erro só vira aviso no log e o restart
	// acontece mesmo assim (contrato best-effort).
	stopSession := func() (pomodoro.State, error) {
		sessionStopped.Add(1)
		return pomodoro.State{}, errors.New("sessão estrita")
	}

	go restartAfterUpdate(sched, stopSession)

	select {
	case code := <-exited:
		if code != 1 {
			t.Errorf("expected exit code 1 (falha p/ SCM recovery), got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("os.Exit não foi chamado — update com bloqueios ativos deve reiniciar na hora")
	}

	if sessionStopped.Load() != 1 {
		t.Error("esperava a sessão pomodoro a ser encerrada antes do restart")
	}
}

func TestStartPeriodicUpdateCheck_Stops(t *testing.T) {
	server := ipc.NewServer(nil)
	fake := &fakeUpdateChecker{}
	server.SetUpdateChecker(fake)

	stop := startPeriodicUpdateCheck(server, time.Hour)
	if stop == nil {
		t.Fatal("expected non-nil stop func")
	}
	stop()
	stop()
}

type fakeUpdateChecker struct {
	calls int32
}

func (f *fakeUpdateChecker) Check(_ context.Context, apply bool, _ string) (ipc.UpdateStatus, error) {
	atomic.AddInt32(&f.calls, 1)
	return ipc.UpdateStatus{CurrentVersion: "1.0.0"}, nil
}

// ---- Real store → statewatch → scheduler chain integration tests ----
//
// These wire the exact chain runDaemon uses (store.SetOnSave feeds the watcher's
// content-based self-write marker after each write, and the scheduler is the
// reconciler), with a fake enforcer since the real one needs root/firewall.
// They prove external tampering (edit/delete/rename) is detected through the
// full fsnotify → statewatch → Reconcile → store path and that the disk mirror
// is restored from RAM.

// fakeDaemonEnforcer implements the full enforcer.Enforcer interface with
// call counters; Sync and BlockDoH are the operations the scheduler triggers
// from Reconcile.
type fakeDaemonEnforcer struct {
	syncCalls int32
}

func (f *fakeDaemonEnforcer) BlockDomain(_ string, _ []string) error { return nil }
func (f *fakeDaemonEnforcer) UnblockDomain(_ string, _ []string) error {
	return nil
}
func (f *fakeDaemonEnforcer) Sync(_ map[string][]string) error {
	atomic.AddInt32(&f.syncCalls, 1)
	return nil
}
func (f *fakeDaemonEnforcer) BlockDoH() error           { return nil }
func (f *fakeDaemonEnforcer) UnblockDoH() error         { return nil }
func (f *fakeDaemonEnforcer) BlockAll(_ []string) error { return nil }
func (f *fakeDaemonEnforcer) UnblockAll() error         { return nil }
func (f *fakeDaemonEnforcer) Status() (enforcer.EnforcerStatus, error) {
	return enforcer.EnforcerStatus{}, nil
}

// seededStateFile writes a state.json containing one active block, so the
// scheduler has RAM content after bootstrap without any DNS lookups.
func seededStateFile(t *testing.T) string {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	now := time.Now()
	content := fmt.Sprintf(`{
		"version": 1,
		"blocks": {
			"example.com": {
				"domain": "example.com",
				"started_at": %q,
				"expires_at": %q,
				"resolved_ips": ["127.0.0.1"]
			}
		}
	}`, now.Add(-time.Minute).Format(time.RFC3339), now.Add(24*time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(statePath, []byte(content), 0644); err != nil {
		t.Fatalf("seed state file: %v", err)
	}
	return statePath
}

// newRealWatcherChain wires store → statewatch → scheduler the same way
// runDaemon does and starts everything, returning the pieces for assertions.
func newRealWatcherChain(t *testing.T) (*store.Store, *scheduler.Scheduler, *fakeDaemonEnforcer, string) {
	t.Helper()
	statePath := seededStateFile(t)
	st, err := store.NewStore(statePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	enf := &fakeDaemonEnforcer{}
	sched := scheduler.NewScheduler(st, enf)
	sw := statewatch.New(sched, statePath)
	st.SetOnSave(sw.MarkSelfWrite)
	if err := sw.Start(); err != nil {
		t.Fatalf("statewatch.Start: %v", err)
	}
	t.Cleanup(sw.Stop)
	if err := sched.Start(); err != nil {
		t.Fatalf("scheduler.Start: %v", err)
	}
	return st, sched, enf, statePath
}

// ---------------------------------------------------------------------------
// Pomodoro prefs & completion watcher
// ---------------------------------------------------------------------------

// stubBlockerForDaemon records BlockDomains calls without touching the OS.
type stubBlockerForDaemon struct{ calls int }

func (s *stubBlockerForDaemon) BlockDomains([]string, time.Duration) ([]policy.Block, error) {
	s.calls++
	return nil, nil
}

// TestPersistPomodoroSummary_PersistsMinutes verifies the daemon persists the
// resolved work/rest/cycles of a finished session as the next defaults — in
// MINUTES (the IPC/CLI unit), so a 25m session saves 25, not 0.
func TestPersistPomodoroSummary_PersistsMinutes(t *testing.T) {
	prefsPath := filepath.Join(t.TempDir(), "pomodoro.json")
	prefs := pomodoro.NewPrefs(prefsPath)

	persistPomodoroSummary(prefs, pomodoro.CompletionSummary{
		Preset: "social",
		Work:   25 * time.Minute,
		Rest:   5 * time.Minute,
		Cycles: 4,
		Focus:  50 * time.Minute,
	})

	w, r, cy := prefs.Resolve(0, -1, 0)
	if w != 25 || r != 5 || cy != 4 {
		t.Errorf("persisted defaults = %d/%d/%d, want 25/5/4", w, r, cy)
	}
	// Reabrindo o arquivo (persistência real): os padrões sobrevivem ao restart.
	prefs2 := pomodoro.NewPrefs(prefsPath)
	if w2, r2, c2 := prefs2.Resolve(0, -1, 0); w2 != 25 || r2 != 5 || c2 != 4 {
		t.Errorf("reloaded defaults = %d/%d/%d, want 25/5/4", w2, r2, c2)
	}
}

// TestPersistPomodoroSummary_SubMinuteDurations verifies work/rest sub-minute
// round UP to 1 minute (the minimum IPC unit) so Resolve never falls back to
// the default because of a 0.
func TestPersistPomodoroSummary_SubMinuteDurations(t *testing.T) {
	prefs := pomodoro.NewPrefs(filepath.Join(t.TempDir(), "pomodoro.json"))
	persistPomodoroSummary(prefs, pomodoro.CompletionSummary{
		Preset: "social", Work: 30 * time.Millisecond, Rest: 10 * time.Millisecond, Cycles: 2, Focus: 60 * time.Millisecond,
	})
	w, r, cy := prefs.Resolve(0, -1, 0)
	if w != 1 || r != 1 || cy != 2 {
		t.Errorf("sub-minute defaults = %d/%d/%d, want 1/1/2", w, r, cy)
	}
}

// TestWatchPomodoroCompletions_NilController is a no-op (daemon with a nil
// controller must not panic).
func TestWatchPomodoroCompletions_NilController(t *testing.T) {
	stop := watchPomodoroCompletions(nil, nil, nil)
	stop() // não deve panico
}

// ---------------------------------------------------------------------------
// Process Guard wiring
// ---------------------------------------------------------------------------

type fakeProcessGuard struct {
	onStart  func(func() bool)
	stopped  bool
	denylist []string
}

func (f *fakeProcessGuard) Start(isActive func() bool) {
	if f.onStart != nil {
		f.onStart(isActive)
	}
}

func (f *fakeProcessGuard) Stop() { f.stopped = true }

func (f *fakeProcessGuard) SetDenylist(names []string) {
	f.denylist = append([]string(nil), names...)
}

type fakeActivityScheduler struct{ active bool }

func (f *fakeActivityScheduler) HasActiveBlocks() bool { return f.active }

// TestStartProcessGuard_WiresSchedulerChecker verifies startProcessGuard calls
// guard.Start with the scheduler's HasActiveBlocks as the activity checker, so
// the guard only kills processes while a focus session is active.
func TestStartProcessGuard_WiresSchedulerChecker(t *testing.T) {
	origNew := newProcessGuard
	defer func() { newProcessGuard = origNew }()

	var captured func() bool
	newProcessGuard = func(denylist []string) processGuardStarter {
		return &fakeProcessGuard{onStart: func(fn func() bool) { captured = fn }}
	}

	sched := &fakeActivityScheduler{active: true}
	pg := startProcessGuard(sched, []string{"steam.exe", "discord.exe"})
	if pg == nil {
		t.Fatal("startProcessGuard should return a guard")
	}
	if captured == nil {
		t.Fatal("guard.Start should be called with the scheduler checker")
	}
	if !captured() {
		t.Error("checker should report true when the scheduler has active blocks")
	}
	sched.active = false
	if captured() {
		t.Error("checker should report false when the scheduler has no active blocks")
	}
	pg.Stop()
}

func TestStartProcessGuard_NilGuardIsNoOp(t *testing.T) {
	origNew := newProcessGuard
	defer func() { newProcessGuard = origNew }()

	newProcessGuard = func(denylist []string) processGuardStarter { return nil }
	if pg := startProcessGuard(&fakeActivityScheduler{}, nil); pg != nil {
		t.Error("expected nil guard when the constructor returns nil")
	}
}

// TestStartProcessGuard_ReceivesStoreDenylist verifies the guard is created
// with the persisted apps store denylist (not a hardcoded default anymore).
func TestStartProcessGuard_ReceivesStoreDenylist(t *testing.T) {
	origNew := newProcessGuard
	defer func() { newProcessGuard = origNew }()

	var got []string
	newProcessGuard = func(denylist []string) processGuardStarter {
		got = denylist
		return &fakeProcessGuard{}
	}

	pg := startProcessGuard(&fakeActivityScheduler{}, []string{"spotify", "steam"})
	if pg == nil {
		t.Fatal("expected a guard")
	}
	if len(got) != 2 || got[0] != "spotify" {
		t.Errorf("guard should receive the store denylist, got %v", got)
	}
}

// TestGuardApps_AddRefreshesGuard verifies the apps manager that wires store +
// process guard refreshes the live guard denylist after every change, so a
// user running "focusguard apps add spotify" takes effect on the next scan.
func TestGuardApps_AddRefreshesGuard(t *testing.T) {
	st := apps.NewStore(filepath.Join(t.TempDir(), "apps.json"))
	pg := &fakeProcessGuard{}
	ga := &guardApps{store: st, guard: pg}

	if err := ga.Add("spotify.exe"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !slicesContains(pg.denylist, "spotify") {
		t.Errorf("guard denylist should contain spotify after Add, got %v", pg.denylist)
	}
	if len(st.List()) != 3 {
		t.Errorf("store should persist 3 entries (defaults+spotify), got %v", st.List())
	}
}

func TestGuardApps_RemoveRefreshesGuard(t *testing.T) {
	st := apps.NewStore(filepath.Join(t.TempDir(), "apps.json"))
	pg := &fakeProcessGuard{}
	ga := &guardApps{store: st, guard: pg}

	if err := ga.Remove("steam"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, x := range pg.denylist {
		if x == "steam" {
			t.Errorf("guard denylist should drop steam after Remove, got %v", pg.denylist)
		}
	}
}

func slicesContains(list []string, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Agendamento recorrente (wiring no daemon)
// ---------------------------------------------------------------------------

// fakeScheduleBlocker implements schedule.Blocker recording BlockDomains calls.
type fakeScheduleBlocker struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeScheduleBlocker) ListBlocks() ([]policy.Block, error) { return nil, nil }

func (f *fakeScheduleBlocker) BlockDomains(domains []string, _ time.Duration) ([]policy.Block, error) {
	f.mu.Lock()
	f.calls = append(f.calls, strings.Join(domains, ","))
	f.mu.Unlock()
	return nil, nil
}

func (f *fakeScheduleBlocker) blockCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeScheduleResolver maps preset names to domains (stands in for preset.Store).
type fakeScheduleResolver map[string][]string

func (f fakeScheduleResolver) Resolve(name string) ([]string, error) {
	domains, ok := f[name]
	if !ok {
		return nil, fmt.Errorf("preset desconhecido %q", name)
	}
	return domains, nil
}

// TestStartScheduleWorker_AppliesActiveRule verifies the daemon's schedule
// worker applies an active recurring rule on startup (first tick runs
// immediately, so the daemon catches a window that began while it was down).
func TestStartScheduleWorker_AppliesActiveRule(t *testing.T) {
	mgr := schedule.NewManager(filepath.Join(t.TempDir(), "schedules.json"))
	// regra ativa em qualquer horário: todos os dias, 00:00-23:59
	if _, err := mgr.Add(schedule.Rule{
		Preset:  "social",
		Days:    []int{0, 1, 2, 3, 4, 5, 6},
		Start:   "00:00",
		End:     "23:59",
		Enabled: true,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	b := &fakeScheduleBlocker{}
	resolver := fakeScheduleResolver{"social": {"twitter.com", "facebook.com"}}

	stop := startScheduleWorker(mgr, resolver, b, time.Hour)
	defer stop()

	// o primeiro tick é imediato; aguarda a aplicação da regra
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if calls := b.blockCalls(); len(calls) > 0 {
			if !strings.Contains(calls[0], "twitter.com") || !strings.Contains(calls[0], "facebook.com") {
				t.Errorf("BlockDomains inesperado: %v", calls)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("worker não aplicou a regra ativa dentro do timeout")
}

// TestStartScheduleWorker_NoActiveRuleNoCalls verifies the worker stays quiet
// when no rule window is active (no spurious BlockDomains).
func TestStartScheduleWorker_NoActiveRuleNoCalls(t *testing.T) {
	mgr := schedule.NewManager("")
	b := &fakeScheduleBlocker{}

	stop := startScheduleWorker(mgr, fakeScheduleResolver{}, b, 30*time.Millisecond)
	defer stop()

	time.Sleep(150 * time.Millisecond)
	if calls := b.blockCalls(); len(calls) != 0 {
		t.Errorf("sem regras ativas não deve bloquear nada, got %v", calls)
	}
}

// waitForCondition polls cond until it returns true or the timeout elapses.
func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

// waitForFileContains polls path until its content contains want or times out.
func waitForFileContains(t *testing.T, path, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("timed out waiting for %s to contain %q; got %q", path, want, data)
}

// waitForCalls polls an atomic counter until it reaches at least want or times
// out — a way to observe the enforcer side of a completed bootstrap/reconcile.
func waitForCalls(t *testing.T, counter *int32, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(counter) >= int32(want) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for counter to reach %d (got %d)", want, atomic.LoadInt32(counter))
}

// requireRoot skips the test unless running as root: runDaemon-based tests
// write the state file to the real /var/lib/focusguard path.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root: runDaemon writes state to /var/lib/focusguard")
	}
}

// stubProbeDaemonAlive desativa o ping de singleton (probeDaemonAlive) durante
// um teste, devolvendo o resultado dado. Sem isso, um teste que roda com o
// daemon real de pé (porta IPC respondendo) veria o runDaemon encerrar cedo.
func stubProbeDaemonAlive(t *testing.T, result bool) {
	t.Helper()
	orig := probeDaemonAlive
	probeDaemonAlive = func() bool { return result }
	t.Cleanup(func() { probeDaemonAlive = orig })
}

// TestRunDaemon_Singleton_ExitsEarly verifies a second daemon instance (another
// one already answering the IPC port) exits cleanly instead of crash-looping on
// the bind — the fix for "bind: address already in use → Reiniciando..." para
// sempre. runDaemon must return true (clean exit) WITHOUT starting statewatch.
func TestRunDaemon_Singleton_ExitsEarly(t *testing.T) {
	stubProbeDaemonAlive(t, true)

	origNewHostswatch := newHostswatch
	newHostswatch = func(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
		t.Error("hostswatch não deveria iniciar quando outra instância já está ativa")
		return nil
	}
	defer func() { newHostswatch = origNewHostswatch }()

	var statewatchStarted bool
	origNewStatewatch := newStatewatch
	newStatewatch = func(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
		statewatchStarted = true
		return statewatch.New(rec, statePath)
	}
	defer func() { newStatewatch = origNewStatewatch }()

	if result := runDaemon(); !result {
		t.Error("runDaemon deveria retornar true (exit limpo) quando outra instância está ativa")
	}
	if statewatchStarted {
		t.Error("statewatch não deveria iniciar em uma instância duplicada")
	}
}

// TestIsAddrInUse_DetectsBindError verifies isAddrInUse reconhece um erro de
// bind real (EADDRINUSE/WSAEADDRINUSE) — o gatilho do crash-loop.
func TestIsAddrInUse_DetectsBindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("primeiro listen: %v", err)
	}
	defer ln.Close()

	if _, err := net.Listen("tcp", ln.Addr().String()); err == nil {
		t.Skip("SO_REUSEADDR permitiu bind duplo — não dá para simular EADDRINUSE")
	} else if !isAddrInUse(err) {
		t.Errorf("isAddrInUse = false para %v", err)
	}
}

// TestIsAddrInUse_OtherErrors verifies isAddrInUse não engole erros que não
// são de porta em uso.
func TestIsAddrInUse_OtherErrors(t *testing.T) {
	if isAddrInUse(errors.New("permission denied")) {
		t.Error("permission denied não é address in use")
	}
	if isAddrInUse(nil) {
		t.Error("nil não é address in use")
	}
}

// TestProbeDaemonAlive_NoDaemon verifies o ping de singleton responde false
// quando não há ninguém escutando na porta IPC (dial falha).
func TestProbeDaemonAlive_NoDaemon(t *testing.T) {
	// Porta livre: reserva um listener, pega o endereço e fecha, para termos um
	// endereço com quase certeza sem serviço escutando.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	orig := ipc.TestDialAddr
	ipc.TestDialAddr = addr
	defer func() { ipc.TestDialAddr = orig }()

	if probeDaemonAlive() {
		t.Error("probeDaemonAlive deveria ser false sem daemon escutando")
	}
}

// TestProbeDaemonAlive_DaemonResponds verifies o ping de singleton responde
// true quando um servidor IPC real responde na porta.
func TestProbeDaemonAlive_DaemonResponds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Servidor IPC mínimo: aceita conexões e responde um ping de verdade.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var req ipc.Request
				_ = json.NewDecoder(c).Decode(&req)
				_ = json.NewEncoder(c).Encode(&ipc.Response{Success: true, Message: "pong"})
			}(conn)
		}
	}()

	orig := ipc.TestDialAddr
	ipc.TestDialAddr = ln.Addr().String()
	defer func() { ipc.TestDialAddr = orig }()

	if !probeDaemonAlive() {
		t.Error("probeDaemonAlive deveria ser true com um daemon respondendo")
	}
}

func TestRealChain_ExternalTamperRestoresDisk(t *testing.T) {
	_, sched, enf, statePath := newRealWatcherChain(t)

	// Bootstrap (sched.Start → Reconcile → Save → onSave) must have completed
	// so the self-write hash is recorded; wait for the Sync it triggers.
	waitForCalls(t, &enf.syncCalls, 1, 5*time.Second)

	// An external process wipes the block from the disk mirror.
	tampered := `{"version":1,"blocks":{}}`
	if err := os.WriteFile(statePath, []byte(tampered), 0644); err != nil {
		t.Fatalf("external tamper: %v", err)
	}

	// The watcher must detect it and the scheduler must restore disk from RAM.
	waitForFileContains(t, statePath, "example.com", 5*time.Second)

	blocks, err := sched.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Domain != "example.com" {
		t.Errorf("expected example.com still in RAM, got %+v", blocks)
	}
	if calls := atomic.LoadInt32(&enf.syncCalls); calls < 2 {
		t.Errorf("expected ≥2 Sync calls (bootstrap + tamper restore), got %d", calls)
	}
}

func TestRealChain_ExternalDeleteRecreatesFile(t *testing.T) {
	_, sched, _, statePath := newRealWatcherChain(t)

	// Deleting the disk mirror must be detected and the file recreated from RAM.
	if err := os.Remove(statePath); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}

	waitForFileContains(t, statePath, "example.com", 5*time.Second)

	blocks, err := sched.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Domain != "example.com" {
		t.Errorf("expected example.com still in RAM, got %+v", blocks)
	}
}

func TestRealChain_ExternalRenameRecreatesFile(t *testing.T) {
	_, sched, _, statePath := newRealWatcherChain(t)

	// Renaming the state file away must be detected and the file recreated.
	if err := os.Rename(statePath, statePath+".bak"); err != nil {
		t.Fatalf("os.Rename: %v", err)
	}

	waitForFileContains(t, statePath, "example.com", 5*time.Second)

	blocks, err := sched.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Domain != "example.com" {
		t.Errorf("expected example.com still in RAM, got %+v", blocks)
	}
}

// TestRealChain_ExternalEditAfterSelfWriteDetected is the regression test for
// the old 500ms blind spot: an external edit landing immediately after a daemon
// self-write must still be detected, because the self-write marker is the
// content hash (recorded post-write by store.onSave), not a time window.
func TestRealChain_ExternalEditAfterSelfWriteDetected(t *testing.T) {
	st, sched, _, statePath := newRealWatcherChain(t)
	waitForFileContains(t, statePath, "example.com", 5*time.Second)

	// The daemon persists its current state (store.Save fires onSave after the
	// write, recording the SHA-256 of the content just written).
	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := st.Save(state); err != nil {
		t.Fatalf("daemon Save: %v", err)
	}

	// An external edit lands immediately after (inside what used to be the
	// 500ms window). It must be detected and restored away.
	tampered := fmt.Sprintf(`{
		"version": 1,
		"blocks": {
			"evil.com": {
				"domain": "evil.com",
				"started_at": %q,
				"expires_at": %q,
				"resolved_ips": ["127.0.0.1"]
			}
		}
	}`, time.Now().Add(-time.Minute).Format(time.RFC3339), time.Now().Add(24*time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(statePath, []byte(tampered), 0644); err != nil {
		t.Fatalf("external edit: %v", err)
	}

	// The disk mirror must be restored to the daemon's RAM content.
	waitForFileContains(t, statePath, "example.com", 5*time.Second)
	data, _ := os.ReadFile(statePath)
	if strings.Contains(string(data), "evil.com") {
		t.Error("tampered content should have been restored away")
	}

	if blocks, err := sched.ListBlocks(); err != nil {
		t.Fatalf("ListBlocks: %v", err)
	} else if len(blocks) != 1 || blocks[0].Domain != "example.com" {
		t.Errorf("expected example.com in RAM, got %+v", blocks)
	}
}

// TestRealChain_RestoreIsStable_NoLoop guards against a restore loop: after a
// tamper is fixed, the daemon's own restore write must not cascade into more
// restores/Sync calls.
//
// This only holds because (a) the scheduler's restore path goes through
// store.Save, so onSave records the SHA-256 of the content just written (the
// matching fsnotify event is suppressed), and (b) Reconcile only calls the
// enforcer when the disk actually diverges from RAM — a redundant reconcile
// from the benign fsnotify-before-onSave race is therefore a no-op that cannot
// add Sync calls.
func TestRealChain_RestoreIsStable_NoLoop(t *testing.T) {
	_, _, enf, statePath := newRealWatcherChain(t)

	tampered := `{"version":1,"blocks":{}}`
	if err := os.WriteFile(statePath, []byte(tampered), 0644); err != nil {
		t.Fatalf("external tamper: %v", err)
	}
	waitForFileContains(t, statePath, "example.com", 5*time.Second)

	after := atomic.LoadInt32(&enf.syncCalls)
	time.Sleep(600 * time.Millisecond) // well past the 200ms debounce
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != after {
		t.Errorf("Sync count changed after settling: before=%d after=%d", after, calls)
	}
}
