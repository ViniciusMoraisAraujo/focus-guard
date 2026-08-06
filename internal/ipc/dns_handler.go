package ipc

import (
	"context"
	"fmt"
)

// dnsController returns the wired DNS sinkhole controller under the lock —
// espelho do padrão analyticsProvider() (o daemon pode configurá-lo depois de
// NewServer).
func (s *Server) dnsController() DNSController {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dnsCtrl
}

// ---------------------------------------------------------------------------
// dns-start / dns-stop / dns-status / dns-set-upstream
// ---------------------------------------------------------------------------
//
// O flag persistido e o upstream vivem no scheduler; o estado vivo (listener +
// contadores) no controller — as ações combinam os dois via mergeDNS.

// handleDNSStart sobe o sinkhole e persiste o flag "ligado".
func (s *Server) handleDNSStart(_ context.Context, _ *Request) (*Response, error) {
	c := s.dnsController()
	if c == nil {
		return nil, Err(CodeNotConfigured, "servidor DNS não configurado")
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	// Persiste o flag só depois de o listener subir; se a gravação falhar,
	// desliga o servidor para o estado nunca ficar "ligado mas não
	// persistido" (no próximo boot voltaria desligado).
	if err := s.scheduler.SetDNSEnabled(true); err != nil {
		_ = c.Stop()
		return nil, err
	}
	s.mu.RLock()
	fn := s.onDNSStarted
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
	resp := &Response{Success: true, Message: "Servidor DNS iniciado em " + c.Status().Addr}
	mergeDNS(resp, c.Status(), s.scheduler.DNSEnabled())
	return resp, nil
}

// handleDNSStop desliga o sinkhole e persiste o flag "desligado".
func (s *Server) handleDNSStop(_ context.Context, _ *Request) (*Response, error) {
	c := s.dnsController()
	if c == nil {
		return nil, Err(CodeNotConfigured, "servidor DNS não configurado")
	}
	if err := c.Stop(); err != nil {
		return nil, err
	}
	if err := s.scheduler.SetDNSEnabled(false); err != nil {
		return nil, err
	}
	resp := &Response{Success: true, Message: "Servidor DNS desligado"}
	mergeDNS(resp, c.Status(), s.scheduler.DNSEnabled())
	return resp, nil
}

// handleDNSStatus reporta o estado vivo + persistido do sinkhole.
func (s *Server) handleDNSStatus(_ context.Context, _ *Request) (*Response, error) {
	c := s.dnsController()
	if c == nil {
		return nil, Err(CodeNotConfigured, "servidor DNS não configurado")
	}
	resp := &Response{Success: true}
	mergeDNS(resp, c.Status(), s.scheduler.DNSEnabled())
	return resp, nil
}

// handleDNSSetUpstream troca o resolvedor upstream (persistido no scheduler e
// aplicado no controller vivo, com restart se ligado).
func (s *Server) handleDNSSetUpstream(_ context.Context, req *Request) (*Response, error) {
	c := s.dnsController()
	if c == nil {
		return nil, Err(CodeNotConfigured, "servidor DNS não configurado")
	}
	upstream, err := normalizeUpstream(req.Upstream)
	if err != nil {
		return nil, Err(CodeInvalid, err.Error())
	}
	// Persiste primeiro (espelho em disco), depois aplica no listener vivo
	// (restart se estiver ligado). Um restart que falhe deixa o servidor
	// parado com o erro no dns-status — e o próximo boot usa o valor
	// persistido (mesmo padrão do dns-start com bind ocupado).
	if err := s.scheduler.SetDNSUpstream(upstream); err != nil {
		return nil, err
	}
	if err := c.SetUpstream(upstream); err != nil {
		return nil, err
	}
	resp := &Response{Success: true, Message: fmt.Sprintf("Upstream DNS alterado para %s", upstream)}
	mergeDNS(resp, c.Status(), s.scheduler.DNSEnabled())
	return resp, nil
}
