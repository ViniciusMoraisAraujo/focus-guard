package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"focusguard/internal/ipc"
	"focusguard/internal/recovery"
)

const (
	checkInterval = 30 * time.Second
	pingTimeout   = 5 * time.Second
	startupGrace  = 15 * time.Second
	daemonProc    = "focusguard-daemon.exe"

	// Smart Recovery (Feature 4): o watchdog restaura a versão anterior quando
	// o daemon crasha logo após um update aplicado e há um .bak recente — uma
	// release quebrada não deixa a proteção morta. minDowntime é a janela de
	// graça para o restart pós-update (o daemon sai sozinho e o SCM o reergue
	// em segundos): só decidimos o rollback depois que ele fica fora por mais
	// que dois ciclos de checagem, para nunca reverter uma atualização boa.
	minDowntime  = 2 * checkInterval // 60s — restart pós-update legítimo
	crashWindow  = 30 * time.Second
	backupMaxAge = 24 * time.Hour
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

// daemonTracker acompanha o estado do daemon entre os ciclos: quando ele
// respondeu pela última vez (para detectar crash rápido pós-update) e quando
// começou. Protegido por mutex para o caso de um rollback assíncrono.
type daemonTracker struct {
	mu            sync.Mutex
	lastHealthyAt time.Time
}

func (t *daemonTracker) markHealthy() {
	t.mu.Lock()
	t.lastHealthyAt = time.Now()
	t.mu.Unlock()
}

func (t *daemonTracker) lastHealthy() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastHealthyAt
}

func watchLoop() {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	log.Printf("[FocusGuard Watchdog] Monitorando daemon a cada %v (timeout: %v)...\n", checkInterval, pingTimeout)

	// Sem dependência SCM no daemon, aguarda ele concluir o boot antes do
	// primeiro check para não matá-lo durante a inicialização.
	log.Printf("[FocusGuard Watchdog] Aguardando %v para o daemon concluir o boot...\n", startupGrace)
	time.Sleep(startupGrace)

	tracker := &daemonTracker{}
	checkDaemon(tracker)

	for range ticker.C {
		checkDaemon(tracker)
	}
}

func checkDaemon(tracker *daemonTracker) {
	if daemonResponds() {
		tracker.markHealthy()
		return
	}

	log.Println("[FocusGuard Watchdog] Daemon não respondeu — forçando reinicialização...")

	// Smart Recovery: se o daemon caiu logo após o último start e há um .bak
	// recente (update aplicado), restaura a versão anterior antes de reiniciar.
	if restored, err := maybeRollback(tracker.lastHealthy()); err != nil {
		log.Printf("[FocusGuard Watchdog] Rollback automático falhou: %v", err)
	} else if restored {
		log.Println("[FocusGuard Watchdog] Binário restaurado do backup (versão anterior). Reiniciando...")
	}

	killDaemon()
}

// daemonBinaryPath localiza o binário do daemon ao lado do próprio watchdog
// (mesmo diretório, mesmo sufixo). O watchdog externo roda de /usr/local/bin
// (Linux) ou junto ao daemon (Windows). Stubbable nos testes.
var daemonBinaryPath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join(".", daemonProc)
	}
	return filepath.Join(filepath.Dir(exe), "focusguard-daemon"+filepath.Ext(exe))
}

// maybeRollback aplica o Smart Recovery: restaura o backup recente do daemon
// quando ele crashou dentro da janela pós-update. Retorna se houve rollback.
// Stubbable nos testes (var, não const) para exercitar o fluxo sem tocar em
// binários reais.
var maybeRollback = func(lastHealthy time.Time) (bool, error) {
	if lastHealthy.IsZero() {
		return false, nil // nunca vimos o daemon saudável — não decide nada
	}
	return recovery.RecoverIfNeeded(
		daemonBinaryPath(),
		lastHealthy,
		time.Now(),
		minDowntime,
		crashWindow,
		backupMaxAge,
	)
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

// daemonResponds e killDaemon são stubbable nos testes (o watchdog real usa
// IPC + taskkill/pkill).
var daemonResponds = func() bool {
	client := ipc.NewClient()
	resp, err := client.SendWithTimeout(ipc.Request{Action: "ping"}, pingTimeout)
	return err == nil && resp.Success
}

var killDaemon = killDaemonImpl

// killDaemonImpl é a implementação real de killDaemon.
func killDaemonImpl() {
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
