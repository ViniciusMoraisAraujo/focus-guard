package dns

import (
	"context"
	"fmt"
	"log"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/infrastructure/netdns"
)

// ---------------------------------------------------------------------------
// dns-start
// ---------------------------------------------------------------------------

// StartHandler executes "dns-start". It starts the port-53 listener, configures
// system network adapters to point to the local DNS sinkhole, applies firewall
// rules, and persists the enabled flag.
type StartHandler struct {
	ctrl      Controller
	persist   Persister
	adapter   AdapterConfigurator
	onStarted func()
}

// NewStart builds the "dns-start" handler.
func NewStart(ctrl Controller, persist Persister, adapter AdapterConfigurator, onStarted func()) *StartHandler {
	return &StartHandler{
		ctrl:      ctrl,
		persist:   persist,
		adapter:   adapter,
		onStarted: onStarted,
	}
}

func (h *StartHandler) Action() string { return "dns-start" }

func (h *StartHandler) Validate(*NoInput) error { return nil }

func (h *StartHandler) Handle(ctx context.Context, _ *NoInput) (*StartResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}

	// 1. Inicia o listener DNS na porta 53
	if err := h.ctrl.Start(); err != nil {
		return nil, err
	}

	// 2. Conecta os adaptadores de rede (Wi-Fi/Ethernet) ao DNS local
	var configuredAdapters []string
	if h.adapter != nil {
		adapters, err := h.adapter.SetLocalDNS()
		if err != nil {
			log.Printf("[FocusGuard DNS] aviso: falha ao configurar adaptadores de rede automaticamente: %v", err)
		} else {
			configuredAdapters = adapters
			log.Printf("[FocusGuard DNS] adaptadores de rede conectados ao DNS local: %v", adapters)
		}
	}

	// 3. Persiste o flag ligado em disco
	if err := h.persist.SetDNSEnabled(true); err != nil {
		_ = h.ctrl.Stop()
		if h.adapter != nil {
			_, _ = h.adapter.RestoreDNS()
		}
		return nil, err
	}

	// 4. Executa hook pós-inicialização (firewall inbound, DoH block, flush DNS)
	if h.onStarted != nil {
		h.onStarted()
	}

	msg := "Servidor DNS iniciado em " + h.ctrl.Status().Addr
	if len(configuredAdapters) > 0 {
		msg += fmt.Sprintf(" (adaptadores conectados: %s)", fmt.Sprintf("%v", configuredAdapters))
	}

	res := &StartResult{Message: msg}
	var adapterSt *netdns.AdapterStatus
	if h.adapter != nil {
		st := h.adapter.Status()
		adapterSt = &st
	}
	MergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled(), adapterSt)
	return res, nil
}

// ---------------------------------------------------------------------------
// dns-stop
// ---------------------------------------------------------------------------

// StopHandler executes "dns-stop". It releases the port-53 listener, restores
// network adapters DNS to DHCP/automatic, and persists the disabled state.
type StopHandler struct {
	ctrl    Controller
	persist Persister
	adapter AdapterConfigurator
}

// NewStop builds the "dns-stop" handler.
func NewStop(ctrl Controller, persist Persister, adapter AdapterConfigurator) *StopHandler {
	return &StopHandler{
		ctrl:    ctrl,
		persist: persist,
		adapter: adapter,
	}
}

func (h *StopHandler) Action() string { return "dns-stop" }

func (h *StopHandler) Validate(*NoInput) error { return nil }

func (h *StopHandler) Handle(ctx context.Context, _ *NoInput) (*StopResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}

	// 1. Para o listener DNS
	if err := h.ctrl.Stop(); err != nil {
		return nil, err
	}

	// 2. Restaura os adaptadores de rede para DHCP
	if h.adapter != nil {
		if restored, err := h.adapter.RestoreDNS(); err != nil {
			log.Printf("[FocusGuard DNS] aviso: falha ao restaurar adaptadores de rede: %v", err)
		} else if len(restored) > 0 {
			log.Printf("[FocusGuard DNS] adaptadores de rede restaurados para DHCP: %v", restored)
		}
	}

	// 3. Persiste o flag desligado
	if err := h.persist.SetDNSEnabled(false); err != nil {
		return nil, err
	}

	res := &StopResult{Message: "Servidor DNS desligado e adaptadores de rede restaurados"}
	var adapterSt *netdns.AdapterStatus
	if h.adapter != nil {
		st := h.adapter.Status()
		adapterSt = &st
	}
	MergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled(), adapterSt)
	return res, nil
}

// ---------------------------------------------------------------------------
// dns-status
// ---------------------------------------------------------------------------

// StatusHandler executes "dns-status".
type StatusHandler struct {
	ctrl    Controller
	persist Persister
	adapter AdapterConfigurator
}

// NewStatus builds the "dns-status" handler.
func NewStatus(ctrl Controller, persist Persister, adapter AdapterConfigurator) *StatusHandler {
	return &StatusHandler{
		ctrl:    ctrl,
		persist: persist,
		adapter: adapter,
	}
}

func (h *StatusHandler) Action() string { return "dns-status" }

func (h *StatusHandler) Validate(*NoInput) error { return nil }

func (h *StatusHandler) Handle(ctx context.Context, _ *NoInput) (*StatusResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}

	res := &StatusResult{}
	var adapterSt *netdns.AdapterStatus
	if h.adapter != nil {
		st := h.adapter.Status()
		adapterSt = &st
	}
	MergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled(), adapterSt)
	return res, nil
}

// ---------------------------------------------------------------------------
// dns-set-upstream
// ---------------------------------------------------------------------------

// SetUpstreamHandler executes "dns-set-upstream".
type SetUpstreamHandler struct {
	ctrl    Controller
	persist Persister
	adapter AdapterConfigurator
}

// NewSetUpstream builds the "dns-set-upstream" handler.
func NewSetUpstream(ctrl Controller, persist Persister, adapter AdapterConfigurator) *SetUpstreamHandler {
	return &SetUpstreamHandler{
		ctrl:    ctrl,
		persist: persist,
		adapter: adapter,
	}
}

func (h *SetUpstreamHandler) Action() string { return "dns-set-upstream" }

func (h *SetUpstreamHandler) Validate(*SetUpstreamInput) error { return nil }

func (h *SetUpstreamHandler) Handle(ctx context.Context, req *SetUpstreamInput) (*SetUpstreamResult, error) {
	if h.ctrl == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "servidor DNS não configurado")
	}

	upstream, err := NormalizeUpstream(req.Upstream)
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
	var adapterSt *netdns.AdapterStatus
	if h.adapter != nil {
		st := h.adapter.Status()
		adapterSt = &st
	}
	MergeDNS(&res.Status, h.ctrl.Status(), h.persist.DNSEnabled(), adapterSt)
	return res, nil
}
