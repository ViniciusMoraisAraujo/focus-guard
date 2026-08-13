//go:build windows

package filelog

import (
	"path/filepath"
	"testing"
)

func TestUserLogPath_WindowsUsesProgramData(t *testing.T) {
	t.Setenv("PROGRAMDATA", `C:\ProgramData`)
	want := filepath.Join(`C:\ProgramData`, "FocusGuard", "focusguard-web.log")
	if got := UserLogPath("focusguard-web.log"); got != want {
		t.Errorf("UserLogPath = %q, want %q", got, want)
	}
}

func TestUserLogPath_WindowsFallbackDefaultProgramData(t *testing.T) {
	t.Setenv("PROGRAMDATA", "")
	want := filepath.Join(`C:\ProgramData`, "FocusGuard", "focusguard-web.log")
	if got := UserLogPath("focusguard-web.log"); got != want {
		t.Errorf("UserLogPath sem PROGRAMDATA = %q, want %q", got, want)
	}
}
