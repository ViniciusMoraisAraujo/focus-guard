//go:build windows

package store

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// machineID returns the Windows MachineGuid — a per-OS-install GUID under
// HKLM\SOFTWARE\Microsoft\Cryptography, unique per machine and stable for the
// lifetime of the installation.
func machineID() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	id, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("MachineGuid vazio")
	}
	return id, nil
}
