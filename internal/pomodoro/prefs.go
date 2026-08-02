// Package pomodoro preferences: persisted default work/rest/cycles so users
// don't have to pass --work/--rest/--cycles on every invocation. The file is
// a tiny JSON next to the daemon state; corruption falls back to classics.
package pomodoro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Defaults follow the classic pomodoro technique.
const (
	DefaultWork   = 25
	DefaultRest   = 5
	DefaultCycles = 4
)

// Prefs persists the last-used pomodoro parameters as the defaults for the
// next session. Thread-safe: the daemon reads prefs from the IPC handler and
// writes them when a session completes.
type Prefs struct {
	mu   sync.Mutex
	path string
	work int
	// rest -1 means "never saved"; 0 is a meaningful saved value (no rest).
	rest      int
	cycles    int
	hasCycles bool
}

type prefsJSON struct {
	Work   int `json:"work"`
	Rest   int `json:"rest"`
	Cycles int `json:"cycles"`
}

// NewPrefs loads (or lazily starts from) the prefs file at path. A missing or
// corrupt file silently falls back to the classic defaults.
func NewPrefs(path string) *Prefs {
	p := &Prefs{path: path, rest: -1}
	p.load()
	return p
}

func (p *Prefs) load() {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var j prefsJSON
	if json.Unmarshal(data, &j) != nil {
		return
	}
	p.work = j.Work
	// rest é salvo como veio (0 = sem descanso é um valor válido persistido).
	p.rest = j.Rest
	if j.Cycles > 0 {
		p.cycles = j.Cycles
		p.hasCycles = true
	}
}

// Resolve merges explicit CLI values with the saved defaults. The sentinel
// values from the IPC layer are: 0 = not provided (use saved/default), -1 =
// not provided for rest (where 0 is a meaningful "no rest"). Positive values
// are explicit and win over the saved defaults.
func (p *Prefs) Resolve(work, rest, cycles int) (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	w := p.work
	if w <= 0 {
		w = DefaultWork
	}
	if work > 0 {
		w = work
	}

	// rest: -1 = não informado → salvo (se houver) senão default;
	// 0 = explicitamente sem descanso; >0 = explícito.
	r := p.rest
	if r < 0 {
		r = DefaultRest
	}
	switch rest {
	case -1: // não informado → salvo/default
	case 0: // explicitamente sem descanso
		r = 0
	default: // explícito
		r = rest
	}

	c := DefaultCycles
	if p.hasCycles && p.cycles > 0 {
		c = p.cycles
	}
	if cycles > 0 {
		c = cycles
	}
	return w, r, c
}

// Remember persists the resolved values so the next session defaults to them.
func (p *Prefs) Remember(work, rest, cycles int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.work = work
	p.rest = rest
	p.cycles = cycles
	p.hasCycles = cycles > 0

	data, err := json.MarshalIndent(prefsJSON{Work: work, Rest: rest, Cycles: cycles}, "", "  ")
	if err != nil {
		return
	}
	_ = osWriteFile(p.path, data)
}

// osWriteFile is stubbable so tests can simulate a corrupt file.
var osWriteFile = func(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
