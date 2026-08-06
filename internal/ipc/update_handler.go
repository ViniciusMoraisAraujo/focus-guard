package ipc

import (
	"context"

	"focusguard/internal/update"
)

// updateCheckerBridge adapta o ipc.UpdateChecker do wire (devolve
// ipc.UpdateStatus) ao update.Checker do domínio (update.Status) — o domínio
// não pode importar ipc (ciclo: ipc importa update para o adapter).
type updateCheckerBridge struct{ c UpdateChecker }

func (b updateCheckerBridge) Check(ctx context.Context, apply bool, channel string) (update.Status, error) {
	st, err := b.c.Check(ctx, apply, channel)
	return update.Status{
		CurrentVersion: st.CurrentVersion,
		NewVersion:     st.NewVersion,
		Available:      st.Available,
		Applied:        st.Applied,
		PendingReboot:  st.PendingReboot,
	}, err
}

// updater returns the wired update checker under the lock, bridgeado para o
// tipo do domínio (nil → nil: o serviço devolve "auto-update não configurado").
// O nome é curto porque o campo do Server já é s.updateChecker — espelho do
// padrão analyticsProvider() e demais accessors.
func (s *Server) updater() update.Checker {
	s.mu.RLock()
	c := s.updateChecker
	s.mu.RUnlock()
	if c == nil {
		return nil
	}
	return updateCheckerBridge{c: c}
}

// markUpdateApplied arma o latch que o roteador consome APÓS escrever a
// resposta no socket, para disparar o hook de restart (mesma ordem do legado).
func (s *Server) markUpdateApplied() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateApplied = true
}

// takeUpdateApplied lê-e-limpa o latch de update aplicado (read-and-clear:
// cada aplicação notifica o hook uma única vez).
func (s *Server) takeUpdateApplied() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	applied := s.updateApplied
	s.updateApplied = false
	return applied
}

// dispatchUpdateHook notifica o onUpdateApplied (daemon sai para o
// supervisor/subir o binário novo) em goroutine — nunca bloqueia a conexão.
func (s *Server) dispatchUpdateHook() {
	s.mu.RLock()
	fn := s.onUpdateApplied
	s.mu.RUnlock()
	if fn != nil {
		go fn()
	}
}

// ---------------------------------------------------------------------------
// update / update-check
// ---------------------------------------------------------------------------
//
// As duas ações delegam ao serviço de domínio update.Service (Fase 4). O
// adapter mantém no Server o que é estado do processo — o latch de restart
// (markUpdateApplied) e o cache do status para a ação "status" — e aplica o
// orçamento de timeout (updateTimeout). Erros do checker (dev builds: checker
// ausente) viram CodeNotConfigured, semântica exata do switch legado.

// handleUpdate aplica uma atualização disponível aos binários.
func (s *Server) handleUpdate(_ context.Context, req *Request) (*Response, error) {
	return s.runUpdateAction(req, true)
}

// handleUpdateCheck verifica atualizações sem aplicar (botão "Verificar" da UI,
// consulta o GitHub na hora em vez de ler o cache do status).
func (s *Server) handleUpdateCheck(_ context.Context, req *Request) (*Response, error) {
	return s.runUpdateAction(req, false)
}

// runUpdateAction roda o checker dentro do orçamento updateTimeout, cacheia o
// resultado (a ação "status" o expõe) e sinaliza o restart quando um update
// foi aplicado.
func (s *Server) runUpdateAction(req *Request, apply bool) (*Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	res, err := update.NewService(s.updater(), apply).Run(ctx, req.Channel)
	cancel()
	if err != nil {
		// O único erro conhecido aqui é o checker ausente (dev builds) — a
		// semântica exata de CodeNotConfigured.
		return nil, Err(CodeNotConfigured, err.Error())
	}

	s.mu.Lock()
	s.updateStatus = UpdateStatus{
		CurrentVersion: res.Status.CurrentVersion,
		NewVersion:     res.Status.NewVersion,
		Available:      res.Status.Available,
		Applied:        res.Status.Applied,
		PendingReboot:  res.Status.PendingReboot,
	}
	s.mu.Unlock()

	if res.Applied {
		s.markUpdateApplied()
	}

	resp := &Response{
		Success:         true,
		UpdateAvailable: res.Status.Available,
		UpdateVersion:   res.Status.NewVersion,
		CurrentVersion:  res.Status.CurrentVersion,
	}
	if apply {
		resp.UpdatePendingReboot = res.Status.PendingReboot
	}
	resp.Message = res.Message
	return resp, nil
}
