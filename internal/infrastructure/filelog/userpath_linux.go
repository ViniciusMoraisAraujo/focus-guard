//go:build linux

package filelog

import (
	"os"
	"path/filepath"
)

// UserLogPath returns a log path writable by a user-space FocusGuard binary
// (tray/web) when the install directory (/opt/focusguard, root-only) is not
// writable: the XDG state dir (~/.local/state/focusguard, ou
// $XDG_STATE_HOME/focusguard quando definido) — sempre dentro do $HOME do
// usuário, ao contrário do /var/lib/focusguard do daemon (root-only). Sem
// home determinável, cai para o nome puro no diretório atual (defensivo).
func UserLogPath(fileName string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return fileName
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "focusguard", fileName)
}
