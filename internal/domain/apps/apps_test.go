package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_DefaultDenylist(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "apps.json"))
	got := s.List()
	if len(got) != 2 {
		t.Fatalf("expected default denylist of 2 entries, got %v", got)
	}
	for _, want := range []string{"steam.exe", "discord.exe"} {
		if !contains(got, want) {
			t.Errorf("default denylist should contain %q, got %v", want, got)
		}
	}
}

func TestStore_AddAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	s := NewStore(path)

	if err := s.Add("Spotify.exe"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !contains(s.List(), "spotify") {
		t.Errorf("Add should normalize (strip .exe) and append, got %v", s.List())
	}
	// Reabre do disco — persistência real.
	s2 := NewStore(path)
	if !contains(s2.List(), "spotify") {
		t.Errorf("reopened store should contain spotify, got %v", s2.List())
	}
}

func TestStore_AddDuplicateIsNoOp(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "apps.json"))
	n := len(s.List())
	if err := s.Add("Discord.EXE"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(s.List()) != n {
		t.Errorf("duplicate add should be a no-op, got %v", s.List())
	}
}

func TestStore_AddInvalidName(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "apps.json"))
	for _, bad := range []string{"", "   ", "a/b", "c\\d"} {
		if err := s.Add(bad); err == nil {
			t.Errorf("Add(%q) should fail", bad)
		}
	}
}

func TestStore_Remove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	s := NewStore(path)
	if err := s.Remove("steam.exe"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if contains(s.List(), "steam.exe") {
		t.Errorf("steam.exe should be removed, got %v", s.List())
	}
	s2 := NewStore(path)
	if contains(s2.List(), "steam.exe") {
		t.Errorf("removal should persist, got %v", s2.List())
	}
	if err := s.Remove("nope.exe"); err == nil {
		t.Error("removing an unknown app should fail")
	}
}

func TestStore_RemoveAllLeavesEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "apps.json"))
	for _, a := range s.List() {
		if err := s.Remove(a); err != nil {
			t.Fatalf("Remove(%s): %v", a, err)
		}
	}
	if len(s.List()) != 0 {
		t.Errorf("expected empty denylist, got %v", s.List())
	}
}

func TestStore_MissingFileGetsDefaults(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nope.json"))
	if len(s.List()) != 2 {
		t.Errorf("missing file should fall back to defaults, got %v", s.List())
	}
}

func TestStore_CorruptFileFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	if err := os.WriteFile(path, []byte("{corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if len(s.List()) != 2 {
		t.Errorf("corrupt file should fall back to defaults, got %v", s.List())
	}
}

func contains(list []string, want string) bool {
	for _, x := range list {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}
