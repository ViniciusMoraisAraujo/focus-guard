package hostswatch

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"focusguard/internal/domain/policy"
	"focusguard/internal/infrastructure/fsutil"
	"focusguard/internal/infrastructure/tamper"
)

const (
	defaultDebounce   = 200 * time.Millisecond
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
	mu            sync.Mutex
	HostsPath     string
	enf           Enforcer
	sched         Scheduler
	debounce      time.Duration
	events        chan fsEvent
	stopCh        chan struct{}
	stopOnce      sync.Once
	watcher       *fsnotify.Watcher
	sweepInterval time.Duration
	lastSelfHash  fsutil.Hash
	detectRunning bool
	detectPending bool
	tamperLogger  TamperLogger
}

// TamperLogger receives an entry whenever a tamper attempt is detected and
// restored. The daemon wires the tamper recorder; nil disables logging.
type TamperLogger interface {
	Log(event tamper.Event)
}

// SetTamperLogger wires an optional logger for detected tampering attempts
// (external edits to the hosts file).
func (w *HostsWatcher) SetTamperLogger(l TamperLogger) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tamperLogger = l
}

// logTamper records a tamper event with the given action and detail.
func (w *HostsWatcher) logTamper(action, detail string) {
	w.mu.Lock()
	l := w.tamperLogger
	w.mu.Unlock()
	if l != nil {
		l.Log(tamper.Event{At: time.Now(), Source: "hosts", Action: action, Detail: detail})
	}
}

// MarkSelfWrite records the SHA-256 of the hosts file content as written by
// the daemon itself, so an fsnotify event matching exactly that content is
// ignored (avoids redundant Sync). Content-based matching has no time window,
// so an external edit that changes the content right after a self-write is
// still detected — there is no 500ms blind spot.
func (w *HostsWatcher) MarkSelfWrite() {
	sum, err := fsutil.HashFile(w.HostsPath)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.lastSelfHash = fsutil.Hash{}
		return
	}
	w.lastSelfHash = sum
}

func (w *HostsWatcher) isSelfWrite() bool {
	sum, err := fsutil.HashFile(w.HostsPath)
	if err != nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastSelfHash != (fsutil.Hash{}) && w.lastSelfHash == sum
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
			w.detectTamperAsync()
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
			w.detectTamperAsync()
			return
		case <-w.stopCh:
			return
		}
	}
}

// detectTamperAsync runs detectTamper asynchronously so a slow Sync never
// freezes the event loop. A boolean lock ensures only one detect runs at a
// time; events arriving while one is in flight are coalesced into a single
// follow-up run instead of being lost or spawning concurrent runs.
func (w *HostsWatcher) detectTamperAsync() {
	w.mu.Lock()
	if w.detectRunning {
		w.detectPending = true
		w.mu.Unlock()
		return
	}
	w.detectRunning = true
	w.mu.Unlock()

	go func() {
		defer w.detectDone()
		if err := w.detectTamper(); err != nil {
			// A failed Sync means the hosts restore did not happen (e.g. no
			// permission to write the hosts file) — surface it instead of
			// silently dropping it, mirroring the statewatch error logging.
			log.Printf("[HostsWatcher] Erro ao restaurar hosts após adulteração: %v", err)
		}
	}()
}

// detectDone releases the boolean lock and re-runs once if a detect was
// requested while another one was in flight.
func (w *HostsWatcher) detectDone() {
	w.mu.Lock()
	w.detectRunning = false
	pending := w.detectPending
	w.detectPending = false
	w.mu.Unlock()
	if pending {
		w.detectTamperAsync()
	}
}

func (w *HostsWatcher) detectTamper() error {
	blocks, err := w.sched.ListBlocks()
	if err != nil || len(blocks) == 0 {
		// A ListBlocks failure or an empty block list is intentionally
		// ignored: without the block list there is nothing to restore, and
		// the next event/sweep retries.
		return nil
	}

	activeBlocks := make(map[string][]string, len(blocks))
	for _, b := range blocks {
		activeBlocks[b.Domain] = b.ResolvedIPs
	}

	data, err := os.ReadFile(w.HostsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			// An unreadable-but-existing hosts file is left alone; the next
			// event/sweep retries rather than risking a bad rewrite.
			return nil
		}
		// The hosts file was deleted (e.g. by an admin) — recreate it. The
		// error is propagated so the async caller can log a failed restore.
		w.logTamper("restore", "hosts deletado")
		return w.enf.Sync(activeBlocks)
	}

	hostsContent := string(data)
	for _, b := range blocks {
		marker := fmt.Sprintf("# FOCUSGUARD: %s", b.Domain)
		if !strings.Contains(hostsContent, marker) {
			w.logTamper("restore", b.Domain)
			return w.enf.Sync(activeBlocks)
		}
	}
	return nil
}
