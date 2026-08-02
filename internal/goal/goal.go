// Package goal persists the user's daily focus goal (e.g. "4h of focus per
// day") used by the stats report and the TUI to show progress. The goal is a
// single duration stored in a JSON file next to the state; a missing or
// corrupt file simply means "no goal set".
package goal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store holds the daily focus goal with file persistence.
type Store struct {
	mu   sync.Mutex
	path string
	goal time.Duration
}

type fileShape struct {
	Minutes int64 `json:"daily_goal_minutes"`
}

// NewStore loads (or initializes) a Store backed by path. An empty path keeps
// the store in memory (tests). A missing/corrupt file yields a zero goal.
func NewStore(path string) *Store {
	s := &Store{path: path}
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var f fileShape
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	if f.Minutes > 0 {
		s.goal = time.Duration(f.Minutes) * time.Minute
	}
}

func (s *Store) save() error {
	if s.path == "" {
		return nil // em memória (testes)
	}
	data, err := json.MarshalIndent(fileShape{Minutes: int64(s.goal / time.Minute)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "goal-*.tmp")
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

// Get returns the configured daily goal (0 means none set).
func (s *Store) Get() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.goal
}

// Set updates the daily goal (0 clears it) and persists it. Negative values
// are rejected.
func (s *Store) Set(d time.Duration) error {
	if d < 0 {
		return errors.New("goal: meta diária não pode ser negativa")
	}
	s.mu.Lock()
	s.goal = d
	err := s.save()
	s.mu.Unlock()
	return err
}
