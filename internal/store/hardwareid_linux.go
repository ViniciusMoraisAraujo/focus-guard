//go:build linux

package store

import (
	"errors"
	"os"
	"strings"
)

// machineID returns the stable machine identifier used to bind the replica key
// to this hardware: /etc/machine-id (systemd) with the dbus fallback.
func machineID() (string, error) {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		b, err := os.ReadFile(p)
		if err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id, nil
			}
		}
	}
	return "", errors.New("machine-id não encontrado")
}
