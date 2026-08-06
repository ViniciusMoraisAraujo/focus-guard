package ipc

// ---------------------------------------------------------------------------
// Adapters de REFERÊNCIA das ações de domínio (somente testes).
//
// Fase 5: os handlers reais de block/block-all, apps-*, goal-*, presets,
// preset-*, user-* e dns-* vivem nos pacotes de domínio (internal/blocks,
// internal/dns, internal/goal, internal/presets, internal/users,
// internal/apps) e são registrados pelo composition root
// (cmd/focusguard-daemon) via Server.Register. O pacote ipc NÃO pode
// importá-los (ciclo de import: domínio→ipc), então os testes internos — que
// exercitam o roteador pelo socket (setupTestServer + executeRequest) — usam
// estes adapters de referência, que reproduzem 1:1 o comportamento do switch
// legado. Os testes dos pacotes de domínio cobrem os handlers reais; o teste
// externo domain_wiring_test.go compõe os reais com o roteador.
//
// As dependências de apps/user/dns (denylist, credenciais, hook DoH) NÃO
// vivem mais no Server (SetApps/SetUsers/SetOnDNSStarted foram removidos na
// Fase 5) — chegam via refDeps, espelhando o construtor dos handlers reais de
// domínio no composition root do daemon. As demais (presets, goal, dns
// controller, scheduler) continuam no Server (SetPresets/SetGoal/SetDNS
// permanecem — o status e os adapters de pomodoro/schedule as leem).
// ---------------------------------------------------------------------------

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"focusguard/internal/preset"
)

// refDeps são as dependências dos adapters de referência que o Server não
// carrega mais. s aponta para o servidor que registrou os handlers (o
// dns-start precisa do controller + scheduler).
type refDeps struct {
	s            *Server
	apps         AppsManager
	users        UserManager
	onDNSStarted func()
}

// registerDomainReferenceHandlers registra as ações de domínio com os adapters
// de referência — chamado pelo setupTestServer para o servidor de teste ter o
// mesmo conjunto de ações que o daemon real (34 handlers, specs↔registry
// fechado). deps nil equivale a deps vazias (apps/users/hook desconfigurados).
func registerDomainReferenceHandlers(s *Server, deps *refDeps) {
	if deps == nil {
		deps = &refDeps{}
	}
	if deps.s == nil {
		deps.s = s
	}
	s.registry.Register(funcHandler{action: "presets", handle: s.handlePresets})
	s.registry.Register(funcHandler{action: "preset-add", handle: s.handlePresetAdd})
	s.registry.Register(funcHandler{action: "preset-remove", handle: s.handlePresetRemove})
	s.registry.Register(funcHandler{action: "apps-list", handle: deps.handleAppsList})
	s.registry.Register(funcHandler{action: "apps-add", handle: deps.handleAppsAdd})
	s.registry.Register(funcHandler{action: "apps-remove", handle: deps.handleAppsRemove})
	s.registry.Register(funcHandler{action: "goal-get", handle: s.handleGoalGet})
	s.registry.Register(funcHandler{action: "goal-set", handle: s.handleGoalSet})
	s.registry.Register(funcHandler{action: "block", handle: s.handleBlock})
	s.registry.Register(funcHandler{action: "block-all", handle: s.handleBlockAll})
	s.registry.Register(funcHandler{action: "user-list", handle: deps.handleUserList})
	s.registry.Register(funcHandler{action: "user-verify", handle: deps.handleUserVerify})
	s.registry.Register(funcHandler{action: "user-add", handle: deps.handleUserAdd})
	s.registry.Register(funcHandler{action: "user-remove", handle: deps.handleUserRemove})
	s.registry.Register(funcHandler{action: "user-set-password", handle: deps.handleUserSetPassword})
	s.registry.Register(funcHandler{action: "dns-start", handle: deps.handleDNSStart})
	s.registry.Register(funcHandler{action: "dns-stop", handle: s.handleDNSStop})
	s.registry.Register(funcHandler{action: "dns-status", handle: s.handleDNSStatus})
	s.registry.Register(funcHandler{action: "dns-set-upstream", handle: s.handleDNSSetUpstream})
}

// ---------------------------------------------------------------------------
// presets / preset-add / preset-remove
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// apps-*
// ---------------------------------------------------------------------------

