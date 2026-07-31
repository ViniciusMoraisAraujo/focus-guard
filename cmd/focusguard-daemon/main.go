package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"focusguard/internal/enforcer"
	"focusguard/internal/hostswatch"
	"focusguard/internal/ipc"
	"focusguard/internal/scheduler"
	"focusguard/internal/statewatch"
	"focusguard/internal/store"
	"focusguard/internal/watchdog"
)

var daemonVersion = "0.0.0-dev"

var goos = runtime.GOOS
var newHostswatch = hostswatch.New
var newStatewatch = statewatch.New

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
	}

	sw := startStatewatch(sched, statePath)
	if sw != nil {
		defer sw.Stop()
	}

	server := ipc.NewServer(sched)

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
