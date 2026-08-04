package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogPathFor_LinuxInstallDir(t *testing.T) {
	got := logPathFor("/opt/focusguard/focusguard-daemon")
	want := filepath.Join("/opt/focusguard", logFileName)
	if got != want {
		t.Errorf("logPathFor = %q, want %q", got, want)
	}
}

func TestLogPathFor_WindowsInstallDir(t *testing.T) {
	got := logPathFor(`C:\Program Files\FocusGuard\focusguard-daemon.exe`)
	want := filepath.Join(`C:\Program Files\FocusGuard`, logFileName)
	if got != want {
		t.Errorf("logPathFor = %q, want %q", got, want)
	}
}

func TestLogPathFor_InstallDirIsExecutableDir(t *testing.T) {
	dir := filepath.Join(string(filepath.Separator), "opt", "focusguard")
	got := logPathFor(filepath.Join(dir, "focusguard-daemon"))
	want := filepath.Join(dir, logFileName)
	if got != want {
		t.Errorf("logPathFor = %q, want %q", got, want)
	}
}

func TestSetupLoggingAt_WritesToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)

	restore, err := setupLoggingAt(path)
	if err != nil {
		t.Fatalf("setupLoggingAt: %v", err)
	}
	defer restore()

	msg := "mensagem de teste do daemon"
	log.Println("[FocusGuard Daemon] " + msg)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), msg) {
		t.Errorf("log file %q não contém %q: %q", path, msg, data)
	}
	if !strings.Contains(string(data), "20") { // ano presente (LstdFlags)
		t.Errorf("esperava data/hora no log, got %q", data)
	}
}

func TestSetupLoggingAt_AppendsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)

	restore, err := setupLoggingAt(path)
	if err != nil {
		t.Fatalf("setupLoggingAt: %v", err)
	}
	log.Println("[FocusGuard Daemon] primeira execução")
	restore()

	restore, err = setupLoggingAt(path)
	if err != nil {
		t.Fatalf("setupLoggingAt: %v", err)
	}
	defer restore()
	log.Println("[FocusGuard Daemon] segunda execução")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "primeira execução") || !strings.Contains(text, "segunda execução") {
		t.Errorf("esperava append preservando as duas execuções, got %q", text)
	}
}

func TestSetupLoggingAt_RestoresPreviousOutput(t *testing.T) {
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)

	restore, err := setupLoggingAt(path)
	if err != nil {
		t.Fatalf("setupLoggingAt: %v", err)
	}
	log.Println("[FocusGuard Daemon] vai para o arquivo")
	restore()

	log.Println("[FocusGuard Daemon] volta para o buffer")
	if !strings.Contains(buf.String(), "volta para o buffer") {
		t.Error("após restore, o log deveria voltar ao output anterior")
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "volta para o buffer") {
		t.Error("mensagem pós-restore não deveria ir para o arquivo")
	}
}

func TestSetupLoggingAt_FailsOnBadPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "subdir", logFileName)
	if _, err := setupLoggingAt(path); err == nil {
		t.Fatal("expected error when the parent directory does not exist")
	}
}

func TestSetupLoggingAt_RotatesOversizedLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)

	origMax := maxLogSizeBeforeRotate
	maxLogSizeBeforeRotate = 1024
	defer func() { maxLogSizeBeforeRotate = origMax }()

	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 4096), 0644); err != nil {
		t.Fatalf("seed oversized log: %v", err)
	}

	restore, err := setupLoggingAt(path)
	if err != nil {
		t.Fatalf("setupLoggingAt: %v", err)
	}
	defer restore()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error("esperava o log antigo rotacionado para .1")
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Errorf("esperava um log novo vazio, got err=%v size=%d", err, info.Size())
	}
}

func TestSetupLoggingAt_DoesNotRotateSmallLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)

	origMax := maxLogSizeBeforeRotate
	maxLogSizeBeforeRotate = 1024
	defer func() { maxLogSizeBeforeRotate = origMax }()

	if err := os.WriteFile(path, []byte("log pequeno"), 0644); err != nil {
		t.Fatalf("seed small log: %v", err)
	}

	restore, err := setupLoggingAt(path)
	if err != nil {
		t.Fatalf("setupLoggingAt: %v", err)
	}
	defer restore()

	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Error("log pequeno não deveria ser rotacionado")
	}
}

func TestSetupLogging_FallsBackOnExecutableError(t *testing.T) {
	orig := osExecutable
	osExecutable = func() (string, error) {
		return "", os.ErrNotExist
	}
	defer func() { osExecutable = orig }()

	stop := setupLogging()
	defer stop()
	if stop == nil {
		t.Fatal("setupLogging deve sempre retornar uma func de restore")
	}
}
