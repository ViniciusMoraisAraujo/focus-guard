package main

import (
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"focusguard/internal/ipc"
)

const (
	checkInterval = 10 * time.Second
	pingTimeout   = 5 * time.Second
	startupGrace  = 15 * time.Second
	daemonProc    = "focusguard-daemon.exe"
)

func main() {
	if runtime.GOOS == "windows" {
		if tryRunAsService() {
			return
		}
	}

	log.Println("[FocusGuard Watchdog] Iniciando em modo console...")
	watchLoop()
}

func watchLoop() {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	log.Printf("[FocusGuard Watchdog] Monitorando daemon a cada %v (timeout: %v)...\n", checkInterval, pingTimeout)

	// Sem dependência SCM no daemon, aguarda ele concluir o boot antes do
	// primeiro check para não matá-lo durante a inicialização.
	log.Printf("[FocusGuard Watchdog] Aguardando %v para o daemon concluir o boot...\n", startupGrace)
	time.Sleep(startupGrace)

	checkDaemon()

	for range ticker.C {
		checkDaemon()
	}
}

func checkDaemon() {
	if daemonResponds() {
		return
	}

	log.Println("[FocusGuard Watchdog] Daemon não respondeu — forçando reinicialização...")
	killDaemon()
}

func killDaemon() {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("taskkill", "/f", "/im", daemonProc)
		if out, err := cmd.CombinedOutput(); err != nil {
			outStr := strings.ToLower(string(out))
			if !strings.Contains(outStr, "não está em execução") &&
				!strings.Contains(outStr, "not running") &&
				!strings.Contains(outStr, "no tasks") &&
				!strings.Contains(outStr, "could not find") {
				log.Printf("[FocusGuard Watchdog] Erro ao matar daemon: %v (%s)", err, strings.TrimSpace(string(out)))
				return
			}
		}
		log.Println("[FocusGuard Watchdog] Daemon finalizado. Aguardando SCM reiniciar...")

	default:
		cmd := exec.Command("pkill", "-9", "focusguard-daemon")
		out, _ := cmd.CombinedOutput()
		if len(out) > 0 {
			log.Printf("[FocusGuard Watchdog] Saída do pkill: %s", strings.TrimSpace(string(out)))
		}
	}

	waitForDaemon()
}

func waitForDaemon() {
	log.Println("[FocusGuard Watchdog] Aguardando daemon ficar disponível (polling a cada 2s)...")

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if daemonResponds() {
			log.Println("[FocusGuard Watchdog] Daemon voltou a responder.")
			return
		}
		time.Sleep(2 * time.Second)
	}

	log.Println("[FocusGuard Watchdog] Aviso: daemon não respondeu após 60s de polling. Continuando ciclo normal...")
}

func daemonResponds() bool {
	client := ipc.NewClient()

	done := make(chan bool, 1)
	go func() {
		resp, err := client.Send(ipc.Request{Action: "ping"})
		if err == nil && resp.Success {
			done <- true
			return
		}
		done <- false
	}()

	select {
	case alive := <-done:
		return alive
	case <-time.After(pingTimeout):
		return false
	}
}

func tryRunAsService() bool {
	ok, err := isWindowsService()
	if err != nil {
		log.Printf("[FocusGuard Watchdog] Erro ao verificar modo serviço: %v", err)
		return false
	}
	if !ok {
		return false
	}

	log.Println("[FocusGuard Watchdog] Executando como serviço Windows...")
	runAsService()
	return true
}


