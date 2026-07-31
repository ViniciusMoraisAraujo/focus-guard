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

	"focusguard/internal/enforcer"
	"focusguard/internal/hostswatch"
	"focusguard/internal/ipc"
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

var goos = runtime.GOOS
var newHostswatch = hostswatch.New
var newStatewatch = statewatch.New
var newUpdater = func(owner, repo string) updaterAPI {
	return update.NewUpdater(owner, repo, update.WithVersion(daemonVersion))
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
	UpdateTo(ctx context.Context, result *update.UpdateResult, binaryPath string) (string, error)
}

type daemonUpdater struct {
	u      updaterAPI
	binary string
}

func (d *daemonUpdater) Check(ctx context.Context, apply bool) (ipc.UpdateStatus, error) {
	st := ipc.UpdateStatus{CurrentVersion: daemonVersion}
	if d == nil || d.u == nil {
		return st, nil
	}

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
		if _, err := d.u.UpdateTo(ctx, res, d.binary); err != nil {
			return st, fmt.Errorf("falha ao aplicar atualização: %w", err)
		}
		st.Applied = true
	}
	return st, nil
}

func setupUpdateIntegration(server *ipc.Server) func() {
	if daemonVersion == "" || strings.HasSuffix(daemonVersion, "-dev") {
		log.Println("[FocusGuard Daemon] Auto-update desativado (versão de desenvolvimento).")
		return nil
	}

	binaryPath, err := os.Executable()
	if err != nil {
		log.Printf("[FocusGuard Daemon] Auto-update desativado: %v", err)
		return nil
	}

	server.SetUpdateChecker(&daemonUpdater{u: newUpdater(updateOwner, updateRepo), binary: binaryPath})
	return startPeriodicUpdateCheck(server, updateCheckInterval)
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

	server := ipc.NewServer(sched)
	if stopUpdate := setupUpdateIntegration(server); stopUpdate != nil {
		defer stopUpdate()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		for {
			select {
			case sig := <-sigChan:
				if sched.HasActiveBlocks() {
					log.Printf("[FocusGuard Daemon] Sinal %v ignorado: existem bloqueios ativos.", sig)
					continue
				}
				log.Println("[FocusGuard Daemon] Nenhum bloqueio ativo. Encerrando servidor IPC...")
			case <-serviceStopCh:
				if sched.HasActiveBlocks() {
					log.Println("[FocusGuard Daemon] Parada do serviço ignorada: existem bloqueios ativos.")
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
