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
	got := logPathFor(`C:\Program Files\FocusGuard\focusguard-web.exe`)
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

	msg := "mensagem de teste do web"
	log.Println("[focusguard-web] " + msg)

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

func TestFallbackLogPath_WindowsUsesProgramData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("comportamento específico do Windows")
	}
	orig := os.Getenv("PROGRAMDATA")
	if orig == "" {
		os.Setenv("PROGRAMDATA", `C:\ProgramData`)
		defer os.Unsetenv("PROGRAMDATA")
	}
	want := filepath.Join(os.Getenv("PROGRAMDATA"), "FocusGuard", logFileName)
	if got := fallbackLogPath(); got != want {
		t.Errorf("fallbackLogPath() = %q, want %q", got, want)
	}
}

func TestSetupLogging_FallsBackToWritableDir(t *testing.T) {
	// Caminho primário inacessível (diretório inexistente) → o log cai no
	// fallback e a mensagem chega ao arquivo — sem tocar em ProgramData real.
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

	msg := "mensagem pós-fallback"
	log.Println("[focusguard-web] " + msg)

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
