// focusguard-watchdog — Monitor externo do daemon FocusGuard
//
// Executa como um processo SEPARADO do daemon principal.
// Conecta via IPC, envia "ping" e, se o daemon não responder,
// força o kill do processo — o Service Control Manager (SCM)
// reinicia automaticamente (recovery configurado no instalador).
//
// Uso:
//   focusguard-watchdog   (modo console / foreground)
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"focusguard/internal/ipc"
)

const (
	checkInterval = 10 * time.Second
	pingTimeout   = 5 * time.Second
	daemonProc    = "focusguard-daemon.exe"
)

func main() {
	// Tentar rodar como serviço Windows primeiro
	if runtime.GOOS == "windows" {
		if tryRunAsService() {
			return
		}
	}

	// Modo console / foreground
	log.Println("[FocusGuard Watchdog] Iniciando em modo console...")
	watchLoop()
}

func watchLoop() {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	log.Printf("[FocusGuard Watchdog] Monitorando daemon a cada %v (timeout: %v)...\n", checkInterval, pingTimeout)

	// Verificação imediata na inicialização
	checkDaemon()

	for range ticker.C {
		checkDaemon()
	}
}

func checkDaemon() {
	if daemonResponds() {
		return // saudável
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

	// Polling progressivo: tenta pingar o daemon a cada 2s até 60s
	// Assim que ele responder, retorna — sem esperar um tempo fixo.
	waitForDaemon()
}

// waitForDaemon faz polling do daemon até ele responder ao ping.
// Tenta a cada 2 segundos, com timeout total de 60 segundos.
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

// daemonResponds tenta um ping no daemon e retorna true se saudável.
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

// tryRunAsService tenta rodar como serviço Windows.
// Retorna true se executou como serviço (a função não retorna até o serviço parar).
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

// isWindowsService e runAsService são implementadas em:
//   service_windows.go (Windows — integração SCM)
//   service_other.go   (outros — no-op)

// ─── Helpers para instalação (usados pelo CLI focusguard install-watchdog) ────

const ServiceName = "FocusGuardWatchdog"
const DaemonServiceName = "FocusGuard"

// InstallService registra o watchdog como serviço Windows.
// Chamado pelo CLI via sc create, não diretamente pelo watchdog.
func InstallService(watchdogExe string) error {
	if runtime.GOOS != "windows" {
		return nil // watchdog não é necessário no Linux (systemd já cobre)
	}

	if _, err := os.Stat(watchdogExe); os.IsNotExist(err) {
		return err
	}

	// Cria o serviço
	args := []string{
		"create", ServiceName,
		"binpath=", watchdogExe,
		"start=", "auto",
		"displayname=", "FocusGuard Watchdog",
		"description=", "Monitora o daemon FocusGuard e o reinicia se não responder.",
	}
	cmd := exec.Command("sc", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sc create falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// Dependência do daemon principal
	_ = exec.Command("sc", "config", ServiceName, "depend="+DaemonServiceName).Run()

	// Recovery: restart a cada 5s
	failArgs := []string{
		"failure", ServiceName,
		"reset=", "86400",
		"actions=", "restart/5000/restart/5000/restart/5000",
	}
	if out, err := exec.Command("sc", failArgs...).CombinedOutput(); err != nil {
		log.Printf("[FocusGuard Watchdog] Aviso: não foi configurar recovery: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	// Inicia o serviço
	if out, err := exec.Command("sc", "start", ServiceName).CombinedOutput(); err != nil {
		return fmt.Errorf("sc start falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// UninstallService remove o serviço watchdog.
func UninstallService() error {
	if runtime.GOOS != "windows" {
		return nil
	}

	_ = exec.Command("sc", "stop", ServiceName).Run()
	time.Sleep(1 * time.Second)

	if out, err := exec.Command("sc", "delete", ServiceName).CombinedOutput(); err != nil {
		return fmt.Errorf("sc delete falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
