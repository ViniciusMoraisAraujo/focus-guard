package fsutil

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestHashFile_KnownDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	sum, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}

	// SHA-256("hello")
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got := fmt.Sprintf("%x", [sha256.Size]byte(sum)); got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestHashFile_ChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if before == after {
		t.Error("expected different digests for different content")
	}
}

func TestHashFile_MissingFile(t *testing.T) {
	if _, err := HashFile(filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Error("expected error for missing file")
	}
}