// handleAppsList lista a denylist de processos.
func (d *refDeps) handleAppsList(_ context.Context, _ *Request) (*Response, error) {
	am := d.apps
	if am == nil {
		return nil, Err(CodeNotConfigured, "denylist de apps não configurada")
	}
	return &Response{Success: true, Apps: am.List()}, nil
}

// handleAppsAdd adiciona um processo à denylist.
func (d *refDeps) handleAppsAdd(_ context.Context, req *Request) (*Response, error) {
	am := d.apps
	if am == nil {
		return nil, Err(CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := am.Add(req.AppName); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Processo %s adicionado à denylist", req.AppName)}, nil
}

// handleAppsRemove remove um processo da denylist.
func (d *refDeps) handleAppsRemove(_ context.Context, req *Request) (*Response, error) {
	am := d.apps
	if am == nil {
		return nil, Err(CodeNotConfigured, "denylist de apps não configurada")
	}
	if err := am.Remove(req.AppName); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Processo %s removido da denylist", req.AppName)}, nil
}

// ---------------------------------------------------------------------------
// goal-get / goal-set
// ---------------------------------------------------------------------------

// goalManager devolve a meta diária configurada (com lock), espelho do padrão
// catalog() — usada apenas pelos adapters de referência abaixo (os handlers
// reais de domínio recebem as dependências por construtor).
func (s *Server) goalManager() GoalManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.goalStore
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

// ---------------------------------------------------------------------------
// block / block-all
// ---------------------------------------------------------------------------

// handleBlock bloqueia um domínio (ou um preset inteiro) por um período.
func (s *Server) handleBlock(_ context.Context, req *Request) (*Response, error) {
	d, err := time.ParseDuration(req.Duration)
	// d <= 0 também é rejeitado: um bloqueio de 0s expiraria imediatamente
	// (bloqueio sem efeito que ainda aplica/remove regras de firewall).
	if err != nil || d <= 0 {
		return nil, Err(CodeDurationInvalid, "Duration invalid. Ex: --duration 4h, 30m")
	}
	if req.Preset != "" {
		p, perr := s.catalog().Resolve(req.Preset)
		if perr != nil {
			return nil, perr
		}
		blocks, berr := s.scheduler.BlockDomains(p.Domains, d)
		if berr != nil {
			return nil, berr
		}
		if len(blocks) == 0 {
			// Defensivo: nunca indexar blocks[0] — evita pânico no servidor se
			// o scheduler um dia retornar sucesso sem blocos.
			return &Response{Success: true, Message: fmt.Sprintf("Preset %s: nenhum domínio novo bloqueado", p.Name)}, nil
		}
		return &Response{
			Success: true,
			Message: fmt.Sprintf("Preset %s bloqueado (%d domínios) até %s", p.Name, len(blocks), blocks[0].ExpiresAt.Local().Format("15:04:05 02/01/2006")),
		}, nil
	}
	if req.Domain == "" {
		return nil, Err(CodeDomainRequired, "Informe um domínio ou --preset para bloquear.")
	}
	// --extend: soma a duração ao bloqueio ativo (ou cria um novo se não
	// houver). Não passa pela detecção de conflito.
	if req.Extend {
		block, err := s.scheduler.ExtendBlock(req.Domain, d)
		if err != nil {
			return nil, err
		}
		return &Response{Success: true, Message: fmt.Sprintf("Domain %s extended until %s", block.Domain, block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
	}
	// Comportamento padrão (ask-first): um domínio já bloqueado é um
	// CONFLITO a ser resolvido pelo usuário (somar/substituir), não um
	// sobrescrita silenciosa. --replace pula o conflito e reinicia a janela.
	if !req.Replace {
		if existing := s.scheduler.ActiveBlock(req.Domain); existing != nil {
			return &Response{
				Success:       false,
				Code:          CodeDomainConflict,
				Conflict:      true,
				ConflictBlock: existing,
				Message: fmt.Sprintf("Domínio já bloqueado até %s. Use --extend para somar ou --replace para reiniciar.",
					existing.ExpiresAt.Local().Format("15:04:05 02/01/2006")),
			}, nil
		}
	}
	block, err := s.scheduler.Block(req.Domain, d)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Domain %s blocked  %s", block.Domain, block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}

// handleBlockAll bloqueia toda a internet (panic mode) ou tudo exceto a
// allowlist (deep-focus mode).
func (s *Server) handleBlockAll(_ context.Context, req *Request) (*Response, error) {
	d, err := time.ParseDuration(req.Duration)
	if err != nil || d <= 0 {
		return nil, Err(CodeDurationInvalid, "Duration invalid. Ex: --duration 4h, 30m")
	}
	block, err := s.scheduler.BlockAllInternet(req.Allowlist, d)
	if err != nil {
		return nil, err
	}
	return &Response{
		Success: true,
		Message: fmt.Sprintf("Internet bloqueada até %s%s", block.ExpiresAt.Local().Format("15:04:05 02/01/2006"), blockAllModeSuffix(req.Allowlist)),
	}, nil
}

// blockAllModeSuffix descreve a variante do block-all na mensagem de sucesso:
// modo pânico (internet toda) vs deep-focus (só a allowlist acessível).
func blockAllModeSuffix(allowlist []string) string {
	if len(allowlist) == 0 {
		return " (toda a internet)"
	}
	return fmt.Sprintf(" (apenas %s permitido)", strings.Join(allowlist, ", "))
}

// ---------------------------------------------------------------------------
// dns-*
// ---------------------------------------------------------------------------

// dnsController returns the wired DNS sinkhole controller under the lock
// (o daemon pode configurá-lo depois de NewServer).
func (s *Server) dnsController() DNSController {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dnsCtrl
}

// handleDNSStart sobe o sinkhole e persiste o flag "ligado". O hook
// onDNSStarted (bloqueio DoH do daemon) vem das refDeps — o daemon o injeta
// no handler real (dns.NewStart) por construtor.
func (d *refDeps) handleDNSStart(_ context.Context, _ *Request) (*Response, error) {
	c := d.s.dnsController()
	if c == nil {
		return nil, Err(CodeNotConfigured, "servidor DNS não configurado")
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	// Persiste o flag só depois de o listener subir; se a gravação falhar,
	// desliga o servidor para o estado nunca ficar "ligado mas não
	// persistido" (no próximo boot voltaria desligado).
	if err := d.s.scheduler.SetDNSEnabled(true); err != nil {
		_ = c.Stop()
		return nil, err
	}
	if fn := d.onDNSStarted; fn != nil {
		fn()
	}
	resp := &Response{Success: true, Message: "Servidor DNS iniciado em " + c.Status().Addr}
	mergeDNS(resp, c.Status(), d.s.scheduler.DNSEnabled())
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

// normalizeUpstream valida um upstream fornecido pelo usuário e o devolve em
// host:porta (um host puro ganha a porta padrão do DNS, 53). Entrada vazia é
// rejeitada — o chamador pode sempre passar um resolvedor concreto.
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

// ---------------------------------------------------------------------------
// user-*
// ---------------------------------------------------------------------------

// handleUserList lista os usuários cadastrados.
func (d *refDeps) handleUserList(_ context.Context, _ *Request) (*Response, error) {
	m := d.users
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	return &Response{Success: true, Users: m.List()}, nil
}

// handleUserVerify valida credenciais (web-only: sem ActionSpec, isento do
// fechamento specs↔registry — o proxy web nunca o encaminha, evitando um
// oracle de senha sem o rate limit do login).
func (d *refDeps) handleUserVerify(_ context.Context, req *Request) (*Response, error) {
	m := d.users
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
func (d *refDeps) handleUserAdd(_ context.Context, req *Request) (*Response, error) {
	m := d.users
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	if err := m.Add(req.UserName, req.UserPassword); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Usuário %s criado", req.UserName)}, nil
}

// handleUserRemove remove um usuário.
func (d *refDeps) handleUserRemove(_ context.Context, req *Request) (*Response, error) {
	m := d.users
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	if err := m.Remove(req.UserName); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Usuário %s removido", req.UserName)}, nil
}

// handleUserSetPassword altera a senha de um usuário.
func (d *refDeps) handleUserSetPassword(_ context.Context, req *Request) (*Response, error) {
	m := d.users
	if m == nil {
		return nil, Err(CodeNotConfigured, "usuários não configurados")
	}
	if err := m.SetPassword(req.UserName, req.UserPassword); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Senha de %s atualizada", req.UserName)}, nil
}
