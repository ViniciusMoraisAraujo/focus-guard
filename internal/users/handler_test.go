package users

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/ipc"
	"focusguard/internal/user"
)

type fakeStore struct {
	users     []string
	lastUser  string
	verifyOK  bool
	isAdmin   bool
	err       error
}

func (f *fakeStore) List() []string { return f.users }

func (f *fakeStore) Verify(username, password string) (user.User, bool) {
	return user.User{Username: username, IsAdmin: f.isAdmin}, f.verifyOK
}

func (f *fakeStore) Add(username, password string) error {
	if f.err != nil {
		return f.err
	}
	f.lastUser = username
	f.users = append(f.users, username)
	return nil
}

func (f *fakeStore) Remove(username string) error {
	if f.err != nil {
		return f.err
	}
	f.lastUser = username
	return nil
}

func (f *fakeStore) SetPassword(username, password string) error {
	if f.err != nil {
		return f.err
	}
	f.lastUser = username
	return nil
}

func TestUserSetPassword_RejeitaSemUsuario(t *testing.T) {
	h := NewSetPassword(&fakeStore{})
	err := h.Validate(&ipc.Request{UserPassword: "nova-senha-123"})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeInvalid {
		t.Fatalf("esperava ERR_INVALID, got %v", err)
	}
}

func TestUserSetPassword_RejeitaSenhaCurta(t *testing.T) {
	h := NewSetPassword(&fakeStore{})
	err := h.Validate(&ipc.Request{UserName: "joao", UserPassword: "curta"})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeInvalid {
		t.Fatalf("esperava ERR_INVALID, got %v", err)
	}
}

func TestUserSetPassword_OK(t *testing.T) {
	st := &fakeStore{}
	h := NewSetPassword(st)
	resp, err := h.Handle(context.Background(), &ipc.Request{
		UserName: "joao", UserPassword: "nova-senha-123",
	})
	if err != nil || resp == nil || !resp.Success {
		t.Fatalf("esperava sucesso, got resp=%v err=%v", resp, err)
	}
	if st.lastUser != "joao" {
		t.Fatalf("SetPassword chamado com %q", st.lastUser)
	}
}

func TestUserSetPassword_SemStore(t *testing.T) {
	h := NewSetPassword(nil)
	_, err := h.Handle(context.Background(), &ipc.Request{UserName: "joao", UserPassword: "nova-senha-123"})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeNotConfigured {
		t.Fatalf("esperava ERR_NOT_CONFIGURED, got %v", err)
	}
}

func TestUserList_OK(t *testing.T) {
	h := NewList(&fakeStore{users: []string{"admin", "joao"}})
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "user-list"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(resp.Users) != 2 {
		t.Fatalf("esperava 2 usuários, got %+v", resp)
	}
}

func TestUserList_SemStore(t *testing.T) {
	h := NewList(nil)
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "user-list"})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeNotConfigured {
		t.Fatalf("esperava ERR_NOT_CONFIGURED, got %v", err)
	}
}

func TestUserVerify_CredenciaisInvalidas(t *testing.T) {
	h := NewVerify(&fakeStore{verifyOK: false})
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "user-verify", UserName: "x", UserPassword: "y"})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeInvalid {
		t.Fatalf("esperava ERR_INVALID, got %v", err)
	}
}

func TestUserVerify_OK(t *testing.T) {
	h := NewVerify(&fakeStore{verifyOK: true, isAdmin: true})
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "user-verify", UserName: "admin", UserPassword: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !resp.UserIsAdmin {
		t.Fatalf("esperava sucesso + admin, got %+v", resp)
	}
}

func TestUserAdd_OK(t *testing.T) {
	st := &fakeStore{}
	h := NewAdd(st)
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "user-add", UserName: "maria", UserPassword: "senha-segura-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || st.lastUser != "maria" {
		t.Fatalf("esperava sucesso, got resp=%+v last=%q", resp, st.lastUser)
	}
}

func TestUserAdd_ErroDoStore(t *testing.T) {
	st := &fakeStore{err: errors.New("user: já existe um usuário chamado \"x\"")}
	h := NewAdd(st)
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "user-add", UserName: "x", UserPassword: "senha-segura-1"})
	if err == nil {
		t.Fatal("esperava o erro do store propagado")
	}
}

func TestUserRemove_OK(t *testing.T) {
	st := &fakeStore{}
	h := NewRemove(st)
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "user-remove", UserName: "joao"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || st.lastUser != "joao" {
		t.Fatalf("esperava sucesso, got resp=%+v last=%q", resp, st.lastUser)
	}
}
