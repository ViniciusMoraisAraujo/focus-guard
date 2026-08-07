//go:build windows || (linux && cgo)

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"focusguard/internal/infrastructure/autostart"
	"focusguard/internal/system/tray"
	"focusguard/internal/transport/ipc"
)

func main() {
	stopLog := setupLogging()
	defer stopLog()

	log.Println("[FocusGuard Tray] Tray iniciado.")

	// Auto-registro do tray no HKCU Run do usuário logado. O .msi (instalação
	// per-machine roda como SYSTEM) não consegue gravar a chave Run do usuário
	// real, então o tray se registra no primeiro launch — garantindo que ele
	// volte a iniciar com o Windows para aquele usuário. Best-effort: falha não
	// impede o tray de abrir.
	ensureTrayAutostart()

	ctrl := tray.NewController(tray.NewSystray(), ipc.NewClient(), openPanel)
	ctrl.Run()
}

// ensureTrayAutostart registra o tray na inicialização do usuário (HKCU Run)
// quando instalado em Program Files e ainda não registrado. Usa o caminho do
// diretório de instalação quando o sistema está instalado lá (EnsureInInstallDir
// copia o binário se necessário e retorna o caminho canônico).
func ensureTrayAutostart() {
	if runtime.GOOS != "windows" {
		return
	}
	installed, err := autostart.IsTrayInstalled()
	if err == nil && installed {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	path, err := autostart.EnsureInInstallDir(exe)
	if err != nil || path == "" {
		return
	}
	if err := autostart.InstallTray(path); err != nil {
		log.Printf("[FocusGuard Tray] Aviso: não foi possível registrar o autostart: %v", err)
	}
}

// openPanel abre a interface web no navegador. O comando "focusguard web"
// (sem argumentos também) sonda o focusguard-web, sobe o servidor por demanda
// se necessário e abre o navegador padrão. O spawn do CLI é específico por
// plataforma (startCli): no Windows sem janela de console (HideWindow), no
// Linux em nova sessão (setsid), para não deixar um terminal visível preso ao
// tray.
func openPanel() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cli := filepath.Join(filepath.Dir(exe), "focusguard")
	if runtime.GOOS == "windows" {
		cli += ".exe"
	}
	startCli(cli)
}
