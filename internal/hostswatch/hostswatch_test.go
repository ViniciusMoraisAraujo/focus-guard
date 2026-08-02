package hostswatch

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"focusguard/internal/policy"
	"focusguard/internal/tamper"
)

type mockEnforcer struct {
	mu         sync.Mutex
	syncCalls  int32
	lastBlocks map[string][]string
}

func (m *mockEnforcer) Sync(blocks map[string][]string) error {
	atomic.AddInt32(&m.syncCalls, 1)
	m.mu.Lock()
	m.lastBlocks = make(map[string][]string)
	for k, v := range blocks {
		m.lastBlocks[k] = v
	}
	m.mu.Unlock()
	return nil
}

// errorEnforcer returns an error from Sync so tests can verify a failed
// restore is surfaced (returned by detectTamper and logged by the async path)
// instead of being silently discarded.
type errorEnforcer struct {
	syncCalls int32
}

func (m *errorEnforcer) Sync(_ map[string][]string) error {
	atomic.AddInt32(&m.syncCalls, 1)
	return errors.New("permission denied")
}

type mockScheduler struct {
	blocks []policy.Block
}

func (m *mockScheduler) ListBlocks() ([]policy.Block, error) {
	return m.blocks, nil
}

func writeHosts(t *testing.T, path string, lines []string) {
	t.Helper()
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeHosts: %v", err)
	}
}

func TestNew_Defaults(t *testing.T) {
	enf := &mockEnforcer{}
	sched := &mockScheduler{}
	w := New(enf, sched)

	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
	if w.enf != enf {
		t.Error("enforcer not set")
	}
	if w.sched != sched {
		t.Error("scheduler not set")
	}
	if w.debounce != defaultDebounce {
		t.Errorf("expected debounce %v, got %v", defaultDebounce, w.debounce)
	}
}

func TestDetectTamper_Intact(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"::1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
		"::1 twitter.com # FOCUSGUARD: twitter.com",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
	}

	w.detectTamper()
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 0 {
		t.Errorf("expected 0 Sync calls for intact hosts, got %d", calls)
	}
}

func TestDetectTamper_MissingEntry(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
	}

	w.detectTamper()
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 1 {
		t.Fatalf("expected 1 Sync call after tamper, got %d", calls)
	}

	enf.mu.Lock()
	if _, ok := enf.lastBlocks["twitter.com"]; !ok {
		t.Error("expected twitter.com in synced blocks")
	}
	enf.mu.Unlock()
}

func TestDetectTamper_PartialMissing(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
			{Domain: "youtube.com", ResolvedIPs: []string{"5.6.7.8"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
	}

	w.detectTamper()
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 1 {
		t.Fatalf("expected 1 Sync call when a block is missing, got %d", calls)
	}

	enf.mu.Lock()
	if _, ok := enf.lastBlocks["youtube.com"]; !ok {
		t.Error("expected youtube.com in synced blocks")
	}
	if _, ok := enf.lastBlocks["twitter.com"]; !ok {
		t.Error("expected twitter.com in synced blocks")
	}
	enf.mu.Unlock()
}

// fakeTamperLogger records tamper events for assertions.
type fakeTamperLogger struct {
	mu     sync.Mutex
	events []tamper.Event
}

func (f *fakeTamperLogger) Log(e tamper.Event) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}

func (f *fakeTamperLogger) snapshot() []tamper.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tamper.Event(nil), f.events...)
}

func TestDetectTamper_LogsViolation(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
	}
	logr := &fakeTamperLogger{}
	w.SetTamperLogger(logr)

	if err := w.detectTamper(); err != nil {
		t.Fatalf("detectTamper: %v", err)
	}
	ev := logr.snapshot()
	if len(ev) != 1 {
		t.Fatalf("expected 1 tamper event, got %+v", ev)
	}
	if ev[0].Source != "hosts" || ev[0].Action != "restore" || ev[0].Detail != "twitter.com" {
		t.Errorf("evento inesperado: %+v", ev[0])
	}
}

func TestDetectTamper_IntactDoesNotLog(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
	})

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       &mockEnforcer{},
		sched: &mockScheduler{
			blocks: []policy.Block{{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}}},
		},
	}
	logr := &fakeTamperLogger{}
	w.SetTamperLogger(logr)

	if err := w.detectTamper(); err != nil {
		t.Fatalf("detectTamper: %v", err)
	}
	if ev := logr.snapshot(); len(ev) != 0 {
		t.Errorf("hosts intacto não deveria logar, got %+v", ev)
	}
}

