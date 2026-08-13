//go:build linux

package filelog

import (
	"path/filepath"
	"testing"
)

func TestUserLogPath_LinuxUsesXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/home/usuario/.local/state")
	want := filepath.Join("/home/usuario/.local/state", "focusguard", "focusguard-tray.log")
	if got := UserLogPath("focusguard-tray.log"); got != want {
		t.Errorf("UserLogPath = %q, want %q", got, want)
	}
}

func TestUserLogPath_LinuxFallsBackToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/usuario")
	want := filepath.Join("/home/usuario", ".local", "state", "focusguard", "focusguard-tray.log")
	if got := UserLogPath("focusguard-tray.log"); got != want {
		t.Errorf("UserLogPath = %q, want %q", got, want)
	}
}
