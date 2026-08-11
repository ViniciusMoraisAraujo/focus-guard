package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"focusguard/internal/domain/achievements"
	"focusguard/internal/domain/analytics"
	"focusguard/internal/domain/apps"
	"focusguard/internal/domain/blocks"
	"focusguard/internal/domain/clockguard"
	"focusguard/internal/domain/devices"
	"focusguard/internal/domain/goal"
	interceptordomain "focusguard/internal/domain/interceptor"
	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/domain/pomodoro"
	"focusguard/internal/domain/preset"
	"focusguard/internal/domain/presets"
	"focusguard/internal/domain/reports"
	"focusguard/internal/domain/schedule"
	"focusguard/internal/domain/scheduler"
	"focusguard/internal/domain/telemetry"
	"focusguard/internal/domain/user"
	"focusguard/internal/domain/users"
	"focusguard/internal/infrastructure/dns"
	"focusguard/internal/infrastructure/dnsserver"
	"focusguard/internal/infrastructure/enforcer"
	"focusguard/internal/infrastructure/hostswatch"
	"focusguard/internal/infrastructure/interceptor"
	"focusguard/internal/infrastructure/ntp"
	"focusguard/internal/infrastructure/processguard"
	"focusguard/internal/infrastructure/statewatch"
	"focusguard/internal/infrastructure/store"
	"focusguard/internal/infrastructure/tamper"
	"focusguard/internal/infrastructure/tlsca"
	"focusguard/internal/infrastructure/update"
	"focusguard/internal/system/daemon"
	"focusguard/internal/system/watchdog"
	"focusguard/internal/transport/eventhub"
	"focusguard/internal/transport/ipc"
	"focusguard/internal/transport/metrics"
)

var daemonVersion = "0.0.0-dev"

const (
	updateOwner = "ViniciusMoraisAraujo"
	updateRepo  = "focus-guard"
)

var updateCheckInterval = 24 * time.Hour

// updateInProgressFile é a flag do Bug 2 — vive no internal/update
// (InProgressFileName) com a orquestração de update (Orchestrator, B4). Este
// alias mantém o nome histórico usado pelos testes do daemon.
const updateInProgressFile = update.InProgressFileName

// markUpdateInProgress/clearUpdateInProgress delegam ao internal/update — o
// ciclo de vida da flag (escrever antes do swap, remover só no boot saudável
// ou no caminho de erro) é responsabilidade do Orchestrator.
func markUpdateInProgress(installDir string)  { update.MarkInProgress(installDir) }
func clearUpdateInProgress(installDir string) { update.ClearInProgress(installDir) }

// scheduleCheckInterval is how often the daemon re-evaluates recurring rules:
// fine enough to start a block within a minute of its window opening, cheap
// enough to never matter (a no-op ListBlocks when no rule is due).
var scheduleCheckInterval = 30 * time.Second

var goos = runtime.GOOS
var newHostswatch = hostswatch.New
var newStatewatch = statewatch.New

// probeDaemonAlive pings a daemon on the IPC port. Stubbable nos testes para
// exercitar o comportamento de singleton sem um daemon real.
var probeDaemonAlive = func() bool {
	client := ipc.NewClient()
	resp, err := client.SendWithTimeout(ipc.Request{Action: "ping"}, 3*time.Second)
	return err == nil && resp.Success
}

// isAddrInUse reports whether err is a TCP bind "address already in use"
// (EADDRINUSE / WSAEADDRINUSE) — o modo de falha por trás do crash-loop.
func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	// No Windows o syscall.EADDRINUSE do Go não mapeia para o errno do Winsock
	// (WSAEADDRINUSE = 10048) — sem esse check, a mensagem localizada (ex. PT-BR)
	// não casa com as heurísticas abaixo e um daemon duplicado entraria em retry
	// em vez de encerrar limpo.
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 10048 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "in use") || strings.Contains(msg, "em uso") ||
		strings.Contains(msg, "already in use")
}

var newUpdater = func(owner, repo string) updaterAPI {
	return update.NewUpdater(owner, repo, update.WithVersion(daemonVersion))
}

// processGuardStarter is the subset of *processguard.Guard the daemon needs,
// kept as an interface so tests can stub the constructor without killing real
// processes.
type processGuardStarter interface {
	Start(isActive func() bool)
	Stop()
	SetDenylist(names []string)
}

// newProcessGuard is stubbable (mirrors newHostswatch/newStatewatch) so daemon
// integration tests can disable the guard — a live guard would pkill real
// games/chat apps during the test.
var newProcessGuard = func(denylist []string) processGuardStarter {
	return processguard.New(denylist)
}

// defaultProcessDenylist: executáveis de entretenimento/comunicação encerrados
// enquanto houver sessão de foco ativa.
var defaultProcessDenylist = []string{"steam.exe", "discord.exe"}

// startProcessGuard builds and starts the process guard with the given
// denylist (sourced from the persisted apps store). It is a no-op when the
// constructor returns nil.
// persistPomodoroSummary persists the resolved work/rest/cycles of a finished
// session as the next session's defaults and logs the post-session summary.
// Extracted as a pure function so tests can exercise it without running a
// (minutes-long) real pomodoro session.
func persistPomodoroSummary(prefs *pomodoro.Prefs, sum pomodoro.CompletionSummary) {
	// Arredonda para cima: uma sessão de 30s vira 1m, nunca 0 (que o Resolve
	// trataria como "não salvo" e cairia para o default). O minuto é a unidade
	// mínima do IPC/CLI.
	roundUpMin := func(d time.Duration) int {
		m := int(d / time.Minute)
		if d%time.Minute > 0 {
			m++
		}
		return m
	}
	prefs.Remember(roundUpMin(sum.Work), roundUpMin(sum.Rest), sum.Cycles)
	verb := "Concluída"
	if sum.Stopped {
		verb = "Encerrada"
	}
	mode := ""
	if sum.Strict {
		mode = " [estrita]"
	}
	log.Printf("[FocusGuard Daemon] Sessão %s: preset=%s %d ciclos (%s foco)%s",
		verb, sum.Preset, sum.Cycles, sum.Focus.Round(time.Second), mode)
}

// watchPomodoroCompletions consumes the controller's completion summary
// stream: logs a post-session summary (daemon log), persists the resolved
// work/rest/cycles as the next session's defaults and fires onComplete (the
// event-hub publish of pomodoro-complete, Fase 7) once per finished session.
// Returns a stop func; a nil controller produces a no-op stop.
func watchPomodoroCompletions(c *pomodoro.Controller, prefs *pomodoro.Prefs, onComplete func()) func() {
	if c == nil || prefs == nil {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case sum := <-c.WatchCompletion():
				persistPomodoroSummary(prefs, sum)
				if onComplete != nil {
					onComplete()
				}
			}
		}
	}()
	return func() { close(stop) }
}

func startProcessGuard(sched processguard.ActivityChecker, denylist []string) processGuardStarter {
	pg := newProcessGuard(denylist)
	if pg == nil {
		return nil
	}
	pg.Start(sched.HasActiveBlocks)
	return pg
}

// mergeDNSWire projeta o Status de domínio do DNS (dns.Status) no wire
// (ipc.Response.DNS*). Vive no composition root porque conhece os dois lados
// — o pacote dns usa tipos próprios (DIP, pós-reorg item 2).
func mergeDNSWire(resp *ipc.Response, st dns.Status) {
	resp.DNSEnabled = st.Enabled
	resp.DNSListening = st.Listening
	resp.DNSAddr = st.Addr
	resp.DNSUpstream = st.Upstream
	resp.DNSQueries = st.Queries
	resp.DNSBlocked = st.Blocked
	resp.DNSBindError = st.BindError
}

// dnsCtrlAdapter projeta o *dnsserver.Controller real no ipc.DNSController: o
// ipc usa tipo próprio de status (DNSStatus — pós-reorg item 1) e o pacote
// dns (domínio) usa dnsserver.Status; este adapter fica no composition root
// porque conhece os dois lados.
type dnsCtrlAdapter struct{ c *dnsserver.Controller }

func (a dnsCtrlAdapter) Start() error                      { return a.c.Start() }
func (a dnsCtrlAdapter) Stop() error                       { return a.c.Stop() }
func (a dnsCtrlAdapter) SetUpstream(upstream string) error { return a.c.SetUpstream(upstream) }
func (a dnsCtrlAdapter) Status() ipc.DNSStatus {
	st := a.c.Status()
	return ipc.DNSStatus{
		Listening: st.Listening,
		Addr:      st.Addr,
		Upstream:  st.Upstream,
		Queries:   st.Queries,
		Blocked:   st.Blocked,
		BindError: st.BindError,
	}
}

// clockLockdownAdapter adapta o *scheduler.Scheduler ao clockguard.Lockdown
// (o BlockAllInternet do scheduler devolve *policy.Block; o guard só quer o
// erro). O composition root conhece os dois lados.
type clockLockdownAdapter struct{ s *scheduler.Scheduler }

func (a clockLockdownAdapter) BlockAllInternet(allowlist []string, duration time.Duration) error {
	_, err := a.s.BlockAllInternet(allowlist, duration)
	return err
}

// clockLoggerAdapter adapta o *tamper.Recorder (Log(Event)) ao
// clockguard.Logger (Log(source, action, detail)).
type clockLoggerAdapter struct{ rec *tamper.Recorder }

func (a clockLoggerAdapter) Log(source, action, detail string) {
	a.rec.Log(tamper.Event{At: time.Now(), Source: source, Action: action, Detail: detail})
}

