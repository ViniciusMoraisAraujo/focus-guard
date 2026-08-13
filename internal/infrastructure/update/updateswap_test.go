package update

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestIncludesBinary_TrayDecision cobre a decisão de parar o tray antes do
// swap (bug-hunt Etapa 7): o StopForBinarySwap encerra o processo do tray
// SOMENTE quando a lista de binários do update o contém (o tray roda na
// sessão do usuário e não é gerenciado pelo SCM — sem o taskkill o exe fica
// travado e o rename falha). A comparação é por base name com EqualFold (a
// case-insensitivity é o comportamento do filepath no Windows; no Linux o
// EqualFold cobre o case do próprio nome). Os caminhos são montados com o
// separador da plataforma — no Linux um caminho com `\` é um nome de arquivo
// puro e a comparação de base não faria sentido.
func TestIncludesBinary_TrayDecision(t *testing.T) {
	winDir := filepath.Join("C:\\", "FocusGuard")
	posixDir := "/opt/focusguard"

	cases := []struct {
		name      string
		binaries  []string
		procName  string
		wantFound bool
	}{
		{
			name:      "tray presente na lista por base name",
			binaries:  []string{filepath.Join(winDir, "focusguard-daemon.exe"), filepath.Join(winDir, "focusguard-tray.exe")},
			procName:  "focusguard-tray.exe",
			wantFound: true,
		},
		{
			name:      "tray presente com outro case",
			binaries:  []string{filepath.Join(winDir, "FocusGuard-TRAY.EXE")},
			procName:  "focusguard-tray.exe",
			wantFound: true,
		},
		{
			name:      "tray presente com caminho POSIX",
			binaries:  []string{filepath.Join(posixDir, "focusguard-tray")},
			procName:  "focusguard-tray.exe",
			wantFound: false, // base é focusguard-tray, não focusguard-tray.exe
		},
		{
			name:      "update sem tray na lista não o encerra",
			binaries:  []string{filepath.Join(posixDir, "focusguard-daemon"), filepath.Join(posixDir, "focusguard")},
			procName:  "focusguard-tray",
			wantFound: false,
		},
		{
			name:      "daemon não dispara a decisão do tray",
			binaries:  []string{filepath.Join(winDir, "focusguard-daemon.exe")},
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

	// No Linux, a decisão também é por base name com separador `/` — o tray
	// Linux (sem .exe) é casado quando a lista o contém.
	if runtime.GOOS == "linux" {
		if !includesBinary([]string{"/opt/focusguard/focusguard-tray", "/opt/focusguard/focusguard-daemon"}, "focusguard-tray") {
			t.Error("tray Linux na lista deveria casar por base name")
		}
	}
}

// TestIncludesBinary_WindowsCaseInsensitive is Windows-only: o StopForBinarySwap
// roda apenas no Windows, onde a imagem do processo (focusguard-tray.exe) casa
// com qualquer case do caminho na lista.
func TestIncludesBinary_WindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("EqualFold sobre nome de arquivo é o comportamento efetivo do Windows (procName real tem case fixo)")
	}
	if !includesBinary([]string{strings.ToUpper(`C:\FocusGuard\FOCUSGUARD-TRAY.EXE`)}, "focusguard-tray.exe") {
		t.Error("case diferente do nome do processo deveria casar")
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
