package store

import (
	"focusguard/internal/policy"
	"os"
	"path/filepath"
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