// updateCheckerBridge adapta o ipc.UpdateChecker do wire (devolve
// ipc.UpdateStatus) ao update.Checker do domínio (update.Status) — o domínio
// não pode importar ipc (ciclo: ipc importa update para o adapter). Vivia no
// transport até o pós-reorg item 1; agora é responsabilidade do composition
// root, que conhece os dois lados.
type updateCheckerBridge struct{ c ipc.UpdateChecker }

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

// guardApps wires the persisted apps store to the live process guard: every
// apps-add/apps-remove refreshes the guard's denylist so the change takes
// effect on the next scan. Satisfies the interface of the apps domain handlers
// (apps.NewList/NewAdd/NewRemove) — o ipc.Server não carrega mais a denylist
// (SetApps removido na Fase 5).
type guardApps struct {
	store *apps.Store
	guard interface{ SetDenylist([]string) }
}

func (g *guardApps) List() []string { return g.store.List() }

func (g *guardApps) Add(name string) error {
	if err := g.store.Add(name); err != nil {
		return err
	}
	g.refreshGuard()
	return nil
}

func (g *guardApps) Remove(name string) error {
	if err := g.store.Remove(name); err != nil {
		return err
	}
	g.refreshGuard()
	return nil
}

func (g *guardApps) refreshGuard() {
	if g.guard != nil {
		g.guard.SetDenylist(g.store.List())
	}
}

// presetResolver adapts *preset.Store to schedule.Resolver (preset name → the
// store's domain list), so the schedule worker and the IPC catalog share the
// same user-customizable source of truth.
type presetResolver struct{ store *preset.Store }

func (p presetResolver) Resolve(name string) ([]string, error) {
	pr, err := p.store.Resolve(name)
	if err != nil {
		return nil, err
	}
	return pr.Domains, nil
}

