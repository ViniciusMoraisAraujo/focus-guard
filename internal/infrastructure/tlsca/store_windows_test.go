//go:build windows

package tlsca

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// fakeRunner grava os comandos executados e devolve saída/erro configuráveis —
// espelho do fakeExec do doctor_test.
type fakeRunner struct {
	commands []string
	store    string // saída do certutil -store Root
	fail     bool
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	if f.fail {
		return []byte("erro de execução"), os.ErrPermission
	}
	return []byte(f.store), nil
}

// certutilStoreOutput monta uma saída realista do "certutil -store Root": o
// serial em pares hex separados por espaço (formato do certutil), com o
// prefixo 00 quando o bit alto está setado (encoding positivo do Windows).
// Com present=false, devolve um store vazio.
func certutilStoreOutput(ca *CA, present bool) string {
	if !present {
		return "Root:\r\n\r\n  Certificados no armazenamento \"Root\"...\r\n\r\n0 certificados.\r\n"
	}
	hex := strings.ToUpper(ca.crt.SerialNumber.Text(16))
	if len(hex)%2 == 1 {
		hex = "0" + hex
	}
	if hex[0] >= '8' {
		hex = "00" + hex
	}
	var pairs []string
	for i := 0; i < len(hex); i += 2 {
		pairs = append(pairs, hex[i:i+2])
	}
	return fmt.Sprintf("Root:\r\n===== Cert 1 =====\r\nSerial Number: %s\r\nSubject: CN=%s\r\n", strings.Join(pairs, " "), commonName)
}

func TestInstallIntoStore_Windows(t *testing.T) {
	ca := newTestCA(t)
	// Idempotente: CA presente no store (serial no output do -store) → nenhum
	// comando de escrita.
	f := &fakeRunner{store: certutilStoreOutput(ca, true)}
	if err := ca.InstallIntoStore(f.run); err != nil {
		t.Fatalf("InstallIntoStore: %v", err)
	}
	if len(f.commands) != 1 {
		t.Fatalf("comandos = %v, want só o -store", f.commands)
	}

	// CA ausente → certutil -addstore com o .cer temporário (removido depois).
	f2 := &fakeRunner{store: certutilStoreOutput(ca, false)}
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

// TestIsInStore_Windows_SerialBased: a identidade da CA no store é o SERIAL,
// não o CN. O cenário chave é o da CA REGENERADA (mesmo CN, serial novo): o
// cert antigo no store não pode fazer a reinstalação ser pulada (senão o
// navegador rejeita os leafs novos). Também tolera o serial com espaços/00.
func TestIsInStore_Windows_SerialBased(t *testing.T) {
	ca := newTestCA(t)

	// Serial presente (formato certutil: pares com espaço + prefixo 00) → true.
	got, err := ca.IsInStore(func(_ string, _ ...string) ([]byte, error) {
		return []byte(certutilStoreOutput(ca, true)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("CA deveria ser detectada no store (serial presente)")
	}

	// Mesmo CN, serial de OUTRA CA (a âncora antiga da CA regenerada) → false:
	// a reinstalação NÃO pode ser pulada.
	other := newTestCA(t)
	got, err = ca.IsInStore(func(_ string, _ ...string) ([]byte, error) {
		return []byte(certutilStoreOutput(other, true)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("cert antigo com o mesmo CN mas serial diferente NÃO deveria contar como instalado (CA regenerada)")
	}

	// Store vazio → false.
	got, err = ca.IsInStore(func(_ string, _ ...string) ([]byte, error) {
		return []byte(certutilStoreOutput(ca, false)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("store vazio não deveria reportar instalado")
	}
}

// TestIsInStore_Windows_NormalizedSerial: o serial pode aparecer sem espaços
// nem prefixo (variação do output) — a normalização precisa casar mesmo assim.
func TestIsInStore_Windows_NormalizedSerial(t *testing.T) {
	ca := newTestCA(t)
	hex := strings.ToUpper(ca.crt.SerialNumber.Text(16))
	got, err := ca.IsInStore(func(_ string, _ ...string) ([]byte, error) {
		return []byte("Root:\r\nSerial Number: " + hex + "\r\nSubject: CN=" + commonName + "\r\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("serial sem espaços (minimal hex) deveria ser detectado")
	}
}

func TestRemoveFromStore_Windows(t *testing.T) {
	ca := newTestCA(t)
	// Instalada (serial presente) → -delstore é chamado; ausente → no-op.
	f := &fakeRunner{store: certutilStoreOutput(ca, true)}
	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore: %v", err)
	}
	if !strings.Contains(f.commands[len(f.commands)-1], "-delstore") {
		t.Errorf("comandos = %v, want certutil -delstore", f.commands)
	}

	f2 := &fakeRunner{store: certutilStoreOutput(ca, false)}
	if err := ca.RemoveFromStore(f2.run); err != nil {
		t.Fatalf("RemoveFromStore (ausente): %v", err)
	}
	if len(f2.commands) != 1 {
		t.Errorf("comandos = %v, want só o -store (no-op)", f2.commands)
	}
}
