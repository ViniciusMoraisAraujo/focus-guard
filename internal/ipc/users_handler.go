package ipc

import (
	"context"
	"fmt"
)

// usersManager returns the wired credential store under the lock — espelho do
// padrão analyticsProvider() e demais accessors (o daemon pode configurá-lo
// depois de NewServer).
func (s *Server) usersManager() UserManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.users
}

// ---------------------------------------------------------------------------
// user-list / user-verify / user-add / user-remove / user-set-password
// ---------------------------------------------------------------------------

// handleUserList lista os usuários cadastrados.
func (s *Server) handleUserList(_ context.Context, _ *Request) (*Response, error) {
	m := s.usersManager()
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	return &Response{Success: true, Users: m.List()}, nil
}

// handleUserVerify valida credenciais (web-only: sem ActionSpec, isento do
// fechamento specs↔registry — o proxy web nunca o encaminha, evitando um
// oracle de senha sem o rate limit do login).
func (s *Server) handleUserVerify(_ context.Context, req *Request) (*Response, error) {
	m := s.usersManager()
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	u, ok := m.Verify(req.UserName, req.UserPassword)
	if !ok {
		// Mensagem única para usuário desconhecido e senha errada — não
		// revela qual dos dois falhou (best-effort; o IPC é local).
		return nil, Err(CodeInvalid, "usuário ou senha inválidos")
	}
	return &Response{Success: true, UserIsAdmin: u.IsAdmin}, nil
}

// handleUserAdd cria um usuário.
func (s *Server) handleUserAdd(_ context.Context, req *Request) (*Response, error) {
	m := s.usersManager()
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	if err := m.Add(req.UserName, req.UserPassword); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Usuário %s criado", req.UserName)}, nil
}

// handleUserRemove remove um usuário.
func (s *Server) handleUserRemove(_ context.Context, req *Request) (*Response, error) {
	m := s.usersManager()
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	if err := m.Remove(req.UserName); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Usuário %s removido", req.UserName)}, nil
}

// handleUserSetPassword altera a senha de um usuário.
func (s *Server) handleUserSetPassword(_ context.Context, req *Request) (*Response, error) {
	m := s.usersManager()
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	if err := m.SetPassword(req.UserName, req.UserPassword); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Senha de %s atualizada", req.UserName)}, nil
}
