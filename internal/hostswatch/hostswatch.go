package hostswatch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"focusguard/internal/policy"
)

const (
	defaultDebounce   = 200 * time.Millisecond
	selfWriteWindow   = 500 * time.Millisecond
	defaultSweepEvery = 30 * time.Second
)

type fsEvent struct {
	op string
}

type Enforcer interface {
	Sync(blocks map[string][]string) error
}

type Scheduler interface {
	ListBlocks() ([]policy.Block, error)
}

type HostsWatcher struct {
	mu             sync.Mutex
	HostsPath      string
	enf            Enforcer
	sched          Scheduler
	debounce       time.Duration
	events         chan fsEvent
	stopCh         chan struct{}
	stopOnce       sync.Once
	watcher        *fsnotify.Watcher
	sweepInterval  time.Duration
	lastSelfWrite  time.Time
	selfWriteDelay time.Duration
}

// MarkSelfWrite records that a write to the hosts file was performed by the
// daemon itself, so the next fsnotify event is ignored (avoids redundant Sync).
func (w *HostsWatcher) MarkSelfWrite() {
	delay := w.selfWriteDelay
	if delay == 0 {
		delay = selfWriteWindow
	}
	w.mu.Lock()
	w.lastSelfWrite = time.Now().Add(delay)
	w.mu.Unlock()
}

func (w *HostsWatcher) isSelfWrite() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.lastSelfWrite.IsZero() && time.Now().Before(w.lastSelfWrite)
}

func New(enf Enforcer, sched Scheduler) *HostsWatcher {
	path := "/etc/hosts"
	if runtime.GOOS == "windows" {
		path = filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
	}
	return &HostsWatcher{
		HostsPath: path,
		enf:       enf,
		sched:     sched,
		debounce:  defaultDebounce,
		events:    make(chan fsEvent, 64),
		stopCh:    make(chan struct{}),
	}
}

func (w *HostsWatcher) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("hostswatch: fsnotify: %w", err)
	}
	dir := filepath.Dir(w.HostsPath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("hostswatch: watch dir: %w", err)
	}
	w.mu.Lock()
	w.watcher = watcher
	w.mu.Unlock()

	go w.watchFsEvents()
	go w.eventLoop()
	return nil
}

func (w *HostsWatcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
}

func (w *HostsWatcher) watchFsEvents() {
	w.mu.Lock()
	watcher := w.watcher
	w.mu.Unlock()
	if watcher == nil {
		return
	}
	defer watcher.Close()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) ||
				event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				// On Windows fsnotify may report the path with different casing than
				// the HostsPath built from the SystemRoot env var, so compare
				// case-insensitively to avoid missing (or misfiring on) host edits.
				if !strings.EqualFold(event.Name, w.HostsPath) &&
					!strings.EqualFold(filepath.Base(event.Name), filepath.Base(w.HostsPath)) {
					continue
				}
				if w.isSelfWrite() {
					continue
				}
				select {
				case w.events <- fsEvent{op: "write"}:
				default:
				}
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		case <-w.stopCh:
			return
		}
	}
}

func (w *HostsWatcher) eventLoop() {
	interval := w.sweepInterval
	if interval <= 0 {
		interval = defaultSweepEvery
	}
	sweep := time.NewTicker(interval)
	defer sweep.Stop()

	for {
		select {
		case <-w.events:
			w.debounceAndDetect()
		case <-sweep.C:
			w.detectTamper()
		case <-w.stopCh:
			return
		}
	}
}

func (w *HostsWatcher) debounceAndDetect() {
	timer := time.NewTimer(w.debounce)
	defer timer.Stop()
	for {
		select {
		case <-w.events:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(w.debounce)
		case <-timer.C:
			w.detectTamper()
			return
		case <-w.stopCh:
			return
		}
	}
}

func (w *HostsWatcher) detectTamper() {
	blocks, err := w.sched.ListBlocks()
	if err != nil || len(blocks) == 0 {
		return
	}

	activeBlocks := make(map[string][]string, len(blocks))
	for _, b := range blocks {
		activeBlocks[b.Domain] = b.ResolvedIPs
	}

	data, err := os.ReadFile(w.HostsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
		// The hosts file was deleted (e.g. by an admin) — recreate it.
		_ = w.enf.Sync(activeBlocks)
		return
	}

	hostsContent := string(data)
	for _, b := range blocks {
		marker := fmt.Sprintf("# FOCUSGUARD: %s", b.Domain)
		if !strings.Contains(hostsContent, marker) {
			_ = w.enf.Sync(activeBlocks)
			return
		}
	}
}
