package ipc

import (
	"context"
	"errors"

	"focusguard/internal/eventhub"
)

// registerHandlers wires the server-level actions into the registry: transport
// health (ping), server state (status), the tamper log and the domain-service
// adapters that must live here (the services cannot import ipc, so the wire
// translation stays in this package). The domain-backed actions (block,
// block-all, apps-*, goal-*, presets, preset-*, user-*, dns-*) are registered
// by the composition root (cmd/focusguard-daemon) with the handlers from the
// domain packages — internal/blocks, internal/dns, internal/goal,
// internal/presets, internal/users, internal/apps.
//
// Regra do strangler: cada handler reproduz 1:1 o case antigo (mensagens,
// códigos e ordem inalterados) — comportamento preservado.
func (s *Server) registerHandlers() {
	s.registry.Register(funcHandler{action: "ping", handle: s.handlePing})
	s.registry.Register(funcHandler{action: "tamper-log", handle: s.handleTamperLog})
	s.registry.Register(funcHandler{action: "stats", handle: s.handleStats})
	s.registry.Register(funcHandler{action: "missions", handle: s.handleMissions})
	s.registry.Register(funcHandler{action: "sessions", handle: s.handleSessions})
	s.registry.Register(funcHandler{action: "schedule-list", handle: s.handleScheduleList})
	s.registry.Register(funcHandler{action: "schedule-add", handle: s.handleScheduleAdd})
	s.registry.Register(funcHandler{action: "schedule-import", handle: s.handleScheduleImport})
	s.registry.Register(funcHandler{action: "schedule-remove", handle: s.handleScheduleRemove})
	s.registry.Register(funcHandler{action: "pomodoro", handle: s.handlePomodoroStart})
	s.registry.Register(funcHandler{action: "pomodoro-defaults", handle: s.handlePomodoroDefaults})
	s.registry.Register(funcHandler{action: "pomodoro-stop", handle: s.handlePomodoroStop})
	s.registry.Register(funcHandler{action: "update", handle: s.handleUpdate})
	s.registry.Register(funcHandler{action: "update-check", handle: s.handleUpdateCheck})
	s.registry.Register(funcHandler{action: "status", handle: s.handleStatus})
	s.registry.Register(funcHandler{action: "event-subscribe", handle: s.handleEventSubscribe})
}

// eventHubProvider é o accessor com lock do hub de eventos (o daemon o
// configura depois de NewServer via SetEventHub).
func (s *Server) eventHubProvider() *eventhub.Hub {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eventHub
}

// handleEventSubscribe implementa o long-poll de eventos (Fase 7): bloqueia
// até um evento com seq > req.Since ser publicado ou o orçamento interno
// (eventSubscribeTimeout) expirar. Timeout vira uma resposta vazia
// bem-sucedida (o ciclo de keepalive que mantém o SSE vivo); hub não
// configurado é um erro estável (CodeNotConfigured).
func (s *Server) handleEventSubscribe(ctx context.Context, req *Request) (*Response, error) {
	hub := s.eventHubProvider()
	if hub == nil {
		return nil, Err(CodeNotConfigured, "eventos não configurados")
	}
	waitCtx, cancel := context.WithTimeout(ctx, eventSubscribeTimeout)
	defer cancel()
	evs, rev, err := hub.Wait(waitCtx, req.Since)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return &Response{Success: true, Rev: rev}, nil
		}
		return nil, err
	}
	wire := make([]Event, 0, len(evs))
	for _, e := range evs {
		wire = append(wire, Event{Type: e.Type, At: e.At})
	}
	return &Response{Success: true, Rev: rev, Events: wire}, nil
}

// tamperProvider is o accessor com lock do provedor de eventos de burla
// (o daemon pode configurá-lo depois de NewServer).
func (s *Server) tamperProvider() TamperProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tamperLog
}

// handlePing responde "pong" se o scheduler responder (sinal de conectividade
// do CLI/tray/web).
func (s *Server) handlePing(_ context.Context, _ *Request) (*Response, error) {
	if err := s.scheduler.Ping(); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: "pong"}, nil
}

// handleTamperLog lista as tentativas de burla detectadas e revertidas.
func (s *Server) handleTamperLog(_ context.Context, _ *Request) (*Response, error) {
	p := s.tamperProvider()
	if p == nil {
		return nil, Err(CodeNotConfigured, "tamper-log não configurado")
	}
	events, err := p.Events()
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, TamperLog: events}, nil
}
