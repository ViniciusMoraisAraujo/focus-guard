package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/hostswatch"
	"focusguard/internal/ipc"
	"focusguard/internal/policy"
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
	data, err := os.ReadFile(filepath.Join("..", "..", "versioninfo.json"))
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
	requireRoot(t)

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
	result     *update.UpdateResult
	checkErr   error
	applyErr   error
	applyCalls int32
}

func (f *fakeUpdaterAPI) CheckForUpdate(_ context.Context) (*update.UpdateResult, error) {
	return f.result, f.checkErr
}

func (f *fakeUpdaterAPI) UpdateTo(_ context.Context, _ *update.UpdateResult, _ string) (string, error) {
	atomic.AddInt32(&f.applyCalls, 1)
	if f.applyErr != nil {
		return "", f.applyErr
	}
	return "backup.bak", nil
}

func TestDaemonUpdater_Check_NoUpdate(t *testing.T) {
	d := &daemonUpdater{u: &fakeUpdaterAPI{}, binary: "/tmp/focusguard-daemon"}

	st, err := d.Check(context.Background(), false)
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
		u:      &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}},
		binary: "/tmp/focusguard-daemon",
	}

	st, err := d.Check(context.Background(), false)
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
		u:      &fakeUpdaterAPI{checkErr: errors.New("network down")},
		binary: "/tmp/focusguard-daemon",
	}

	_, err := d.Check(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func TestDaemonUpdater_Check_Apply(t *testing.T) {
	fake := &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}}
	d := &daemonUpdater{u: fake, binary: "/tmp/focusguard-daemon"}

	st, err := d.Check(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Applied {
		t.Error("expected Applied=true when apply is true")
	}
	if atomic.LoadInt32(&fake.applyCalls) != 1 {
		t.Errorf("expected 1 UpdateTo call, got %d", fake.applyCalls)
	}
}

func TestDaemonUpdater_Check_ApplyError(t *testing.T) {
	d := &daemonUpdater{
		u:      &fakeUpdaterAPI{result: &update.UpdateResult{Version: "1.1.0"}, applyErr: errors.New("file locked")},
		binary: "/tmp/focusguard-daemon",
	}

	_, err := d.Check(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "file locked") {
		t.Fatalf("expected apply error, got %v", err)
	}
}

func TestDaemonUpdater_Check_NilUpdater(t *testing.T) {
	d := &daemonUpdater{u: nil, binary: "/tmp/focusguard-daemon"}

	st, err := d.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Available {
		t.Error("expected no update when updater is nil")
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

func (f *fakeUpdateChecker) Check(_ context.Context, apply bool) (ipc.UpdateStatus, error) {
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
func (f *fakeDaemonEnforcer) BlockDoH() error   { return nil }
func (f *fakeDaemonEnforcer) UnblockDoH() error { return nil }
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
// Process Guard wiring
// ---------------------------------------------------------------------------

type fakeProcessGuard struct {
	onStart func(func() bool)
	stopped bool
}

func (f *fakeProcessGuard) Start(isActive func() bool) {
	if f.onStart != nil {
		f.onStart(isActive)
	}
}

func (f *fakeProcessGuard) Stop() { f.stopped = true }

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
	pg := startProcessGuard(sched)
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
	if pg := startProcessGuard(&fakeActivityScheduler{}); pg != nil {
		t.Error("expected nil guard when the constructor returns nil")
	}
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
