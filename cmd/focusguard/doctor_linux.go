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
// inexistente (exit 4 — "Unit .service could not be found") vira
// serviceMissing; um serviço instalado mas parado (exit 3 — "inactive"/
// "failed") vira serviceInstalled; qualquer outra falha de execução é
// reportada como erro (o doctor degrada para warn, nunca falha por um
// binário ausente).
func queryService(exec func(string, ...string) ([]byte, error), name string) (serviceState, error) {
	out, err := exec("systemctl", "is-active", name)
	if err == nil {
		return serviceRunning, nil
	}
	msg := strings.ToLower(string(out)) + " " + strings.ToLower(err.Error())
	// A mensagem real do systemd is-active para unit inexistente é "Unit
	// focusguard.service could not be found." — casa por "could not be found"
	// (a variante "not found" cobre outras localizações do marcador).
	if strings.Contains(msg, "could not be found") || strings.Contains(msg, "not found") {
		return serviceMissing, nil
	}
	// Exit 3 do systemctl is-active: unit existe, mas não está rodando
	// (inactive/failed). Outros exits sem o marcador de unit inexistente
	// (ex.: systemctl ausente) são erro de execução, não estado do serviço.
	if strings.Contains(msg, "exit status 3") || strings.Contains(msg, "inactive") || strings.Contains(msg, "failed") {
		return serviceInstalled, nil
	}
	return serviceInstalled, err
}
