//go:build windows || (linux && cgo)

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"focusguard/internal/autostart"
	"focusguard/internal/ipc"
	"focusguard/internal/tray"
)

func main() {
	// Auto-registro do tray no HKCU Run do usuário logado. O .msi (instalação
	// per-machine roda como SYSTEM) não consegue gravar a chave Run do usuário
	// real, então o tray se registra no primeiro launch — garantindo que ele
	// volte a iniciar com o Windows para aquele usuário. Best-effort: falha não
	// impede o tray de abrir.
	ensureTrayAutostart()

	ctrl := tray.NewController(tray.NewSystray(), ipc.NewClient(), openTUI)
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

func openTUI() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cli := filepath.Join(filepath.Dir(exe), "focusguard")
	if runtime.GOOS == "windows" {
		cli += ".exe"
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("cmd", "/c", "start", "", cli).Start()
		return
	}
	for _, term := range [][]string{
		{"x-terminal-emulator", "-e", cli},
		{"gnome-terminal", "--", cli},
	} {
		if path, err := exec.LookPath(term[0]); err == nil {
			_ = exec.Command(path, term[1:]...).Start()
			return
		}
	}
}
