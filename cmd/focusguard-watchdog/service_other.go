//go:build !windows

package main

// isWindowsService retorna false em plataformas não-Windows.
func isWindowsService() (bool, error) {
	return false, nil
}

// runAsService não faz nada em plataformas não-Windows.
func runAsService() {}
