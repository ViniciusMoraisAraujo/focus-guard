package autostart

import (
	"fmt"
	"strings"
)

const (
	trayRunKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	trayRunValue = "FocusGuardTray"
)

func installTrayWindows(exePath string) error {
	args := []string{
		"add", trayRunKey,
		"/v", trayRunValue,
		"/t", "REG_SZ",
		"/d", exePath,
		"/f",
	}
	out, err := execCommand("reg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("autostart: falha ao registrar tray na inicialização: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func uninstallTrayWindows() error {
	args := []string{"delete", trayRunKey, "/v", trayRunValue, "/f"}
	out, err := execCommand("reg", args...).CombinedOutput()
	if err != nil {
		if isRegValueMissing(err) {
			return nil
		}
		return fmt.Errorf("autostart: falha ao remover registro do tray: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isTrayInstalledWindows() (bool, error) {
	args := []string{"query", trayRunKey, "/v", trayRunValue}
	out, err := execCommand("reg", args...).CombinedOutput()
	if err != nil {
		if isRegValueMissing(err) {
			return false, nil
		}
		return false, fmt.Errorf("autostart: falha ao consultar registro do tray: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func isRegValueMissing(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "exit status 1")
}
