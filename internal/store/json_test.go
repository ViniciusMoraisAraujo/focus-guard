package store

import (
	"encoding/json"

	"focusguard/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreSaveAndLoad(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "focusguard-test*")
	if err != nil {
		t.Fatalf("failed to make temp dir: %v", err)
	}

	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	state, err := s.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if len(state.Blocks) != 0 {
		t.Fatalf("Initial state should be empty. %v", state.Blocks)
	}

	now := time.Now()
	state.Blocks["twitter.com"] = policy.Block{
		Domain:      "twitter.com",
		StartedAt:   now,
		ExpiresAt:   now.Add(2 * time.Hour),
		ResolvedIPs: []string{"104.244.42.1"},
	}

	if err := s.Save(state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	loadedState, err := s.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	block, exists := loadedState.Blocks["twitter.com"]
	if !exists {
		t.Fatalf("twitter.com block should exist")
	}
	if block.Domain != "twitter.com" || len(block.ResolvedIPs) != 1 {
		t.Fatalf("twitter.com block should contain resolved ips")
	}
}

// TestSave_OnSaveRunsAfterWrite verifies that the onSave callback is invoked
// after the file content is already on disk, so watchers can hash exactly what
// was written (content-based self-write detection instead of a time window).
func TestSave_OnSaveRunsAfterWrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "focusguard-test*")
	if err != nil {
		t.Fatalf("failed to make temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	var sawNewContent bool
	s.SetOnSave(func() {
		data, err := os.ReadFile(dbPath)
		if err == nil && strings.Contains(string(data), "twitter.com") {
			sawNewContent = true
		}
	})

	now := time.Now()
	state := &State{
		Version: 1,
		Blocks: map[string]policy.Block{
			"twitter.com": {
				Domain:      "twitter.com",
				StartedAt:   now,
				ExpiresAt:   now.Add(2 * time.Hour),
				ResolvedIPs: []string{"104.244.42.1"},
			},
		},
	}

	if err := s.Save(state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	if !sawNewContent {
		t.Error("expected onSave to observe the freshly written content (called after the write)")
	}
}

// TestLoad_CorruptedJSON_ReturnsCleanState verifies that a state.json with
// invalid JSON does not abort the daemon: Load must return a clean, empty
// state so the scheduler can re-write the RAM state over the corrupted disk
// copy instead of failing to boot.
func TestLoad_CorruptedJSON_ReturnsCleanState(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "focusguard-test*")
	if err != nil {
		t.Fatalf("failed to make temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := os.WriteFile(dbPath, []byte(`{not valid json`), 0644); err != nil {
		t.Fatalf("failed to corrupt state file: %v", err)
	}

	state, err := s.Load()
	if err != nil {
		t.Fatalf("Load() should not error on corrupted JSON, got: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Errorf("expected clean empty state from corrupted file, got %d blocks", len(state.Blocks))
	}
	if state.Blocks == nil {
		t.Error("expected non-nil Blocks map in the clean state")
	}
}

// TestLoad_ZeroByteFile_ReturnsCleanState verifies that a 0-byte state.json
// (e.g. crash mid-write) does not abort the daemon: Load returns a clean state
// instead of an unmarshal error.
func TestLoad_ZeroByteFile_ReturnsCleanState(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "focusguard-test*")
	if err != nil {
		t.Fatalf("failed to make temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := os.WriteFile(dbPath, nil, 0644); err != nil {
		t.Fatalf("failed to truncate state file: %v", err)
	}

	state, err := s.Load()
	if err != nil {
		t.Fatalf("Load() should not error on empty file, got: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Errorf("expected clean empty state from 0-byte file, got %d blocks", len(state.Blocks))
	}
	if state.Blocks == nil {
		t.Error("expected non-nil Blocks map in the clean state")
	}
}

// TestLoad_CorruptedJSON_HealsFile verifies that Load() not only returns a
// clean state but also rewrites the corrupted disk copy, so a later Reconcile
// with empty RAM cannot mistake corruption for "in sync" — the file heals
// itself and never stays corrupted until the next restart.
func TestLoad_CorruptedJSON_HealsFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "focusguard-test*")
	if err != nil {
		t.Fatalf("failed to make temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := os.WriteFile(dbPath, []byte(`{not valid json`), 0644); err != nil {
		t.Fatalf("failed to corrupt state file: %v", err)
	}

	if _, err := s.Load(); err != nil {
		t.Fatalf("Load() should not error on corrupted JSON, got: %v", err)
	}

	// The disk copy must now be valid JSON (healed in place).
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read after heal: %v", err)
	}
	var healed State
	if err := json.Unmarshal(data, &healed); err != nil {
		t.Fatalf("file should be healed to valid JSON, got: %v (%q)", err, string(data))
	}
	if len(healed.Blocks) != 0 {
		t.Errorf("expected healed file with no blocks, got %d", len(healed.Blocks))
	}
}
