package statewatch

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"focusguard/internal/infrastructure/fsutil"
	"focusguard/internal/infrastructure/tamper"
)

const (
	defaultDebounce = 200 * time.Millisecond
)

type fsEvent struct {
	op string
}

type Reconciler interface {
	Reconcile() error
}

type StateWatcher struct {
	mu               sync.Mutex
	StatePath        string
	reconciler       Reconciler
	debounce         time.Duration
	events           chan fsEvent
	stopCh           chan struct{}
	stopOnce         sync.Once
	watcher          *fsnotify.Watcher
	lastSelfHash     fsutil.Hash
	reconcileRunning bool
	reconcilePending bool
	tamperLogger     TamperLogger
}

// TamperLogger receives an entry whenever an external modification to the
// state file is detected and reconciled away. The daemon wires the tamper
// recorder; nil disables logging.
type TamperLogger interface {
	Log(event tamper.Event)
}

// SetTamperLogger wires an optional logger for detected tampering attempts
// (external edits to the state.json).
func (w *StateWatcher) SetTamperLogger(l TamperLogger) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tamperLogger = l
}

// logTamper records a tamper event with the given action and detail.
func (w *StateWatcher) logTamper(action, detail string) {
	w.mu.Lock()
	l := w.tamperLogger
	w.mu.Unlock()
	if l != nil {
		l.Log(tamper.Event{At: time.Now(), Source: "state", Action: action, Detail: detail})
	}
}

// MarkSelfWrite records the SHA-256 of the state file content as written by
// the daemon itself, so an fsnotify event matching exactly that content is
// ignored (avoids redundant Reconcile cycles). Content-based matching has no
// time window, so an external edit that changes the content right after a
// self-write is still detected — there is no 500ms blind spot.
func (w *StateWatcher) MarkSelfWrite() {
	sum, err := fsutil.HashFile(w.StatePath)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.lastSelfHash = fsutil.Hash{}
		return
	}
	w.lastSelfHash = sum
}

func (w *StateWatcher) isSelfWrite() bool {
	sum, err := fsutil.HashFile(w.StatePath)
	if err != nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastSelfHash != (fsutil.Hash{}) && w.lastSelfHash == sum
}

func New(reconciler Reconciler, statePath string) *StateWatcher {
	return &StateWatcher{
		StatePath:  statePath,
		reconciler: reconciler,
		debounce:   defaultDebounce,
		events:     make(chan fsEvent, 64),
		stopCh:     make(chan struct{}),
	}
}

func (w *StateWatcher) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("statewatch: fsnotify: %w", err)
	}

	dir := filepath.Dir(w.StatePath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("statewatch: watch dir: %w", err)
	}

	w.mu.Lock()
	w.watcher = watcher
	w.mu.Unlock()

	go w.watchFsEvents()
	go w.eventLoop()
	return nil
}

func (w *StateWatcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
}

func (w *StateWatcher) watchFsEvents() {
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
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				if event.Name == w.StatePath || filepath.Base(event.Name) == filepath.Base(w.StatePath) {
					if w.isSelfWrite() {
						continue
					}
					select {
					case w.events <- fsEvent{op: "write"}:
					default:
					}
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

func (w *StateWatcher) eventLoop() {
	for {
		select {
		case <-w.events:
			w.debounceAndReconcile()
		case <-w.stopCh:
			return
		}
	}
}

func (w *StateWatcher) debounceAndReconcile() {
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
			w.detectAndReconcile()
			return
		case <-w.stopCh:
			return
		}
	}
}

// detectAndReconcile runs the reconcile asynchronously so a slow Reconcile
// never freezes the event loop. A boolean lock ensures only one reconcile runs
// at a time; events arriving while one is in flight are coalesced into a
// single follow-up run instead of being lost or spawning concurrent runs.
func (w *StateWatcher) detectAndReconcile() {
	w.mu.Lock()
	if w.reconcileRunning {
		w.reconcilePending = true
		w.mu.Unlock()
		return
	}
	w.reconcileRunning = true
	w.mu.Unlock()

	go func() {
		defer w.reconcileDone()
		// Todo evento que chega aqui é uma alteração externa (self-writes são
		// filtrados antes por hash) — registra no tamper-log para accountability.
		w.logTamper("reconcile", w.StatePath)
		log.Printf("[StateWatcher] Alteração detectada em %s — executando reconciliação...", w.StatePath)
		if err := w.reconciler.Reconcile(); err != nil {
			log.Printf("[StateWatcher] Erro na reconciliação após alteração em %s: %v", w.StatePath, err)
		} else {
			log.Printf("[StateWatcher] Reconciliação concluída com sucesso para %s", w.StatePath)
		}
	}()
}

// reconcileDone releases the boolean lock and re-runs once if a reconcile was
// requested while another one was in flight.
func (w *StateWatcher) reconcileDone() {
	w.mu.Lock()
	w.reconcileRunning = false
	pending := w.reconcilePending
	w.reconcilePending = false
	w.mu.Unlock()
	if pending {
		w.detectAndReconcile()
	}
}
