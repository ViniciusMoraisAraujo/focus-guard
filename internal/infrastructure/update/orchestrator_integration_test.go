package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/creativeprojects/go-selfupdate"
)

// integrationUpdater embrulha o Updater real com um CheckForUpdate canhão (sem
// rede — o go-selfupdate consultaria a API do GitHub): a parte crítica
// exercitada é o UpdateToAll REAL (download → extração → swap → rollback)
// orquestrado pelo Orchestrator.
type integrationUpdater struct {
	*Updater
	result *UpdateResult
}

func (u *integrationUpdater) CheckForUpdate(_ context.Context) (*UpdateResult, error) {
	return u.result, nil
}

// releaseResult monta um UpdateResult apontando para um zip servido por httptest.
func releaseResult(serverURL, name string) *UpdateResult {
	return &UpdateResult{
		Version: "1.1.0",
		Release: &selfupdate.Release{AssetURL: serverURL + "/" + name, AssetName: name},
	}
}

// countBakFiles conta os backups .bak.<timestamp> do diretório.
func countBakFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.") {
			n++
		}
	}
	return n
}

// TestOrchestrator_Integration_ApplyWithActiveBlocks cobre o cenário "update
// com bloqueios ativos": o state.json com bloqueios vivos fica ao lado dos
// binários (mesmo diretório do daemon) e o update não pode tocá-lo — os
// bloqueios sobrevivem para o Reconcile do daemon reiniciado. O apply usa o
// UpdateToAll real (httptest serve o zip da release).
func TestOrchestrator_Integration_ApplyWithActiveBlocks(t *testing.T) {
	dir := t.TempDir()
	bins := []string{
		writeBinary(t, dir, "focusguard-daemon", "old-daemon"),
		writeBinary(t, dir, "focusguard", "old-cli"),
	}

	// Bloqueios ativos persistidos (estado de um daemon protegendo agora).
	stateJSON := []byte(`{"version":1,"blocks":{"youtube.com":{"domain":"youtube.com","started_at":"2026-08-10T10:00:00Z","expires_at":"2026-08-10T14:00:00Z","resolved_ips":["203.0.113.7"]}},"dns_enabled":false,"dns_upstream":""}`)
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, stateJSON, 0600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	archive := filepath.Join(t.TempDir(), "focusguard_1.1.0.zip")
	makeTestZip(t, archive, "focusguard_1.1.0", map[string]string{
		"focusguard-daemon": "new-daemon",
		"focusguard":        "new-cli",
	})
	server := serveZip(t, archive)
	defer server.Close()

	u := &integrationUpdater{Updater: NewUpdater("o", "r"), result: releaseResult(server.URL, "focusguard_1.1.0.zip")}
	o := NewOrchestrator(u, bins, "1.0.0")
	o.StopForSwap = func(_ []string) func() { return func() {} }

	st, err := o.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !st.Applied {
		t.Fatalf("esperava Applied=true, got %+v", st)
	}

	// Binários trocados pelo UpdateToAll real.
	if got := readBinary(t, bins[0]); got != "new-daemon" {
		t.Errorf("daemon = %q, want new-daemon", got)
	}
	if got := readBinary(t, bins[1]); got != "new-cli" {
		t.Errorf("cli = %q, want new-cli", got)
	}

	// Bug 2: a flag permanece (só o boot saudável da nova versão a remove).
	if _, err := os.Stat(filepath.Join(dir, InProgressFileName)); err != nil {
		t.Error("update.inprogress deve existir após o apply")
	}

	// Bloqueios intactos: o update não tocou o state.json (byte a byte).
	if got, err := os.ReadFile(statePath); err != nil || string(got) != string(stateJSON) {
		t.Errorf("state.json mudou com o update (bloqueios ativos): err=%v got=%s", err, got)
	}

	// CleanupStale manteve só o .bak mais novo por binário (2, um para cada).
	if n := countBakFiles(t, dir); n != 2 {
		t.Errorf("esperava 2 .bak (um por binário), got %d", n)
	}
}

// TestOrchestrator_Integration_RenameFailedSchedulesReboot cobre o cenário
// "renomeação falhou → reboot" de ponta a ponta: Orchestrator + Updater real,
// com o rename-aside do daemon falhando (exe travado no Windows) — a suíte é
// agendada para o próximo boot (ErrScheduledOnReboot → PendingReboot), a flag é
// removida e o daemon segue rodando a versão antiga.
func TestOrchestrator_Integration_RenameFailedSchedulesReboot(t *testing.T) {
	origGoos, origRetry, origReplace, origSchedule := goos, renameRetryEnabled, replaceOneBinary, scheduleReplaceOnReboot
	goos = "windows"
	renameRetryEnabled = true
	defer func() {
		goos, renameRetryEnabled, replaceOneBinary, scheduleReplaceOnReboot = origGoos, origRetry, origReplace, origSchedule
	}()

	dir := t.TempDir()
	daemonBin := writeBinary(t, dir, "focusguard-daemon", "old-daemon")
	bins := []string{daemonBin}

	// O rename-aside do daemon sempre falha (executável em execução).
	replaceOneBinary = func(_, _ string) error { return errors.New("rename: acesso negado") }

	var scheduled []string
	scheduleReplaceOnReboot = func(targetPath, _ string) error {
		scheduled = append(scheduled, targetPath)
		return nil
	}

	archive := filepath.Join(t.TempDir(), "focusguard.zip")
	makeTestZip(t, archive, "", map[string]string{"focusguard-daemon": "new-daemon"})
	server := serveZip(t, archive)
	defer server.Close()

	u := &integrationUpdater{Updater: NewUpdater("o", "r"), result: releaseResult(server.URL, "focusguard.zip")}
	o := NewOrchestrator(u, bins, "1.0.0")
	o.StopForSwap = func(_ []string) func() { return func() {} }

	st, err := o.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("ErrScheduledOnReboot não é erro para o caller: %v", err)
	}
	if !st.PendingReboot || st.Applied {
		t.Fatalf("esperava PendingReboot=true e Applied=false, got %+v", st)
	}

	// A flag é removida: o daemon segue rodando a versão antiga.
	if _, err := os.Stat(filepath.Join(dir, InProgressFileName)); !os.IsNotExist(err) {
		t.Error("update.inprogress deve ser removida no caminho PendingReboot")
	}

	// O binário não foi trocado.
	if got := readBinary(t, daemonBin); got != "old-daemon" {
		t.Errorf("daemon = %q, deve continuar old-daemon", got)
	}

	// A troca foi agendada para o próximo boot.
	if len(scheduled) != 1 || scheduled[0] != daemonBin {
		t.Errorf("scheduled = %v, want [%s]", scheduled, daemonBin)
	}

	// O .bak fica para o smart recovery do watchdog pós-reboot.
	if n := countBakFiles(t, dir); n != 1 {
		t.Errorf("esperava 1 .bak para o smart recovery, got %d", n)
	}
}
