package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestList_ContainsExpectedPresets(t *testing.T) {
	presets := List()

	names := make(map[string]bool, len(presets))
	for _, p := range presets {
		if p.Name == "" || p.Label == "" {
			t.Errorf("preset with empty name/label: %+v", p)
		}
		if len(p.Domains) == 0 {
			t.Errorf("preset %q has no domains", p.Name)
		}
		for _, d := range p.Domains {
			if strings.TrimSpace(d) == "" {
				t.Errorf("preset %q contains an empty domain", p.Name)
			}
		}
		names[p.Name] = true
	}

	for _, want := range []string{"social", "video", "news", "games"} {
		if !names[want] {
			t.Errorf("expected built-in preset %q in List(), got %v", want, names)
		}
	}
}

func TestResolve_Found(t *testing.T) {
	p, err := Resolve("social")
	if err != nil {
		t.Fatalf("Resolve(social) erro: %v", err)
	}
	if p.Name != "social" {
		t.Errorf("Name = %q, want social", p.Name)
	}
	if len(p.Domains) == 0 {
		t.Error("social preset should have domains")
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	for _, name := range []string{"Social", "SOCIAL", "Games", "VIDEO"} {
		if _, err := Resolve(name); err != nil {
			t.Errorf("Resolve(%q) should be case-insensitive, erro: %v", name, err)
		}
	}
}

func TestResolve_Unknown_ReturnsError(t *testing.T) {
	if _, err := Resolve("nonexistent"); err == nil {
		t.Error("expected error for unknown preset")
	}
}

func TestList_ReturnsCopies(t *testing.T) {
	first := List()
	second := List()

	// Mutating the returned slice must not corrupt the built-in catalog.
	first[0].Name = "hacked"
	first[0].Domains[0] = "evil.com"
	if len(first) != len(second) {
		t.Fatal("List() returned inconsistent lengths")
	}

	p, err := Resolve(second[0].Name)
	if err != nil {
		t.Fatalf("Resolve(%s) after mutation: %v", second[0].Name, err)
	}
	if p.Domains[0] == "evil.com" {
		t.Error("built-in catalog was mutated through List()")
	}
}

// ---------------------------------------------------------------------------
// Store (presets personalizados, persistidos em arquivo)
// ---------------------------------------------------------------------------

func TestStore_List_MergesBuiltinsAndCustom(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	if err := s.Add(Preset{Name: "estudo", Label: "Estudo", Domains: []string{"khanacademy.org", "coursera.org"}}); err != nil {
		t.Fatalf("Add erro: %v", err)
	}

	all := s.List()
	names := make(map[string]bool, len(all))
	for _, p := range all {
		names[p.Name] = true
	}
	for _, want := range []string{"social", "video", "news", "games", "estudo"} {
		if !names[want] {
			t.Errorf("List() deveria conter %q, got %v", want, names)
		}
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")
	s1 := NewStore(path)
	if err := s1.Add(Preset{Name: "estudo", Label: "Estudo", Domains: []string{"khanacademy.org"}}); err != nil {
		t.Fatalf("Add erro: %v", err)
	}

	// Nova instância lê o mesmo arquivo (simula restart do daemon).
	s2 := NewStore(path)
	p, err := s2.Resolve("estudo")
	if err != nil {
		t.Fatalf("Resolve após reload erro: %v", err)
	}
	if p.Label != "Estudo" || len(p.Domains) != 1 {
		t.Errorf("preset reloaded inesperado: %+v", p)
	}
}

func TestStore_Resolve_CustomFirst(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	if err := s.Add(Preset{Name: "meusites", Label: "Meus sites", Domains: []string{"exemplo.com"}}); err != nil {
		t.Fatalf("Add erro: %v", err)
	}
	p, err := s.Resolve("MEUSITES")
	if err != nil {
		t.Fatalf("Resolve custom erro: %v", err)
	}
	if p.Name != "meusites" {
		t.Errorf("Name = %q, want meusites", p.Name)
	}
}

func TestStore_Add_EmptyName(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	if err := s.Add(Preset{Name: "  ", Domains: []string{"x.com"}}); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestStore_Add_CannotOverrideBuiltin(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	err := s.Add(Preset{Name: "social", Domains: []string{"x.com"}})
	if err == nil || !strings.Contains(err.Error(), "padrão") {
		t.Errorf("expected error about builtin override, got %v", err)
	}
}

func TestStore_Add_DuplicateCustom(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	if err := s.Add(Preset{Name: "estudo", Domains: []string{"a.com"}}); err != nil {
		t.Fatalf("primeiro Add erro: %v", err)
	}
	if err := s.Add(Preset{Name: "estudo", Domains: []string{"b.com"}}); err == nil {
		t.Error("expected error for duplicate custom name")
	}
}

func TestStore_Add_NoDomains(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	if err := s.Add(Preset{Name: "vazio"}); err == nil {
		t.Error("expected error when no domains")
	}
}

func TestStore_Remove_CustomOnly(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	_ = s.Add(Preset{Name: "estudo", Domains: []string{"a.com"}})
	if err := s.Remove("estudo"); err != nil {
		t.Fatalf("Remove custom erro: %v", err)
	}
	if _, err := s.Resolve("estudo"); err == nil {
		t.Error("custom preset deveria ter sido removido")
	}
	// Builtins nunca podem ser removidos.
	if err := s.Remove("social"); err == nil {
		t.Error("expected error when removing a builtin preset")
	}
}

func TestStore_Remove_Unknown(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	if err := s.Remove("nao-existe"); err == nil {
		t.Error("expected error for unknown preset")
	}
}

func TestStore_CorruptFile_StartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")
	if err := os.WriteFile(path, []byte("{not json!!"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := NewStore(path)
	if err := s.Add(Preset{Name: "estudo", Domains: []string{"a.com"}}); err != nil {
		t.Fatalf("Add após arquivo corrompido erro: %v", err)
	}
	if _, err := s.Resolve("estudo"); err != nil {
		t.Errorf("custom preset deveria funcionar após cura: %v", err)
	}
}

func TestStore_List_DoesNotExposeInternalSlice(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "presets.json"))
	_ = s.Add(Preset{Name: "estudo", Domains: []string{"a.com"}})
	all := s.List()
	for i := range all {
		all[i].Name = "hacked"
		all[i].Domains[0] = "evil.com"
	}
	p, err := s.Resolve("estudo")
	if err != nil {
		t.Fatalf("Resolve após mutação: %v", err)
	}
	if p.Domains[0] == "evil.com" {
		t.Error("custom catalog foi mutado através de List()")
	}
}
