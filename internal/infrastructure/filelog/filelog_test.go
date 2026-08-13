package filelog

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPathFor_NextToExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("caminho com separador Windows só faz sentido no Windows")
	}
	got := PathFor(`C:\Program Files\FocusGuard\focusguard-watchdog.exe`, "focusguard-watchdog.log")
	want := filepath.Join(`C:\Program Files\FocusGuard`, "focusguard-watchdog.log")
	if got != want {
		t.Errorf("PathFor = %q, want %q", got, want)
	}
}

func TestSetup_WritesToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "focusguard-test.log")

	restore, err := Setup(path, DefaultMaxSize)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer restore()

	msg := "mensagem de teste"
	log.Println(msg)

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

func TestSetup_AppendsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "focusguard-test.log")

	restore, err := Setup(path, DefaultMaxSize)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	log.Println("primeira execução")
	restore()

	restore, err = Setup(path, DefaultMaxSize)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer restore()
	log.Println("segunda execução")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "primeira execução") || !strings.Contains(text, "segunda execução") {
		t.Errorf("esperava append preservando as duas execuções, got %q", text)
	}
}

func TestSetup_RestoresPreviousOutput(t *testing.T) {
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, "focusguard-test.log")

	restore, err := Setup(path, DefaultMaxSize)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	log.Println("vai para o arquivo")
	restore()

	log.Println("volta para o buffer")
	if !strings.Contains(buf.String(), "volta para o buffer") {
		t.Error("após restore, o log deveria voltar ao output anterior")
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "volta para o buffer") {
		t.Error("mensagem pós-restore não deveria ir para o arquivo")
	}
}

func TestSetup_FailsOnBadPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "subdir", "focusguard-test.log")
	if _, err := Setup(path, DefaultMaxSize); err == nil {
		t.Fatal("expected error when the parent directory does not exist")
	}
}

func TestSetup_RotatesOversizedLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "focusguard-test.log")

	const maxSize int64 = 1024
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 4096), 0644); err != nil {
		t.Fatalf("seed oversized log: %v", err)
	}

	restore, err := Setup(path, maxSize)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer restore()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error("esperava o log antigo rotacionado para .1")
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Errorf("esperava um log novo vazio, got err=%v size=%d", err, info.Size())
	}
}

func TestSetup_DoesNotRotateSmallLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "focusguard-test.log")

	const maxSize int64 = 1024
	if err := os.WriteFile(path, []byte("log pequeno"), 0644); err != nil {
		t.Fatalf("seed small log: %v", err)
	}

	restore, err := Setup(path, maxSize)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer restore()

	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Error("log pequeno não deveria ser rotacionado")
	}
}
