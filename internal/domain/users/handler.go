// Package users implements the domain service for the user-* IPC actions (web
// login and user management — Fase 4 do refactor-plan). Each action is a
// self-contained Handler depending only on the minimal Store interface (DIP);
// the *user.Store satisfies it structurally. Handlers use package-local types;
// the transport adapts them via ipc.DomainAction (pós-reorg item 2).
package users

import (
	"context"
	"fmt"
	"strings"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/domain/user"
)

// minPasswordLen espelha a regra do user.Store (minPasswordLen=8) — aqui é só
// fail-fast sem chamar o daemon; o store continua a autoridade final.
const minPasswordLen = 8

// Store is the credential surface the user-* actions need.
type Store interface {
	List() []string
	Verify(username, password string) (user.User, bool)
	Add(username, password string) error
	Remove(username string) error
	SetPassword(username, password string) error
}

// Tipos de entrada/saída das ações — adaptados pelo transporte (DIP).
type NoInput struct{}
type ListResult struct{ Users []string }
type VerifyInput struct{ UserName, UserPassword string }
type VerifyResult struct{ UserIsAdmin bool }
type AddInput struct{ UserName, UserPassword string }
type AddResult struct{ Message string }
type RemoveInput struct{ UserName string }
type RemoveResult struct{ Message string }
type SetPasswordInput struct{ UserName, UserPassword string }
type SetPasswordResult struct{ Message string }

// ---------------------------------------------------------------------------
// user-list
// ---------------------------------------------------------------------------

// ListHandler executes "user-list".
type ListHandler struct {
	store Store
}

// NewList builds the "user-list" handler. A nil store makes the action fail
// with the "não configurado" message (tests/dev builds), as the switch did.
func NewList(store Store) *ListHandler { return &ListHandler{store: store} }

func (h *ListHandler) Action() string { return "user-list" }

func (h *ListHandler) Validate(*NoInput) error { return nil }

func (h *ListHandler) Handle(ctx context.Context, _ *NoInput) (*ListResult, error) {
	if h.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "usuários não configurados")
	}
	return &ListResult{Users: h.store.List()}, nil
}

// ---------------------------------------------------------------------------
// user-verify
// ---------------------------------------------------------------------------

// VerifyHandler executes "user-verify" (web login).
type VerifyHandler struct {
	store Store
}

// NewVerify builds the "user-verify" handler.
func NewVerify(store Store) *VerifyHandler { return &VerifyHandler{store: store} }

func (h *VerifyHandler) Action() string { return "user-verify" }

func (h *VerifyHandler) Validate(*VerifyInput) error { return nil }

func (h *VerifyHandler) Handle(ctx context.Context, req *VerifyInput) (*VerifyResult, error) {
	if h.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "usuários não configurados")
	}
	u, ok := h.store.Verify(req.UserName, req.UserPassword)
	if !ok {
		// Mensagem única para usuário desconhecido e senha errada — não
		// revela qual dos dois falhou (best-effort; o IPC é local).
		return nil, ipcerr.New(ipcerr.CodeInvalid, "usuário ou senha inválidos")
	}
	return &VerifyResult{UserIsAdmin: u.IsAdmin}, nil
}

// ---------------------------------------------------------------------------
// user-add
// ---------------------------------------------------------------------------

// AddHandler executes "user-add".
type AddHandler struct {
	store Store
}

// NewAdd builds the "user-add" handler.
func NewAdd(store Store) *AddHandler { return &AddHandler{store: store} }

func (h *AddHandler) Action() string { return "user-add" }

func (h *AddHandler) Validate(*AddInput) error { return nil }

func (h *AddHandler) Handle(ctx context.Context, req *AddInput) (*AddResult, error) {
	if h.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "usuários não configurados")
	}
	if err := h.store.Add(req.UserName, req.UserPassword); err != nil {
		return nil, err // o user.Store já devolve mensagens PT-BR prontas
	}
	return &AddResult{Message: fmt.Sprintf("Usuário %s criado", req.UserName)}, nil
}

// ---------------------------------------------------------------------------
// user-remove
// ---------------------------------------------------------------------------

// RemoveHandler executes "user-remove".
type RemoveHandler struct {
	store Store
}

// NewRemove builds the "user-remove" handler.
func NewRemove(store Store) *RemoveHandler { return &RemoveHandler{store: store} }

func (h *RemoveHandler) Action() string { return "user-remove" }

func (h *RemoveHandler) Validate(*RemoveInput) error { return nil }

func (h *RemoveHandler) Handle(ctx context.Context, req *RemoveInput) (*RemoveResult, error) {
	if h.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "usuários não configurados")
	}
	if err := h.store.Remove(req.UserName); err != nil {
		return nil, err
	}
	return &RemoveResult{Message: fmt.Sprintf("Usuário %s removido", req.UserName)}, nil
}

// ---------------------------------------------------------------------------
// user-set-password
// ---------------------------------------------------------------------------

// SetPasswordHandler executes "user-set-password" (self-change ou do admin).
type SetPasswordHandler struct {
	store Store
}

// NewSetPassword builds the "user-set-password" handler.
func NewSetPassword(store Store) *SetPasswordHandler { return &SetPasswordHandler{store: store} }

func (h *SetPasswordHandler) Action() string { return "user-set-password" }

// Validate é fail-fast (sem chamar o daemon) sobre os campos do request; o
// store continua a autoridade final das regras de senha.
func (h *SetPasswordHandler) Validate(req *SetPasswordInput) error {
	if strings.TrimSpace(req.UserName) == "" {
		return ipcerr.New(ipcerr.CodeInvalid, "informe o nome de usuário")
	}
	if len(req.UserPassword) < minPasswordLen {
		return ipcerr.New(ipcerr.CodeInvalid, fmt.Sprintf("a senha precisa de ao menos %d caracteres", minPasswordLen))
	}
	return nil
}

func (h *SetPasswordHandler) Handle(ctx context.Context, req *SetPasswordInput) (*SetPasswordResult, error) {
	if h.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "usuários não configurados")
	}
	if err := h.store.SetPassword(req.UserName, req.UserPassword); err != nil {
		return nil, err // user.Store já devolve mensagens PT-BR prontas
	}
	return &SetPasswordResult{Message: fmt.Sprintf("Senha de %s atualizada", req.UserName)}, nil
}
