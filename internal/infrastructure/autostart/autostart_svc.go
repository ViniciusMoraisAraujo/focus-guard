package autostart

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	serviceName         = "FocusGuard"
	watchdogServiceName = "FocusGuardWatchdog"
)

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

	// Recovery do SCM em no máximo 1 segundo (spec DNS sinkhole §5): como o
	// daemon é o DNS da rede, uma queda do processo derruba a internet da casa
	// — o restart/1000 ressuscita o serviço em ~1s. O watchdog (não-DNS) mantém
	// o 5000; o daemon NÃO, por ser crítico de rede.
	failureArgs := []string{
		"failure", serviceName,
		"reset=", "86400",
		"actions=", "restart/1000/restart/1000/restart/1000",
	}
	if out2, err2 := execCommand("sc", failureArgs...).CombinedOutput(); err2 != nil {
		log.Printf("[FocusGuard Daemon] Aviso: não foi possível configurar recuperação automática: %v (%s)", err2, strings.TrimSpace(string(out2)))
	}

	if out3, err3 := execCommand("sc", "start", serviceName).CombinedOutput(); err3 != nil {
		log.Printf("[FocusGuard Daemon] Aviso: serviço instalado, mas não iniciou: %v (%s)", err3, strings.TrimSpace(string(out3)))
	}

	return nil
}

func isServiceMissing(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "exit status 1060")
}

func uninstallWindows() error {
	execCommand("sc", "stop", serviceName).CombinedOutput()

	args := []string{"delete", serviceName}
	out, err := execCommand("sc", args...).CombinedOutput()
	if err != nil {
		if isServiceMissing(err) {
			return nil
		}
		return fmt.Errorf("autostart: falha ao remover serviço: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isInstalledWindows() (bool, error) {
	args := []string{"query", serviceName}

	out, err := execCommand("sc", args...).CombinedOutput()
	if err != nil {
		if isServiceMissing(err) {
			return false, nil
		}
		return false, fmt.Errorf("autostart: falha ao consultar serviço: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func installWatchdogWindows(exePath string) error {
	args := []string{
		"create", watchdogServiceName,
		"binPath=", exePath,
		"start=", "auto",
		"displayname=", "FocusGuard Watchdog",
	}
	out, err := execCommand("sc", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("autostart: falha ao criar serviço watchdog: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	execCommand("sc", "description", watchdogServiceName, "Monitora o daemon FocusGuard e o reinicia se não responder.").CombinedOutput()

	failureArgs := []string{
		"failure", watchdogServiceName,
		"reset=", "86400",
		"actions=", "restart/5000/restart/5000/restart/5000",
	}
	if out2, err2 := execCommand("sc", failureArgs...).CombinedOutput(); err2 != nil {
		log.Printf("[FocusGuard Watchdog] Aviso: não foi configurar recovery: %v (%s)", err2, strings.TrimSpace(string(out2)))
	}

	if out3, err3 := execCommand("sc", "start", watchdogServiceName).CombinedOutput(); err3 != nil {
		return fmt.Errorf("autostart: falha ao iniciar watchdog: %w (%s)", err3, strings.TrimSpace(string(out3)))
	}

	return nil
}

func uninstallWatchdogWindows() error {
	execCommand("sc", "stop", watchdogServiceName).CombinedOutput()

	args := []string{"delete", watchdogServiceName}
	out, err := execCommand("sc", args...).CombinedOutput()
	if err != nil {
		if isServiceMissing(err) {
			return nil
		}
		return fmt.Errorf("autostart: falha ao remover watchdog: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
