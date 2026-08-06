//go:build !linux && !windows

package store

import "errors"

func machineID() (string, error) {
	return "", errors.New("hardware ID não suportado nesta plataforma")
}
