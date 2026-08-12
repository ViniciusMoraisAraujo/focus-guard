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
// As dependências de apps/user/dns/analytics/schedules/pomodoroPrefs NÃO
// vivem mais no Server (SetApps/SetUsers/SetOnDNSStarted removidos na Fase 5;
// SetAnalytics/SetSchedules/SetPomodoroPrefs removidos no item 1 do pós-reorg)
// — chegam via refDeps, espelhando o construtor dos handlers reais de domínio
// no composition root do daemon. As demais (presets, goal, dns controller,
// scheduler, pomodoro runner, update checker) continuam no Server
// (SetPresets/SetGoal/SetDNS/SetPomodoro/SetUpdateChecker permanecem — o
// status e os adapters de pomodoro/schedule as leem).
// ---------------------------------------------------------------------------

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"focusguard/internal/domain/achievements"
	"focusguard/internal/domain/analytics"
	"focusguard/internal/domain/devices"
	"focusguard/internal/domain/pomodoro"
	"focusguard/internal/domain/preset"
	"focusguard/internal/domain/reports"
	"focusguard/internal/domain/schedule"
	"focusguard/internal/domain/telemetry"
	"focusguard/internal/infrastructure/update"
)

// refDeps são as dependências dos adapters de referência que o Server não
// carrega mais. s aponta para o servidor que registrou os handlers (o
// dns-start precisa do controller + scheduler).
type refDeps struct {
	s             *Server
	apps          AppsManager
	users         UserManager
	onDNSStarted  func()
	analytics     AnalyticsProvider
	schedules     ScheduleManager
	pomodoroPrefs PomodoroPrefs
	telemetry     TelemetryQuerier
	interceptor   InterceptorPersister
	devices       DevicesService
	reports       ReportsConfigStore
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
	s.registry.Register(funcHandler{action: "dns-telemetry", handle: deps.handleDNSTelemetry})
	s.registry.Register(funcHandler{action: "interceptor-set", handle: deps.handleInterceptorSet})
	s.registry.Register(funcHandler{action: "interceptor-status", handle: deps.handleInterceptorStatus})
	s.registry.Register(funcHandler{action: "devices-list", handle: deps.handleDevicesList})
	s.registry.Register(funcHandler{action: "devices-upsert", handle: deps.handleDevicesUpsert})
	s.registry.Register(funcHandler{action: "devices-remove", handle: deps.handleDevicesRemove})
	s.registry.Register(funcHandler{action: "reports-config-get", handle: deps.handleReportConfigGet})
	s.registry.Register(funcHandler{action: "reports-config-set", handle: deps.handleReportConfigSet})
	s.registry.Register(funcHandler{action: "reports-generate", handle: deps.handleReportGenerate})
	s.registry.Register(funcHandler{action: "achievements-get", handle: deps.handleAchievements})
	// Serviços (pós-reorg item 1): os handlers reais vivem nos domínios
	// (analytics/pomodoro/schedule/update) e o composition root os registra via
	// ipc.DomainAction; aqui os testes internos usam os adapters de referência
	// abaixo (mesmo padrão dos demais), que reproduzem 1:1 o legado.
	s.registry.Register(funcHandler{action: "stats", handle: deps.handleStats})
	s.registry.Register(funcHandler{action: "missions", handle: deps.handleMissions})
	s.registry.Register(funcHandler{action: "sessions", handle: deps.handleSessions})
	s.registry.Register(funcHandler{action: "schedule-list", handle: deps.handleScheduleList})
	s.registry.Register(funcHandler{action: "schedule-add", handle: deps.handleScheduleAdd})
	s.registry.Register(funcHandler{action: "schedule-import", handle: deps.handleScheduleImport})
	s.registry.Register(funcHandler{action: "schedule-remove", handle: deps.handleScheduleRemove})
	s.registry.Register(funcHandler{action: "pomodoro", handle: deps.handlePomodoroStart})
	s.registry.Register(funcHandler{action: "pomodoro-defaults", handle: deps.handlePomodoroDefaults})
	s.registry.Register(funcHandler{action: "pomodoro-stop", handle: deps.handlePomodoroStop})
	s.registry.Register(funcHandler{action: "update", handle: s.handleUpdate})
	s.registry.Register(funcHandler{action: "update-check", handle: s.handleUpdateCheck})
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
// dns-telemetry (adapter de referência — Fase 1.2 do features-plan)
// ---------------------------------------------------------------------------

// TelemetryQuerier é a superfície de leitura do telemetria (satisfeita por
// *telemetry.Recorder). O ipc não pode importar o domínio (ciclo), então o
// adapter de referência usa a interface — espelho do handler real de domínio.
type TelemetryQuerier interface {
	Queries() ([]telemetry.BlockedQuery, error)
}

// handleDNSTelemetry lista as queries bloqueadas recentes + resumo agregado.
func (d *refDeps) handleDNSTelemetry(_ context.Context, req *Request) (*Response, error) {
	q := d.telemetry
	if q == nil {
		return nil, Err(CodeNotConfigured, "telemetria não configurada")
	}
	qs, err := q.Queries()
	if err != nil {
		return nil, err
	}
	limit := req.TelemetryLimit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return &Response{
		Success:          true,
		TelemetryEntries: telemetry.Recent(qs, limit),
		TelemetrySummary: telemetry.Summarize(qs),
		TelemetryTotal:   len(qs),
		TelemetryLimit:   limit,
	}, nil
}

// ---------------------------------------------------------------------------
// interceptor-set / interceptor-status (adapters de referência — Fase 3)
// ---------------------------------------------------------------------------

// InterceptorPersister é a superfície do flag persistido da Focus Interceptor
// Page (satisfeita pelo *scheduler.Scheduler; o ipc não pode importá-lo).
type InterceptorPersister interface {
	SetInterceptorEnabled(enabled bool) error
	InterceptorEnabled() bool
}

// handleInterceptorSet persiste o flag e responde com o novo estado.
func (d *refDeps) handleInterceptorSet(_ context.Context, req *Request) (*Response, error) {
	p := d.interceptor
	if p == nil {
		return nil, Err(CodeNotConfigured, "interceptor não configurado")
	}
	if err := p.SetInterceptorEnabled(req.InterceptorEnabled); err != nil {
		return nil, err
	}
	resp := &Response{Success: true, InterceptorEnabled: p.InterceptorEnabled()}
	if req.InterceptorEnabled {
		resp.Message = "Página de bloqueio ativada — domínios bloqueados agora mostram o aviso (requer porta 80 livre)"
	} else {
		resp.Message = "Página de bloqueio desativada — domínios bloqueados voltam a resolver para endereço morto"
	}
	return resp, nil
}

// handleInterceptorStatus reporta o flag persistido.
func (d *refDeps) handleInterceptorStatus(_ context.Context, _ *Request) (*Response, error) {
	p := d.interceptor
	if p == nil {
		return nil, Err(CodeNotConfigured, "interceptor não configurado")
	}
	return &Response{Success: true, InterceptorEnabled: p.InterceptorEnabled()}, nil
}

// ---------------------------------------------------------------------------
// devices-list / devices-upsert / devices-remove (adapters de referência —
// Fase 4, edição Server)
// ---------------------------------------------------------------------------

// DevicesService é a superfície do catálogo de dispositivos (satisfeita pelo
// *devices.Store; o ipc não pode importá-lo diretamente nos adapters).
type DevicesService interface {
	List() []devices.Device
	Get(ip string) (devices.Device, bool)
	Upsert(d devices.Device) error
	Remove(ip string) error
}

// handleDevicesList lista o catálogo de dispositivos.
func (d *refDeps) handleDevicesList(_ context.Context, _ *Request) (*Response, error) {
	svc := d.devices
	if svc == nil {
		return nil, Err(CodeNotConfigured, "catálogo de dispositivos não configurado")
	}
	return &Response{Success: true, Devices: svc.List()}, nil
}

// handleDevicesUpsert cria/atualiza a política de um dispositivo.
func (d *refDeps) handleDevicesUpsert(_ context.Context, req *Request) (*Response, error) {
	svc := d.devices
	if svc == nil {
		return nil, Err(CodeNotConfigured, "catálogo de dispositivos não configurado")
	}
	if req.Device == nil {
		return nil, Err(CodeInvalid, "dispositivo ausente na requisição")
	}
	if err := svc.Upsert(*req.Device); err != nil {
		return nil, err
	}
	label := req.Device.Name
	if label == "" {
		label = req.Device.IP
	}
	return &Response{Success: true, Message: fmt.Sprintf("Política de %s atualizada", label)}, nil
}

// handleDevicesRemove remove a política de um dispositivo.
func (d *refDeps) handleDevicesRemove(_ context.Context, req *Request) (*Response, error) {
	svc := d.devices
	if svc == nil {
		return nil, Err(CodeNotConfigured, "catálogo de dispositivos não configurado")
	}
	if err := svc.Remove(req.DeviceIP); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Dispositivo %s removido", req.DeviceIP)}, nil
}

// ---------------------------------------------------------------------------
// reports-config-get / reports-config-set / reports-generate (adapters de
// referência — Fase 5.1, relatório semanal automático)
// ---------------------------------------------------------------------------

// ReportsConfigStore é a superfície da config persistida (satisfeita por
// *reports.Store).
type ReportsConfigStore interface {
	Get() reports.Config
	Set(reports.Config) error
}

// ReportsProvider fornece as sessões para a geração (satisfeito por
// *analytics.Recorder).
type ReportsProvider interface {
	Sessions() ([]analytics.Session, error)
}

// handleReportConfigGet devolve o agendamento atual.
func (d *refDeps) handleReportConfigGet(_ context.Context, _ *Request) (*Response, error) {
	s := d.reports
	if s == nil {
		return nil, Err(CodeNotConfigured, "relatório semanal não configurado")
	}
	cfg := s.Get()
	return &Response{Success: true, ReportConfig: &cfg}, nil
}

// handleReportConfigSet persiste o agendamento.
func (d *refDeps) handleReportConfigSet(_ context.Context, req *Request) (*Response, error) {
	s := d.reports
	if s == nil {
		return nil, Err(CodeNotConfigured, "relatório semanal não configurado")
	}
	if req.ReportConfig == nil {
		return nil, Err(CodeInvalid, "agendamento ausente na requisição")
	}
	if err := s.Set(*req.ReportConfig); err != nil {
		return nil, err
	}
	cfg := s.Get()
	msg := "Relatório semanal desativado"
	if cfg.Enabled {
		msg = "Relatório semanal ativado"
	}
	return &Response{Success: true, Message: msg, ReportConfig: &cfg}, nil
}

// handleReportGenerate gera o relatório agora (pasta configurada ou override).
func (d *refDeps) handleReportGenerate(_ context.Context, req *Request) (*Response, error) {
	s := d.reports
	if s == nil || d.analytics == nil {
		return nil, Err(CodeNotConfigured, "relatório semanal não configurado")
	}
	cfg := s.Get()
	if req.ReportExportPath != "" {
		cfg.ExportPath = req.ReportExportPath
	}
	htmlPath, _, err := reports.Generate(d.analytics, cfg, time.Now())
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: "Relatório gerado: " + htmlPath, ReportPath: htmlPath}, nil
}

