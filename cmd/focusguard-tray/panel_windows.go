//go:build windows

package main

import (
	"io"
	"os/exec"
	"syscall"
)

// startCli inicia o CLI "focusguard web" sem janela de console (HideWindow) e
// retorna imediatamente — o CLI sobe o focusguard-web por demanda e abre o
// navegador, sem deixar um terminal visível preso ao tray.
func startCli(cli string) {
	cmd := exec.Command(cli, "web")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Start()
}
