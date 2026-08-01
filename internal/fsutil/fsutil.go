// Package fsutil provides small filesystem helpers shared by the watchers.
package fsutil

import (
	"crypto/sha256"
	"os"
)

// Hash is a SHA-256 digest.
type Hash [sha256.Size]byte

// HashFile returns the SHA-256 digest of the file at path, or an error if the
// file cannot be read (e.g. it does not exist).
func HashFile(path string) (Hash, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Hash{}, err
	}
	return sha256.Sum256(data), nil
}
