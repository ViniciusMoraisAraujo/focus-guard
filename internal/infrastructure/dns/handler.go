// Package dns implements the domain service for the dns-* IPC actions
// (DNS sinkhole lifecycle — Fase 4 do refactor-plan). Handlers combine the
// live *dnsserver.Controller state with the persisted enabled flag/upstream,
// which live in the scheduler — the two sources of truth the switch already
// merged for status. Handlers use package-local types; the transport adapts
// them via ipc.DomainAction (pós-reorg item 2).
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/infrastructure/dnsserver"
)

// Controller drives the DNS sinkhole server lifecycle.
type Controller interface {
	Start() error
	Stop() error
	SetUpstream(upstream string) error
	Status() dnsserver.Status
}

// Persister is the persisted DNS setting surface (satisfied by
// *scheduler.Scheduler): the enabled flag and the upstream live on disk, not
// in the controller.
type Persister interface {
	SetDNSEnabled(enabled bool) error
	SetDNSUpstream(upstream string) error
	DNSEnabled() bool
}

// Tipos de entrada/saída das ações — adaptados pelo transporte (DIP).
type NoInput struct{}

// Status agrega o estado vivo do controller + o flag persistido — o que o
// wire Response.DNS* expõe.
type Status struct {
	Enabled   bool
	Listening bool
	Addr      string
	Upstream  string
	Queries   uint64
	Blocked   uint64
	BindError string
}

type StartResult struct {
	Message string
	Status  Status
}
type StopResult struct {
	Message string
	Status  Status
}
type StatusResult struct{ Status Status }
type SetUpstreamInput struct{ Upstream string }
type SetUpstreamResult struct {
	Message string
	Status  Status
}

// mergeDNS copies the live DNS controller state into the result Status
// together with the persisted enabled flag (which lives in the scheduler).
func mergeDNS(st *Status, s dnsserver.Status, enabled bool) {
	st.Enabled = enabled
	st.Listening = s.Listening
	st.Addr = s.Addr
	st.Upstream = s.Upstream
	st.Queries = s.Queries
	st.Blocked = s.Blocked
	st.BindError = s.BindError
}

// ---------------------------------------------------------------------------
// dns-start
// ---------------------------------------------------------------------------

// StartHandler executes "dns-start". Persists the enabled flag only after the
// listener is up; if the write fails the server is stopped so the state never
// stays "ligado mas não persistido".
type StartHandler struct {
	ctrl    Controller
	persist Persister
	// onStarted fires after the flag was persisted (the daemon uses it to
	// apply the DoH firewall block). May be nil.
	onStarted func()
}

// NewStart builds the "dns-start" handler.
func NewStart(ctrl Controller, persist Persister, onStarted func()) *StartHandler {
	return &StartHandler{ctrl: ctrl, persist: persist, onStarted: onStarted}
}

func (h *StartHandler) Action() string { return "dns-start" }

func (h *StartHandler) Validate(*NoInput) error { return nil }

func (h *StartHandler) Handle(ctx context.Context, _ *NoInput) (*StartResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}
	if err := h.ctrl.Start(); err != nil {
		return nil, err
	}
	// Persiste o flag só depois de o listener subir; se a gravação falhar,
	// desliga o servidor para o estado nunca ficar "ligado mas não
	// persistido" (no próximo boot voltaria desligado).
	if err := h.persist.SetDNSEnabled(true); err != nil {
		_ = h.ctrl.Stop()
		return nil, err
	}
	if h.onStarted != nil {
		h.onStarted()
	}
	res := &StartResult{Message: "Servidor DNS iniciado em " + h.ctrl.Status().Addr}
	mergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled())
	return res, nil
}

// ---------------------------------------------------------------------------
// dns-stop
// ---------------------------------------------------------------------------

// StopHandler executes "dns-stop".
type StopHandler struct {
	ctrl    Controller
	persist Persister
}

