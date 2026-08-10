//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// stateFilePath localiza o state.json na instalação Windows (%PROGRAMDATA%).
func stateFilePath() string {
	return filepath.Join(os.Getenv("PROGRAMDATA"), "FocusGuard", "state.json")
}

// hostsFilePath localiza o arquivo hosts do Windows.
func hostsFilePath() string {
	return `C:\Windows\System32\drivers\etc\hosts`
}

// isElevated reporta se o processo atual roda com privilégio de
// administrador. No Windows a forma stdlib confiável é tentar um comando que
// só um admin consegue executar (net session falha com acesso negado para
// usuários comuns).
func isElevated() bool {
	out, err := execCommand("net", "session").CombinedOutput()
	if err != nil {
		_ = out
		return false
	}
	return true
}

// queryService consulta o estado de um serviço Windows via sc query. Um
// serviço inexistente (exit 1060) vira serviceMissing; qualquer outra falha
// de execução é reportada como erro.
func queryService(exec func(string, ...string) ([]byte, error), name string) (serviceState, error) {
	out, err := exec("sc", "query", name)
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "1060") || strings.Contains(strings.ToLower(err.Error()), "1060") {
			return serviceMissing, nil
		}
		return serviceMissing, err
	}
	lower := strings.ToLower(string(out))
	if strings.Contains(lower, "running") {
		return serviceRunning, nil
	}
	return serviceInstalled, nil
}
