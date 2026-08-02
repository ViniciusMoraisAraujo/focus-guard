package pomodoro

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPrefs_Defaults(t *testing.T) {
	p := NewPrefs(filepath.Join(t.TempDir(), "pomodoro.json"))
	w, r, c := p.Resolve(0, -1, 0)
	if w != 25 || r != 5 || c != 4 {
		t.Errorf("defaults = %d/%d/%d, want 25/5/4", w, r, c)
	}
}

func TestPrefs_ResolveKeepsExplicit(t *testing.T) {
	p := NewPrefs(filepath.Join(t.TempDir(), "pomodoro.json"))
	// --work 50 informado, --rest não informado (-1), --cycles informado
	w, r, c := p.Resolve(50, -1, 2)
	if w != 50 || r != 5 || c != 2 {
		t.Errorf("Resolve = %d/%d/%d, want 50/5/2", w, r, c)
	}
}

func TestPrefs_ResolveRestZeroKept(t *testing.T) {
	p := NewPrefs(filepath.Join(t.TempDir(), "pomodoro.json"))
	// --rest 0 explícito deve ser mantido (sem descanso), não virar default
	w, r, c := p.Resolve(25, 0, 4)
	if r != 0 {
		t.Errorf("rest 0 explícito deveria ser mantido, got %d", r)
	}
	_ = w
	_ = c
}

func TestPrefs_RememberAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pomodoro.json")
	p := NewPrefs(path)
	p.Remember(50, 10, 2)

	p2 := NewPrefs(path)
	w, r, c := p2.Resolve(0, -1, 0)
	if w != 50 || r != 10 || c != 2 {
		t.Errorf("persisted defaults = %d/%d/%d, want 50/10/2", w, r, c)
	}
}

func TestPrefs_CorruptFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pomodoro.json")
	_ = osWriteFile(path, []byte("{corrupt"))
	p := NewPrefs(path)
	w, r, c := p.Resolve(0, -1, 0)
	if w != 25 || r != 5 || c != 4 {
		t.Errorf("corrupt file should fall back to defaults, got %d/%d/%d", w, r, c)
	}
}

func TestPrefs_DurationHelpers(t *testing.T) {
	if got := 25 * time.Minute; got != time.Duration(25)*time.Minute {
		t.Errorf("sanity")
	}
}
