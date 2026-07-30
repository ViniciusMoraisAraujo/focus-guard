package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type cmdRunner interface {
	CombinedOutput() ([]byte, error)
}

var goos = runtime.GOOS

var execCommand = func(name string, args ...string) cmdRunner {
	return exec.Command(name, args...)
}

var osMkdirAll = os.MkdirAll
var osStat = os.Stat

var systemdServiceDir = func() string {
	return "/etc/systemd/system"
}

func Install(exePath string) error {
	if exePath == "" {
		return fmt.Errorf("autostart: caminho do executável não pode ser vazio")
	}
	if goos != "windows" {
		return fmt.Errorf("autostart: instalação via serviço Windows não é suportada em %s", goos)
	}
	return installWindows(exePath)
}

func Uninstall() error {
	if goos != "windows" {
		return fmt.Errorf("autostart: desinstalação via serviço Windows não é suportada em %s", goos)
	}
	return uninstallWindows()
}

func IsInstalled() (bool, error) {
	if goos == "windows" {
		return isInstalledWindows()
	}
	svcPath := filepath.Join(systemdServiceDir(), "focusguard.service")
	if _, err := osStat(svcPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func InstallSvc(exePath string) error {
	if exePath == "" {
		return fmt.Errorf("autostart: caminho do executável não pode ser vazio")
	}
	if goos != "linux" {
		return fmt.Errorf("autostart: instalação via systemd não é suportada em %s", goos)
	}
	return installSystemd(exePath)
}

func UninstallSvc() error {
	if goos != "linux" {
		return fmt.Errorf("autostart: desinstalação via systemd não é suportada em %s", goos)
	}
	return uninstallSystemd()
}
