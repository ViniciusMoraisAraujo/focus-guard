package main

import (
	"os/exec"
)

// execCommand adapta *exec.Cmd para o executor do doctor (CombinedOutput).
// Separado do doctor.go para os testes stubarem via doctorEnv.exec sem tocar
// em comandos reais do SO.
func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// serviceState é o estado de um serviço consultado (sc query / systemctl).
type serviceState int

const (
	// serviceMissing: não instalado.
	serviceMissing serviceState = iota
	// serviceInstalled: instalado, mas não rodando.
	serviceInstalled
	// serviceRunning: instalado e em execução.
	serviceRunning
)