// ---------------------------------------------------------------------------
// achievements-get (adapter de referência — Fase 5.2, gamificação)
// ---------------------------------------------------------------------------

// handleAchievements deriva as badges das sessões atuais (mesmo caminho do
// handler real de domínio).
func (d *refDeps) handleAchievements(_ context.Context, _ *Request) (*Response, error) {
	if d.analytics == nil {
		return nil, Err(CodeNotConfigured, "achievements não configurado")
	}
	sessions, err := d.analytics.Sessions()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	st := analytics.Summarize(sessions, 7, now)
	return &Response{Success: true, Achievements: achievements.Calculate(st, sessions, now)}, nil
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

// ---------------------------------------------------------------------------
// stats / missions / sessions (adapters de referência — pós-reorg item 1)
// ---------------------------------------------------------------------------

// handleStats devolve o relatório agregado de foco (últimos 7 dias),
// opcionalmente filtrado por missão.
func (d *refDeps) handleStats(ctx context.Context, req *Request) (*Response, error) {
	st, err := analytics.NewService(d.analytics).Stats(ctx, req.Mission)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Stats: st}, nil
}

// handleMissions devolve o foco agregado por missão nomeada.
func (d *refDeps) handleMissions(ctx context.Context, _ *Request) (*Response, error) {
	ls, err := analytics.NewService(d.analytics).Missions(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, LabelStats: ls}, nil
}

