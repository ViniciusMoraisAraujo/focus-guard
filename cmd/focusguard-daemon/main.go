package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/enforcer"
	"focusguard/internal/goal"
	"focusguard/internal/hostswatch"
	"focusguard/internal/ipc"
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
	"focusguard/internal/processguard"
	"focusguard/internal/schedule"
	"focusguard/internal/scheduler"
	"focusguard/internal/statewatch"
	"focusguard/internal/store"
	"focusguard/internal/update"
	"focusguard/internal/watchdog"
)

var daemonVersion = "0.0.0-dev"

const (
	updateOwner = "ViniciusMoraisAraujo"
	updateRepo  = "focus-guard"
)

var updateCheckInterval = 24 * time.Hour

// scheduleCheckInterval is how often the daemon re-evaluates recurring rules:
// fine enough to start a block within a minute of its window opening, cheap
// enough to never matter (a no-op ListBlocks when no rule is due).
var scheduleCheckInterval = 30 * time.Second

var goos = runtime.GOOS
var newHostswatch = hostswatch.New
var newStatewatch = statewatch.New
var newUpdater = func(owner, repo string) updaterAPI {
	return update.NewUpdater(owner, repo, update.WithVersion(daemonVersion))
}

// processGuardStarter is the subset of *processguard.Guard the daemon needs,
// kept as an interface so tests can stub the constructor without killing real
// processes.
type processGuardStarter interface {
	Start(isActive func() bool)
	Stop()
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

func startProcessGuard(sched processguard.ActivityChecker) processGuardStarter {
	pg := newProcessGuard(defaultProcessDenylist)
	if pg == nil {
		return nil
	}
	pg.Start(sched.HasActiveBlocks)
	return pg
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

func startHostswatch(enf hostswatch.Enforcer, sched hostswatch.Scheduler) *hostswatch.HostsWatcher {
	hw := newHostswatch(enf, sched)
	if err := hw.Start(); err != nil {
		log.Printf("[FocusGuard Daemon] Erro ao iniciar hostswatch: %v", err)
		return nil
	}
	log.Println("[FocusGuard Daemon] Hosts watcher ativo.")
	return hw
}

func startStatewatch(rec statewatch.Reconciler, statePath string) *statewatch.StateWatcher {
	sw := newStatewatch(rec, statePath)
	if err := sw.Start(); err != nil {
		log.Printf("[FocusGuard Daemon] Erro ao iniciar statewatch: %v", err)
		return nil
	}
	log.Println("[FocusGuard Daemon] State watcher ativo.")
	return sw
}

type updaterAPI interface {
	CheckForUpdate(ctx context.Context) (*update.UpdateResult, error)
	UpdateToAll(ctx context.Context, result *update.UpdateResult, binaries []string) ([]string, error)
	SetChannel(channel string)
}

type daemonUpdater struct {
	u        updaterAPI
	binaries []string
}

func (d *daemonUpdater) Check(ctx context.Context, apply bool, channel string) (ipc.UpdateStatus, error) {
	st := ipc.UpdateStatus{CurrentVersion: daemonVersion}
	if d == nil || d.u == nil {
		return st, nil
	}

	// Canal de release por request: "beta" opta por prereleases; o padrão
	// ("" / "stable") as ignora. O updater é compartilhado entre as conexões
	// IPC (cada uma em sua goroutine): SetChannel é atômico e a checagem usa
	// um snapshot consistente — em flips rápidos prevalece o último canal
	// escrito (last-writer-wins), semântica benigna e livre de data race.
	d.u.SetChannel(channel)

	res, err := d.u.CheckForUpdate(ctx)
	if err != nil {
		return st, err
	}
	if res == nil {
		return st, nil
	}

	st.Available = true
	st.NewVersion = res.Version
	if apply {
		// Bug 1 corrigido: atualiza a SUITE inteira (daemon + CLI + tray +
		// watchdog), não só o daemon. Um update parcial quebraria o protocolo
		// IPC (daemon novo ↔ CLI antiga). O rollback é atômico dentro do
		// UpdateToAll; qualquer falha aqui mantém Applied=false — e portanto
		// o daemon NÃO reinicia para um estado meio-atualizado.
		if _, err := d.u.UpdateToAll(ctx, res, d.binaries); err != nil {
			return st, fmt.Errorf("falha ao aplicar atualização: %w", err)
		}
		st.Applied = true
	}
	return st, nil
}

// siblingBinaries returns the paths of every FocusGuard binary expected next
// to the daemon (CLI, tray, watchdog) — same directory, same extension.
func siblingBinaries(daemonPath string) []string {
	dir := filepath.Dir(daemonPath)
	ext := filepath.Ext(daemonPath) // ".exe" no Windows, "" no Linux
	names := []string{"focusguard", "focusguard-daemon", "focusguard-tray", "focusguard-watchdog"}
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
	if daemonVersion == "" || strings.HasSuffix(daemonVersion, "-dev") {
		log.Println("[FocusGuard Daemon] Auto-update desativado (versão de desenvolvimento).")
		return nil
	}

	binaryPath, err := osExecutable()
	if err != nil {
		log.Printf("[FocusGuard Daemon] Auto-update desativado: %v", err)
		return nil
	}

	server.SetUpdateChecker(newDaemonUpdater(binaryPath, newUpdater(updateOwner, updateRepo)))
	return startPeriodicUpdateCheck(server, updateCheckInterval)
}

// osExit é stubbable nos testes (o main real usa os.Exit).
var osExit = os.Exit

// restartAfterUpdate é o callback registrado no server (SetOnUpdateApplied):
// depois que o update é aplicado com sucesso, o daemon encerra o processo para
// que o supervisor (systemd Restart=always no Linux, SCM recovery no Windows)
// o suba já com o binário novo — corrige o "daemon zumbi" que ficava rodando a
// versão antiga em RAM. Só sai se não houver bloqueios/sessão ativos: reiniciar
// no meio de um bloqueio derrubaria a proteção.
//
// O exit code é 1 (e não 0) de propósito: o SCM do Windows só aplica as ações
// de recovery (sc failure ... actions= restart) quando o serviço termina com
// falha; um exit 0 é tratado como parada limpa e o serviço ficaria morto até
// reboot. No Linux o systemd reinicia com qualquer código, então 1 é seguro.
func restartAfterUpdate(server *ipc.Server, sched interface{ HasActiveBlocks() bool }) {
	time.Sleep(500 * time.Millisecond) // deixa a resposta IPC chegar ao CLI
	if sched.HasActiveBlocks() || server.HasActiveSession() {
		log.Println("[FocusGuard Daemon] Update aplicado, mas há bloqueios/sessão ativos. O restart fica para quando expirarem.")
		return
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
	if tryRunAsService() {
		return
	}

	for {
		shouldExit := runDaemon()
		if shouldExit {
			log.Println("[FocusGuard Daemon] Encerrado normalmente (sem bloqueios ativos).")
			return
		}
		log.Println("[FocusGuard Daemon] Reiniciando...")
		time.Sleep(1 * time.Second)
	}
}

func runDaemon() bool {
	log.Println("[FocusGuard Daemon] Iniciando serviço...")

	statePath := getStateFilePath()
	st, err := store.NewStore(statePath)
	if err != nil {
		log.Printf("[FocusGuard Daemon] Erro ao criar store: %v", err)
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

	if err := sched.Start(); err != nil {
		log.Printf("[FocusGuard Daemon] Erro na reconciliação: %v", err)
		return false
	}
	log.Println("[FocusGuard Daemon] Estado reconciliado com sucesso.")

	watchdogSec := getWatchdogSec()
	wd := watchdog.New(sched, watchdogSec)
	go wd.Start()

	hw := startHostswatch(enf, sched)
	if hw != nil {
		defer hw.Stop()
		if notifier, ok := enf.(interface{ SetOnHostsWrite(func()) }); ok {
			notifier.SetOnHostsWrite(hw.MarkSelfWrite)
		}
	}

	sw := startStatewatch(sched, statePath)
	if sw != nil {
		defer sw.Stop()
		st.SetOnSave(sw.MarkSelfWrite)
	}

	// Process Guard: encerra executáveis da denylist (steam, discord) enquanto
	// houver sessão de foco ativa — a checagem de atividade é o HasActiveBlocks
	// do scheduler.
	pg := startProcessGuard(sched)
	if pg != nil {
		defer pg.Stop()
	}

	server := ipc.NewServer(sched)

	// Modo Pomodoro & Presets: ciclos de trabalho/descanso sobre as categorias
	// (--preset social, video, news, games). O controller bloqueia via o
	// scheduler; os blocos de trabalho expiram sozinhos pelos timers. Sessões
	// estritas (--strict) não podem ser encerradas antecipadamente e, ao
	// terminar, são registradas no analytics para o "focusguard stats".
	pomo := pomodoro.New(sched)
	server.SetPomodoro(pomo)

	// Presets personalizados do usuário: catálogo persistido (builtins + custom)
	// ao lado do state.json — "focusguard preset add/remove".
	presetStore := preset.NewStore(filepath.Join(filepath.Dir(statePath), "presets.json"))
	server.SetPresets(presetStore)

	// Agendamento recorrente: regras "bloquear social seg-sex 08:00-12:00"
	// persistidas em schedules.json. O worker de background aplica as janelas
	// vencidas a cada 30s (e no boot); o IPC expõe schedule add/list/remove.
	scheduleMgr := schedule.NewManager(filepath.Join(filepath.Dir(statePath), "schedules.json"))
	server.SetSchedules(scheduleMgr)
	stopSchedule := startScheduleWorker(scheduleMgr, presetResolver{store: presetStore}, sched, scheduleCheckInterval)
	if stopSchedule != nil {
		defer stopSchedule()
	}

	// Strict Mode & Analytics: histórico de sessões em JSONL ao lado do
	// state.json (best-effort — sem o arquivo o recorder fica em memória e
	// apenas perde o histórico entre restarts).
	rec := analytics.NewRecorder(filepath.Join(filepath.Dir(statePath), "analytics.jsonl"))
	pomo.SetRecorder(rec)
	server.SetAnalytics(rec)

	// Meta diária de foco (ex: 4h/dia) persistida em goal.json — alimenta o
	// "focusguard goal" e o status da TUI.
	server.SetGoal(goal.NewStore(filepath.Join(filepath.Dir(statePath), "goal.json")))

	if stopUpdate := setupUpdateIntegration(server); stopUpdate != nil {
		defer stopUpdate()
		// Bug 2 corrigido: após aplicar um update, o daemon se reinicia sozinho
		// (os.Exit(0)) para o systemd/watchdog subirem a nova versão — em vez
		// de ficar rodando o binário antigo em RAM até o usuário reiniciar a
		// máquina.
		server.SetOnUpdateApplied(func() { restartAfterUpdate(server, sched) })
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		for {
			select {
			case sig := <-sigChan:
				if sched.HasActiveBlocks() || server.HasActiveSession() {
					log.Printf("[FocusGuard Daemon] Sinal %v ignorado: existem bloqueios/sessão ativos.", sig)
					continue
				}
				log.Println("[FocusGuard Daemon] Nenhum bloqueio/sessão ativo. Encerrando servidor IPC...")
			case <-serviceStopCh:
				if sched.HasActiveBlocks() || server.HasActiveSession() {
					log.Println("[FocusGuard Daemon] Parada do serviço ignorada: existem bloqueios/sessão ativos.")
					continue
				}
				log.Println("[FocusGuard Daemon] Serviço parando. Encerrando servidor IPC...")
			}
			_ = server.Stop()
			return
		}
	}()

	serverErr := make(chan error, 1)
	go func() {
		log.Println("[FocusGuard Daemon] Servidor IPC ativo e aguardando requisições...")
		serverErr <- server.Start()
	}()

	err = <-serverErr
	if err != nil {
		log.Printf("[FocusGuard Daemon] Servidor IPC encerrado inesperadamente: %v", err)
		signal.Stop(sigChan)
		close(sigChan)
		return false
	}

	signal.Stop(sigChan)
	close(sigChan)
	log.Println("[FocusGuard Daemon] Servidor IPC finalizado.")
	return true
}
