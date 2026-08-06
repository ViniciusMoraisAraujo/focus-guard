package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"focusguard/internal/domain/policy"
)

// hardwareID is stubbable in tests; the real implementations live in
// hardwareid_{linux,windows,other}.go (machine-id / MachineGuid).
var hardwareID = machineID

// deriveKey derives a 32-byte AES-256 key from the machine secret. In
// production the secret is the hardware ID, so the replica can only be
// decrypted on the same machine that sealed it.
func deriveKey(secret []byte) ([32]byte, error) {
	if len(secret) == 0 {
		return [32]byte{}, errors.New("segredo vazio: impossível derivar chave")
	}
	return sha256.Sum256(secret), nil
}

// encryptReplica seals plain with AES-256-GCM. The random 12-byte nonce is
// prepended and the GCM authentication tag appended to the ciphertext — the
// tag is the "assinatura": any tampering or a wrong key fails decryption.
func encryptReplica(key [32]byte, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("replica aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("replica gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("replica nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// decryptReplica opens a blob sealed by encryptReplica, authenticating the
// ciphertext (GCM tag) against the key before returning the plaintext.
func decryptReplica(key [32]byte, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("replica aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("replica gcm: %w", err)
	}
	if len(blob) < gcm.NonceSize() {
		return nil, errors.New("replica: blob menor que nonce+tag")
	}
	nonce, sealed := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("replica: falha na autenticação/descriptografia: %w", err)
	}
	return plain, nil
}

// replicaFilePath returns the hidden dotfile that stores the sealed replica
// (e.g. ".state.json.replica") — hidden on Unix, invisible in the listing of
// the state directory.
func replicaFilePath(filePath string) string {
	return filepath.Join(filepath.Dir(filePath), "."+filepath.Base(filePath)+".replica")
}

// EnableReplica activates the encrypted hidden replica: every successful Save
// also writes a sealed copy to "<dir>/.<base>.replica" so a missing or
// corrupted state.json can be auto-healed by LoadAndHeal. When secret is empty
// the key is derived from the machine hardware ID (machine-id / MachineGuid),
// binding the backup to this machine. A failure only disables replicas — it
// never breaks the primary store path.
func (s *Store) EnableReplica(secret []byte) error {
	if len(secret) == 0 {
		id, err := hardwareID()
		if err != nil {
			return fmt.Errorf("replica: ID de hardware indisponível: %w", err)
		}
		secret = []byte(id)
	}
	key, err := deriveKey(secret)
	if err != nil {
		return fmt.Errorf("replica: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replicaKey != nil {
		return nil // já ativada — idempotente
	}
	s.replicaKey = &key
	s.replicaPath = replicaFilePath(s.filePath)
	return nil
}

// writeReplicaLocked seals data and atomically writes the hidden replica.
// Best-effort: a replica write failure must never fail the primary save, and
// the next successful save retries. Caller must hold s.mu.
func (s *Store) writeReplicaLocked(data []byte) {
	if s.replicaKey == nil || s.replicaPath == "" {
		return
	}
	blob, err := encryptReplica(*s.replicaKey, data)
	if err != nil {
		return
	}
	dir := filepath.Dir(s.replicaPath)
	tmp, err := os.CreateTemp(dir, ".replica-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	_ = tmp.Chmod(0600)
	if _, err := tmp.Write(blob); err != nil {
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, s.replicaPath)
}

// LoadAndHeal returns the primary state when valid; otherwise it recovers from
// the hidden encrypted replica (decrypt → validate → restore the primary
// file) before falling back to a clean state. Load() is fully replica-aware,
// so this is a thin, self-documenting entry point for daemon boot: RAM is
// still the source of truth, the replica only prevents data loss when the
// disk mirror is missing, empty or corrupted.
func (s *Store) LoadAndHeal() (*State, error) {
	return s.Load()
}

// loadFromReplicaIfEnabled tries the hidden encrypted replica when replicas
// are enabled. Returns the recovered state (and restores the primary) on
// success.
func (s *Store) loadFromReplicaIfEnabled() (*State, bool) {
	s.mu.RLock()
	key, path := s.replicaKey, s.replicaPath
	s.mu.RUnlock()
	if key == nil {
		return nil, false
	}
	return s.loadFromReplica(*key, path)
}

// loadFromReplica reads the sealed replica, authenticates+decrypts it and
// restores the primary file. Returns the recovered state on success.
func (s *Store) loadFromReplica(key [32]byte, path string) (*State, bool) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	plain, err := decryptReplica(key, blob)
	if err != nil {
		return nil, false
	}
	var st State
	if err := json.Unmarshal(plain, &st); err != nil {
		return nil, false
	}
	if st.Blocks == nil {
		st.Blocks = make(map[string]policy.Block)
	}

	// Restaura o primary (melhor esforço). Sob o write lock, re-checa se outro
	// writer já gravou um primary válido desde a nossa leitura (mesmo guard do
	// healCorrupted): se sim, esse estado é mais novo — usa-o em vez da
	// réplica e não sobrescreve. Uma falha aqui não perde nada: a RAM recebe
	// o estado recuperado e o próximo Save regrava os dois arquivos.
	s.mu.Lock()
	if data, rerr := os.ReadFile(s.filePath); rerr == nil {
		var cur State
		if json.Unmarshal(data, &cur) == nil {
			if cur.Blocks == nil {
				cur.Blocks = make(map[string]policy.Block)
			}
			// Não regrava a réplica com cur: o Save concorrente que a originou
			// já a atualizou (writeReplicaLocked roda a cada Save) — regravar
			// aqui só causaria churn desnecessário.
			s.mu.Unlock()
			return &cur, true
		}
	}
	_ = s.saveLocked(&st)
	s.mu.Unlock()
	return &st, true
}
