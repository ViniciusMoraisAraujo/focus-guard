package main

import (
	"os"
	"path/filepath"
	"testing"

	"focusguard/internal/infrastructure/tlsca"
)

// TestEnsureCA_GeneratesAndInstalls: com o interceptor ativo, o ensureCA gera
// a CA no caDir (primeiro boot), seta no lifecycle e instala no trust store
// (runner stubbable — a suíte nunca toca o trust store real).
func TestEnsureCA_GeneratesAndInstalls(t *testing.T) {
	orig := ensureCAInstalled
	var installed *tlsca.CA
	ensureCAInstalled = func(ca *tlsca.CA) error {
		installed = ca
		return nil
	}
	defer func() { ensureCAInstalled = orig }()

	dir := t.TempDir()
	il := &interceptorLifecycle{caDir: filepath.Join(dir, "ca")}
	il.ensureCA()

	if il.ca == nil {
		t.Fatal("ensureCA deveria setar a CA no lifecycle")
	}
	if installed == nil || installed != il.ca {
		t.Error("ensureCAInstalled deveria receber a CA gerada")
	}
	if !tlsca.Exists(il.caDir) {
		t.Error("CA deveria estar persistida no caDir")
	}
}

// TestEnsureCA_IsIdempotent: segunda chamada não regenera a CA (o cert
// persistido continua o mesmo) nem reinstala no trust store.
func TestEnsureCA_IsIdempotent(t *testing.T) {
	orig := ensureCAInstalled
	calls := 0
	var first *tlsca.CA
	ensureCAInstalled = func(ca *tlsca.CA) error {
		calls++
		first = ca
		return nil
	}
	defer func() { ensureCAInstalled = orig }()

	il := &interceptorLifecycle{caDir: filepath.Join(t.TempDir(), "ca")}
	il.ensureCA()
	before := il.ca.CertPEM()
	il.ensureCA()

	if calls != 1 {
		t.Errorf("ensureCAInstalled chamado %d vezes, want 1 (idempotente)", calls)
	}
	if string(il.ca.CertPEM()) != string(before) {
		t.Error("CA foi regenerada na segunda chamada")
	}
	if first != il.ca {
		t.Error("a mesma CA deveria ser reutilizada")
	}
}

// TestEnsureCA_NoCaDirIsNoOp: sem caDir configurado (fallback auto-assinado),
// o ensureCA não gera nada nem panica.
func TestEnsureCA_NoCaDirIsNoOp(t *testing.T) {
	il := &interceptorLifecycle{}
	il.ensureCA() // não deve panico
	if il.ca != nil {
		t.Error("sem caDir não deveria gerar CA")
	}
}

// TestEnsureCA_CleansOrphanCER: um .cer órfão (crash do certutil no boot
// anterior) é removido durante o ensureCA do boot — sem afetar os artefatos
// reais da CA.
func TestEnsureCA_CleansOrphanCER(t *testing.T) {
	orig := ensureCAInstalled
	ensureCAInstalled = func(ca *tlsca.CA) error { return nil }
	defer func() { ensureCAInstalled = orig }()

	caDir := filepath.Join(t.TempDir(), "ca")
	ca, err := tlsca.LoadOrCreate(caDir)
	if err != nil {
		t.Fatal(err)
	}
	orphan := ca.CertPath() + ".cer"
	if err := os.WriteFile(orphan, ca.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}

	il := &interceptorLifecycle{caDir: caDir}
	il.ensureCA()

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("ensureCA deveria remover o .cer órfão do boot anterior")
	}
	if !tlsca.Exists(caDir) {
		t.Error("artefatos reais da CA não deveriam ser afetados")
	}
}

// TestEnsureCA_InstallFailureDegradesGracefully: falha no trust store apenas
// loga — a CA continua setada (os leafs ainda são assinados por ela; o aviso
// no navegador é o que permanece até o ca-install manual).
func TestEnsureCA_InstallFailureDegradesGracefully(t *testing.T) {
	orig := ensureCAInstalled
	ensureCAInstalled = func(ca *tlsca.CA) error {
		return errFakeInstall
	}
	defer func() { ensureCAInstalled = orig }()

	il := &interceptorLifecycle{caDir: filepath.Join(t.TempDir(), "ca")}
	il.ensureCA() // não deve panico
	if il.ca == nil {
		t.Error("falha no trust store não deveria descartar a CA (leafs ainda assinados)")
	}
}

var errFakeInstall = &fakeInstallError{"trust store não gravável"}

type fakeInstallError struct{ msg string }

func (e *fakeInstallError) Error() string { return e.msg }
