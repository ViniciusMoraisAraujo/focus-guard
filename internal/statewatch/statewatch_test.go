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
	mu             sync.Mutex
	reconcileCalls int32
}

func (m *mockReconciler) Reconcile() error {
	atomic.AddInt32(&m.reconcileCalls, 1)
	return nil
}

type failingReconciler struct {
	mu             sync.Mutex
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
