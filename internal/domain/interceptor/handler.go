// Package interceptor implements the domain service for the "interceptor-set"
// IPC action (Focus Interceptor Page — Fase 3 do features-plan): persisting
// the enabled flag and answering with the current state. The actual HTTP
// listener lifecycle lives in the daemon (internal/infrastructure/interceptor
// + the DNS answer IP); this handler only owns the persisted setting, mirroring
// the dns-* handlers' split between flag (scheduler) and live state (daemon).
package interceptor

import (
	"context"

	"focusguard/internal/domain/ipcerr"
)

// Persister é a superfície do setting persistido (satisfeita pelo
// *scheduler.Scheduler): o flag liga/desliga a página de bloqueio.
type Persister interface {
	SetInterceptorEnabled(enabled bool) error
	InterceptorEnabled() bool
}

// NoInput é a ausência de payload (a consulta não precisa de argumento).
type NoInput struct{}

// SetInput carrega o novo valor do flag.
type SetInput struct {
	Enabled bool
}

// Status é o estado combinado (persistido + resposta da ação).
type Status struct {
	Enabled bool
}

// SetResult é a resposta de interceptor-set.
type SetResult struct {
	Message string
	Status  Status
}

// StatusResult é a resposta da consulta (interceptor-status).
type StatusResult struct{ Status Status }

// SetHandler executa "interceptor-set".
type SetHandler struct {
	persist Persister
	// onChanged fires after the flag was persisted (the daemon uses it to
	// start/stop the HTTP listener and toggle the DNS answer IP). May be nil.
	onChanged func(enabled bool)
}

// NewSet builds the "interceptor-set" handler.
func NewSet(persist Persister, onChanged func(enabled bool)) *SetHandler {
	return &SetHandler{persist: persist, onChanged: onChanged}
}

func (h *SetHandler) Action() string { return "interceptor-set" }

func (h *SetHandler) Validate(*SetInput) error { return nil }

func (h *SetHandler) Handle(_ context.Context, in *SetInput) (*SetResult, error) {
	if h.persist == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "interceptor não configurado")
	}
	if err := h.persist.SetInterceptorEnabled(in.Enabled); err != nil {
		return nil, err
	}
	if h.onChanged != nil {
		h.onChanged(in.Enabled)
	}
	res := &SetResult{Message: interceptorMessage(in.Enabled)}
	merge(&res.Status, h.persist.InterceptorEnabled())
	return res, nil
}

// StatusHandler executa "interceptor-status".
type StatusHandler struct {
	persist Persister
}

// NewStatus builds the "interceptor-status" handler.
func NewStatus(persist Persister) *StatusHandler {
	return &StatusHandler{persist: persist}
}

func (h *StatusHandler) Action() string { return "interceptor-status" }

func (h *StatusHandler) Validate(*NoInput) error { return nil }

func (h *StatusHandler) Handle(_ context.Context, _ *NoInput) (*StatusResult, error) {
	if h.persist == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "interceptor não configurado")
	}
	res := &StatusResult{}
	merge(&res.Status, h.persist.InterceptorEnabled())
	return res, nil
}

// merge projeta o flag persistido no Status da resposta.
func merge(st *Status, enabled bool) {
	st.Enabled = enabled
}

// interceptorMessage descreve o novo estado na resposta de sucesso.
func interceptorMessage(enabled bool) string {
	if enabled {
		return "Página de bloqueio ativada — domínios bloqueados agora mostram o aviso (requer porta 80 livre)"
	}
	return "Página de bloqueio desativada — domínios bloqueados voltam a resolver para endereço morto"
}
