// Package apps persists the process denylist consumed by the process guard
// (which executables are terminated while a focus session is active). The
// list lives in apps.json next to the state file; a missing or corrupt file
// falls back to the default denylist (steam, discord).
package apps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// defaultDenylist is the fallback denylist (as in the historical hardcoded
// default in the daemon) used when no apps.json exists yet.
var defaultDenylist = []string{"steam.exe", "discord.exe"}

// Store owns the denylist with file persistence.
type Store struct {
	mu       sync.Mutex
	path     string
	denylist []string
}

// NewStore loads (or initializes with the defaults) a Store backed by path.
// A missing or corrupt file yields the default denylist — never an error.
func NewStore(path string) *Store {
	s := &Store{path: path, denylist: append([]string(nil), defaultDenylist...)}
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return // corrompido — mantém os defaults
	}
	s.denylist = list
}

func (s *Store) save() error {
	if s.path == "" {
		return nil // em memória (testes)
	}
	data, err := json.MarshalIndent(s.denylist, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "apps-*.tmp")
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

// List returns a copy of the denylist (normalized, lowercase, .exe stripped
// by normalizeName on the way in).
func (s *Store) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.denylist...)
}

// Add appends a process name (normalized) unless already present, and
// persists. Empty names and names with path separators are rejected.
func (s *Store) Add(name string) error {
	n := normalizeName(name)
	if n == "" {
		return fmt.Errorf("apps: informe o nome do processo (ex: discord.exe)")
	}
	if strings.ContainsAny(n, "/\\") {
		return fmt.Errorf("apps: nome de processo inválido %q", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasLocked(n) {
		return nil
	}
	s.denylist = append(s.denylist, n)
	return s.save()
}

// Remove deletes a process name from the denylist and persists. Removing an
// unknown entry is an error.
func (s *Store) Remove(name string) error {
	n := normalizeName(name)
	if n == "" {
		return fmt.Errorf("apps: informe o nome do processo (ex: steam)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.denylist {
		if normalizeName(x) == n {
			s.denylist = append(s.denylist[:i], s.denylist[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("apps: processo %q não está na denylist", name)
}

func (s *Store) hasLocked(n string) bool {
	for _, x := range s.denylist {
		if normalizeName(x) == n {
			return true
		}
	}
	return false
}

// normalizeName lowercases and strips a trailing ".exe" (any casing), so
// "Discord.exe", "DISCORD.EXE" and "discord" all normalize to "discord".
func normalizeName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(n, ".exe")
}
