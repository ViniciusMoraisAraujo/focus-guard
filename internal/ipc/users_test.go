package ipc

import (
	"errors"
	"os"
	"strings"
	"testing"

	"focusguard/internal/user"
)

// fakeUserManager is a stubbable UserManager used to test the server's user
// wiring without touching disk.
type fakeUserManager struct {
	users      []user.User
	verifyUser user.User
	verifyOK   bool
	added      []string
	removed    []string
	set        []string
	addErr     error
	remErr     error
	setErr     error
}

func (f *fakeUserManager) List() []string {
	out := make([]string, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u.Username)
	}
	return out
}

func (f *fakeUserManager) Verify(_, _ string) (user.User, bool) {
	return f.verifyUser, f.verifyOK
}

func (f *fakeUserManager) Add(username, _ string) error {
	f.added = append(f.added, username)
	return f.addErr
}

func (f *fakeUserManager) Remove(username string) error {
	f.removed = append(f.removed, username)
	return f.remErr
}

func (f *fakeUserManager) SetPassword(username, _ string) error {
	f.set = append(f.set, username)
	return f.setErr
}

func TestServer_UserList_ReturnsNames(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{users: &fakeUserManager{
		users: []user.User{
			{Username: "admin", IsAdmin: true},
			{Username: "maria"},
		},
	}})

	resp := executeRequest(t, server, Request{Action: "user-list"})
	if !resp.Success {
		t.Fatalf("user-list falhou: %s", resp.Message)
	}
	if len(resp.Users) != 2 || resp.Users[0] != "admin" || resp.Users[1] != "maria" {
		t.Errorf("Users = %v, want [admin maria]", resp.Users)
	}
}

func TestServer_UserList_WithoutManager(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "user-list"})
	if resp.Success {
		t.Fatal("user-list sem manager deveria falhar")
	}
	if !strings.Contains(resp.Message, "não configurados") {
		t.Errorf("mensagem inesperada: %q", resp.Message)
	}
}

func TestServer_UserVerify_Success_Admin(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{users: &fakeUserManager{
		verifyUser: user.User{Username: "admin", IsAdmin: true},
		verifyOK:   true,
	}})

	resp := executeRequest(t, server, Request{Action: "user-verify", UserName: "admin", UserPassword: "x"})
	if !resp.Success {
		t.Fatalf("user-verify falhou: %s", resp.Message)
	}
	if !resp.UserIsAdmin {
		t.Error("UserIsAdmin deveria ser true para o admin")
	}
}

func TestServer_UserVerify_Success_NonAdmin(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{users: &fakeUserManager{
		verifyUser: user.User{Username: "maria"},
		verifyOK:   true,
	}})

	resp := executeRequest(t, server, Request{Action: "user-verify", UserName: "maria", UserPassword: "x"})
	if !resp.Success {
		t.Fatalf("user-verify falhou: %s", resp.Message)
	}
	if resp.UserIsAdmin {
		t.Error("UserIsAdmin deveria ser false para usuário comum")
	}
}

func TestServer_UserVerify_InvalidCredentials(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{users: &fakeUserManager{verifyOK: false}})

	resp := executeRequest(t, server, Request{Action: "user-verify", UserName: "maria", UserPassword: "errada"})
	if resp.Success {
		t.Fatal("user-verify com credenciais inválidas deveria falhar")
	}
	// Mensagem única: não revela se o usuário ou a senha falhou.
	if !strings.Contains(resp.Message, "usuário ou senha inválidos") {
		t.Errorf("mensagem inesperada: %q", resp.Message)
	}
}

func TestServer_UserVerify_WithoutManager(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "user-verify"})
	if resp.Success {
		t.Fatal("user-verify sem manager deveria falhar")
	}
}

func TestServer_UserAdd_Success(t *testing.T) {
	fake := &fakeUserManager{}
	server := setupTestServerWithDeps(t, &refDeps{users: fake})

	resp := executeRequest(t, server, Request{Action: "user-add", UserName: "maria", UserPassword: "senha-forte-1"})
	if !resp.Success {
		t.Fatalf("user-add falhou: %s", resp.Message)
	}
	if len(fake.added) != 1 || fake.added[0] != "maria" {
		t.Errorf("added = %v, want [maria]", fake.added)
	}
}

func TestServer_UserAdd_ValidationError(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{users: &fakeUserManager{addErr: errors.New("user: a senha precisa de ao menos 8 caracteres")}})

	resp := executeRequest(t, server, Request{Action: "user-add", UserName: "maria", UserPassword: "curta"})
	if resp.Success {
		t.Fatal("user-add rejeitado pelo store deveria falhar")
	}
	if !strings.Contains(resp.Message, "ao menos 8") {
		t.Errorf("mensagem deveria carregar o erro do store, got %q", resp.Message)
	}
}

