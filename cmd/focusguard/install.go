package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"focusguard/internal/infrastructure/autostart"
	"focusguard/internal/infrastructure/tlsca"
)

func daemonExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := "focusguard-daemon"
	if ext != "" {
		base += ext
	}
	return filepath.Join(dir, base)
}

func handleInstallCommand() {
	if runtime.GOOS == "linux" {
		path := daemonExePath()
		if path == "" {
			fmt.Println("Erro: Não foi possível determinar o caminho do daemon.")
			osExit(1)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("Erro: Daemon não encontrado em %s\n", path)
			fmt.Println("Compile o daemon primeiro: go build ./cmd/focusguard-daemon")
			osExit(1)
		}
		if err := autostart.InstallSvc(path); err != nil {
			fmt.Printf("Falha ao instalar: %v\n", err)
			osExit(1)
		}
		fmt.Printf("✔ Daemon instalado como serviço de inicialização.\n")
		fmt.Printf("  Binário: %s\n", path)
		fmt.Printf("  Iniciará automaticamente no próximo login.\n")
		return
	}

	// Windows: instala sempre em %ProgramFiles%\FocusGuard (Sistema / Todos os
	// Usuários) e cria um atalho na área de trabalho pública.
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("Erro: Não foi possível determinar o caminho do instalador: %v\n", err)
		osExit(1)
	}
	srcDir := filepath.Dir(exe)

	daemonSrc := filepath.Join(srcDir, "focusguard-daemon.exe")
	if _, err := os.Stat(daemonSrc); os.IsNotExist(err) {
		fmt.Printf("Erro: Daemon não encontrado em %s\n", daemonSrc)
		fmt.Println("Compile o daemon primeiro: go build ./cmd/focusguard-daemon")
		osExit(1)
	}

	installDir, err := autostart.InstallBinaries(srcDir)
	if err != nil {
		fmt.Printf("Falha ao copiar os binários para %s: %v\n", autostart.InstallDir(), err)
		fmt.Println("Se o daemon estiver em execução, rode 'focusguard uninstall' antes de reinstalar.")
		osExit(1)
	}

	daemonInstalled := filepath.Join(installDir, "focusguard-daemon.exe")
	if err := autostart.Install(daemonInstalled); err != nil {
		fmt.Printf("Falha ao instalar: %v\n", err)
		osExit(1)
	}

	cliInstalled := filepath.Join(installDir, "focusguard.exe")
	if err := autostart.CreateDesktopShortcut(cliInstalled); err != nil {
		fmt.Printf("⚠ Aviso: serviço instalado, mas não foi possível criar o atalho: %v\n", err)
	} else {
		fmt.Printf("✔ Atalho 'FocusGuard' criado na área de trabalho (Todos os Usuários).\n")
	}

	// Registra o tray na inicialização automaticamente, como faria o comando
	// install-tray (releases sempre incluem o tray; builds de dev podem não ter).
	trayInstalled := filepath.Join(installDir, "focusguard-tray.exe")
	if _, err := os.Stat(trayInstalled); err == nil {
		if err := autostart.InstallTray(trayInstalled); err != nil {
			fmt.Printf("⚠ Aviso: tray instalado, mas não foi possível registrar na inicialização: %v\n", err)
		} else {
			fmt.Printf("✔ Tray registrado para iniciar com o Windows (HKCU Run).\n")
		}
	} else if !os.IsNotExist(err) {
		fmt.Printf("⚠ Aviso: não foi possível verificar o tray: %v\n", err)
	}

	// Instala o watchdog externo (Windows) automaticamente, como faria o
	// comando install-watchdog (releases sempre incluem; builds de dev podem não).
	watchdogInstalled := filepath.Join(installDir, "focusguard-watchdog.exe")
	if _, err := os.Stat(watchdogInstalled); err == nil {
		if err := autostart.InstallWatchdog(watchdogInstalled); err != nil {
			fmt.Printf("⚠ Aviso: não foi possível instalar o watchdog: %v\n", err)
		} else {
			fmt.Printf("✔ Watchdog instalado como serviço e iniciado.\n")
		}
	} else if !os.IsNotExist(err) {
		fmt.Printf("⚠ Aviso: não foi possível verificar o watchdog: %v\n", err)
	}

	fmt.Printf("✔ Daemon instalado como serviço de inicialização.\n")
	fmt.Printf("  Instalação: %s\n", installDir)
	fmt.Printf("  Binário do daemon: %s\n", daemonInstalled)
	fmt.Printf("  Iniciará automaticamente no próximo login.\n")
}

