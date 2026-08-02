package goal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SetAndGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal.json")
	s := NewStore(path)

	if got := s.Get(); got != 0 {
		t.Errorf("meta inicial = %v, want 0 (sem meta definida)", got)
	}

	if err := s.Set(4 * time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get(); got != 4*time.Hour {
		t.Errorf("Get = %v, want 4h", got)
	}
}

func TestStore_PersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal.json")
	s := NewStore(path)
	if err := s.Set(90 * time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	s2 := NewStore(path) // recarrega do disco (daemon restart)
	if got := s2.Get(); got != 90*time.Minute {
		t.Errorf("Get após reload = %v, want 90m", got)
	}
}

func TestStore_ZeroResetsMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal.json")
	s := NewStore(path)
	if err := s.Set(2 * time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set(0); err != nil {
		t.Fatalf("Set(0): %v", err)
	}
	if got := s.Get(); got != 0 {
		t.Errorf("Get após Set(0) = %v, want 0", got)
	}
}

func TestStore_NegativeRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal.json")
	s := NewStore(path)
	if err := s.Set(-time.Hour); err == nil {
		t.Error("meta negativa deve ser rejeitada")
	}
}

func TestStore_CorruptFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal.json")
	if err := os.WriteFile(path, []byte("{corrompido"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if got := s.Get(); got != 0 {
		t.Errorf("arquivo corrompido deve resultar em meta vazia, got %v", got)
	}
}

func TestStore_InMemoryNoFile(t *testing.T) {
	s := NewStore("")
	if err := s.Set(time.Hour); err != nil {
		t.Fatalf("Set em memória: %v", err)
	}
	if got := s.Get(); got != time.Hour {
		t.Errorf("Get = %v, want 1h", got)
	}
}
