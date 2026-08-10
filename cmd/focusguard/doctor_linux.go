//go:build !windows

package main

import (
	"os"
	"strings"
)

// stateFilePath localiza o state.json na instalação Linux.
func stateFilePath() string {
	return "/var/lib/focusguard/state.json"
}

// hostsFilePath localiza o arquivo hosts do Linux.
func hostsFilePath() string {
	return "/etc/hosts"
}

// isElevated reporta se o processo roda como root (Linux).
func isElevated() bool {
	return os.Geteuid() == 0
}

// queryService consulta o estado de um serviço via systemctl. Um serviço
// inexistente (unit not found) vira serviceMissing; qualquer outra falha de
// execução é reportada como erro.
func queryService(exec func(string, ...string) ([]byte, error), name string) (serviceState, error) {
	out, err := exec("systemctl", "is-active", name)
	if err == nil {
		return serviceRunning, nil
	}
	msg := strings.ToLower(string(out)) + " " + strings.ToLower(err.Error())
	if strings.Contains(msg, "not found") || strings.Contains(msg, "could not find") {
		return serviceMissing, nil
	}
	return serviceInstalled, nil
}
