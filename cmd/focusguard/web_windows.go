//go:build windows

package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
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

// killStaleWebServer encerra instâncias antigas do focusguard-web que estejam
// segurando a porta 48902 sem responder ao health (taskkill por imagem, todas
// as sessões). O CLI chama ANTES de subir uma instância nova: sem isso, cada
// spawn esbarra num bind em uso e morre em silêncio — o \"loop\" da edição
// Server. \"Nenhum processo encontrado\" não é erro.
func killStaleWebServer() error {
	out, err := exec.Command("taskkill", "/f", "/im", "focusguard-web.exe").CombinedOutput()
	if err == nil {
		return nil
	}
	low := strings.ToLower(string(out))
	if strings.Contains(low, "não está em execução") ||
		strings.Contains(low, "not running") ||
		strings.Contains(low, "no tasks") ||
		strings.Contains(low, "could not find") ||
		strings.Contains(low, "not found") {
		return nil // nada para encerrar
	}
	return fmt.Errorf("taskkill focusguard-web: %v (%s)", err, strings.TrimSpace(string(out)))
}
