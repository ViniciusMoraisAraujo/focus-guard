package hostswatch

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"focusguard/internal/policy"
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
