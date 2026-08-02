// Package preset provides the domain-group catalog used by the "block --preset"
// and pomodoro commands: named categories (social, video, news, games) mapped
// to the domains blocked together. Built-in presets are always available;
// user-defined presets (Store) are persisted in a JSON file next to the state
// and merged on top of the catalog.
package preset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Preset is a named group of domains blocked together by category.
type Preset struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Domains []string `json:"domains"`
}

// builtins is the default catalog. The exact domain list per category is a
// product decision; names are the stable identifiers used by the CLI/IPC.
var builtins = []Preset{
	{
		Name:    "social",
		Label:   "Redes sociais e mensageiros",
		Domains: []string{"twitter.com", "x.com", "facebook.com", "instagram.com", "tiktok.com", "whatsapp.com", "linkedin.com"},
	},
	{
		Name:    "video",
		Label:   "Vídeo e streaming",
		Domains: []string{"youtube.com", "netflix.com", "twitch.tv", "primevideo.com", "disneyplus.com"},
	},
	{
		Name:    "news",
		Label:   "Notícias e fóruns",
		Domains: []string{"reddit.com", "news.ycombinator.com", "cnn.com", "bbc.com", "globo.com"},
	},
	{
		Name:    "games",
		Label:   "Jogos",
		Domains: []string{"steam.com", "epicgames.com", "roblox.com", "xbox.com", "playstation.com"},
	},
}

// List returns a copy of the catalog so callers cannot mutate the builtins.
func List() []Preset {
	out := make([]Preset, len(builtins))
	for i, p := range builtins {
		out[i] = Preset{
			Name:    p.Name,
			Label:   p.Label,
			Domains: append([]string(nil), p.Domains...),
		}
	}
	return out
}

// Resolve returns the preset with the given name (case-insensitive). Unknown
// names return an error listing the available options.
func Resolve(name string) (Preset, error) {
	return resolveIn(builtins, name)
}

// resolveIn finds a preset by name (case-insensitive) within the given list.
func resolveIn(list []Preset, name string) (Preset, error) {
	for _, p := range list {
		if strings.EqualFold(p.Name, strings.TrimSpace(name)) {
			return Preset{
				Name:    p.Name,
				Label:   p.Label,
				Domains: append([]string(nil), p.Domains...),
			}, nil
		}
	}
	var names []string
	for _, p := range list {
		names = append(names, p.Name)
	}
	return Preset{}, fmt.Errorf("preset desconhecido %q (disponíveis: %s)", name, strings.Join(names, ", "))
}

// ---------------------------------------------------------------------------
// Store — presets personalizados do usuário, persistidos em um JSON file
// ---------------------------------------------------------------------------

// Store is a file-backed catalog of user-defined presets. The built-in catalog
// is always available; custom presets are loaded from a JSON file in the state
// directory and merged on top (Resolve checks custom first). A missing or
// corrupt file starts the store empty — the catalog is never lost to a bad
// file, and the next Add rewrites it.
type Store struct {
	mu     sync.Mutex
	path   string
	custom []Preset
}

// NewStore loads (or creates) a Store backed by path.
func NewStore(path string) *Store {
	s := &Store{path: path}
	s.load()
	return s
}

// load reads the persisted custom presets. Missing/corrupt files leave the
// store empty (best-effort — never aborts daemon startup).
func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var custom []Preset
	if err := json.Unmarshal(data, &custom); err != nil {
		return
	}
	s.custom = custom
}

// save writes the custom presets atomically (temp file + rename).
func (s *Store) save() error {
	if s.path == "" {
		return nil // store em memória (testes)
	}
	data, err := json.MarshalIndent(s.custom, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "presets-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// List returns the full catalog: built-ins followed by user presets (copies,
// so callers cannot mutate the store).
func (s *Store) List() []Preset {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Preset, 0, len(builtins)+len(s.custom))
	out = append(out, List()...)
	for _, p := range s.custom {
		out = append(out, Preset{
			Name:    p.Name,
			Label:   p.Label,
			Domains: append([]string(nil), p.Domains...),
		})
	}
	return out
}

// Resolve looks up a preset by name: custom presets first (case-insensitive),
// then the built-in catalog.
func (s *Store) Resolve(name string) (Preset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, err := resolveIn(s.custom, name); err == nil {
		return p, nil
	}
	return resolveIn(builtins, name)
}

// Add validates and persists a new custom preset. Names that collide with a
// built-in preset or an existing custom preset are rejected; at least one
// non-empty domain is required. Domains are trimmed, lowercased and
// de-duplicated.
func (s *Store) Add(p Preset) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := strings.ToLower(strings.TrimSpace(p.Name))
	if name == "" {
		return fmt.Errorf("preset: nome não pode ser vazio")
	}
	for _, b := range builtins {
		if strings.EqualFold(b.Name, name) {
			return fmt.Errorf("preset: %q é um preset padrão e não pode ser substituído", b.Name)
		}
	}
	for _, c := range s.custom {
		if strings.EqualFold(c.Name, name) {
			return fmt.Errorf("preset: já existe um preset personalizado chamado %q", name)
		}
	}

	seen := make(map[string]bool, len(p.Domains))
	var domains []string
	for _, d := range p.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		domains = append(domains, d)
	}
	if len(domains) == 0 {
		return fmt.Errorf("preset: informe ao menos um domínio")
	}

	label := strings.TrimSpace(p.Label)
	if label == "" {
		label = name
	}
	s.custom = append(s.custom, Preset{Name: name, Label: label, Domains: domains})
	return s.save()
}

// Remove deletes a user-defined preset. Built-in presets are never removable.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.ToLower(strings.TrimSpace(name))
	for _, b := range builtins {
		if strings.EqualFold(b.Name, name) {
			return fmt.Errorf("preset: %q é um preset padrão e não pode ser removido", b.Name)
		}
	}
	for i, c := range s.custom {
		if strings.EqualFold(c.Name, name) {
			s.custom = append(s.custom[:i], s.custom[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("preset: personalizado %q não encontrado", name)
}
