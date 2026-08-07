package main

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"focusguard/internal/daemon"
	"focusguard/internal/domain/analytics"
	"focusguard/internal/domain/apps"
	"focusguard/internal/domain/blocks"
	"focusguard/internal/domain/goal"
	"focusguard/internal/domain/pomodoro"
	"focusguard/internal/domain/preset"
	"focusguard/internal/domain/presets"
	"focusguard/internal/domain/schedule"
	"focusguard/internal/domain/scheduler"
	"focusguard/internal/domain/user"
	"focusguard/internal/domain/users"
	"focusguard/internal/infrastructure/dns"
	"focusguard/internal/infrastructure/dnsserver"
	"focusguard/internal/infrastructure/enforcer"
	"focusguard/internal/infrastructure/hostswatch"
	"focusguard/internal/infrastructure/processguard"
	"focusguard/internal/infrastructure/statewatch"
	"focusguard/internal/infrastructure/store"
	"focusguard/internal/infrastructure/tamper"
	"focusguard/internal/infrastructure/update"
	"focusguard/internal/transport/eventhub"
	"focusguard/internal/transport/ipc"
	"focusguard/internal/transport/metrics"
	"focusguard/internal/watchdog"
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
// o mesmo teardown ordenado que o internal/daemon executa (B10). Usado nos
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
	// do lifecycle (Fase 5 — B10): o internal/daemon os para em ordem inversa
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
	server.SetDNS(dnsSrv)
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
	server.SetPomodoroPrefs(pomoPrefs)
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
	server.SetSchedules(scheduleMgr)
	stopSchedule := startScheduleWorker(scheduleMgr, presetResolver{store: presetStore}, sched, scheduleCheckInterval)
	if stopSchedule != nil {
		components = append(components, daemon.StopOnly(stopSchedule))
	}

	// Strict Mode & Analytics: histórico de sessões em JSONL ao lado do
	// state.json (best-effort — sem o arquivo o recorder fica em memória e
	// apenas perde o histórico entre restarts).
	rec := analytics.NewRecorder(filepath.Join(filepath.Dir(statePath), "analytics.jsonl"))
	pomo.SetRecorder(rec)
	server.SetAnalytics(rec)

	// Meta diária de foco (ex: 4h/dia) persistida em goal.json — alimenta o
	// "focusguard goal" e o status da TUI.
	goalStore := goal.NewStore(filepath.Join(filepath.Dir(statePath), "goal.json"))
	server.SetGoal(goalStore)

	// Composition root (Fase 5): as ações de domínio (block/block-all, apps-*,
	// goal-*, presets, preset-*, user-*, dns-*) são atendidas pelos handlers
	// dos pacotes de domínio (interfaces estreitas, DIP) — não pelos adapters
	// do ipc. O ipc.Server registra só os handlers de nível servidor
	// (ping/status/tamper/services); este bloco fecha o registry (34 ações)
	// que o ValidateRegistry abaixo verifica no boot.
	server.Register(blocks.New(sched, presetStore))
	server.Register(blocks.NewBlockAll(sched))
	server.Register(presets.NewList(presetStore))
	server.Register(presets.NewAdd(presetStore))
	server.Register(presets.NewRemove(presetStore))
	server.Register(goal.NewGet(goalStore))
	server.Register(goal.NewSet(goalStore))
	server.Register(dns.NewStart(dnsSrv, sched, dohHook))
	server.Register(dns.NewStop(dnsSrv, sched))
	server.Register(dns.NewStatus(dnsSrv, sched))
	server.Register(dns.NewSetUpstream(dnsSrv, sched))
	server.Register(users.NewList(userStore))
	server.Register(users.NewVerify(userStore))
	server.Register(users.NewAdd(userStore))
	server.Register(users.NewRemove(userStore))
	server.Register(users.NewSetPassword(userStore))
	// apps registrado incondicionalmente (pg pode ser nil quando o process
	// guard não sobe — ex.: testes que o stubam): o refreshGuard do guardApps
	// já é no-op com guard nil, e o ValidateRegistry abaixo exige handler para
	// todos os specs (apps-list/add/remove) — sem eles o boot falharia.
	ga := &guardApps{store: appsStore, guard: pg}
	server.Register(apps.NewList(ga))
	server.Register(apps.NewAdd(ga))
	server.Register(apps.NewRemove(ga))

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

	// Lifecycle (Fase 5 — B10): o internal/daemon executa o servidor IPC e o
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
