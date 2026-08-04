//go:build linux && cgo

package main

import (
	"io"
	"os/exec"
	"syscall"
)

// startCli inicia o CLI "focusguard web" em uma nova sessão (setsid) e retorna
// imediatamente — o CLI sobe o focusguard-web por demanda e abre o navegador,
// sem ficar preso ao terminal do tray.
func startCli(cli string) {
	cmd := exec.Command(cli, "web")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Start()
}
