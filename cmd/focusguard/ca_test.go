package main

import (
	"errors"
	"testing"

	"focusguard/internal/infrastructure/tlsca"
)

// TestCAInstall_NotElevated_DoesNotGenerateCA trava a pendência INFO do
// docs/verification-plan.md: a elevação é checada ANTES de gerar a CA — um
// ca-install em shell não elevado aborta sem criar os artefatos da CA (antes,
// a CA era gerada e o erro só aparecia no write do trust store, com uma
// mensagem menos clara). O teste é hermetitico: diretório temporário e
// isElevatedCheck stubado; o caminho elevado (install real no trust store)
// fica coberto pelos testes do pacote tlsca, que injetam o runner.
func TestCAInstall_NotElevated_DoesNotGenerateCA(t *testing.T) {
	orig := isElevatedCheck
	isElevatedCheck = func() bool { return false }
	t.Cleanup(func() { isElevatedCheck = orig })

	dir := t.TempDir()
	err := caInstallRuns(dir)
	if !errors.Is(err, errCARequiresElevation) {
		t.Fatalf("caInstallRuns = %v, want errCARequiresElevation", err)
	}
	if tlsca.Exists(dir) {
		t.Error("ca-install não elevado gerou a CA — a elevação deveria ser checada antes de qualquer efeito")
	}
}
