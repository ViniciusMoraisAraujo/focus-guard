package user

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "user.json")
	return NewStore(path), path
}

func TestNewStore_MissingFileStartsEmpty(t *testing.T) {
	s, path := newTestStore(t)
	if len(s.List()) != 0 {
		t.Errorf("List() = %v, want empty", s.List())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("NewStore não deveria criar o arquivo; err = %v", err)
	}
}

func TestEnsureAdmin_SeedsDefaultAdmin(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if got := s.List(); len(got) != 1 || got[0] != DefaultAdminUsername {
		t.Errorf("List() = %v, want [admin]", got)
	}
	u, ok := s.Verify(DefaultAdminUsername, defaultAdminPassword)
	if !ok {
		t.Fatal("Verify(admin, senha padrão) deveria passar")
	}
	if !u.IsAdmin {
		t.Error("admin deveria ser IsAdmin")
	}
	if _, ok := s.Verify(DefaultAdminUsername, "senha-errada"); ok {
		t.Error("Verify com senha errada deveria falhar")
	}
}

func TestEnsureAdmin_Idempotent(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin segunda vez: %v", err)
	}
	if got := s.List(); len(got) != 1 {
		t.Errorf("List() = %v, want apenas [admin]", got)
	}
}

func TestEnsureAdmin_DoesNotResetChangedPassword(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if err := s.SetPassword(DefaultAdminUsername, "nova-senha-123"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	// Um segundo boot (EnsureAdmin) não pode re-criar o admin com a senha
	// padrão — a troca de senha sobrevive.
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin pós-troca: %v", err)
	}
	if _, ok := s.Verify(DefaultAdminUsername, "nova-senha-123"); !ok {
		t.Error("senha trocada deveria continuar valendo após EnsureAdmin")
	}
	if _, ok := s.Verify(DefaultAdminUsername, defaultAdminPassword); ok {
		t.Error("senha padrão não deveria voltar a valer após EnsureAdmin")
	}
}

func TestAdd_CreatesAndVerifiesUser(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if err := s.Add("Maria", "senha-forte-1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Username é normalizado para minúsculas.
	u, ok := s.Verify("maria", "senha-forte-1")
	if !ok {
		t.Fatal("Verify(maria, senha) deveria passar")
	}
	if u.IsAdmin {
		t.Error("usuário criado por Add não deveria ser admin")
	}
	// Case-insensitive no login.
	if _, ok := s.Verify("MARIA", "senha-forte-1"); !ok {
		t.Error("Verify deveria ser case-insensitive no username")
	}
}

func TestAdd_Validation(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "empty username", username: "", password: "senha-forte-1"},
		{name: "whitespace username", username: "  ", password: "senha-forte-1"},
		{name: "username with space", username: "maria jose", password: "senha-forte-1"},
		{name: "short password", username: "maria", password: "curta"},
		{name: "admin cannot be recreated", username: "admin", password: "senha-forte-1"},
		{name: "admin case-insensitive", username: "Admin", password: "senha-forte-1"},
		{name: "password over 72 bytes", username: "maria", password: strings.Repeat("a", 73)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestStore(t)
			if err := s.Add(tc.username, tc.password); err == nil {
				t.Errorf("Add(%q, ...) deveria falhar", tc.username)
			}
		})
	}
}

func TestAdd_DuplicateRejected(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Add("maria", "senha-forte-1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add("MARIA", "outra-senha-2"); err == nil {
		t.Error("Add duplicado deveria falhar")
	}
}

func TestVerify_UnknownUser(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if _, ok := s.Verify("nao-existe", "qualquer-senha"); ok {
		t.Error("Verify de usuário desconhecido deveria falhar")
	}
}

func TestRemove_RemovesUser(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Add("maria", "senha-forte-1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Remove("maria"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := s.Verify("maria", "senha-forte-1"); ok {
		t.Error("Verify após Remove deveria falhar")
	}
	if len(s.List()) != 0 {
		t.Errorf("List() = %v, want empty", s.List())
	}
}

