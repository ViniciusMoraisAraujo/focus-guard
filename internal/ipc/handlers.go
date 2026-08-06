package ipc

import (
	"context"
	"fmt"
	"time"

	"focusguard/internal/preset"
)

// registerHandlers wires the migrated actions into the registry (Fase 3). O
// Server mantém os Set* como fonte das dependências — os handlers leem os
// campos sob o mutex no momento do Handle (o daemon pode configurá-los depois
// de NewServer). A Fase 4 move cada handler para o pacote de domínio com
// interfaces estreitas (DIP) e o registro para o composition root.
//
// Regra do estrangler: o corpo de cada handler reproduz 1:1 o case antigo
// (mensagens, códigos e ordem inalterados) — comportamento preservado.
func (s *Server) registerHandlers() {
	s.registry.Register(funcHandler{action: "ping", handle: s.handlePing})
	s.registry.Register(funcHandler{action: "presets", handle: s.handlePresets})
	s.registry.Register(funcHandler{action: "preset-add", handle: s.handlePresetAdd})
	s.registry.Register(funcHandler{action: "preset-remove", handle: s.handlePresetRemove})
	s.registry.Register(funcHandler{action: "apps-list", handle: s.handleAppsList})
	s.registry.Register(funcHandler{action: "apps-add", handle: s.handleAppsAdd})
	s.registry.Register(funcHandler{action: "apps-remove", handle: s.handleAppsRemove})
	s.registry.Register(funcHandler{action: "tamper-log", handle: s.handleTamperLog})
	s.registry.Register(funcHandler{action: "goal-get", handle: s.handleGoalGet})
	s.registry.Register(funcHandler{action: "goal-set", handle: s.handleGoalSet})
}

// appsManager, tamperProvider e goalManager são accessors com lock, espelho do
// padrão catalog() usado pelo resto do servidor.
func (s *Server) appsManager() AppsManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.apps
}

func (s *Server) tamperProvider() TamperProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tamperLog
}

func (s *Server) goalManager() GoalManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.goalStore
}

// handlePing responde "pong" se o scheduler responder (sinal de conectividade
// do CLI/tray/web).
func (s *Server) handlePing(_ context.Context, _ *Request) (*Response, error) {
	if err := s.scheduler.Ping(); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: "pong"}, nil
}

// handlePresets lista o catálogo (built-ins + presets do usuário).
func (s *Server) handlePresets(_ context.Context, _ *Request) (*Response, error) {
	return &Response{Success: true, Presets: s.catalog().List()}, nil
}

// handlePresetAdd cria um preset personalizado (a validação de payload fica no
// store, como no switch antigo).
func (s *Server) handlePresetAdd(_ context.Context, req *Request) (*Response, error) {
	if err := s.catalog().Add(preset.Preset{
		Name:    req.PresetName,
		Label:   req.PresetLabel,
		Domains: req.PresetDomains,
	}); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Preset %s criado (%d domínios)", req.PresetName, len(req.PresetDomains))}, nil
}

// handlePresetRemove remove um preset personalizado.
func (s *Server) handlePresetRemove(_ context.Context, req *Request) (*Response, error) {
	if err := s.catalog().Remove(req.PresetName); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Preset %s removido", req.PresetName)}, nil
}

// handleAppsList lista a denylist de processos.
func (s *Server) handleAppsList(_ context.Context, _ *Request) (*Response, error) {
	am := s.appsManager()
	if am == nil {
		return nil, Err(CodeNotConfigured, "denylist de apps não configurada")
	}
	return &Response{Success: true, Apps: am.List()}, nil
}

// handleAppsAdd adiciona um processo à denylist.
func (s *Server) handleAppsAdd(_ context.Context, req *Request) (*Response, error) {
	am := s.appsManager()
	if am == nil {
		return nil, Err(CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := am.Add(req.AppName); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Processo %s adicionado à denylist", req.AppName)}, nil
}

// handleAppsRemove remove um processo da denylist.
func (s *Server) handleAppsRemove(_ context.Context, req *Request) (*Response, error) {
	am := s.appsManager()
	if am == nil {
		return nil, Err(CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := am.Remove(req.AppName); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Processo %s removido da denylist", req.AppName)}, nil
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

// handleGoalGet devolve a meta diária de foco atual.
func (s *Server) handleGoalGet(_ context.Context, _ *Request) (*Response, error) {
	g := s.goalManager()
	if g == nil {
		return nil, Err(CodeNotConfigured, "meta diária não configurada")
	}
	return &Response{Success: true, Goal: g.Get()}, nil
}

// handleGoalSet define a meta diária de foco. A validação fica no Handle (e
// não no Validate) para preservar a ordem do switch legado: o store não
// configurado é verificado antes do range — comportamento idêntico.
func (s *Server) handleGoalSet(_ context.Context, req *Request) (*Response, error) {
	g := s.goalManager()
	if g == nil {
		return nil, Err(CodeNotConfigured, "meta diária não configurada")
	}
	if req.GoalMinutes <= 0 || req.GoalMinutes > 24*60 {
		return nil, Err(CodeInvalid, "meta inválida (entre 1 e 1440 minutos)")
	}
	d := time.Duration(req.GoalMinutes) * time.Minute
	if err := g.Set(d); err != nil {
		return nil, err
	}
	return &Response{Success: true, Goal: g.Get(), Message: fmt.Sprintf("Meta diária definida: %s", d.Round(time.Minute))}, nil
}