// handleSessions devolve as sessões concluídas mais recentes (mais novas
// primeiro, limitadas — o teto vive no serviço de domínio).
func (d *refDeps) handleSessions(ctx context.Context, _ *Request) (*Response, error) {
	sessions, err := analytics.NewService(d.analytics).Sessions(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Sessions: sessions}, nil
}

// ---------------------------------------------------------------------------
// schedule-list/add/import/remove (adapters de referência — pós-reorg item 1)
// ---------------------------------------------------------------------------

// scheduleService monta o serviço de domínio com o manager + catálogo atuais
// (o prefs/resolver do catálogo ficam no Server — SetPresets permanece).
func (d *refDeps) scheduleService() *schedule.Service {
	return schedule.NewService(d.schedules, d.s.catalog())
}

// handleScheduleList devolve o catálogo de regras recorrentes.
func (d *refDeps) handleScheduleList(ctx context.Context, _ *Request) (*Response, error) {
	rules, err := d.scheduleService().List(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Schedules: rules}, nil
}

// handleScheduleAdd cria uma regra recorrente.
func (d *refDeps) handleScheduleAdd(ctx context.Context, req *Request) (*Response, error) {
	r, err := d.scheduleService().Add(ctx, req.ScheduleRule)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Regra %s criada: %s das %s às %s", r.ID, r.Preset, r.Start, r.End)}, nil
}

