//go:build windows || (linux && cgo)

package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogPathFor_NextToExecutable(t *testing.T) {
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
