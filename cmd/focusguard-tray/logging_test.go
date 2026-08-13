//go:build windows || (linux && cgo)

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLogPathFor_NextToExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("caminho com separador Windows só faz sentido no Windows")
	}
	got := logPathFor(`C:\Program Files\FocusGuard\focusguard-tray.exe`)
	want := filepath.Join(`C:\Program Files\FocusGuard`, logFileName)
	if got != want {
		t.Errorf("logPathFor = %q, want %q", got, want)
	}
}

func TestLogPath_FallsBackOnExecutableError(t *testing.T) {
	orig := osExecutable
	osExecutable = func() (string, error) { return "", os.ErrNotExist }
	defer func() { osExecutable = orig }()

	if got := logPath(); got != logFileName {
		t.Errorf("logPath = %q, want %q (fallback)", got, logFileName)
	}
}

func TestSetupLoggingAt_WritesToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)

	restore, err := setupLoggingAt(path)
	if err != nil {
		t.Fatalf("setupLoggingAt: %v", err)
	}
	defer restore()

	msg := "mensagem de teste do tray"
	log.Println("[FocusGuard Tray] " + msg)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), msg) {
		t.Errorf("log file %q não contém %q: %q", path, msg, data)
	}
}

func TestSetupLogging_FallsBackOnExecutableError(t *testing.T) {
	orig := osExecutable
	osExecutable = func() (string, error) { return "", os.ErrNotExist }
	defer func() { osExecutable = orig }()
	// setupLogging abre o arquivo com o nome puro no diretório do package
	// (fallback) — remove-o para não sujar a árvore de trabalho.
	defer os.Remove(logFileName)

	stop := setupLogging()
	defer stop()
	if stop == nil {
		t.Fatal("setupLogging deve sempre retornar uma func de restore")
	}
}

// TestFallbackLogPath_UsesUserStateDir cobre o fallback por plataforma: o
// diretório de instalação (C:\Program Files / /opt/focusguard) não é
// gravável por um processo user-space como o tray — o log de auditoria cai
// no state dir do usuário (%PROGRAMDATA%\FocusGuard no Windows, XDG state
// dir no Linux).
func TestFallbackLogPath_UsesUserStateDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("PROGRAMDATA", `C:\ProgramData`)
		want := filepath.Join(`C:\ProgramData`, "FocusGuard", logFileName)
		if got := fallbackLogPath(); got != want {
			t.Errorf("fallbackLogPath() = %q, want %q", got, want)
		}
		return
	}
	t.Setenv("XDG_STATE_HOME", "/home/usuario/.local/state")
	want := filepath.Join("/home/usuario/.local/state", "focusguard", logFileName)
	if got := fallbackLogPath(); got != want {
		t.Errorf("fallbackLogPath() = %q, want %q", got, want)
	}
}

// TestSetupLogging_FallsBackToWritableDir cobre o fluxo completo do fallback:
// caminho primário inacessível → log cai no fallback e a mensagem chega ao
// arquivo — sem tocar em state dir real.
func TestSetupLogging_FallsBackToWritableDir(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "missing-dir", logFileName)
	fbDir := t.TempDir()
	fb := filepath.Join(fbDir, logFileName)

	origPath, origFallback := logPathFn, fallbackLogPathFn
	logPathFn = func() string { return primary }
	fallbackLogPathFn = func() string { return fb }
	defer func() { logPathFn, fallbackLogPathFn = origPath, origFallback }()

	stop := setupLogging()
	defer stop()
	if stop == nil {
		t.Fatal("setupLogging deve retornar uma func de restore mesmo no fallback")
	}

	msg := "mensagem pós-fallback do tray"
	log.Println("[FocusGuard Tray] " + msg)

	data, err := os.ReadFile(fb)
	if err != nil {
		t.Fatalf("ReadFile(fallback %q): %v", fb, err)
	}
	if !strings.Contains(string(data), msg) {
		t.Errorf("fallback %q não contém %q: %q", fb, msg, data)
	}
	if _, err := os.Stat(primary); err == nil {
		t.Errorf("arquivo primário %q não deveria existir", primary)
	}
}
