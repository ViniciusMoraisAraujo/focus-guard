// Package user persists the FocusGuard web UI credentials in user.json next
// to the state file. It follows the same file pattern as the other stores
// (presets.json, apps.json): a Store guarded by a mutex, best-effort load and
// atomic temp-file+rename saves. Passwords are never stored in plain text —
// only bcrypt hashes — and the system always has a built-in admin account.
package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Built-in admin credential, seeded once (EnsureAdmin) when user.json does
// not exist yet and hashed at seed time — the plain text never reaches disk.
// It is the recovery account: change it after the first login
// (user-set-password).
const (
	DefaultAdminUsername = "admin"
	defaultAdminPassword = "SP02cfasm#"
)

const (
	// minPasswordLen bounds the weakest acceptable password.
	minPasswordLen = 8
	// maxPasswordBytes caps the password at bcrypt's 72-byte input limit.
	// Longer inputs are truncated silently by bcrypt, so they are rejected
	// outright instead of producing a hash of only the first 72 bytes.
	maxPasswordBytes = 72
	// maxUsernameLen is a sanity cap for the display name.
	maxUsernameLen = 32
)

// User is one credential entry of the web UI.
type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
}

// file mirrors the on-disk shape (snake_case + version, like state.json).
type file struct {
	Version int    `json:"version"`
	Users   []User `json:"users"`
}

// Store owns the credential list with file persistence. A missing or corrupt
// file leaves the store empty — the daemon seeds the admin via EnsureAdmin.
type Store struct {
	mu    sync.Mutex
	path  string
	users []User
}

// NewStore loads (or starts empty) a Store backed by path. Missing/corrupt
// files never fail: the admin seed (EnsureAdmin) restores a working state.
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
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return // corrompido — mantém vazio; EnsureAdmin cura
	}
	s.users = f.Users
}

func (s *Store) save() error {
	if s.path == "" {
		return nil // em memória (testes)
	}
	f := file{Version: 1, Users: s.users}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "user-*.tmp")
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

// EnsureAdmin seeds the built-in admin when no admin user exists yet. It is
// idempotent and safe to call at every daemon boot: once an admin exists
// (possibly with a changed password), nothing is touched.
func (s *Store) EnsureAdmin() error {
	s.mu.Lock()
	if s.hasAdminLocked() {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	// bcrypt fora do lock: ~50-100ms por hash, não pode segurar o mutex.
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user: falha ao gerar hash do admin: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasAdminLocked() {
		return nil // outro goroutine seedou enquanto gerávamos o hash
	}
	s.users = append(s.users, User{
		Username:     DefaultAdminUsername,
		PasswordHash: string(hash),
		IsAdmin:      true,
		CreatedAt:    time.Now(),
	})
	return s.save()
}

// hasAdminLocked reports whether an admin account exists. Caller must hold
// s.mu.
func (s *Store) hasAdminLocked() bool {
	for _, u := range s.users {
		if u.IsAdmin {
			return true
		}
	}
	return false
}

// find returns the user matching name (case-insensitive) or nil. Callers must
// hold s.mu.
func (s *Store) find(name string) *User {
	for i := range s.users {
		if strings.EqualFold(s.users[i].Username, name) {
			return &s.users[i]
		}
	}
	return nil
}

// dummyHashOnce lazily builds a valid bcrypt hash used only to keep Verify's
// response time roughly constant for unknown usernames.
var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

// dummyBcryptHash returns a bcrypt hash of a throwaway password (best-effort:
// nil when generation fails, in which case Verify just short-circuits).
func dummyBcryptHash() []byte {
	dummyHashOnce.Do(func() {
		h, err := bcrypt.GenerateFromPassword([]byte("focusguard-dummy-verification"), bcrypt.DefaultCost)
		if err == nil {
			dummyHash = h
		}
	})
	return dummyHash
}

// Verify checks a username/password pair against the store. It returns the
// user and true only when the username exists and the bcrypt hash matches.
// The single false result does not reveal whether the username or the
// password was wrong, and an unknown username still pays one bcrypt compare
// so it cannot be distinguished from a wrong password by response time.
func (s *Store) Verify(username, password string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.find(username)
	if u == nil {
		if h := dummyBcryptHash(); h != nil {
			_ = bcrypt.CompareHashAndPassword(h, []byte(password))
		}
		return User{}, false
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return User{}, false
	}
	return *u, true
}

// List returns the usernames (copies — never the hashes).
func (s *Store) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u.Username)
	}
	return out
}

// Add creates a new non-admin user. Usernames are lowercased; duplicates,
// empty names and weak passwords are rejected. The built-in admin cannot be
// re-created through Add.
func (s *Store) Add(username, password string) error {
	name := strings.ToLower(strings.TrimSpace(username))
	if name == "" {
		return errors.New("user: informe um nome de usuário")
	}
	if strings.ContainsAny(name, " \t\n") {
		return errors.New("user: o nome de usuário não pode conter espaços")
	}
	if len(name) > maxUsernameLen {
		return fmt.Errorf("user: nome de usuário muito longo (máx %d caracteres)", maxUsernameLen)
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	// bcrypt fora do lock (mesmo motivo do EnsureAdmin).
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user: falha ao gerar hash: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.EqualFold(name, DefaultAdminUsername) {
		return errors.New("user: o usuário admin é único e não pode ser recriado")
	}
	if s.find(name) != nil {
		return fmt.Errorf("user: já existe um usuário chamado %q", name)
	}
	s.users = append(s.users, User{
		Username:     name,
		PasswordHash: string(hash),
		IsAdmin:      false,
		CreatedAt:    time.Now(),
	})
	return s.save()
}

// Remove deletes a user. The built-in admin can never be removed — the
// system must always keep its recovery account.
func (s *Store) Remove(username string) error {
	name := strings.TrimSpace(username)
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.EqualFold(name, DefaultAdminUsername) {
		return errors.New("user: o usuário admin não pode ser removido")
	}
	for i, u := range s.users {
		if strings.EqualFold(u.Username, name) {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("user: usuário %q não encontrado", name)
}

// SetPassword changes a user's password. Works for the admin too (recovery /
// self-change).
func (s *Store) SetPassword(username, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	// bcrypt fora do lock (mesmo motivo do EnsureAdmin).
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user: falha ao gerar hash: %w", err)
	}

	name := strings.TrimSpace(username)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.find(name)
	if u == nil {
		return fmt.Errorf("user: usuário %q não encontrado", name)
	}
	u.PasswordHash = string(hash)
	return s.save()
}

func validatePassword(password string) error {
	if len(password) < minPasswordLen {
		return fmt.Errorf("user: a senha precisa de ao menos %d caracteres", minPasswordLen)
	}
	if len(password) > maxPasswordBytes {
		return fmt.Errorf("user: a senha pode ter no máximo %d bytes", maxPasswordBytes)
	}
	return nil
}
