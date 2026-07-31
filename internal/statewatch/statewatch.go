package statewatch

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultDebounce = 200 * time.Millisecond

type fsEvent struct {
	op string
}

type Reconciler interface {
	Reconcile() error
}

type StateWatcher struct {
	mu         sync.Mutex
	StatePath  string
	reconciler Reconciler
	debounce   time.Duration
	events     chan fsEvent
	stopCh     chan struct{}
	stopOnce   sync.Once
	watcher    *fsnotify.Watcher
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
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if event.Name == w.StatePath || filepath.Base(event.Name) == filepath.Base(w.StatePath) {
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

func (w *StateWatcher) detectAndReconcile() {
	log.Printf("[StateWatcher] Alteração detectada em %s — executando reconciliação...", w.StatePath)
	if err := w.reconciler.Reconcile(); err != nil {
		log.Printf("[StateWatcher] Erro na reconciliação após alteração em %s: %v", w.StatePath, err)
	} else {
		log.Printf("[StateWatcher] Reconciliação concluída com sucesso para %s", w.StatePath)
	}
}