func TestDetectTamper_NoBlocks(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{blocks: nil}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
	}

	w.detectTamper()
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 0 {
		t.Errorf("expected 0 Sync calls when no active blocks, got %d", calls)
	}
}

func TestDetectTamper_PropagatesSyncError_MissingMarker(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &errorEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
	}

	// No FOCUSGUARD marker → Sync runs and its failure must be returned, not
	// discarded with _=.
	if err := w.detectTamper(); err == nil {
		t.Fatal("expected Sync error to be propagated by detectTamper")
	}
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 1 {
		t.Errorf("expected Sync to be called once, got %d calls", calls)
	}
}

func TestDetectTamper_PropagatesSyncError_FileDeleted(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &errorEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
	}

	if err := os.Remove(hostsPath); err != nil {
		t.Fatalf("failed to remove hosts file: %v", err)
	}

	// Deleted hosts file → recreation Sync runs and its failure must be
	// returned, not discarded.
	if err := w.detectTamper(); err == nil {
		t.Fatal("expected Sync error to be propagated by detectTamper")
	}
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 1 {
		t.Errorf("expected Sync to be called once, got %d calls", calls)
	}
}

func TestDetectTamper_FileDeleted(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
	}

	if err := os.Remove(hostsPath); err != nil {
		t.Fatalf("failed to remove hosts file: %v", err)
	}

	w.detectTamper()
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 1 {
		t.Fatalf("expected 1 Sync call after file deletion, got %d", calls)
	}

	enf.mu.Lock()
	if _, ok := enf.lastBlocks["twitter.com"]; !ok {
		t.Error("expected twitter.com in synced blocks")
	}
	enf.mu.Unlock()
}

func waitForSync(t *testing.T, enf *mockEnforcer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&enf.syncCalls) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected Sync call within timeout")
}

// removeHosts removes the hosts file, retrying on Windows where a lingering
// handle (indexer/Defender) can transiently hold the file open.
func removeHosts(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		err := os.Remove(path)
		if err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("failed to remove hosts file: %v", path)
}

func TestRemoveEvent_TriggersSync(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := New(enf, sched)
	w.HostsPath = hostsPath
	w.debounce = 10 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	removeHosts(t, hostsPath)

	waitForSync(t, enf, 3*time.Second)
}

