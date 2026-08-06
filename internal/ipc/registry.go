package ipc

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds the action handlers by action name. The daemon registers
// every handler at boot (Fase 5 moves the registration to the composition
// root); the Server's router dispatches through it with the legacy switch as
// fallback until every action migrates (Fase 4).
type Registry struct {
	mu   sync.RWMutex
	byID map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Handler)}
}

// Register stores (or overwrites) the handler for h.Action(). One place to
// register — a new action needs only a handler + a spec line (OCP, B2).
func (r *Registry) Register(h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[h.Action()] = h
}

// Get returns the handler for action, if registered.
func (r *Registry) Get(action string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.byID[action]
	return h, ok
}

// Actions returns the registered action names, sorted — for boot validation
// and tests.
func (r *Registry) Actions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byID))
	for a := range r.byID {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// ValidateSpecs checks the registry→specs closure: every registered handler
// must have an ActionSpec (otherwise the web proxy would 403 it silently — a
// handler forgotten in the spec table is a boot-time bug, not a runtime
// surprise). The reverse direction (every spec has a handler) is enforced
// once the legacy switch empties in Fase 4; web-only actions the daemon still
// serves (user-verify) are exempt.
func (r *Registry) ValidateSpecs(webOnly ...string) error {
	exempt := make(map[string]bool, len(webOnly))
	for _, a := range webOnly {
		exempt[a] = true
	}
	var missing []string
	for _, action := range r.Actions() {
		if exempt[action] {
			continue
		}
		if _, ok := SpecFor(action); !ok {
			missing = append(missing, action)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("handlers sem ActionSpec: %v (adicione a linha em internal/ipc/spec.go)", missing)
	}
	return nil
}