// NewStop builds the "dns-stop" handler.
func NewStop(ctrl Controller, persist Persister) *StopHandler {
	return &StopHandler{ctrl: ctrl, persist: persist}
}

func (h *StopHandler) Action() string { return "dns-stop" }

func (h *StopHandler) Validate(*NoInput) error { return nil }

func (h *StopHandler) Handle(ctx context.Context, _ *NoInput) (*StopResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}
	if err := h.ctrl.Stop(); err != nil {
		return nil, err
	}
	if err := h.persist.SetDNSEnabled(false); err != nil {
		return nil, err
	}
	res := &StopResult{Message: "Servidor DNS desligado"}
	mergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled())
	return res, nil
}

// ---------------------------------------------------------------------------
// dns-status
// ---------------------------------------------------------------------------

// StatusHandler executes "dns-status".
type StatusHandler struct {
	ctrl    Controller
	persist Persister
}

// NewStatus builds the "dns-status" handler.
func NewStatus(ctrl Controller, persist Persister) *StatusHandler {
	return &StatusHandler{ctrl: ctrl, persist: persist}
}

func (h *StatusHandler) Action() string { return "dns-status" }

func (h *StatusHandler) Validate(*NoInput) error { return nil }

func (h *StatusHandler) Handle(ctx context.Context, _ *NoInput) (*StatusResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}
	res := &StatusResult{}
	mergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled())
	return res, nil
}

// ---------------------------------------------------------------------------
// dns-set-upstream
// ---------------------------------------------------------------------------

// SetUpstreamHandler executes "dns-set-upstream". Persiste primeiro (espelho
// em disco), depois aplica no listener vivo (restart se estiver ligado) — um
// restart que falhe deixa o servidor parado com o erro no dns-status e o
// próximo boot usa o valor persistido.
type SetUpstreamHandler struct {
	ctrl    Controller
	persist Persister
}

// NewSetUpstream builds the "dns-set-upstream" handler.
func NewSetUpstream(ctrl Controller, persist Persister) *SetUpstreamHandler {
	return &SetUpstreamHandler{ctrl: ctrl, persist: persist}
}

func (h *SetUpstreamHandler) Action() string { return "dns-set-upstream" }

func (h *SetUpstreamHandler) Validate(*SetUpstreamInput) error { return nil }

func (h *SetUpstreamHandler) Handle(ctx context.Context, req *SetUpstreamInput) (*SetUpstreamResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}
	upstream, err := normalizeUpstream(req.Upstream)
	if err != nil {
		return nil, ipcerr.New(ipcerr.CodeInvalid, err.Error())
	}
	if err := h.persist.SetDNSUpstream(upstream); err != nil {
		return nil, err
	}
	if err := h.ctrl.SetUpstream(upstream); err != nil {
		return nil, err
	}
	res := &SetUpstreamResult{Message: fmt.Sprintf("Upstream DNS alterado para %s", upstream)}
	mergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled())
	return res, nil
}

// normalizeUpstream validates a user-supplied upstream resolver and returns it
// in host:port form (a bare host gets the DNS default port 53). Empty input is
// rejected — the caller can always pass a concrete resolver explicitly.
func normalizeUpstream(in string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", errors.New("informe um upstream (ex: 1.1.1.2, 9.9.9.9:53)")
	}
	host, port, err := net.SplitHostPort(in)
	if err != nil {
		// Sem porta explícita (ex: "1.1.1.2", "dns.google") → porta 53.
		if !strings.Contains(in, ":") {
			return net.JoinHostPort(in, "53"), nil
		}
		return "", fmt.Errorf("upstream inválido %q (use host ou host:porta)", in)
	}
	if host == "" || port == "" {
		return "", fmt.Errorf("upstream inválido %q (use host ou host:porta)", in)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("porta de upstream inválida %q", port)
	}
	return net.JoinHostPort(host, port), nil
}
