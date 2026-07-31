//go:build windows || (linux && cgo)

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"focusguard/internal/ipc"
	"focusguard/internal/tray"
)

func main() {
	ctrl := tray.NewController(tray.NewSystray(), ipc.NewClient(), openTUI)
	ctrl.Run()
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
