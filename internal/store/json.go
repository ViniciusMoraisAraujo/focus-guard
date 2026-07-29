package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"focusguard/internal/policy"
)

type State struct {
	Version int                     `json:"version"`
	Blocks  map[string]policy.Block `json:"blocks"`
}

type Store struct {
	mu       sync.RWMutex
	filePath string
}

func NewStore(filePath string) *Store {
	return &Store{
		filePath: filePath}
}

func (s *Store) Load() (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath)
	if os.IsNotExist(err) {
		return &State{
			Version: 1,
			Blocks:  make(map[string]policy.Block),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}
	if state.Blocks == nil {
		state.Blocks = make(map[string]policy.Block)
	}
	return &state, nil
}

func (s *Store) Save(state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if state.Version == 0 {
		state.Version = 1
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmpFile.Name()

	defer func() {
		if tmpFile != nil {
			_ = tmpFile.Close()
		}
		if _, err := os.Stat(tmpName); err == nil {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	tmpFile = nil

	if err := os.Rename(tmpName, s.filePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}
