package autostart

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const serviceName = "FocusGuard"

func installWindows(exePath string) error {
	stateDir := filepath.Join(os.Getenv("PROGRAMDATA"), "FocusGuard")
	if err := osMkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("autostart: falha ao criar diretório de estado: %w", err)
	}

	args := []string{
		"create", serviceName,
		"binPath=", exePath,
		"start=", "auto",
		"displayname=", "FocusGuard Daemon",
	}

	out, err := execCommand("sc", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("autostart: falha ao criar serviço: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// Configure auto-restart on failure (Windows recovery)
	failureArgs := []string{
		"failure", serviceName,
		"reset=", "86400",
		"actions=", "restart/5000/restart/10000/restart/30000",
	}
	if out2, err2 := execCommand("sc", failureArgs...).CombinedOutput(); err2 != nil {
		log.Printf("[FocusGuard Daemon] Aviso: não foi possível configurar recuperação automática: %v (%s)", err2, strings.TrimSpace(string(out2)))
	}

	return nil
}

func uninstallWindows() error {
	args := []string{"delete", serviceName}

	out, err := execCommand("sc", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("autostart: falha ao remover serviço: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isInstalledWindows() (bool, error) {
	args := []string{"query", serviceName}

	out, err := execCommand("sc", args...).CombinedOutput()
	if err != nil {
		errMsg := strings.ToLower(string(out))
		if strings.Contains(errMsg, "does not exist") || strings.Contains(errMsg, "não existe") || strings.Contains(errMsg, "failed 1060") {
			return false, nil
		}
		return false, fmt.Errorf("autostart: falha ao consultar serviço: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}