func handleUninstallCommand() {
	// Higiene da âncora de confiança: o uninstall remove a CA local do trust
	// store do SO (best-effort — o uninstall já roda elevado). Sem isso, a CA
	// ficaria órfã na máquina, capaz de validar sites por anos.
	removeTrustedCAIfPresent()

	if runtime.GOOS == "linux" {
		if err := autostart.UninstallSvc(); err != nil {
			fmt.Printf("Falha ao desinstalar: %v\n", err)
			osExit(1)
		}
		fmt.Println("✔ Daemon removido da inicialização automática.")
		return
	}

	if err := autostart.Uninstall(); err != nil {
		fmt.Printf("Falha ao desinstalar: %v\n", err)
		osExit(1)
	}
	// Remove o watchdog e o tray (instalados automaticamente pelo install)
	// antes de limpar o diretório, para não deixar serviços órfãos.
	if err := autostart.UninstallWatchdog(); err != nil {
		fmt.Printf("⚠ Aviso: não foi possível remover o watchdog: %v\n", err)
	}
	if err := autostart.RemoveInstall(); err != nil {
		fmt.Printf("⚠ Aviso: serviço removido, mas não foi possível limpar a instalação: %v\n", err)
	}
	if err := autostart.UninstallTray(); err != nil {
		fmt.Printf("⚠ Aviso: não foi possível remover o tray da inicialização: %v\n", err)
	}
	fmt.Println("✔ Daemon removido da inicialização automática.")
}

// removeTrustedCAIfPresent remove a CA local do trust store do SO, se existir
// e estiver instalada. Best-effort como o resto do uninstall: a CA ausente é
// no-op e a falha de remoção só vira aviso (nunca aborta o uninstall).
func removeTrustedCAIfPresent() {
	caDir := caDirPath()
	if !tlsca.Exists(caDir) {
		return
	}
	ca, err := tlsca.LoadOrCreate(caDir)
	if err != nil {
		fmt.Printf("⚠ Aviso: não foi possível ler a CA local para removê-la do trust store: %v\n", err)
		return
	}
	if err := ca.RemoveFromStore(tlsca.DefaultStoreRunner()); err != nil {
		fmt.Printf("⚠ Aviso: não foi possível remover a CA do trust store: %v\n", err)
		return
	}
	fmt.Println("✔ CA do FocusGuard removida do trust store do sistema.")
}

func watchdogExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := "focusguard-watchdog"
	if ext != "" {
		base += ext
	}
	return filepath.Join(dir, base)
}

func handleInstallWatchdogCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Watchdog externo é exclusivo do Windows (Linux usa systemd watchdog).")
		return
	}

	path := watchdogExePath()
	if path == "" {
		fmt.Println("Erro: Não foi possível determinar o caminho do watchdog.")
		osExit(1)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Erro: Watchdog não encontrado em %s\n", path)
		fmt.Println("Compile o watchdog primeiro: go build ./cmd/focusguard-watchdog")
		osExit(1)
	}

	installed, err := autostart.EnsureInInstallDir(path)
	if err != nil {
		fmt.Printf("Falha ao copiar o watchdog para a instalação: %v\n", err)
		osExit(1)
	}
	if err := autostart.InstallWatchdog(installed); err != nil {
		fmt.Printf("Falha ao instalar watchdog: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Watchdog instalado como serviço e iniciado.")
}

func handleUninstallWatchdogCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Watchdog só está presente no Windows.")
		return
	}

	if err := autostart.UninstallWatchdog(); err != nil {
		fmt.Printf("Falha ao desinstalar watchdog: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Watchdog removido.")
}

func trayExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := "focusguard-tray"
	if ext != "" {
		base += ext
	}
	return filepath.Join(dir, base)
}

func handleInstallTrayCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Autostart do tray é exclusivo do Windows (chave Run HKCU).")
		return
	}

	path := trayExePath()
	if path == "" {
		fmt.Println("Erro: Não foi possível determinar o caminho do tray.")
		osExit(1)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Erro: Tray não encontrado em %s\n", path)
		fmt.Println("Compile o tray primeiro: go build ./cmd/focusguard-tray")
		osExit(1)
	}

	installed, err := autostart.EnsureInInstallDir(path)
	if err != nil {
		fmt.Printf("Falha ao copiar o tray para a instalação: %v\n", err)
		osExit(1)
	}
	if err := autostart.InstallTray(installed); err != nil {
		fmt.Printf("Falha ao registrar tray: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Tray registrado para iniciar com o Windows (HKCU Run).")
}

func handleUninstallTrayCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Autostart do tray é exclusivo do Windows.")
		return
	}

	if err := autostart.UninstallTray(); err != nil {
		fmt.Printf("Falha ao remover registro do tray: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Tray removido da inicialização do Windows.")
}
