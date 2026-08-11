//go:build windows

package tlsca

import (
	"os"
	"strings"
	"testing"
)

// fakeRunner grava os comandos executados e devolve saída/erro configuráveis —
// espelho do fakeExec do doctor_test.
type fakeRunner struct {
	commands []string
	store    string // saída do certutil -store Root (contém ou não o CN)
	fail     bool
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	if f.fail {
		return []byte("erro de execução"), os.ErrPermission
	}
	return []byte(f.store), nil
}

func TestInstallIntoStore_Windows(t *testing.T) {
	ca := newTestCA(t)
	f := &fakeRunner{store: "Root:\n  Certificados...\nSubject: CN=" + commonName}
	if err := ca.InstallIntoStore(f.run); err != nil {
		t.Fatalf("InstallIntoStore: %v", err)
	}
	// Idempotente: CA presente no store (CN no output do -store) → nenhum
	// comando de escrita.
	if len(f.commands) != 1 {
		t.Fatalf("comandos = %v, want só o -store", f.commands)
	}

	// CA ausente → certutil -addstore com o .cer temporário (removido depois).
	f2 := &fakeRunner{store: "Root:\\n  (vazio)"}
	if err := ca.InstallIntoStore(f2.run); err != nil {
		t.Fatalf("InstallIntoStore (ausente): %v", err)
	}
	var sawAdd bool
	for _, c := range f2.commands {
		if strings.Contains(c, "-addstore") && strings.Contains(c, ".cer") {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Errorf("comandos = %v, want certutil -addstore com .cer", f2.commands)
	}
	if _, err := os.Stat(ca.CertPath() + ".cer"); !os.IsNotExist(err) {
		t.Error("arquivo .cer temporário deveria ter sido removido")
	}
}

func TestInstallIntoStore_WindowsError(t *testing.T) {
	ca := newTestCA(t)
	f := &fakeRunner{store: "vazio", fail: true}
	if err := ca.InstallIntoStore(f.run); err == nil {
		t.Error("certutil -addstore falhando deveria propagar erro")
	}
}

func TestIsInStore_Windows(t *testing.T) {
	ca := newTestCA(t)
	// CN presente na saída do certutil → instalada.
	got, err := ca.IsInStore(func(_ string, _ ...string) ([]byte, error) {
		return []byte("Root:\\r\\n===== Cert 1 =====\\r\\nSubject: CN=" + commonName), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("CA deveria ser detectada no store (CN presente)")
	}

	got, err = ca.IsInStore(func(_ string, _ ...string) ([]byte, error) {
		return []byte("Root:\\r\\n===== Cert 1 =====\\r\\nSubject: CN=Outra CA"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("CA não deveria ser detectada (CN ausente)")
	}
}

func TestRemoveFromStore_Windows(t *testing.T) {
	ca := newTestCA(t)
	// Instalada → -delstore é chamado; ausente → no-op.
	f := &fakeRunner{store: "Root:\\nSubject: CN=" + commonName}
	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore: %v", err)
	}
	if !strings.Contains(f.commands[len(f.commands)-1], "-delstore") {
		t.Errorf("comandos = %v, want certutil -delstore", f.commands)
	}

	f2 := &fakeRunner{store: "vazio"}
	if err := ca.RemoveFromStore(f2.run); err != nil {
		t.Fatalf("RemoveFromStore (ausente): %v", err)
	}
	if len(f2.commands) != 1 {
		t.Errorf("comandos = %v, want só o -store (no-op)", f2.commands)
	}
}
