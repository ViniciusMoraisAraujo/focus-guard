//go:build linux

package tlsca

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	commands []string
	fail     bool
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	if f.fail {
		return []byte("erro"), os.ErrPermission
	}
	return nil, nil
}

func TestInstallIntoStore_Linux(t *testing.T) {
	ca := newTestCA(t)
	f := &fakeRunner{}
	if err := ca.InstallIntoStore(f.run); err != nil {
		t.Fatalf("InstallIntoStore: %v", err)
	}
	// Arquivo copiado para o ca-certs dir + update-ca-certificates rodado.
	if _, err := os.Stat(filepath.Join(caCertsDir, storeFileName)); err != nil {
		t.Errorf("CA não copiada para %s: %v", caCertsDir, err)
	}
	if !strings.Contains(f.commands[len(f.commands)-1], "update-ca-certificates") {
		t.Errorf("comandos = %v, want update-ca-certificates", f.commands)
	}

	// Idempotente: segunda chamada é no-op (arquivo já presente).
	n := len(f.commands)
	_ = ca.InstallIntoStore(f.run)
	if len(f.commands) != n {
		t.Errorf("InstallIntoStore idempotente deveria ser no-op, comandos = %v", f.commands)
	}
}

func TestInstallIntoStore_LinuxError(t *testing.T) {
	ca := newTestCA(t)
	f := &fakeRunner{fail: true}
	if err := ca.InstallIntoStore(f.run); err == nil {
		t.Error("update-ca-certificates falhando deveria propagar erro")
	}
}

func TestIsInStore_Linux(t *testing.T) {
	ca := newTestCA(t)
	if _, err := ca.IsInStore(func(string, ...string) ([]byte, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	// Instala de verdade (arquivo no ca-certs dir) e verifica.
	_ = ca.InstallIntoStore((&fakeRunner{}).run)
	got, err := ca.IsInStore(func(string, ...string) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("CA deveria ser detectada no ca-certs dir")
	}
}

func TestRemoveFromStore_Linux(t *testing.T) {
	ca := newTestCA(t)
	f := &fakeRunner{}
	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore: %v", err)
	}
	if len(f.commands) != 0 {
		t.Errorf("RemoveFromStore (ausente) deveria ser no-op, comandos = %v", f.commands)
	}

	_ = ca.InstallIntoStore((&fakeRunner{}).run)
	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore (presente): %v", err)
	}
	if _, err := os.Stat(filepath.Join(caCertsDir, storeFileName)); !os.IsNotExist(err) {
		t.Error("arquivo da CA deveria ter sido removido do ca-certs dir")
	}
	if !strings.Contains(f.commands[len(f.commands)-1], "--fresh") {
		t.Errorf("comandos = %v, want update-ca-certificates --fresh", f.commands)
	}
}