func TestServer_UserAdd_WithoutManager(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "user-add"})
	if resp.Success {
		t.Fatal("user-add sem manager deveria falhar")
	}
}

func TestServer_UserRemove_Success(t *testing.T) {
	fake := &fakeUserManager{}
	server := setupTestServerWithDeps(t, &refDeps{users: fake})

	resp := executeRequest(t, server, Request{Action: "user-remove", UserName: "maria"})
	if !resp.Success {
		t.Fatalf("user-remove falhou: %s", resp.Message)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "maria" {
		t.Errorf("removed = %v, want [maria]", fake.removed)
	}
}

func TestServer_UserRemove_WithoutManager(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "user-remove", UserName: "maria"})
	if resp.Success {
		t.Fatal("user-remove sem manager deveria falhar")
	}
}

func TestServer_UserRemove_Error(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{users: &fakeUserManager{remErr: errors.New("user: o usuário admin não pode ser removido")}})

	resp := executeRequest(t, server, Request{Action: "user-remove", UserName: "admin"})
	if resp.Success {
		t.Fatal("user-remove rejeitado pelo store deveria falhar")
	}
}

func TestServer_UserSetPassword_Success(t *testing.T) {
	fake := &fakeUserManager{}
	server := setupTestServerWithDeps(t, &refDeps{users: fake})

	resp := executeRequest(t, server, Request{Action: "user-set-password", UserName: "maria", UserPassword: "nova-senha-123"})
	if !resp.Success {
		t.Fatalf("user-set-password falhou: %s", resp.Message)
	}
	if len(fake.set) != 1 || fake.set[0] != "maria" {
		t.Errorf("set = %v, want [maria]", fake.set)
	}
}

func TestServer_UserSetPassword_WithoutManager(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "user-set-password", UserName: "maria", UserPassword: "nova-senha-123"})
	if resp.Success {
		t.Fatal("user-set-password sem manager deveria falhar")
	}
}

func TestServer_UserSetPassword_Error(t *testing.T) {
	server := setupTestServerWithDeps(t, &refDeps{users: &fakeUserManager{setErr: errors.New("user: usuário \"x\" não encontrado")}})

	resp := executeRequest(t, server, Request{Action: "user-set-password", UserName: "x", UserPassword: "nova-senha-123"})
	if resp.Success {
		t.Fatal("user-set-password rejeitado pelo store deveria falhar")
	}
}

// TestUserPackage_InterfaceCompliance verifica que o *user.Store real satisfaz
// a interface UserManager (o wiring do daemon depende disso).
func TestUserPackage_InterfaceCompliance(t *testing.T) {
	var _ UserManager = user.NewStore(t.TempDir() + "/user.json")
}

// TestServer_UserVerify_RealStore exercita o fluxo completo de login com o
// store real em disco (sem fake): senha correta passa, errada falha, o
// IsAdmin reflete o admin semeado e o user.json só guarda hashes.
func TestServer_UserVerify_RealStore(t *testing.T) {
	path := t.TempDir() + "/user.json"
	st := user.NewStore(path)
	if err := st.EnsureAdmin(); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	server := setupTestServerWithDeps(t, &refDeps{users: st})

	resp := executeRequest(t, server, Request{Action: "user-verify", UserName: "admin", UserPassword: "SP02cfasm#"})
	if !resp.Success {
		t.Fatalf("user-verify com o store real falhou: %s", resp.Message)
	}
	if !resp.UserIsAdmin {
		t.Error("admin real deveria ter UserIsAdmin=true")
	}

	bad := executeRequest(t, server, Request{Action: "user-verify", UserName: "admin", UserPassword: "errada"})
	if bad.Success {
		t.Error("senha errada deveria falhar no store real")
	}

	// Criar um usuário comum via ação e verificar que não é admin.
	add := executeRequest(t, server, Request{Action: "user-add", UserName: "maria", UserPassword: "senha-forte-1"})
	if !add.Success {
		t.Fatalf("user-add real falhou: %s", add.Message)
	}
	maria := executeRequest(t, server, Request{Action: "user-verify", UserName: "maria", UserPassword: "senha-forte-1"})
	if !maria.Success || maria.UserIsAdmin {
		t.Errorf("maria deveria verificar como não-admin, got success=%v isAdmin=%v", maria.Success, maria.UserIsAdmin)
	}

	// O arquivo em disco contém apenas hashes — nenhuma senha em texto puro.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leitura do user.json: %v", err)
	}
	if strings.Contains(string(data), "SP02cfasm#") {
		t.Error("user.json contém a senha padrão do admin em texto puro")
	}
	if strings.Contains(string(data), "senha-forte-1") {
		t.Error("user.json contém a senha da maria em texto puro")
	}
}
