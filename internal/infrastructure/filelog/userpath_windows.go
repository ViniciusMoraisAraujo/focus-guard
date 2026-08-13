//go:build windows

package filelog

import (
	"os"
	"path/filepath"
)

// UserLogPath returns a log path writable by a user-space FocusGuard binary
// (tray/web) when the install directory (C:\Program Files\FocusGuard) is not
// writable: the shared state dir %PROGRAMDATA%\FocusGuard — the same place
// the daemon keeps state.json.
func UserLogPath(fileName string) string {
	pd := os.Getenv("PROGRAMDATA")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "FocusGuard", fileName)
}
