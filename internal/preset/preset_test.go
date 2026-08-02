package preset

import (
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
