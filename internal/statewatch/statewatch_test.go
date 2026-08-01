package statewatch

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

type mockReconciler struct {
	reconcileCalls int32
}

func (m *mockReconciler) Reconcile() error {
	atomic.AddInt32(&m.reconcileCalls, 1)
	return nil
}

type failingReconciler struct {
	reconcileCalls int32
}

func (m *failingReconciler) Reconcile() error {
	atomic.AddInt32(&m.reconcileCalls, 1)
	return os.ErrPermission
}

func writeStateFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeStateFile: %v", err)
	}
}

func TestNew_Defaults(t *testing.T) {
	rec := &mockReconciler{}
	w := New(rec, "/tmp/test-state.json")

	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
	if w.reconciler != rec {
		t.Error("reconciler not set")
	}
	if w.StatePath != "/tmp/test-state.json" {
		t.Errorf("expected path /tmp/test-state.json, got %s", w.StatePath)
	}
	if w.debounce != defaultDebounce {
		t.Errorf("expected debounce %v, got %v", defaultDebounce, w.debounce)
	}
}

func TestDetectChange_CallsReconcile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{}`)

	rec := &mockReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
		t.Fatalf("watcher.Add: %v", err)
	}
	w.watcher = watcher

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

	close(w.stopCh)
	<-done

	if calls := atomic.LoadInt32(&rec.reconcileCalls); calls != 1 {
		t.Errorf("expected 1 Reconcile call, got %d", calls)
	}
}

func TestDetectChange_FileModifiedExternally(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{"version":1,"blocks":{}}`)

	rec := &mockReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	for i := 0; i < 3; i++ {
		select {
		case w.events <- fsEvent{op: "write"}:
		case <-time.After(time.Second):
			t.Fatal("failed to send event")
		}
	}

	time.Sleep(100 * time.Millisecond)

	close(w.stopCh)
	<-done

	calls := atomic.LoadInt32(&rec.reconcileCalls)
	if calls != 1 {
		t.Errorf("expected exactly 1 Reconcile call (debounced), got %d", calls)
	}
}

func TestDetectChange_ReconcileError(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{}`)

	rec := &failingReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
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

	close(w.stopCh)
	<-done

	if calls := atomic.LoadInt32(&rec.reconcileCalls); calls != 1 {
		t.Errorf("expected 1 Reconcile call even with error, got %d", calls)
	}
}

func TestStartStop(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{}`)

	rec := &mockReconciler{}
	w := New(rec, statePath)
	w.debounce = 10 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	w.mu.Lock()
	hasWatcher := w.watcher != nil
	w.mu.Unlock()

	if !hasWatcher {
		t.Error("expected watcher to be initialized after Start")
	}

	w.Stop()

	w.Stop()
}

func TestDebounce_MultipleEvents(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{}`)

	rec := &mockReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   50 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	for i := 0; i < 5; i++ {
		w.events <- fsEvent{op: "write"}
	}

	time.Sleep(150 * time.Millisecond)

	close(w.stopCh)
	<-done

	calls := atomic.LoadInt32(&rec.reconcileCalls)
	if calls != 1 {
		t.Errorf("expected exactly 1 Reconcile call (debounced), got %d", calls)
	}
}

func TestWatchFsEvents_NotifiesEvents(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{}`)

	rec := &mockReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	w.watcher = watcher

	if err := watcher.Add(dir); err != nil {
		t.Fatalf("watcher.Add: %v", err)
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	doneFs := make(chan struct{})
	go func() {
		w.watchFsEvents()
		close(doneFs)
	}()

	writeStateFile(t, statePath, `{"version":1,"blocks":{"test.com":{"domain":"test.com"}}}`)

	time.Sleep(100 * time.Millisecond)

	close(w.stopCh)
	<-done
	<-doneFs

	calls := atomic.LoadInt32(&rec.reconcileCalls)
	if calls != 1 {
		t.Errorf("expected 1 Reconcile call after external file modification, got %d", calls)
	}
}

func TestStop_NoStart(t *testing.T) {
	rec := &mockReconciler{}
	w := New(rec, "/tmp/nonexistent.json")

	w.Stop()
}

