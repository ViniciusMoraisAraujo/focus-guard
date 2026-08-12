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

// useTempStoreDirs redireciona caCertsDir/storeInstalledDir para diretórios
// temporários (os testes não podem tocar no trust store real da máquina) e
// restaura os valores originais no fim.
func useTempStoreDirs(t *testing.T) {
	t.Helper()
	origCA, origInstalled := caCertsDir, storeInstalledDir
	caCertsDir, storeInstalledDir = t.TempDir(), t.TempDir()
	t.Cleanup(func() { caCertsDir, storeInstalledDir = origCA, origInstalled })
}

// simulateUpdateCACerts simula o efeito do update-ca-certificates: a âncora
// local (caCertsDir) é copiada para o diretório instalado (storeInstalledDir)
// — o fakeRunner não roda o comando real.
func simulateUpdateCACerts(t *testing.T, ca *CA) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(storeInstalledDir, storeInstalledName), ca.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallIntoStore_Linux(t *testing.T) {
	useTempStoreDirs(t)
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

	// Idempotente: com a CA instalada de verdade (cópia em storeInstalledDir),
	// a segunda chamada é no-op.
	simulateUpdateCACerts(t, ca)
	n := len(f.commands)
	_ = ca.InstallIntoStore(f.run)
	if len(f.commands) != n {
		t.Errorf("InstallIntoStore idempotente deveria ser no-op, comandos = %v", f.commands)
	}
}

func TestInstallIntoStore_LinuxError(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	f := &fakeRunner{fail: true}
	if err := ca.InstallIntoStore(f.run); err == nil {
		t.Error("update-ca-certificates falhando deveria propagar erro")
	}
}

// TestIsInStore_Linux_InstalledCopy: a prova real de instalação é a cópia em
// storeInstalledDir — só o arquivo-fonte em caCertsDir NÃO conta (o fix do
// v0.18.1: antes, um update-ca-certificates que falhava depois do write era
// reportado como instalado e a reinstalação era pulada para sempre).
func TestIsInStore_Linux_InstalledCopy(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	run := func(string, ...string) ([]byte, error) { return nil, nil }

	// Nada instalado → false.
	got, err := ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("sem nada no store, IsInStore deveria ser false")
	}

	// Só o arquivo-fonte (update-ca-certificates ainda não rodou — ou falhou
	// depois do write) → NÃO é instalado.
	_ = ca.InstallIntoStore((&fakeRunner{}).run)
	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("arquivo-fonte sem a cópia instalada NÃO deveria contar como instalado (update-ca-certificates pode ter falhado)")
	}

	// Cópia instalada do NOSSO certificado → true.
	simulateUpdateCACerts(t, ca)
	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("cópia instalada da nossa CA deveria ser detectada")
	}
}

// TestIsInStore_Linux_ForeignOrStaleCert: uma âncora com o mesmo nome mas de
// OUTRA CA (ex.: CA regenerada após corrupção) ou um arquivo qualquer não
// conta como instalada — a reinstalação não pode ser pulada nesses casos.
func TestIsInStore_Linux_ForeignOrStaleCert(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	run := func(string, ...string) ([]byte, error) { return nil, nil }

	// Outra CA (regenerada) com o mesmo nome de arquivo instalado.
	other := newTestCA(t)
	if err := os.WriteFile(filepath.Join(storeInstalledDir, storeInstalledName), other.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("cópia de OUTRA CA não deveria contar como instalada")
	}

	// Arquivo que não é PEM.
	if err := os.WriteFile(filepath.Join(storeInstalledDir, storeInstalledName), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("arquivo não-PEM não deveria contar como instalado")
	}
}

func TestRemoveFromStore_Linux(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	f := &fakeRunner{}
	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore: %v", err)
	}
	if len(f.commands) != 0 {
		t.Errorf("RemoveFromStore (ausente) deveria ser no-op, comandos = %v", f.commands)
	}

	_ = ca.InstallIntoStore((&fakeRunner{}).run)
	simulateUpdateCACerts(t, ca)
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
