package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func installSystemd(exePath string) error {
	content := fmt.Sprintf(`[Unit]
Description=FocusGuard Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=always
# DNS da rede: ressuscitar o processo em ~1s se fechar (spec §5). O
# WatchdogSec=30 cobre freeze (o daemon alimenta via sd_notify a cada 15s).
RestartSec=1
WatchdogSec=30

[Install]
WantedBy=multi-user.target
`, exePath)

	svcPath := filepath.Join(systemdServiceDir(), "focusguard.service")
	if err := os.WriteFile(svcPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("autostart: falha ao criar serviço systemd: %w", err)
	}

	if out, err := execCommand("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("autostart: falha ao recarregar systemd: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	if out, err := execCommand("systemctl", "enable", "focusguard").CombinedOutput(); err != nil {
		return fmt.Errorf("autostart: falha ao habilitar serviço: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	if out, err := execCommand("systemctl", "start", "focusguard").CombinedOutput(); err != nil {
		return fmt.Errorf("autostart: falha ao iniciar serviço: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}

func uninstallSystemd() error {
	if out, err := execCommand("systemctl", "disable", "focusguard").CombinedOutput(); err != nil {
		errMsg := strings.ToLower(string(out))
		if !strings.Contains(errMsg, "not loaded") && !strings.Contains(errMsg, "não carregado") {
			return fmt.Errorf("autostart: falha ao desabilitar serviço: %w (%s)", err, strings.TrimSpace(string(out)))
		}
	}

	if out, err := execCommand("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("autostart: falha ao recarregar systemd: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	svcPath := filepath.Join(systemdServiceDir(), "focusguard.service")
	if err := os.Remove(svcPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("autostart: falha ao remover serviço systemd: %w", err)
	}
	return nil
}