// startScheduleWorker runs the recurring-rule evaluator in the background: on
// every tick it blocks the preset domains of every rule whose window is active
// (ApplyActiveRules skips domains already blocked). The first tick runs
// immediately, so a window that opened while the daemon was down is applied
// right at boot. Returns a stop func.
func startScheduleWorker(mgr *schedule.Manager, resolver schedule.Resolver, b schedule.Blocker, interval time.Duration) func() {
	stop := make(chan struct{})
	go func() {
		apply := func() {
			if _, err := schedule.ApplyActiveRules(mgr, resolver, b, time.Now()); err != nil {
				log.Printf("[FocusGuard Daemon] Agendamento: %v", err)
			}
		}
		apply()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				apply()
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// startWeeklyReportWorker gera o relatório semanal automático (Fase 5.1) no
// horário configurado: o worker acorda quando o próximo agendamento vence,
// gera HTML + JSON na pasta de export e re-agenda. No boot, um horário que já
// passou hoje é gerado imediatamente (a primeira execução usa NextRun, que
// cobre o dia atual se ainda não venceu — caso contrário pula para a semana
// que vem; o relatório atrasado fica coberto pela geração manual/on-demand).
// A config é relida a cada ciclo, então reports-config-set vale sem reinício.
// Falha de escrita é best-effort (log) — nunca derruba o daemon. Retorna um
// stop func para o lifecycle.
func startWeeklyReportWorker(store *reports.Store, p reports.Provider, now func() time.Time) func() {
	stop := make(chan struct{})
	go func() {
		generate := func(cfg reports.Config) {
			if !cfg.Enabled {
				return
			}
			htmlPath, _, err := reports.Generate(p, cfg, now())
			if err != nil {
				log.Printf("[FocusGuard Daemon] Relatório semanal falhou: %v", err)
				return
			}
			log.Printf("[FocusGuard Daemon] Relatório semanal gerado: %s", htmlPath)
		}
		for {
			cfg := store.Get()
			if cfg.Enabled {
				// Se o horário de hoje ainda não venceu, gera quando vencer;
				// se já venceu, aguarda a próxima semana (o on-demand cobre o
				// atraso). Um ciclo de espera nunca é menor que alguns minutos.
				next := cfg.NextRun(now())
				wait := time.Until(next)
				if wait < time.Minute {
					wait = time.Minute
				}
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
					generate(cfg)
				case <-stop:
					timer.Stop()
					return
				}
			} else {
				// Desativado: acorda a cada 5 min para pegar uma reativação
				// (reports-config-set) sem reinício do daemon.
				timer := time.NewTimer(5 * time.Minute)
				select {
				case <-timer.C:
				case <-stop:
					timer.Stop()
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// startClockGuardWorker roda a validação do relógio (Clock Tamper Protection
// — Fase 2): um Check imediato no boot + um Check periódico a cada
// clockguard.CheckInterval. O guard consulta NTP quando o gap do wall clock
// ultrapassa a tolerância; um salto CONFIRMADO aplica o bloqueio preventivo
// (sentinela all-internet) e registra no tamper-log. O NTP é best-effort e
// com timeout curto — um daemon offline nunca trava o boot. Retorna um stop
// func para o lifecycle.
func startClockGuardWorker(st clockguard.State, lock clockguard.Lockdown, logger clockguard.Logger, interval time.Duration) func() {
	guard := clockguard.New(clockguard.Deps{
		State:    st,
		NTP:      ntp.New(ntp.DefaultServer, ntp.DefaultTimeout),
		Lockdown: lock,
		Logger:   logger,
	})
	stop := make(chan struct{})
	go func() {
		check := func() {
			out := guard.Check()
			if out.Suspicion {
				log.Printf("[ClockGuard] %s", out.Detail)
			}
		}
		check()
		if interval <= 0 {
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				check()
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// localIP4 devolve o IP IPv4 da interface de saída (a rota default), usado
// pela Interceptor Page no modo Server: o sinkhole responde os bloqueados com
// este endereço para o navegador da rede conectar no listener :80. Best-effort:
// vazio sem interface de saída.
func localIP4() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return strings.Split(conn.LocalAddr().String(), ":")[0]
}

// interceptorLifecycle é o estado do listener HTTP(S) da Interceptor Page
// (Fase 3): os servers (best-effort — porta 80/443 ocupada não derruba o
// daemon) e o IP respondido pelo DNS quando ativo. O daemon os sobe no boot
// quando o flag persistido está ligado e os troca quando interceptor-set muda
// o flag.
//
// No Desktop os listeners são DUAL-STACK loopback (127.0.0.1 + [::1]): o
// hosts do enforcer escreve as duas entradas (IPv4 e IPv6) e navegadores
// modernos tentam IPv6 primeiro — sem o ::1, a conexão seria recusada no
// stack IPv6 antes do fallback para o IPv4, e a página não apareceria.
//
// O listener TLS (:443) cobre sites HTTPS-only (YouTube, Instagram...): o
// HSTS força TLS e o navegador nunca cai no HTTP. Ele responde com
// certificado assinado pela CA local (tlsca) quando disponível — com a CA
// instalada no trust store do SO (feito aqui no boot, best-effort) a página
// abre sem o aviso de "conexão não segura". Sem CA, o fallback histórico é o
// cert auto-assinado por SNI (usuário continua pelo aviso). Best-effort como
// o HTTP: 443 ocupada degrada só a página dos sites HTTPS.
type interceptorLifecycle struct {
	mu        sync.Mutex
	checker   *scheduler.Scheduler
	servers   []*interceptor.Server
	dns       *dnsserver.Controller
	bindAddrs []string // HTTP (:80)
	tlsAddrs  []string // HTTPS (:443)
	dnsAnswer string
	// caDir é onde a CA local vive (ao lado do state.json). Vazio = sem CA
	// (fallback auto-assinado).
	caDir string
	// ca é a CA local carregada no boot — o TLS listener a usa para assinar.
	ca *tlsca.CA
}

// ensureCAInstalled instala a CA local no trust store do SO — stubbable nos
// testes (a suíte não pode tocar no trust store real da máquina). O default
// usa o runner real (certutil/update-ca-certificates), que funciona porque o
// daemon roda como SYSTEM/root.
var ensureCAInstalled = func(ca *tlsca.CA) error {
	return ca.InstallIntoStore(tlsca.DefaultStoreRunner())
}

// ensureCA carrega (ou gera, no primeiro boot) a CA local e a instala no
// trust store do SO. Best-effort: o daemon roda como SYSTEM/root, então a
// instalação funciona — mas uma falha (ex.: trust store não gravável) apenas
// loga e degrada para o fallback auto-assinado, nunca derruba o boot.
// Idempotente: CA já existente não é regenerada e a já instalada não é
// reinstalada.
func (il *interceptorLifecycle) ensureCA() {
	// Idempotente: sem caDir (fallback auto-assinado) ou CA já carregada, é
	// no-op — garante que a CA é gerada/instalada UMA vez por lifecycle, seja
	// no boot (interceptor já ativo) ou no interceptor-set on pelo IPC/web.
	if il.caDir == "" || il.ca != nil {
		return
	}
	// Higiene: remove .cer temporários órfãos (crash no meio do certutil no
	// boot anterior) — best-effort, os artefatos reais (.crt/.key) não são
	// tocados.
	tlsca.CleanupTempCER(il.caDir)
	ca, err := tlsca.LoadOrCreate(il.caDir)
	if err != nil {
		log.Printf("[FocusGuard Interceptor] CA local indisponível (%v) — página HTTPS seguirá com certificado auto-assinado (aviso no navegador)", err)
		return
	}
	il.ca = ca
	if err := ensureCAInstalled(ca); err != nil {
		log.Printf("[FocusGuard Interceptor] CA local não instalada no trust store: %v — página HTTPS seguirá com o aviso de certificado até instalar (focusguard ca-install)", err)
		return
	}
	log.Printf("[FocusGuard Interceptor] CA local no trust store — página de bloqueio HTTPS abre sem aviso de certificado")
}

// set aplica o novo estado do flag: sobe/derruba os listeners e ajusta a
// resposta do DNS (IP local vs endereço morto). Idempotente — cada bind é
// best-effort (porta 80 ocupada só desativa aquele listener, nunca o daemon).
func (il *interceptorLifecycle) set(enabled bool) {
	il.mu.Lock()
	defer il.mu.Unlock()
	if enabled {
		// Garante a CA local em TODOS os caminhos de ativação (boot e
		// interceptor-set on via IPC/web) — sem isso, habilitar a página depois
		// do boot deixava o TLS no fallback auto-assinado. Idempotente (ver
		// ensureCA).
		il.ensureCA()
		if len(il.servers) == 0 {
			for _, addr := range il.bindAddrs {
				srv := interceptor.New(il.checker)
				if err := srv.Start(addr); err != nil {
					log.Printf("[FocusGuard Interceptor] página de bloqueio indisponível em %s (porta ocupada?): %v — o bloqueio continua valendo", addr, err)
					continue
				}
				il.servers = append(il.servers, srv)
				log.Printf("[FocusGuard Interceptor] página de bloqueio ativa em %s (HTTP)", srv.Addr())
			}
			for _, addr := range il.tlsAddrs {
				srv := interceptor.New(il.checker)
				if il.ca != nil {
					srv.SetCA(il.ca)
				}
				if err := srv.StartTLS(addr); err != nil {
					log.Printf("[FocusGuard Interceptor] página HTTPS indisponível em %s (porta 443 ocupada?): %v — sites HTTPS ficam sem a página, o bloqueio segue valendo", addr, err)
					continue
				}
				il.servers = append(il.servers, srv)
				log.Printf("[FocusGuard Interceptor] página de bloqueio ativa em %s (HTTPS)", srv.Addr())
			}
		}
		if il.dns != nil {
			il.dns.SetInterceptIP(ipOrNil(il.dnsAnswer))
		}
		return
	}
	for _, srv := range il.servers {
		_ = srv.Stop()
	}
	il.servers = nil
	if il.dns != nil {
		il.dns.SetInterceptIP(nil)
	}
}

// stop derruba os listeners (shutdown do daemon).
func (il *interceptorLifecycle) stop() {
	il.mu.Lock()
	defer il.mu.Unlock()
	for _, srv := range il.servers {
		_ = srv.Stop()
	}
	il.servers = nil
}

// registerInterceptorSet registra a ação "interceptor-set" no server — o
// handler de domínio persiste o flag no scheduler e dispara o onChanged (o
// il.set do daemon: sobe/derruba os listeners + garante a CA local). Helper
// ÚNICO compartilhado pelo composition root (runDaemon) e pelo teste de
// integração — qualquer mudança na wiring reflete nos dois, sem divergência.
// O onChanged pode ser nil (consultas/outros callers usam o handler de status
// separado).
func registerInterceptorSet(srv *ipc.Server, sched *scheduler.Scheduler, onChanged func(enabled bool)) {
	hSet := interceptordomain.NewSet(sched, onChanged)
	srv.Register(ipc.DomainAction[interceptordomain.SetInput, interceptordomain.SetResult]{
		Name: hSet.Action(),
		Decode: func(r *ipc.Request) (*interceptordomain.SetInput, error) {
			return &interceptordomain.SetInput{Enabled: r.InterceptorEnabled}, nil
		},
		Handle: hSet.Handle,
		Encode: func(out *interceptordomain.SetResult) (*ipc.Response, error) {
			return &ipc.Response{
				Success:            true,
				Message:            out.Message,
				InterceptorEnabled: out.Status.Enabled,
			}, nil
		},
	}.Handler())
}

// ipOrNil converte um endereço IPv4 textual em net.IP (nil quando vazio).
func ipOrNil(s string) net.IP {
	if s == "" {
		return nil
	}
	return net.ParseIP(s)
}

var serviceStopCh = make(chan struct{})
var daemonDoneCh = make(chan struct{})

func getStateFilePath() string {
	if goos == "windows" {
		return filepath.Join(os.Getenv("PROGRAMDATA"), "FocusGuard", "state.json")
	}
	return "/var/lib/focusguard/state.json"
}

// serverRoleFileName is the empty marker the server MSI drops next to the
// daemon to flag the headless "Server" edition. The desktop edition never
// ships it.
const serverRoleFileName = "server.role"

// isServerEdition reports whether the daemon runs in the headless "Server"
// edition (network-wide DNS sinkhole machine). It is the pure check on a
// directory, exposed for tests via isServerEditionFor.
func isServerEdition() bool {
	exe, err := osExecutable()
	if err != nil {
		return false
	}
	return isServerEditionFor(filepath.Dir(exe))
}

// isServerEditionFor is the pure, testable directory check behind
// isServerEdition.
func isServerEditionFor(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, serverRoleFileName))
	return err == nil
}

func startHostswatch(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
	hw := newHostswatch(enf, sched)
	if hw == nil {
		return nil
	}
	if err := hw.Start(); err != nil {
		log.Printf("[FocusGuard Daemon] Erro ao iniciar hostswatch: %v", err)
		return nil
	}
	log.Println("[FocusGuard Daemon] Hosts watcher ativo.")
	return hw
}

func startStatewatch(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
	sw := newStatewatch(rec, statePath)
	if sw == nil {
		return nil
	}
	if err := sw.Start(); err != nil {
		log.Printf("[FocusGuard Daemon] Erro ao iniciar statewatch: %v", err)
		return nil
	}
	log.Println("[FocusGuard Daemon] State watcher ativo.")
	return sw
}

// updaterAPI é o alias da superfície exigida pela orquestração de update
// (internal/update.UpdaterAPI) — o daemon e o Orchestrator compartilham o
// mesmo contrato.
type updaterAPI = update.UpdaterAPI

// stopForBinarySwap é o preparo do swap (parar o serviço do watchdog + o
// processo do tray no Windows para liberar os .exe). A implementação vive no
// internal/update (StopForBinarySwap); este var existe para os testes do
// daemon stubarem sem tocar em serviços/processos reais — o daemonUpdater o
// repassa ao Orchestrator a cada chamada.
var stopForBinarySwap = update.StopForBinarySwap

type daemonUpdater struct {
	u        updaterAPI
	binaries []string
}

func (d *daemonUpdater) Check(ctx context.Context, apply bool, channel string) (ipc.UpdateStatus, error) {
	st := ipc.UpdateStatus{CurrentVersion: daemonVersion}
	if d == nil || d.u == nil {
		return st, nil
	}

	// A orquestração completa do update (canal, flag Bug 2, preparo do swap
	// Bug 3, UpdateToAll + CleanupStale Bug 1, fallback move-on-reboot) vive
	// no internal/update (Orchestrator, B4) — o daemon só delega e converte o
	// Status do domínio para o wire. O preparo do swap é o var stubbable do
	// pacote main (stopForBinarySwap), lido aqui no momento da chamada para os
	// stubs dos testes valerem.
	orch := update.NewOrchestrator(d.u, d.binaries, daemonVersion)
	orch.StopForSwap = stopForBinarySwap

	res, err := orch.Check(ctx, apply, channel)
	return ipc.UpdateStatus{
		CurrentVersion: res.CurrentVersion,
		NewVersion:     res.NewVersion,
		Available:      res.Available,
		Applied:        res.Applied,
		PendingReboot:  res.PendingReboot,
	}, err
}

// siblingBinaries returns the paths of every FocusGuard binary expected next
// to the daemon (CLI, tray, watchdog, web) — same directory, same extension.
//
// O daemon vem PRIMEIRO de propósito (Bug da task.md): é o único binário que
// não pode ser parado antes do swap (é ele quem aplica o update), então a
// troca do próprio exe é o ponto decisivo — se ela falhar mesmo com retry,
// nada mais foi trocado e o UpdateToAll agenda toda a suíte para o próximo
// boot (fallback move-on-reboot) em vez de deixar uma meia-atualização.
func siblingBinaries(daemonPath string) []string {
	dir := filepath.Dir(daemonPath)
	ext := filepath.Ext(daemonPath) // ".exe" no Windows, "" no Linux
	names := []string{"focusguard-daemon", "focusguard", "focusguard-tray", "focusguard-watchdog", "focusguard-web"}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(dir, n+ext))
	}
	return out
}

// newDaemonUpdater builds the daemon updater for the given daemon binary path,
// including every sibling binary that is actually installed (a missing tray is
// skipped; the daemon itself is always present because it is running).
func newDaemonUpdater(daemonPath string, mk updaterAPI) *daemonUpdater {
	var binaries []string
	for _, p := range siblingBinaries(daemonPath) {
		if _, err := os.Stat(p); err == nil {
			binaries = append(binaries, p)
		}
	}
	if len(binaries) == 0 {
		binaries = []string{daemonPath} // defensivo: nunca atualizar lista vazia
	}
	return &daemonUpdater{u: mk, binaries: binaries}
}

var osExecutable = os.Executable

func setupUpdateIntegration(server *ipc.Server) func() {
	binaryPath, err := osExecutable()
	if err != nil {
		log.Printf("[FocusGuard Daemon] Auto-update desativado: %v", err)
		return nil
	}

	// Bug 1: varre artigos de updates passados em TODO boot (inclusive dev) —
	// sujeira de um update de produção (.bak antigos, .old/.trash órfãos) não
	// pode esperar o próximo update para ser limpa. Mantém só o .bak mais novo
	// por binário para o smart recovery do watchdog.
	newUpdater(updateOwner, updateRepo).CleanupStale(filepath.Dir(binaryPath))

	if daemonVersion == "" || strings.HasSuffix(daemonVersion, "-dev") {
		log.Println("[FocusGuard Daemon] Auto-update desativado (versão de desenvolvimento).")
		return nil
	}

	server.SetUpdateChecker(newDaemonUpdater(binaryPath, newUpdater(updateOwner, updateRepo)))
	return startPeriodicUpdateCheck(server, updateCheckInterval)
}

// osExit é stubbable nos testes (o main real usa os.Exit).
var osExit = os.Exit

// restartAfterUpdate é o callback registrado no server (SetOnUpdateApplied):
// depois que o update é aplicado com sucesso, o daemon encerra a sessão
// pomodoro (se houver) e reinicia imediatamente via osExit(1), para o
// supervisor (systemd Restart=always no Linux, SCM recovery no Windows) subir
// o binário novo — corrige o "daemon zumbi" que ficaria rodando a versão
// antiga em RAM. Os bloqueios NÃO são tocados: ficam no state.json (e nas
// regras do firewall/hosts do SO) e o boot da nova versão os restaura do
// state.json — proteção contínua durante o reinício. Só a sessão pomodoro é
// perdida (não é persistida).
//
// O exit code é 1 (e não 0) de propósito: o SCM do Windows só aplica as ações
// de recovery (sc failure ... actions= restart) quando o serviço termina com
// falha; um exit 0 é tratado como parada limpa e o serviço ficaria morto até
// reboot. No Linux o systemd reinicia com qualquer código, então 1 é seguro.
func restartAfterUpdate(sched interface{ HasActiveBlocks() bool }, stopSession func() (pomodoro.State, error)) {
	time.Sleep(500 * time.Millisecond) // deixa a resposta IPC chegar ao CLI
	if sched.HasActiveBlocks() {
		log.Println("[FocusGuard Daemon] Update aplicado com bloqueios ativos — o boot da nova versão os restaura.")
	}
	if stopSession != nil {
		if _, err := stopSession(); err != nil {
			log.Printf("[FocusGuard Daemon] Aviso ao encerrar a sessão pós-update (será descartada no restart): %v", err)
		}
	}
	log.Println("[FocusGuard Daemon] Update aplicado. Reiniciando daemon com a nova versão...")
	osExit(1)
}

func startPeriodicUpdateCheck(server *ipc.Server, interval time.Duration) func() {
	stop := make(chan struct{})
	go func() {
		if interval <= 0 {
			return
		}
		check := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			st, err := server.RefreshUpdateStatus(ctx)
			if err != nil {
				log.Printf("[FocusGuard Daemon] Verificação de atualização falhou: %v", err)
				return
			}
			if st.Available {
				log.Printf("[FocusGuard Daemon] Nova versão disponível: %s", st.NewVersion)
			}
		}
		check()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				check()
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

func getWatchdogSec() int {
	usecStr := os.Getenv("WATCHDOG_USEC")
	if usecStr == "" {
		return 0
	}
	usec, err := strconv.Atoi(usecStr)
	if err != nil || usec <= 0 {
		return 0
	}
	return usec / 1_000_000
}

func main() {
	stopLog := setupLogging()
	defer stopLog()

	if tryRunAsService() {
		return
	}

	for {
		shouldExit := runDaemon()
		if shouldExit {
			log.Println("[FocusGuard Daemon] Processo encerrado.")
			return
		}
		log.Println("[FocusGuard Daemon] Reiniciando...")
		time.Sleep(1 * time.Second)
	}
}

// stopLifecycleComponents para os componentes na ordem inversa de registro —
// o mesmo teardown ordenado que o internal/system/daemon executa (B10). Usado nos
// pontos de falha do boot que antecedem o Run (onde o lifecycle ainda não
// assumiu o controle).
func stopLifecycleComponents(components []daemon.Component) {
	for i := len(components) - 1; i >= 0; i-- {
		components[i].Stop()
	}
}

// ipcServerLifecycle adapta *ipc.Server ao daemon.Server do lifecycle: o Start
// é o ponto do boot saudável (Bug 2 — remove a flag de update quando a nova
// versão já reconciliou o estado e está pronta para servir IPC) seguido do
// serve; o Stop fecha o listener (faz o Start retornar nil).
type ipcServerLifecycle struct{ server *ipc.Server }

func (l ipcServerLifecycle) Start() error {
	if exe, err := osExecutable(); err == nil {
		clearUpdateInProgress(filepath.Dir(exe))
	}
	log.Println("[FocusGuard Daemon] Servidor IPC ativo e aguardando requisições...")
	return l.server.Start()
}

func (l ipcServerLifecycle) Stop() error { return l.server.Stop() }

func runDaemon() bool {
	log.Println("[FocusGuard Daemon] Iniciando serviço...")

	// Satélites do boot (watchers/workers/guards) registrados como componentes
	// do lifecycle (Fase 5 — B10): o internal/system/daemon os para em ordem inversa
	// no shutdown. Os Start são no-op (StopOnly) porque o boot histórico os
	// inicia na construção — o lifecycle assume o servidor IPC e TODO o
	// teardown.
	var components []daemon.Component

	// Singleton: se outra instância do daemon já está atendendo o IPC, esta
	// encerra limpo em vez de crash-loopar no bind (o log mostrou o ciclo
	// "bind: address already in use → Reiniciando..." para sempre). O watchdog
	// e o SCM já mantêm a instância legítima de pé; uma segunda não ajuda.
	if probeDaemonAlive() {
		log.Println("[FocusGuard Daemon] Outra instância já está ativa na porta IPC — encerrando esta (evita conflito de bind e crash-loop).")
		return true
	}

	// Endpoint pprof temporário (FG_PPROF=<porta>), loopback, stdlib — para
	// captura de perfil do processo real. Best-effort: bind falhou não derruba
	// o daemon.
	if addr := pprofAddr(); addr != "" {
		stopPprof, err := startPprof(addr)
		if err != nil {
			log.Printf("[FocusGuard Daemon] pprof indisponível em %s: %v", addr, err)
		} else {
			components = append(components, daemon.StopOnly(stopPprof))
			log.Printf("[FocusGuard Daemon] pprof ativo em http://%s/debug/pprof/ (temporário, loopback)", addr)
		}
	}

	statePath := getStateFilePath()

	// Edição Server: o MSI server instala um marcador vazio "server.role" ao
	// lado do daemon. No PRIMEIRO boot (sem state.json ainda) o DNS sinkhole
	// nasce habilitado — a máquina é o "Rei da Rede" desde a instalação.
	// Depois disso o flag persistido manda; a edição desktop nunca tem o
	// marcador e segue com o DNS desligado por padrão.
	firstBoot := false
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		firstBoot = true
	}

	st, err := store.NewStore(statePath)
	if err != nil {
		log.Printf("[FocusGuard Daemon] Erro ao criar store: %v", err)
		stopLifecycleComponents(components)
		return false
	}

	// Réplicas criptografadas atreladas ao hardware: auto-healing de um
	// state.json apagado/vazio/corrompido a partir do backup oculto. A
	// ativação é best-effort — sem ID de hardware as réplicas apenas ficam
	// desativadas e o boot segue o fluxo histórico.
	if err := st.EnableReplica(nil); err != nil {
		log.Printf("[FocusGuard Daemon] Réplicas criptografadas desativadas: %v", err)
	} else if _, err := st.LoadAndHeal(); err != nil {
		log.Printf("[FocusGuard Daemon] LoadAndHeal: %v", err)
	}

	enf := enforcer.NewEnforcer()
	sched := scheduler.NewScheduler(st, enf)

	// Event hub (Fase 7): o daemon publica mudanças de estado coarse para o
	// focusguard-web, que as relê (status/stats) e entrega ao navegador via
	// SSE — no lugar do polling do frontend. O scheduler avisa em toda mutação
	// de blocos (block/extend/batch/pânico/expiração/reconciliação); o hub tem
	// um ring buffer (64) para um subscriber que reconecta pegar o que perdeu.
	hub := eventhub.New(64)
	sched.SetOnChange(func() { hub.Publish(ipc.EventBlocksChanged) })

	if err := sched.Start(); err != nil {
		log.Printf("[FocusGuard Daemon] Erro na reconciliação: %v", err)
		stopLifecycleComponents(components)
		return false
	}
	log.Println("[FocusGuard Daemon] Estado reconciliado com sucesso.")

	// O refresh periódico de IPs roda numa goroutine do scheduler
	// (startPeriodicIPRefresh); o lifecycle o encerra no shutdown — sem isso a
	// goroutine vazava (bug-hunt Etapa 4).
	components = append(components, daemon.StopOnly(sched.Stop))

	if firstBoot && isServerEdition() {
		if err := sched.SetDNSEnabled(true); err != nil {
			log.Printf("[FocusGuard Daemon] Edição Server: falha ao habilitar DNS no primeiro boot: %v", err)
		} else {
			log.Println("[FocusGuard Daemon] Edição Server detectada — DNS habilitado no primeiro boot.")
		}
	}

	watchdogSec := getWatchdogSec()
	wd := watchdog.New(sched, watchdogSec)
	go wd.Start()

	// Tamper log: histórico de tentativas de burla (adulterações externas do
	// hosts/state detectadas e revertidas) — "focusguard tamper-log".
	tamperRec := tamper.NewRecorder(filepath.Join(filepath.Dir(statePath), "tamper.jsonl"))

	// Clock Tamper Protection (Fase 2): validação do wall clock no boot e a
	// cada 10 min. Um salto CONFIRMADO por NTP aplica o bloqueio preventivo
	// (sentinela all-internet, via scheduler) e registra no tamper-log. O
	// tamperRec satifaz o clockguard.Logger (Log(source, action, detail)).
	stopClock := startClockGuardWorker(sched, clockLockdownAdapter{s: sched}, clockLoggerAdapter{rec: tamperRec}, clockguard.CheckInterval)
	if stopClock != nil {
		components = append(components, daemon.StopOnly(stopClock))
	}

	hw := startHostswatch(enf, sched)
	if hw != nil {
		components = append(components, daemon.StopOnly(hw.Stop))
		hw.SetTamperLogger(tamperRec)
		if notifier, ok := enf.(interface{ SetOnHostsWrite(func()) }); ok {
			notifier.SetOnHostsWrite(hw.MarkSelfWrite)
		}
	}

	sw := startStatewatch(sched, statePath)
	if sw != nil {
		components = append(components, daemon.StopOnly(sw.Stop))
		sw.SetTamperLogger(tamperRec)
		st.SetOnSave(sw.MarkSelfWrite)
	}

	// Process Guard: encerra executáveis da denylist (steam, discord) enquanto
	// houver sessão de foco ativa — a checagem de atividade é o HasActiveBlocks
	// do scheduler. A denylist é persistida (apps.json) e configurável via
	// "focusguard apps add/remove".
	appsStore := apps.NewStore(filepath.Join(filepath.Dir(statePath), "apps.json"))
	pg := startProcessGuard(sched, appsStore.List())
	if pg != nil {
		components = append(components, daemon.StopOnly(pg.Stop))
	}

	server := ipc.NewServer(sched)
	server.SetCurrentVersion(daemonVersion)
	server.SetTamper(tamperRec)
	server.SetEventHub(hub)
	// Métricas de latência por ação (Fase 8 — C3): o ipc.Server mede cada
	// dispatch e loga ações lentas; o snapshot é lido via `focusguard metrics`
	// (CLI) ou GET /api/metrics (web).
	server.SetMetrics(metrics.New(256))

	// DNS Sinkhole ("Rei da Rede"): servidor DNS local (porta 53) que responde
	// 0.0.0.0 para domínios bloqueados e encaminha o resto ao upstream
	// (Cloudflare Security 1.1.1.2). Não depende de sessão de foco nem do
	// arquivo hosts — cobre a rede inteira. Se o flag persistido estiver ativo,
	// o servidor sobe junto com o daemon. Bind é best-effort: porta 53 ocupada
	// não derruba o daemon (fica reportado no dns-status).
	// Upstream persistido via dns-set-upstream (web/CLI) ou o padrão
	// Cloudflare Security 1.1.1.2 — o daemon constrói o controller com o
	// valor em disco para o boot usar o upstream que o usuário escolheu.
	dnsUpstream := sched.DNSUpstream()
	if dnsUpstream == "" {
		dnsUpstream = dnsserver.DefaultUpstream
	}
	dnsSrv := dnsserver.NewController(sched, dnsserver.DefaultBindAddr, dnsUpstream)
	// Telemetria do sinkhole (Fase 1.2 do features-plan): log JSONL das
	// queries bloqueadas (domínio + IP de origem) exposto no painel via
	// dns-telemetry. O hook é não-bloqueante e best-effort — uma falha de
	// escrita nunca afeta o caminho do DNS.
	telRec := telemetry.NewRecorder(filepath.Join(filepath.Dir(statePath), "telemetry.jsonl"))
	telRec.PurgeOld()
	dnsSrv.SetOnBlocked(func(domain, clientIP string) {
		telRec.Record(telemetry.BlockedQuery{Domain: domain, ClientIP: clientIP, Timestamp: time.Now()})
	})
	server.SetDNS(dnsCtrlAdapter{c: dnsSrv})
	// Hook DoH do dns-start: fechado pela porta 853 (browsers com DoH embutido
	// ignorariam o sinkhole). Idempotente — o scheduler também o aplica
	// enquanto houver blocos ativos. Passado ao handler de domínio (dns.Start)
	// no composition root abaixo; o ipc não o consome mais.
	dohHook := func() {
		if err := enf.BlockDoH(); err != nil {
			log.Printf("[FocusGuard Daemon] Falha ao bloquear DoH (porta 853): %v", err)
		}
	}
	if sched.DNSEnabled() {
		if err := dnsSrv.Start(); err != nil {
			log.Printf("[FocusGuard Daemon] DNS habilitado, mas não subiu: %v", err)
		} else {
			log.Printf("[FocusGuard Daemon] Servidor DNS ativo em %s (upstream %s)", dnsserver.DefaultBindAddr, dnsUpstream)
		}
	}
	components = append(components, daemon.StopOnly(func() { _ = dnsSrv.Stop() }))

	// Focus Interceptor Page (Fase 3): listener HTTP :80 + HTTPS :443
	// best-effort que serve a página de bloqueio (com frase motivacional)
	// quando o flag persistido está ativo. Desktop: loopback dual-stack
	// (127.0.0.1 + ::1, casando com as duas entradas que o enforcer escreve
	// no hosts) — a página funciona na edição padrão. Server: todas as
	// interfaces, e o DNS responde os bloqueados com o IP local (em vez de
	// 0.0.0.0) para a rede conectar no listener. O :443 cobre sites
	// HTTPS-only (HSTS) com cert auto-assinado por SNI. Portas ocupadas nunca
	// derrubam o daemon — o bloqueio continua valendo sem a página.
	interceptorBinds := []string{"127.0.0.1:80", "[::1]:80"}
	interceptorTLS := []string{"127.0.0.1:443", "[::1]:443"}
	interceptorAnswer := ""
	if isServerEdition() {
		interceptorBinds = []string{interceptor.DefaultBindAddr}
		interceptorTLS = []string{interceptor.DefaultTLSBindAddr}
		interceptorAnswer = localIP4()
	}
	il := &interceptorLifecycle{
		checker:   sched,
		dns:       dnsSrv,
		bindAddrs: interceptorBinds,
		tlsAddrs:  interceptorTLS,
		dnsAnswer: interceptorAnswer,
		caDir:     filepath.Join(filepath.Dir(statePath), "ca"),
	}
	if sched.InterceptorEnabled() {
		// Com a página ativa, o set(true) garante a CA local no boot: o daemon
		// (SYSTEM/root) gera a CA e a instala no trust store — a página HTTPS
		// abre sem aviso.
		il.set(true)
	}
	components = append(components, daemon.StopOnly(il.stop))

	// Modo Pomodoro & Presets: ciclos de trabalho/descanso sobre as categorias
	// (--preset social, video, news, games). O controller bloqueia via o
	// scheduler; os blocos de trabalho expiram sozinhos pelos timers. Sessões
	// estritas (--strict) não podem ser encerradas antecipadamente e, ao
	// terminar, são registradas no analytics para o "focusguard stats".
	pomo := pomodoro.New(sched)
	pomo.SetNotifier(pomodoro.NewNotifier()) // beep nas transições work/rest/done
	pomo.SetOnChange(func() { hub.Publish(ipc.EventPomodoroChanged) })
	server.SetPomodoro(pomo)

	// Preferências do pomodoro: work/rest/cycles da última sessão (--save)
	// persistidos em pomodoro.json — um "focusguard pomodoro --preset x" sem
	// flags reutiliza os padrões salvos. O watcher abaixo também persiste os
	// valores resolvidos ao fim de cada sessão (mesmo sem --save).
	pomoPrefs := pomodoro.NewPrefs(filepath.Join(filepath.Dir(statePath), "pomodoro.json"))
	stopPomoWatch := watchPomodoroCompletions(pomo, pomoPrefs, func() { hub.Publish(ipc.EventPomodoroComplete) })
	if stopPomoWatch != nil {
		components = append(components, daemon.StopOnly(stopPomoWatch))
	}

	// Presets personalizados do usuário: catálogo persistido (builtins + custom)
	// ao lado do state.json — "focusguard preset add/remove".
	presetStore := preset.NewStore(filepath.Join(filepath.Dir(statePath), "presets.json"))
	server.SetPresets(presetStore)

	// Usuários da interface web: credenciais persistidas em user.json ao lado
	// do state.json (apenas hashes bcrypt, nunca senha em texto puro). O admin
	// é garantido no boot — o sistema sempre mantém a conta de recuperação — e
	// os demais usuários são criados pelo admin (web/CLI).
	userStore := user.NewStore(filepath.Join(filepath.Dir(statePath), "user.json"))
	if err := userStore.EnsureAdmin(); err != nil {
		log.Printf("[FocusGuard Daemon] Aviso: falha ao garantir o usuário admin: %v", err)
	}

	// Agendamento recorrente: regras "bloquear social seg-sex 08:00-12:00"
	// persistidas em schedules.json. O worker de background aplica as janelas
	// vencidas a cada 30s (e no boot); o IPC expõe schedule add/list/remove.
	scheduleMgr := schedule.NewManager(filepath.Join(filepath.Dir(statePath), "schedules.json"))
	scheduleMgr.SetOnChange(func() { hub.Publish(ipc.EventScheduleChanged) })
	stopSchedule := startScheduleWorker(scheduleMgr, presetResolver{store: presetStore}, sched, scheduleCheckInterval)
	if stopSchedule != nil {
		components = append(components, daemon.StopOnly(stopSchedule))
	}

	// Strict Mode & Analytics: histórico de sessões em JSONL ao lado do
	// state.json (best-effort — sem o arquivo o recorder fica em memória e
	// apenas perde o histórico entre restarts).
	rec := analytics.NewRecorder(filepath.Join(filepath.Dir(statePath), "analytics.jsonl"))
	pomo.SetRecorder(rec)

	// Meta diária de foco (ex: 4h/dia) persistida em goal.json — alimenta o
	// "focusguard goal" e o status da TUI.
	goalStore := goal.NewStore(filepath.Join(filepath.Dir(statePath), "goal.json"))
	server.SetGoal(goalStore)

	// Regras por dispositivo (Fase 4 — edição Server): catálogo de políticas
	// por IP persistido em devices.json. O scheduler o consulta em
	// IsBlockedFor (o DNS server decide pelo IP de origem); as ações
	// devices-* abaixo são a superfície de edição. Na edição desktop o
	// catálogo fica vazio e o comportamento é o clássico (sem per-device).
	deviceStore := devices.NewStore(filepath.Join(filepath.Dir(statePath), "devices.json"))
	sched.SetDeviceRules(deviceStore)

	// Relatório semanal automático (Fase 5.1): agendamento persistido em
	// reports.json (dia/hora/pasta). O worker abaixo gera o relatório no
	// horário configurado (e no boot, se o horário já passou); a ação
	// reports-generate permite gerar na hora pela UI/CLI. Falha de escrita é
	// best-effort (log) — nunca derruba o daemon.
	reportStore := reports.NewStore(filepath.Join(filepath.Dir(statePath), "reports.json"))
	stopReports := startWeeklyReportWorker(reportStore, rec, time.Now)
	if stopReports != nil {
		components = append(components, daemon.StopOnly(stopReports))
	}

	// Composition root (Fase 5): as ações de domínio (block/block-all, apps-*,
	// goal-*, presets, preset-*, user-*, dns-*) são atendidas pelos handlers
	// dos pacotes de domínio (interfaces estreitas, DIP) — não pelos adapters
	// do ipc. O ipc.Server registra só os handlers de nível servidor
	// (ping/status/tamper/services); este bloco fecha o registry (34 ações)
	// que o ValidateRegistry abaixo verifica no boot.
	// blocks via ipc.DomainAction (handlers de domínio com tipos próprios,
	// adaptados ao wire — pós-reorg item 2). O conflito ask-first devolve o
	// código estável no resultado; o Encode o projeta no wire.
	hBlock := blocks.New(sched, presetStore)
	server.Register(ipc.DomainAction[blocks.BlockInput, blocks.BlockResult]{
		Name: hBlock.Action(),
		Decode: func(r *ipc.Request) (*blocks.BlockInput, error) {
			return &blocks.BlockInput{Domain: r.Domain, Duration: r.Duration, Preset: r.Preset, Extend: r.Extend, Replace: r.Replace}, nil
		},
		Validate: hBlock.Validate,
		Handle:   hBlock.Handle,
		Encode: func(out *blocks.BlockResult) (*ipc.Response, error) {
			// Success deriva de Code (invariante do domínio: conflito implica
			// código estável) — nunca Success:true + Conflict:true no wire.
			resp := &ipc.Response{Success: out.Code == "", Message: out.Message, Conflict: out.Conflict, ConflictBlock: out.ConflictBlock}
			if out.Code != "" {
				resp.Code = out.Code
			}
			return resp, nil
		},
	}.Handler())
	hBlockAll := blocks.NewBlockAll(sched)
	server.Register(ipc.DomainAction[blocks.BlockAllInput, blocks.BlockAllResult]{
		Name: hBlockAll.Action(),
		Decode: func(r *ipc.Request) (*blocks.BlockAllInput, error) {
			return &blocks.BlockAllInput{Duration: r.Duration, Allowlist: r.Allowlist}, nil
		},
		Validate: hBlockAll.Validate,
		Handle:   hBlockAll.Handle,
		Encode: func(out *blocks.BlockAllResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())

	// presets/goal via ipc.DomainAction (handlers de domínio com tipos
	// próprios, adaptados ao wire — pós-reorg item 2).
	hPresetsList := presets.NewList(presetStore)
	server.Register(ipc.DomainAction[presets.NoInput, presets.ListResult]{
		Name:   hPresetsList.Action(),
		Decode: ipc.NoInputDecode[presets.NoInput](),
		Handle: hPresetsList.Handle,
		Encode: func(out *presets.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Presets: out.Presets}, nil
		},
	}.Handler())
	hPresetsAdd := presets.NewAdd(presetStore)
	server.Register(ipc.DomainAction[presets.AddInput, presets.AddResult]{
		Name: hPresetsAdd.Action(),
		Decode: func(r *ipc.Request) (*presets.AddInput, error) {
			return &presets.AddInput{PresetName: r.PresetName, PresetLabel: r.PresetLabel, PresetDomains: r.PresetDomains}, nil
		},
		Handle: hPresetsAdd.Handle,
		Encode: func(out *presets.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hPresetsRemove := presets.NewRemove(presetStore)
	server.Register(ipc.DomainAction[presets.RemoveInput, presets.RemoveResult]{
		Name: hPresetsRemove.Action(),
		Decode: func(r *ipc.Request) (*presets.RemoveInput, error) {
			return &presets.RemoveInput{PresetName: r.PresetName}, nil
		},
		Handle: hPresetsRemove.Handle,
		Encode: func(out *presets.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hGoalGet := goal.NewGet(goalStore)
	server.Register(ipc.DomainAction[goal.NoInput, goal.GetResult]{
		Name:   hGoalGet.Action(),
		Decode: ipc.NoInputDecode[goal.NoInput](),
		Handle: hGoalGet.Handle,
		Encode: func(out *goal.GetResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Goal: out.Goal}, nil
		},
	}.Handler())
	hGoalSet := goal.NewSet(goalStore)
	server.Register(ipc.DomainAction[goal.SetInput, goal.SetResult]{
		Name:   hGoalSet.Action(),
		Decode: func(r *ipc.Request) (*goal.SetInput, error) { return &goal.SetInput{GoalMinutes: r.GoalMinutes}, nil },
		Handle: hGoalSet.Handle,
		Encode: func(out *goal.SetResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Goal: out.Goal, Message: out.Message}, nil
		},
	}.Handler())
	// dns via ipc.DomainAction (DIP — pós-reorg item 2).
	hDNSStart := dns.NewStart(dnsSrv, sched, dohHook)
	server.Register(ipc.DomainAction[dns.NoInput, dns.StartResult]{
		Name:   hDNSStart.Action(),
		Decode: ipc.NoInputDecode[dns.NoInput](),
		Handle: hDNSStart.Handle,
		Encode: func(out *dns.StartResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true, Message: out.Message}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	hDNSStop := dns.NewStop(dnsSrv, sched)
	server.Register(ipc.DomainAction[dns.NoInput, dns.StopResult]{
		Name:   hDNSStop.Action(),
		Decode: ipc.NoInputDecode[dns.NoInput](),
		Handle: hDNSStop.Handle,
		Encode: func(out *dns.StopResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true, Message: out.Message}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	hDNSStatus := dns.NewStatus(dnsSrv, sched)
	server.Register(ipc.DomainAction[dns.NoInput, dns.StatusResult]{
		Name:   hDNSStatus.Action(),
		Decode: ipc.NoInputDecode[dns.NoInput](),
		Handle: hDNSStatus.Handle,
		Encode: func(out *dns.StatusResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	hDNSSetUpstream := dns.NewSetUpstream(dnsSrv, sched)
	server.Register(ipc.DomainAction[dns.SetUpstreamInput, dns.SetUpstreamResult]{
		Name: hDNSSetUpstream.Action(),
		Decode: func(r *ipc.Request) (*dns.SetUpstreamInput, error) {
			return &dns.SetUpstreamInput{Upstream: r.Upstream}, nil
		},
		Handle: hDNSSetUpstream.Handle,
		Encode: func(out *dns.SetUpstreamResult) (*ipc.Response, error) {
			resp := &ipc.Response{Success: true, Message: out.Message}
			mergeDNSWire(resp, out.Status)
			return resp, nil
		},
	}.Handler())
	// interceptor-set/interceptor-status via ipc.DomainAction (DIP — pós-reorg
	// item 2). O onChanged liga/desliga o listener HTTP e a resposta do DNS
	// (Fase 3) — o mesmo caminho do boot quando o flag persistido está ativo.
	registerInterceptorSet(server, sched, il.set)
	hInterceptorStatus := interceptordomain.NewStatus(sched)
	server.Register(ipc.DomainAction[interceptordomain.NoInput, interceptordomain.StatusResult]{
		Name:   hInterceptorStatus.Action(),
		Decode: ipc.NoInputDecode[interceptordomain.NoInput](),
		Handle: hInterceptorStatus.Handle,
		Encode: func(out *interceptordomain.StatusResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, InterceptorEnabled: out.Status.Enabled}, nil
		},
	}.Handler())
	// devices via ipc.DomainAction (Fase 4 — edição Server). O *devices.Store
	// satisfaz a Service dos handlers; o scheduler o consulta em IsBlockedFor.
	hDevicesList := devices.NewList(deviceStore)
	server.Register(ipc.DomainAction[devices.NoInput, devices.ListResult]{
		Name:   hDevicesList.Action(),
		Decode: ipc.NoInputDecode[devices.NoInput](),
		Handle: hDevicesList.Handle,
		Encode: func(out *devices.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Devices: out.Devices}, nil
		},
	}.Handler())
	hDevicesUpsert := devices.NewUpsert(deviceStore)
	server.Register(ipc.DomainAction[devices.UpsertInput, devices.UpsertResult]{
		Name: hDevicesUpsert.Action(),
		Decode: func(r *ipc.Request) (*devices.UpsertInput, error) {
			if r.Device == nil {
				return nil, ipcerr.New(ipcerr.CodeInvalid, "dispositivo ausente na requisição")
			}
			return &devices.UpsertInput{Device: *r.Device}, nil
		},
		Handle: hDevicesUpsert.Handle,
		Encode: func(out *devices.UpsertResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hDevicesRemove := devices.NewRemove(deviceStore)
	server.Register(ipc.DomainAction[devices.RemoveInput, devices.RemoveResult]{
		Name: hDevicesRemove.Action(),
		Decode: func(r *ipc.Request) (*devices.RemoveInput, error) {
			return &devices.RemoveInput{IP: r.DeviceIP}, nil
		},
		Handle: hDevicesRemove.Handle,
		Encode: func(out *devices.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// reports via ipc.DomainAction (Fase 5.1 — mesmo padrão do composition
	// root). O *reports.Store satisfaz a ConfigStore; o *analytics.Recorder a
	// Provider de sessões.
	hReportConfigGet := reports.NewConfigGet(reportStore)
	server.Register(ipc.DomainAction[reports.NoInput, reports.ConfigResult]{
		Name:   hReportConfigGet.Action(),
		Decode: ipc.NoInputDecode[reports.NoInput](),
		Handle: hReportConfigGet.Handle,
		Encode: func(out *reports.ConfigResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, ReportConfig: &out.Config}, nil
		},
	}.Handler())
	hReportConfigSet := reports.NewConfigSet(reportStore)
	server.Register(ipc.DomainAction[reports.ConfigInput, reports.ConfigSetResult]{
		Name: hReportConfigSet.Action(),
		Decode: func(r *ipc.Request) (*reports.ConfigInput, error) {
			if r.ReportConfig == nil {
				return nil, ipcerr.New(ipcerr.CodeInvalid, "agendamento ausente na requisição")
			}
			return &reports.ConfigInput{Config: *r.ReportConfig}, nil
		},
		Handle: hReportConfigSet.Handle,
		Encode: func(out *reports.ConfigSetResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message, ReportConfig: &out.Config}, nil
		},
	}.Handler())
	hReportGenerate := reports.NewGenerate(reportStore, rec)
	server.Register(ipc.DomainAction[reports.GenerateInput, reports.GenerateResult]{
		Name: hReportGenerate.Action(),
		Decode: func(r *ipc.Request) (*reports.GenerateInput, error) {
			return &reports.GenerateInput{ExportPath: r.ReportExportPath}, nil
		},
		Handle: hReportGenerate.Handle,
		Encode: func(out *reports.GenerateResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message, ReportPath: out.HTMLPath}, nil
		},
	}.Handler())
	// achievements-get via ipc.DomainAction (Fase 5.2 — mesmo padrão do
	// composition root). O *analytics.Recorder satisfaz a Provider de sessões.
	hAchievements := achievements.New(rec)
	server.Register(ipc.DomainAction[achievements.NoInput, achievements.Result]{
		Name:   hAchievements.Action(),
		Decode: ipc.NoInputDecode[achievements.NoInput](),
		Handle: hAchievements.Handle,
		Encode: func(out *achievements.Result) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Achievements: out.Achievements}, nil
		},
	}.Handler())
	// dns-telemetry via ipc.DomainAction (DIP — pós-reorg item 2).
	hTelemetry := telemetry.NewGetHandler(telRec)
	server.Register(ipc.DomainAction[telemetry.TelemetryInput, telemetry.TelemetryResult]{
		Name: hTelemetry.Action(),
		Decode: func(r *ipc.Request) (*telemetry.TelemetryInput, error) {
			return &telemetry.TelemetryInput{Limit: r.TelemetryLimit}, nil
		},
		Handle: hTelemetry.Handle,
		Encode: func(out *telemetry.TelemetryResult) (*ipc.Response, error) {
			return &ipc.Response{
				Success:          true,
				TelemetryEntries: out.Entries,
				TelemetrySummary: out.Summary,
				TelemetryTotal:   out.TotalBlocked,
				TelemetryLimit:   out.Limit,
			}, nil
		},
	}.Handler())
	// users via ipc.DomainAction (DIP — pós-reorg item 2).
	hUsersList := users.NewList(userStore)
	server.Register(ipc.DomainAction[users.NoInput, users.ListResult]{
		Name:   hUsersList.Action(),
		Decode: ipc.NoInputDecode[users.NoInput](),
		Handle: hUsersList.Handle,
		Encode: func(out *users.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Users: out.Users}, nil
		},
	}.Handler())
	hUsersVerify := users.NewVerify(userStore)
	server.Register(ipc.DomainAction[users.VerifyInput, users.VerifyResult]{
		Name: hUsersVerify.Action(),
		Decode: func(r *ipc.Request) (*users.VerifyInput, error) {
			return &users.VerifyInput{UserName: r.UserName, UserPassword: r.UserPassword}, nil
		},
		Handle: hUsersVerify.Handle,
		Encode: func(out *users.VerifyResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, UserIsAdmin: out.UserIsAdmin}, nil
		},
	}.Handler())
	hUsersAdd := users.NewAdd(userStore)
	server.Register(ipc.DomainAction[users.AddInput, users.AddResult]{
		Name: hUsersAdd.Action(),
		Decode: func(r *ipc.Request) (*users.AddInput, error) {
			return &users.AddInput{UserName: r.UserName, UserPassword: r.UserPassword}, nil
		},
		Handle: hUsersAdd.Handle,
		Encode: func(out *users.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hUsersRemove := users.NewRemove(userStore)
	server.Register(ipc.DomainAction[users.RemoveInput, users.RemoveResult]{
		Name:   hUsersRemove.Action(),
		Decode: func(r *ipc.Request) (*users.RemoveInput, error) { return &users.RemoveInput{UserName: r.UserName}, nil },
		Handle: hUsersRemove.Handle,
		Encode: func(out *users.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hUsersSetPassword := users.NewSetPassword(userStore)
	server.Register(ipc.DomainAction[users.SetPasswordInput, users.SetPasswordResult]{
		Name: hUsersSetPassword.Action(),
		Decode: func(r *ipc.Request) (*users.SetPasswordInput, error) {
			return &users.SetPasswordInput{UserName: r.UserName, UserPassword: r.UserPassword}, nil
		},
		Validate: hUsersSetPassword.Validate,
		Handle:   hUsersSetPassword.Handle,
		Encode: func(out *users.SetPasswordResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// apps registrado incondicionalmente (pg pode ser nil quando o process
	// guard não sobe — ex.: testes que o stubam): o refreshGuard do guardApps
	// já é no-op com guard nil, e o ValidateRegistry abaixo exige handler para
	// todos os specs (apps-list/add/remove) — sem eles o boot falharia.
	// Handlers de domínio com tipos próprios (DIP), adaptados ao wire via
	// ipc.DomainAction (pós-reorg item 2).
	ga := &guardApps{store: appsStore, guard: pg}
	hAppsList := apps.NewList(ga)
	server.Register(ipc.DomainAction[apps.NoInput, apps.ListResult]{
		Name:   hAppsList.Action(),
		Decode: ipc.NoInputDecode[apps.NoInput](),
		Handle: hAppsList.Handle,
		Encode: func(out *apps.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Apps: out.Apps}, nil
		},
	}.Handler())
	hAppsAdd := apps.NewAdd(ga)
	server.Register(ipc.DomainAction[apps.AddInput, apps.AddResult]{
		Name:   hAppsAdd.Action(),
		Decode: func(r *ipc.Request) (*apps.AddInput, error) { return &apps.AddInput{AppName: r.AppName}, nil },
		Handle: hAppsAdd.Handle,
		Encode: func(out *apps.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hAppsRemove := apps.NewRemove(ga)
	server.Register(ipc.DomainAction[apps.RemoveInput, apps.RemoveResult]{
		Name:   hAppsRemove.Action(),
		Decode: func(r *ipc.Request) (*apps.RemoveInput, error) { return &apps.RemoveInput{AppName: r.AppName}, nil },
		Handle: hAppsRemove.Handle,
		Encode: func(out *apps.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// analytics/stats via ipc.DomainAction (DIP — pós-reorg item 1).
	hStats := analytics.NewStatsHandler(rec)
	server.Register(ipc.DomainAction[analytics.StatsInput, analytics.StatsResult]{
		Name: hStats.Action(),
		Decode: func(r *ipc.Request) (*analytics.StatsInput, error) {
			return &analytics.StatsInput{Mission: r.Mission}, nil
		},
		Handle: hStats.Handle,
		Encode: func(out *analytics.StatsResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Stats: out.Stats}, nil
		},
	}.Handler())
	hMissions := analytics.NewMissionsHandler(rec)
	server.Register(ipc.DomainAction[analytics.NoInput, analytics.MissionsResult]{
		Name:   hMissions.Action(),
		Decode: ipc.NoInputDecode[analytics.NoInput](),
		Handle: hMissions.Handle,
		Encode: func(out *analytics.MissionsResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, LabelStats: out.LabelStats}, nil
		},
	}.Handler())
	hSessions := analytics.NewSessionsHandler(rec)
	server.Register(ipc.DomainAction[analytics.NoInput, analytics.SessionsResult]{
		Name:   hSessions.Action(),
		Decode: ipc.NoInputDecode[analytics.NoInput](),
		Handle: hSessions.Handle,
		Encode: func(out *analytics.SessionsResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Sessions: out.Sessions}, nil
		},
	}.Handler())
	// schedule via ipc.DomainAction (DIP — pós-reorg item 1). O presetStore
	// satisfaz o PresetResolver do serviço (Resolve → preset.Preset).
	hScheduleList := schedule.NewListHandler(scheduleMgr, presetStore)
	server.Register(ipc.DomainAction[schedule.NoInput, schedule.ListResult]{
		Name:   hScheduleList.Action(),
		Decode: ipc.NoInputDecode[schedule.NoInput](),
		Handle: hScheduleList.Handle,
		Encode: func(out *schedule.ListResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Schedules: out.Schedules}, nil
		},
	}.Handler())
	hScheduleAdd := schedule.NewAddHandler(scheduleMgr, presetStore)
	server.Register(ipc.DomainAction[schedule.AddInput, schedule.AddResult]{
		Name:   hScheduleAdd.Action(),
		Decode: func(r *ipc.Request) (*schedule.AddInput, error) { return &schedule.AddInput{Rule: r.ScheduleRule}, nil },
		Handle: hScheduleAdd.Handle,
		Encode: func(out *schedule.AddResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	hScheduleImport := schedule.NewImportHandler(scheduleMgr, presetStore)
	server.Register(ipc.DomainAction[schedule.ImportInput, schedule.ImportResult]{
		Name: hScheduleImport.Action(),
		Decode: func(r *ipc.Request) (*schedule.ImportInput, error) {
			return &schedule.ImportInput{ICSContent: r.ICSContent, ICSPreset: r.ICSPreset}, nil
		},
		Handle: hScheduleImport.Handle,
		Encode: func(out *schedule.ImportResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Schedules: out.Schedules, Message: out.Message}, nil
		},
	}.Handler())
	hScheduleRemove := schedule.NewRemoveHandler(scheduleMgr, presetStore)
	server.Register(ipc.DomainAction[schedule.RemoveInput, schedule.RemoveResult]{
		Name: hScheduleRemove.Action(),
		Decode: func(r *ipc.Request) (*schedule.RemoveInput, error) {
			return &schedule.RemoveInput{ScheduleID: r.ScheduleID}, nil
		},
		Handle: hScheduleRemove.Handle,
		Encode: func(out *schedule.RemoveResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message}, nil
		},
	}.Handler())
	// pomodoro via ipc.DomainAction (DIP — pós-reorg item 1).
	hPomodoro := pomodoro.NewStartHandler(pomo, pomoPrefs, presetStore)
	server.Register(ipc.DomainAction[pomodoro.StartInput, pomodoro.StartResult]{
		Name: hPomodoro.Action(),
		Decode: func(r *ipc.Request) (*pomodoro.StartInput, error) {
			return &pomodoro.StartInput{
				Preset: r.Preset, Label: r.Label, WorkMin: r.WorkMin, RestMin: r.RestMin,
				Cycles: r.Cycles, Strict: r.Strict, Save: r.Save,
			}, nil
		},
		Handle: hPomodoro.Handle,
		Encode: func(out *pomodoro.StartResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message, Pomodoro: &out.State}, nil
		},
	}.Handler())
	hPomodoroDefaults := pomodoro.NewDefaultsHandler(pomoPrefs)
	server.Register(ipc.DomainAction[pomodoro.NoInput, pomodoro.DefaultsResult]{
		Name:   hPomodoroDefaults.Action(),
		Decode: ipc.NoInputDecode[pomodoro.NoInput](),
		Handle: hPomodoroDefaults.Handle,
		Encode: func(out *pomodoro.DefaultsResult) (*ipc.Response, error) {
			return &ipc.Response{
				Success:       true,
				PomodoroWork:  out.Work,
				PomodoroRest:  out.Rest,
				PomodoroCycle: out.Cycles,
				Message:       out.Message,
			}, nil
		},
	}.Handler())
	hPomodoroStop := pomodoro.NewStopHandler(pomo)
	server.Register(ipc.DomainAction[pomodoro.NoInput, pomodoro.StopResult]{
		Name:   hPomodoroStop.Action(),
		Decode: ipc.NoInputDecode[pomodoro.NoInput](),
		Handle: hPomodoroStop.Handle,
		Encode: func(out *pomodoro.StopResult) (*ipc.Response, error) {
			return &ipc.Response{Success: true, Message: out.Message, Pomodoro: &out.State}, nil
		},
	}.Handler())
	// update/update-check via ipc.DomainAction (DIP — pós-reorg item 1). O
	// checker é lido lazy do server (SetUpdateChecker roda depois do registro —
	// setupUpdateIntegration); o estado de processo (latch de restart + cache
	// do status) fica no Server, setado pelo wrapper abaixo.
	updateBridge := func() update.Checker {
		c := server.UpdateChecker()
		if c == nil {
			return nil
		}
		return updateCheckerBridge{c: c}
	}
	for _, apply := range []bool{true, false} {
		hUpdate := update.NewUpdateHandler(updateBridge, apply)
		server.Register(ipc.DomainAction[update.UpdateInput, update.Result]{
			Name:   hUpdate.Action(),
			Decode: func(r *ipc.Request) (*update.UpdateInput, error) { return &update.UpdateInput{Channel: r.Channel}, nil },
			Handle: func(ctx context.Context, in *update.UpdateInput) (*update.Result, error) {
				ctx, cancel := context.WithTimeout(ctx, ipc.UpdateTimeout)
				defer cancel()
				res, err := hUpdate.Handle(ctx, in)
				if err != nil {
					return nil, err
				}
				// Cache do status (a ação "status" o expõe) + latch de restart
				// (o roteador o consome APÓS escrever a resposta).
				server.CacheUpdateStatus(ipc.UpdateStatus{
					CurrentVersion: res.Status.CurrentVersion,
					NewVersion:     res.Status.NewVersion,
					Available:      res.Status.Available,
					Applied:        res.Status.Applied,
					PendingReboot:  res.Status.PendingReboot,
				})
				if res.Applied {
					server.MarkUpdateApplied()
				}
				return res, nil
			},
			Encode: func(out *update.Result) (*ipc.Response, error) {
				resp := &ipc.Response{
					Success:         true,
					UpdateAvailable: out.Status.Available,
					UpdateVersion:   out.Status.NewVersion,
					CurrentVersion:  out.Status.CurrentVersion,
					Message:         out.Message,
				}
				if apply {
					resp.UpdatePendingReboot = out.Status.PendingReboot
				}
				return resp, nil
			},
		}.Handler())
	}

	// Fechamento specs↔registry no boot (Fase 4): todo handler registrado tem
	// ActionSpec (user-verify isento — web-only) e todo spec tem handler.
	// Drift vira falha de boot, não 403/404 silencioso em runtime.
	if err := server.ValidateRegistry(); err != nil {
		log.Printf("[FocusGuard Daemon] Registry fora do contrato specs: %v", err)
		stopLifecycleComponents(components)
		return false
	}

	if stopUpdate := setupUpdateIntegration(server); stopUpdate != nil {
		components = append(components, daemon.StopOnly(stopUpdate))
		// Após aplicar um update, o daemon encerra a sessão pomodoro e os
		// bloqueios ativos e reinicia sozinho (osExit(1)) para o systemd/SCM
		// subirem a nova versão — em vez de ficar rodando o binário antigo em
		// RAM até o usuário reiniciar a máquina.
		server.SetOnUpdateApplied(func() { restartAfterUpdate(sched, pomo.Stop) })
	}

	// Lifecycle (Fase 5 — B10): o internal/system/daemon executa o servidor IPC e o
	// shutdown ordenado (componentes parados na ordem inversa de registro) com
	// Run(ctx) explícito — no lugar dos defers e da goroutine de sinais
	// manuais. Sinais (SIGINT/SIGTERM) e a parada do serviço (serviceStopCh)
	// são tratados dentro do Run: a parada só é honrada quando não há
	// bloqueios/sessão ativos (CanStop) — com bloqueios ativos o pedido é
	// ignorado e a proteção continua.
	err = daemon.New(daemon.Deps{
		Server:     &ipcServerLifecycle{server: server},
		Components: components,
		Stop:       serviceStopCh,
		CanStop:    func() bool { return !sched.HasActiveBlocks() && !server.HasActiveSession() },
	}).Run(context.Background())

	if err != nil {
		// Bind em uso por outra instância (a varredura de singleton acima já
		// cobre o caso comum; o bind ainda pode falhar por uma corrida) —
		// encerra limpo em vez de crash-loopar.
		if isAddrInUse(err) && probeDaemonAlive() {
			log.Println("[FocusGuard Daemon] Outra instância já está ativa na porta IPC — encerrando esta (evita crash-loop no bind).")
			return true
		}
		log.Printf("[FocusGuard Daemon] Servidor IPC encerrado inesperadamente: %v", err)
		return false
	}

	log.Println("[FocusGuard Daemon] Servidor IPC finalizado.")
	return true
}