func TestPeriodicSweep_RestoresDeletedFile(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	// Run only the event loop (no fsnotify watcher), so the periodic sweep is
	// the sole detection path for the deleted file.
	w := &HostsWatcher{
		HostsPath:     hostsPath,
		enf:           enf,
		sched:         sched,
		debounce:      10 * time.Millisecond,
		sweepInterval: 50 * time.Millisecond,
		events:        make(chan fsEvent, 10),
		stopCh:        make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	removeHosts(t, hostsPath)

	waitForSync(t, enf, 2*time.Second)

	close(w.stopCh)
	<-done
}

func TestStartStop(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &mockEnforcer{}
	sched := &mockScheduler{blocks: nil}

	w := New(enf, sched)
	w.HostsPath = hostsPath
	w.debounce = 10 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	w.Stop()
}

func TestHandleWriteEvent_CallsDetectTamper(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
		events:    make(chan fsEvent, 10),
		stopCh:    make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	select {
	case w.events <- fsEvent{op: "write"}:
	case <-time.After(time.Second):
		t.Fatal("failed to send event")
	}

	time.Sleep(50 * time.Millisecond)

	w.stopCh <- struct{}{}
	<-done

	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 1 {
		t.Errorf("expected 1 Sync call after write event, got %d", calls)
	}
}

func TestDebounce_MultipleEvents(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  50 * time.Millisecond,
		events:    make(chan fsEvent, 10),
		stopCh:    make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	for i := 0; i < 5; i++ {
		w.events <- fsEvent{op: "write"}
	}

	time.Sleep(100 * time.Millisecond)

	w.stopCh <- struct{}{}
	<-done

	calls := atomic.LoadInt32(&enf.syncCalls)
	if calls != 1 {
		t.Errorf("expected exactly 1 Sync call (debounced), got %d", calls)
	}
}

func TestRenameEvent_TriggersSync(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := New(enf, sched)
	w.HostsPath = hostsPath
	w.debounce = 10 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	if err := os.Rename(hostsPath, filepath.Join(dir, "hosts.bak")); err != nil {
		t.Fatalf("os.Rename: %v", err)
	}

	waitForSync(t, enf, 3*time.Second)
}

// blockingEnforcer signals on `started` when the first Sync begins and then
// blocks until `release` is closed, letting tests prove the event loop does
// not freeze while a Sync is in flight.
type blockingEnforcer struct {
	startedOnce sync.Once
	started     chan struct{}
	release     chan struct{}
	syncCalls   int32
}

func (m *blockingEnforcer) Sync(blocks map[string][]string) error {
	atomic.AddInt32(&m.syncCalls, 1)
	m.startedOnce.Do(func() { close(m.started) })
	<-m.release
	return nil
}

func TestDetectTamper_AsyncDoesNotBlockEventLoop(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	enf := &blockingEnforcer{started: make(chan struct{}), release: make(chan struct{})}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := &HostsWatcher{
		HostsPath: hostsPath,
		enf:       enf,
		sched:     sched,
		debounce:  10 * time.Millisecond,
		events:    make(chan fsEvent, 10),
		stopCh:    make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	// First event starts an async detect/Sync that blocks.
	select {
	case w.events <- fsEvent{op: "write"}:
	case <-time.After(time.Second):
		t.Fatal("failed to send event")
	}
	select {
	case <-enf.started:
	case <-time.After(2 * time.Second):
		t.Fatal("Sync did not start")
	}

	// The event loop must remain responsive while Sync is blocked: a second
	// event is consumed but must NOT start a concurrent Sync (boolean lock).
	select {
	case w.events <- fsEvent{op: "write"}:
	case <-time.After(time.Second):
		t.Fatal("failed to send second event")
	}
	time.Sleep(50 * time.Millisecond)
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 1 {
		t.Fatalf("expected 1 in-flight Sync (no concurrent runs), got %d", calls)
	}

	// Stop must let the event loop exit while a Sync is still blocked.
	w.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event loop frozen: did not exit while Sync in flight")
	}

	// Release the blocked Sync; the pending one runs to completion.
	close(enf.release)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&enf.syncCalls) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 2 {
		t.Errorf("expected pending Sync to run after release, got %d calls", calls)
	}
}

func TestMarkSelfWrite_ContentBased(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	w := &HostsWatcher{HostsPath: hostsPath}

	if w.isSelfWrite() {
		t.Error("expected no self-write before MarkSelfWrite")
	}

	w.MarkSelfWrite()
	if !w.isSelfWrite() {
		t.Error("expected self-write after MarkSelfWrite with unchanged content")
	}

	// An external modification right after the self-write (inside what used to
	// be the 500ms window) must NOT be treated as a self-write.
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost", "127.0.0.1 evil.com"})
	if w.isSelfWrite() {
		t.Error("expected external modification to not be treated as self-write")
	}
}

func TestWatchFsEvents_ExternalChangeAfterSelfWrite_Detected(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, []string{
		"127.0.0.1 localhost",
		"127.0.0.1 twitter.com # FOCUSGUARD: twitter.com",
	})

	enf := &mockEnforcer{}
	sched := &mockScheduler{
		blocks: []policy.Block{
			{Domain: "twitter.com", ResolvedIPs: []string{"1.2.3.4"}},
		},
	}

	w := New(enf, sched)
	w.HostsPath = hostsPath
	w.debounce = 10 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	// The daemon rewrites hosts (marker intact) and marks it as its own write.
	w.MarkSelfWrite()

	// Let the self-write event be consumed and suppressed.
	time.Sleep(100 * time.Millisecond)
	if calls := atomic.LoadInt32(&enf.syncCalls); calls != 0 {
		t.Fatalf("self-write should be suppressed, got %d Sync calls", calls)
	}

	// External modification right after the self-write (inside what used to be
	// the 500ms blind spot): the marker is removed, so Sync must run.
	writeHosts(t, hostsPath, []string{"127.0.0.1 localhost"})

	waitForSync(t, enf, 3*time.Second)
}