func TestWatchFsEvents_RemoveTriggersReconcile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{"version":1,"blocks":{"test.com":{"domain":"test.com"}}}`)

	rec := &mockReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	w.watcher = watcher

	if err := watcher.Add(dir); err != nil {
		t.Fatalf("watcher.Add: %v", err)
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	doneFs := make(chan struct{})
	go func() {
		w.watchFsEvents()
		close(doneFs)
	}()

	if err := os.Remove(statePath); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	close(w.stopCh)
	<-done
	<-doneFs

	calls := atomic.LoadInt32(&rec.reconcileCalls)
	if calls != 1 {
		t.Errorf("expected 1 Reconcile call after state file deletion, got %d", calls)
	}
}

// blockingReconciler signals on `started` when the first Reconcile begins and
// then blocks until `release` is closed, letting tests simulate a slow/blocking
// reconcile to prove the event loop does not freeze.
type blockingReconciler struct {
	startedOnce sync.Once
	started     chan struct{}
	release     chan struct{}
	calls       int32
}

func (m *blockingReconciler) Reconcile() error {
	atomic.AddInt32(&m.calls, 1)
	m.startedOnce.Do(func() { close(m.started) })
	<-m.release
	return nil
}

func TestWatchFsEvents_RenameTriggersReconcile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{"version":1,"blocks":{"test.com":{"domain":"test.com"}}}`)

	rec := &mockReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	w.watcher = watcher

	if err := watcher.Add(dir); err != nil {
		t.Fatalf("watcher.Add: %v", err)
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	doneFs := make(chan struct{})
	go func() {
		w.watchFsEvents()
		close(doneFs)
	}()

	if err := os.Rename(statePath, filepath.Join(dir, "state.json.bak")); err != nil {
		t.Fatalf("os.Rename: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	close(w.stopCh)
	<-done
	<-doneFs

	calls := atomic.LoadInt32(&rec.reconcileCalls)
	if calls != 1 {
		t.Errorf("expected 1 Reconcile call after state file rename, got %d", calls)
	}
}

func TestDetectAndReconcile_AsyncDoesNotBlockEventLoop(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{}`)

	rec := &blockingReconciler{started: make(chan struct{}), release: make(chan struct{})}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	// First event starts an async reconcile that blocks.
	select {
	case w.events <- fsEvent{op: "write"}:
	case <-time.After(time.Second):
		t.Fatal("failed to send event")
	}
	select {
	case <-rec.started:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile did not start")
	}

	// The event loop must remain responsive while the reconcile is blocked:
	// a second event is consumed but must NOT start a concurrent reconcile
	// (boolean lock).
	select {
	case w.events <- fsEvent{op: "write"}:
	case <-time.After(time.Second):
		t.Fatal("failed to send second event")
	}
	time.Sleep(50 * time.Millisecond)
	if calls := atomic.LoadInt32(&rec.calls); calls != 1 {
		t.Fatalf("expected 1 in-flight reconcile (no concurrent runs), got %d", calls)
	}

	// Stop must let the event loop exit while a reconcile is still blocked.
	w.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event loop frozen: did not exit while reconcile in flight")
	}

	// Release the blocked reconcile; the pending one runs to completion.
	close(rec.release)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&rec.calls) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls := atomic.LoadInt32(&rec.calls); calls != 2 {
		t.Errorf("expected pending reconcile to run after release, got %d calls", calls)
	}
}

func TestMarkSelfWrite_ContentBased(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{"version":1,"blocks":{}}`)

	w := &StateWatcher{StatePath: statePath}

	if w.isSelfWrite() {
		t.Error("expected no self-write before MarkSelfWrite")
	}

	w.MarkSelfWrite()
	if !w.isSelfWrite() {
		t.Error("expected self-write after MarkSelfWrite with unchanged content")
	}

	// An external modification right after the self-write (inside what used to
	// be the 500ms window) must NOT be treated as a self-write.
	writeStateFile(t, statePath, `{"version":1,"blocks":{"x.com":{"domain":"x.com"}}}`)
	if w.isSelfWrite() {
		t.Error("expected external modification to not be treated as self-write")
	}

	// The recorded hash follows a new MarkSelfWrite, so a matching rewrite of
	// the same content is again a self-write.
	w.MarkSelfWrite()
	if !w.isSelfWrite() {
		t.Error("expected self-write after re-marking with the current content")
	}
}

func TestWatchFsEvents_ExternalChangeAfterSelfWrite_Detected(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	writeStateFile(t, statePath, `{"version":1,"blocks":{}}`)

	rec := &mockReconciler{}

	w := &StateWatcher{
		StatePath:  statePath,
		reconciler: rec,
		debounce:   10 * time.Millisecond,
		events:     make(chan fsEvent, 10),
		stopCh:     make(chan struct{}),
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	w.watcher = watcher

	if err := watcher.Add(dir); err != nil {
		t.Fatalf("watcher.Add: %v", err)
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()

	doneFs := make(chan struct{})
	go func() {
		w.watchFsEvents()
		close(doneFs)
	}()

	// The daemon writes the state file and marks it as its own write.
	writeStateFile(t, statePath, `{"version":1,"blocks":{"x.com":{"domain":"x.com"}}}`)
	w.MarkSelfWrite()

	// Let the self-write event be consumed and suppressed.
	time.Sleep(100 * time.Millisecond)
	if calls := atomic.LoadInt32(&rec.reconcileCalls); calls != 0 {
		t.Fatalf("self-write should be suppressed, got %d reconciles", calls)
	}

	// External modification right after the self-write (inside what used to be
	// the 500ms blind spot) must still be detected.
	writeStateFile(t, statePath, `{"version":1,"blocks":{"y.com":{"domain":"y.com"}}}`)

	time.Sleep(150 * time.Millisecond)

	close(w.stopCh)
	<-done
	<-doneFs

	calls := atomic.LoadInt32(&rec.reconcileCalls)
	if calls != 1 {
		t.Errorf("expected 1 Reconcile call after external change, got %d", calls)
	}
}
