package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// watchdogSvcName é o nome do serviço do watchdog externo (Windows) — o
	// mesmo usado pelo internal/autostart (sc create FocusGuardWatchdog).
	watchdogSvcName = "FocusGuardWatchdog"
	// trayProcName é a imagem do processo do tray (GUI, sem serviço SCM).
	trayProcName = "focusguard-tray.exe"
)

// stopForBinarySwap para tudo que manteria um .exe travado durante a troca
// dos binários no Windows — o serviço do watchdog (cujo binário também é
// substituído) e o processo do tray (GUI de sessão do usuário, que não é
// supervisionado pelo SCM e não pode ser trocado em execução) — e devolve um
// restore que religa o watchdog se ele estava rodando. No Linux um binário em
// execução pode ser renomeado livremente, então é um no-op. Tudo best-effort:
// falha em parar não aborta o update (o rename retry + fallback move-on-reboot
// cobrem o caso residual). Stubbable nos testes para não tocar em serviços ou
// processos reais.
var stopForBinarySwap = func(binaries []string) func() {
	if runtime.GOOS != "windows" {
		return func() {}
	}

	wdState := serviceState(watchdogSvcName)
	if wdState == "running" {
		if err := runSC("stop", watchdogSvcName); err != nil {
			log.Printf("[FocusGuard Daemon] Aviso: não foi possível parar o watchdog para o update: %v", err)
		} else {
			log.Println("[FocusGuard Daemon] Watchdog parado antes da troca dos binários.")
		}
	} else if wdState == "" {
		log.Println("[FocusGuard Daemon] Serviço do watchdog não está instalado — seguindo sem parar.")
	}

	// O tray roda na sessão do usuário e não é gerenciado pelo SCM: sem o
	// taskkill o exe fica travado e o rename falha com "Acesso negado". Só o
	// encerra quando o tray está na lista de binários do update (o parâmetro
	// existe para isso — e como seam de stub nos testes).
	if includesBinary(binaries, trayProcName) {
		killProcessByName(trayProcName)
		waitForProcessExit(trayProcName, 5*time.Second)
	}

	return func() {
		if wdState == "running" {
			if err := runSC("start", watchdogSvcName); err != nil {
				log.Printf("[FocusGuard Daemon] Aviso: não foi possível religar o watchdog pós-update: %v", err)
			} else {
				log.Println("[FocusGuard Daemon] Watchdog religado após o update.")
			}
		}
	}
}

// includesBinary reports whether the update list contains a binary whose base
// name matches procName (used to decide whether the tray must be stopped).
func includesBinary(binaries []string, procName string) bool {
	for _, b := range binaries {
		if strings.EqualFold(filepath.Base(b), procName) {
			return true
		}
	}
	return false
}

// runSC executa um subcomando do `sc` (Service Control) e loga falhas. O `sc`
// existe apenas no Windows — chamado somente quando runtime.GOOS == "windows".
func runSC(args ...string) error {
	out, err := exec.Command("sc", args...).CombinedOutput()
	if err != nil {
		log.Printf("[FocusGuard Daemon] sc %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return err
}

// serviceState consulta o estado de um serviço Windows via `sc query`:
// "running", "stopped" ou "" (não existe / não foi possível consultar).
func serviceState(name string) string {
	out, err := exec.Command("sc", "query", name).CombinedOutput()
	if err != nil {
		return ""
	}
	s := strings.ToLower(string(out))
	switch {
	case strings.Contains(s, "running"):
		return "running"
	case strings.Contains(s, "stopped"):
		return "stopped"
	}
	return "unknown"
}

// killProcessByName encerra todos os processos com a imagem dada (taskkill
// /f /im). Best-effort: "processo não encontrado" é silencioso.
func killProcessByName(name string) {
	out, err := exec.Command("taskkill", "/f", "/im", name).CombinedOutput()
	if err != nil {
		s := strings.ToLower(string(out))
		if !strings.Contains(s, "não está em execução") &&
			!strings.Contains(s, "not running") &&
			!strings.Contains(s, "no tasks") &&
			!strings.Contains(s, "could not find") {
			log.Printf("[FocusGuard Daemon] Aviso ao encerrar %s: %v (%s)", name, err, strings.TrimSpace(string(out)))
		}
		return
	}
	log.Printf("[FocusGuard Daemon] Processo %s encerrado antes da troca dos binários.", name)
}

// waitForProcessExit sonda a imagem até ela sumir do tasklist ou o timeout
// expirar (aí segue best-effort — o rename retry cobre o caso residual).
func waitForProcessExit(name string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tasklist", "/fi", "imagename eq "+name, "/fo", "csv", "/nh").Output()
		if err != nil || !strings.Contains(strings.ToLower(string(out)), strings.ToLower(name)) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	log.Printf("[FocusGuard Daemon] Aviso: %s ainda não encerrou após %v — seguindo best-effort.", name, timeout)
}