func TestRemove_AdminRejected(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if err := s.Remove(DefaultAdminUsername); err == nil {
		t.Error("Remove(admin) deveria falhar")
	}
}

func TestAdd_WhitespaceErrorIsDistinct(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.Add("maria jose", "senha-forte-1")
	if err == nil {
		t.Fatal("Add com espaço no nome deveria falhar")
	}
	if !strings.Contains(err.Error(), "espaços") {
		t.Errorf("mensagem deveria mencionar espaços, got %q", err.Error())
	}
}

func TestRemove_UnknownUser(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Remove("nao-existe"); err == nil {
		t.Error("Remove de usuário desconhecido deveria falhar")
	}
}

func TestSetPassword_ChangesAndVerifies(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if err := s.SetPassword(DefaultAdminUsername, "nova-senha-123"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, ok := s.Verify(DefaultAdminUsername, defaultAdminPassword); ok {
		t.Error("senha antiga não deveria valer após troca")
	}
	u, ok := s.Verify(DefaultAdminUsername, "nova-senha-123")
	if !ok {
		t.Fatal("nova senha deveria valer")
	}
	if !u.IsAdmin {
		t.Error("admin deveria continuar IsAdmin após troca de senha")
	}
}

func TestSetPassword_RegularUser(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Add("maria", "senha-forte-1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.SetPassword("maria", "nova-senha-2"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, ok := s.Verify("maria", "senha-forte-1"); ok {
		t.Error("senha antiga não deveria valer após troca")
	}
	u, ok := s.Verify("maria", "nova-senha-2")
	if !ok {
		t.Fatal("nova senha deveria valer")
	}
	if u.IsAdmin {
		t.Error("maria não deveria virar admin ao trocar a senha")
	}
}

func TestSetPassword_ValidationAndUnknownUser(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.SetPassword("nao-existe", "senha-forte-1"); err == nil {
		t.Error("SetPassword de usuário desconhecido deveria falhar")
	}
	if err := s.SetPassword(DefaultAdminUsername, "curta"); err == nil {
		t.Error("SetPassword com senha curta deveria falhar")
	}
}

func TestLoad_ReadsFromDisk(t *testing.T) {
	s, path := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if err := s.Add("maria", "senha-forte-1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Um novo Store sobre o mesmo arquivo enxerga os usuários persistidos.
	s2 := NewStore(path)
	if got := s2.List(); len(got) != 2 {
		t.Errorf("List() = %v, want [admin maria]", got)
	}
	if _, ok := s2.Verify("maria", "senha-forte-1"); !ok {
		t.Error("usuário persistido deveria verificar no novo store")
	}
}

func TestCorruptFile_BestEffortAndHeals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.json")
	if err := os.WriteFile(path, []byte("{corrompido"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := NewStore(path)
	if len(s.List()) != 0 {
		t.Errorf("store corrompido deveria carregar vazio, got %v", s.List())
	}
	// EnsureAdmin cura: grava um arquivo válido com o admin.
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin sobre arquivo corrompido: %v", err)
	}
	if _, ok := s.Verify(DefaultAdminUsername, defaultAdminPassword); !ok {
		t.Error("admin deveria verificar após a cura")
	}
}

func TestFile_ContainsOnlyHashes(t *testing.T) {
	s, path := newTestStore(t)
	if err := s.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	plain := "senha-segura-123"
	if err := s.Add("maria", plain); err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if bytes.Contains(data, []byte(defaultAdminPassword)) {
		t.Error("arquivo contém a senha padrão do admin em texto puro")
	}
	if bytes.Contains(data, []byte(plain)) {
		t.Error("arquivo contém a senha do usuário em texto puro")
	}
	// Todo hash é bcrypt ($2a$/$2b$/$2y$) — nada de hash fraco.
	for _, u := range s.users {
		if !strings.HasPrefix(u.PasswordHash, "$2") {
			t.Errorf("hash de %s não parece bcrypt: %q", u.Username, u.PasswordHash)
		}
	}
}
