package ipc

import (
	"context"

	"focusguard/internal/domain/pomodoro"
)

// pomodoroRunner returns the configured pomodoro runner under the lock —
// espelho do padrão analyticsProvider()/catalog() (o daemon pode configurá-lo
// depois de NewServer).
func (s *Server) pomodoroRunner() PomodoroRunner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pomodoro
}

// pomodoroPrefsStore returns the configured pomodoro prefs under the lock
// (nome com sufixo _Store porque o campo do Server já é s.pomodoroPrefs).
func (s *Server) pomodoroPrefsStore() PomodoroPrefs {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pomodoroPrefs
}

// pomodoroService builds the domain service per request, reading the wired
// runner/prefs/catalog — NewServer stays un-wired friendly (o serviço devolve
// os mesmos erros "não configurado" do switch legado).
func (s *Server) pomodoroService() *pomodoro.Service {
	return pomodoro.NewService(s.pomodoroRunner(), s.pomodoroPrefsStore(), s.catalog())
}

// ---------------------------------------------------------------------------
// pomodoro / pomodoro-defaults / pomodoro-stop
// ---------------------------------------------------------------------------
//
// As três ações delegam ao serviço de domínio pomodoro.Service (Fase 4). O
// Service não pode importar ipc (ipc importa pomodoro para os tipos do wire —
// Response embute pomodoro.State), então o adapter fica aqui: extrai os campos
// do Request, chama o serviço e monta o Response. Erros estáveis chegam como
// *ipcerr.Error e o writeError os traduz.

// handlePomodoroStart valida a requisição, resolve o preset para domínios e
// entrega a sessão ao runner, mesclando os parâmetros com os padrões salvos.
func (s *Server) handlePomodoroStart(ctx context.Context, req *Request) (*Response, error) {
	res, err := s.pomodoroService().Start(ctx, req.Preset, req.Label, req.WorkMin, req.RestMin, req.Cycles, req.Strict, req.Save)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: res.Message, Pomodoro: &res.State}, nil
}

// handlePomodoroDefaults devolve os padrões atuais de trabalho/descanso/ciclos.
func (s *Server) handlePomodoroDefaults(ctx context.Context, _ *Request) (*Response, error) {
	res, err := s.pomodoroService().Defaults(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{
		Success:       true,
		PomodoroWork:  res.Work,
		PomodoroRest:  res.Rest,
		PomodoroCycle: res.Cycles,
		Message:       res.Message,
	}, nil
}

// handlePomodoroStop encerra a sessão ativa.
func (s *Server) handlePomodoroStop(ctx context.Context, _ *Request) (*Response, error) {
	res, err := s.pomodoroService().Stop(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: res.Message, Pomodoro: &res.State}, nil
}
