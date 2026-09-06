package store

import (
	"encoding/json"

	"focusguard/internal/domain/policy"
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

func TestStore_Load_PurgesLegacySentinels(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")

	legacyJSON := `{
  "version": 1,
  "blocks": {
    "*all-internet*": {
      "domain": "*all-internet*",
      "started_at": "2026-08-23T17:01:00Z",
      "expires_at": "2026-08-23T18:01:00Z"
    },
    "youtube.com": {
      "domain": "youtube.com",
      "started_at": "2026-09-05T20:25:00Z",
      "expires_at": "2026-09-05T20:55:00Z"
    }
  }
}`
	if err := os.WriteFile(dbPath, []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("seed legacy state: %v", err)
	}

	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	state, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, exists := state.Blocks["*all-internet*"]; exists {
		t.Errorf("expected *all-internet* to be purged from state in memory")
	}
	if _, exists := state.Blocks["youtube.com"]; !exists {
		t.Errorf("expected valid domain youtube.com to remain")
	}

	diskData, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read disk: %v", err)
	}
	if strings.Contains(string(diskData), "*all-internet*") {
		t.Errorf("expected *all-internet* to be purged from disk file, got:\n%s", diskData)
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

// TestStoreSaveAndLoad_AdditiveFields verifies the additive schema fields
// round-trip: InterceptorEnabled survives a save/load cycle.
func TestStoreSaveAndLoad_AdditiveFields(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "focusguard-test*")
	if err != nil {
		t.Fatalf("failed to make temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s, err := NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	state := &State{
		Version:            1,
		InterceptorEnabled: true,
		LastKnownTime:      now,
		Blocks: map[string]policy.Block{
			"example.com": {
				Domain:      "example.com",
				StartedAt:   now,
				ExpiresAt:   now.Add(time.Hour),
				ResolvedIPs: []string{"1.2.3.4"},
			},
		},
	}
	if err := s.Save(state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if !loaded.InterceptorEnabled {
		t.Error("InterceptorEnabled não sobreviveu ao round-trip")
	}
	if !loaded.LastKnownTime.Equal(now) {
		t.Errorf("LastKnownTime = %v, want %v", loaded.LastKnownTime, now)
	}
	blk := loaded.Blocks["example.com"]
	if blk.Domain != "example.com" {
		t.Errorf("Domain = %q, want example.com", blk.Domain)
	}
}

// TestLoad_LegacyStateFile_DefaultsInterceptorOff verifies that a state file written
// before the interceptor field existed loads with InterceptorEnabled=false.
func TestLoad_LegacyStateFile_DefaultsInterceptorOff(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "focusguard-test*")
	if err != nil {
		t.Fatalf("failed to make temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.json")
	if err := os.WriteFile(dbPath, []byte(`{"version":1,"blocks":{}}`), 0644); err != nil {
		t.Fatalf("failed to write legacy state: %v", err)
	}

	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	state, err := s.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if state.InterceptorEnabled {
		t.Error("legacy state should load with InterceptorEnabled=false")
	}
}

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
