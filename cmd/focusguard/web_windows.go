//go:build windows

package main

import (
	"io"
	"os/exec"
	"syscall"
)

// spawnWebServer inicia o focusguard-web em segundo plano, sem janela de
// console, e retorna imediatamente. O processo continua vivo após o CLI sair
// (a interface fica disponível para o navegador).
func spawnWebServer(path string) error {
	cmd := exec.Command(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}
