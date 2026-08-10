package update

import (
	"path/filepath"
	"testing"
)

// TestIncludesBinary_TrayDecision cobre a decisão de parar o tray antes do
// swap (bug-hunt Etapa 7): o StopForBinarySwap encerra o processo do tray
// SOMENTE quando a lista de binários do update o contém (o tray roda na
// sessão do usuário e não é gerenciado pelo SCM — sem o taskkill o exe fica
// travado e o rename falha). A comparação é por base name, case-insensitive
// (Windows), com e sem caminho.
func TestIncludesBinary_TrayDecision(t *testing.T) {
	cases := []struct {
		name      string
		binaries  []string
		procName  string
		wantFound bool
	}{
		{
			name:      "tray presente na lista por base name",
			binaries:  []string{"C:\\FocusGuard\\focusguard-daemon.exe", "C:\\FocusGuard\\focusguard-tray.exe"},
			procName:  "focusguard-tray.exe",
			wantFound: true,
		},
		{
			name:      "tray presente com outro case (Windows)",
			binaries:  []string{"C:\\FocusGuard\\FocusGuard-TRAY.EXE"},
			procName:  "focusguard-tray.exe",
			wantFound: true,
		},
		{
			name:      "tray presente com caminho POSIX",
			binaries:  []string{"/opt/focusguard/focusguard-tray"},
			procName:  "focusguard-tray.exe",
			wantFound: false, // base é focusguard-tray, não focusguard-tray.exe
		},
		{
			name:      "update sem tray na lista não o encerra",
			binaries:  []string{"/opt/focusguard/focusguard-daemon", "/opt/focusguard/focusguard"},
			procName:  "focusguard-tray",
			wantFound: false,
		},
		{
			name:      "daemon não dispara a decisão do tray",
			binaries:  []string{"C:\\FocusGuard\\focusguard-daemon.exe"},
			procName:  "focusguard-tray.exe",
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := includesBinary(tc.binaries, tc.procName); got != tc.wantFound {
				t.Errorf("includesBinary(%v, %q) = %v, want %v", tc.binaries, tc.procName, got, tc.wantFound)
			}
		})
	}
}

// TestIncludesBinary_FilePathBase garante que a comparação usa filepath.Base
// (nunca uma substring do caminho completo — ex.: o caminho conter
// "focusguard-tray" numa pasta não pode casar sozinho).
func TestIncludesBinary_FilePathBase(t *testing.T) {
	binaries := []string{
		filepath.Join("C:\\", "FocusGuard", "logs", "focusguard-tray.old"),
	}
	if includesBinary(binaries, "focusguard-tray.exe") {
		t.Error("base focusguard-tray.old não deve casar com focusguard-tray.exe")
	}
	if !includesBinary(binaries, "focusguard-tray.old") {
		t.Error("base focusguard-tray.old deve casar com o próprio nome")
	}
}
