package store

import (
	"encoding/json"
	"errors"
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
	mu          sync.RWMutex
	filePath    string
	onSave      func()
	replicaPath string
	replicaKey  *[32]byte
}

// SetOnSave registers a callback invoked after each save, so external watchers
// (e.g. statewatch) can hash the content the daemon just wrote and suppress
// only the matching fsnotify event — instead of a time-based window that
// creates a blind spot for external edits.
func (s *Store) SetOnSave(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onSave = fn
}

func NewStore(filePath string) (*Store, error) {
	if filePath == "" {
		return nil, errors.New("file path is empty")
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de estado (%s): %w", dir, err)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := os.WriteFile(filePath, []byte("{}"), 0644); err != nil {
			return nil, fmt.Errorf("erro ao criar arquivo de estado (%s): %w", filePath, err)
		}
	}

	return &Store{filePath: filePath}, nil
}

func (s *Store) Load() (*State, error) {
	s.mu.RLock()
	data, err := os.ReadFile(s.filePath)
	s.mu.RUnlock()

	var state State
	valid := false
	switch {
	case os.IsNotExist(err):
		// Apagado — tenta a réplica abaixo antes de devolver estado limpo.
	case err != nil:
		return nil, fmt.Errorf("failed to read state file: %w", err)
	default:
		valid = json.Unmarshal(data, &state) == nil
	}

	if valid {
		if state.Blocks == nil {
			state.Blocks = make(map[string]policy.Block)
		}
		return &state, nil
	}

	// Arquivo corrompido, vazio (0 bytes) ou apagado: tenta a réplica
	// criptografada ANTES de curar com estado limpo. Curar primeiro destruiria
	// o único backup válido — writeReplicaLocked sobrescreveria a réplica boa
	// com o estado limpo. Sem essa ordem, qualquer chamador de Load() direto
	// (ex.: boot do scheduler) tornaria a réplica inútil.
	if st, ok := s.loadFromReplicaIfEnabled(); ok {
		return st, nil
	}

	if os.IsNotExist(err) {
		return cleanState(), nil
	}

	// Sem réplica: re-inicializa um estado limpo e cura o disco imediatamente.
	// Sem essa cura, um Reconcile com RAM vazia veria o arquivo corrompido
	// como "em sincronia" (ambos vazios) e ele ficaria corrompido até o
	// próximo restart. Se a RAM tiver bloqueios, o Reconcile seguinte
	// sobrescreve com o estado real — a RAM continua sendo a fonte da verdade.
	s.healCorrupted()
	return cleanState(), nil
}

// healCorrupted rewrites a corrupted or empty state file with a clean state.
// Best-effort: a failure here only means the next Reconcile/Save retries —
// Load never aborts on a bad file.
func (s *Store) healCorrupted() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-read sob o write lock: se outro writer (ex.: um Save concorrente)
	// já tiver curado/gravado um arquivo válido, não sobrescrevemos.
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var state State
	if err := json.Unmarshal(data, &state); err == nil {
		return // já válido — outro goroutine cuidou
	}
	_ = s.saveLocked(cleanState())
}

// cleanState returns a fresh, empty state used when the file is missing,
// corrupted or empty — the disk copy is only a mirror of the in-memory state.
func cleanState() *State {
	return &State{
		Version: 1,
		Blocks:  make(map[string]policy.Block),
	}
}

func (s *Store) Save(state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(state)
}

// saveLocked writes state to disk atomically (temp file + rename) and fires
// onSave after the rename so watchers can hash the content now on disk. The
// caller must hold s.mu.
func (s *Store) saveLocked(state *State) error {
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

	// No fsync on purpose: state.json is only a mirror of the in-memory state
	// (RAM is the source of truth), so forcing a physical flush on every save
	// (block/unblock/reconcile) would thrash the drive for zero correctness
	// gain — the OS page cache already serves the watchers. The atomic rename
	// below still guarantees the file is never observed half-written.
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	tmpFile = nil

	if err := os.Rename(tmpName, s.filePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Invoke onSave after the rename so watchers can hash the content now on
	// disk. A rare race (the fsnotify event delivered before this callback)
	// only causes one redundant no-op Reconcile, since disk == RAM then.
	if s.onSave != nil {
		s.onSave()
	}

	// Réplica criptografada oculta (auto-healing): best-effort, nunca falha
	// o Save principal — uma falha aqui só adia o backup para o próximo save.
	s.writeReplicaLocked(data)

	return nil
}