// handleScheduleImport importa um calendário .ics como regras recorrentes.
func (d *refDeps) handleScheduleImport(ctx context.Context, req *Request) (*Response, error) {
	added, err := d.scheduleService().Import(ctx, req.ICSContent, req.ICSPreset)
	if err != nil {
		return nil, err
	}
	return &Response{
		Success:   true,
		Schedules: added,
		Message:   fmt.Sprintf("%d regras importadas do calendário (preset %s)", len(added), req.ICSPreset),
	}, nil
}

// handleScheduleRemove remove uma regra recorrente.
func (d *refDeps) handleScheduleRemove(ctx context.Context, req *Request) (*Response, error) {
	if err := d.scheduleService().Remove(ctx, req.ScheduleID); err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: fmt.Sprintf("Regra %s removida", req.ScheduleID)}, nil
}

// ---------------------------------------------------------------------------
// pomodoro/pomodoro-defaults/pomodoro-stop (adapters de referência — item 1)
// ---------------------------------------------------------------------------

// pomodoroService monta o serviço de domínio com o runner/prefs/catálogo
// atuais (o runner fica no Server — SetPomodoro permanece para o status; o
// prefs vem do refDeps).
func (d *refDeps) pomodoroService() *pomodoro.Service {
	return pomodoro.NewService(d.s.pomodoro, d.pomodoroPrefs, d.s.catalog())
}

// handlePomodoroStart valida a requisição, resolve o preset para domínios e
// entrega a sessão ao runner, mesclando os parâmetros com os padrões salvos.
func (d *refDeps) handlePomodoroStart(ctx context.Context, req *Request) (*Response, error) {
	res, err := d.pomodoroService().Start(ctx, req.Preset, req.Label, req.WorkMin, req.RestMin, req.Cycles, req.Strict, req.Save)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: res.Message, Pomodoro: &res.State}, nil
}

// handlePomodoroDefaults devolve os padrões atuais de trabalho/descanso/ciclos.
func (d *refDeps) handlePomodoroDefaults(ctx context.Context, _ *Request) (*Response, error) {
	res, err := d.pomodoroService().Defaults(ctx)
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
func (d *refDeps) handlePomodoroStop(ctx context.Context, _ *Request) (*Response, error) {
	res, err := d.pomodoroService().Stop(ctx)
	if err != nil {
		return nil, err
	}
	return &Response{Success: true, Message: res.Message, Pomodoro: &res.State}, nil
}

// ---------------------------------------------------------------------------
// update / update-check (adapters de referência — pós-reorg item 1)
// ---------------------------------------------------------------------------

// updateCheckerBridge adapta o ipc.UpdateChecker do wire (devolve
// ipc.UpdateStatus) ao update.Checker do domínio (update.Status) — o domínio
// não pode importar ipc. Só testes usam (o composition root tem o seu).
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
func (s *Server) updater() update.Checker {
	s.mu.RLock()
	c := s.updateChecker
	s.mu.RUnlock()
	if c == nil {
		return nil
	}
	return updateCheckerBridge{c: c}
}

// handleUpdate aplica uma atualização disponível aos binários.
func (s *Server) handleUpdate(_ context.Context, req *Request) (*Response, error) {
	return s.runUpdateAction(req, true)
}

// handleUpdateCheck verifica atualizações sem aplicar (botão "Verificar" da UI,
// consulta o GitHub na hora em vez de ler o cache do status).
func (s *Server) handleUpdateCheck(_ context.Context, req *Request) (*Response, error) {
	return s.runUpdateAction(req, false)
}

// runUpdateAction roda o checker dentro do orçamento UpdateTimeout, cacheia o
// resultado (a ação "status" o expõe) e sinaliza o restart quando um update
// foi aplicado.
func (s *Server) runUpdateAction(req *Request, apply bool) (*Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), UpdateTimeout)
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
		s.MarkUpdateApplied()
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
