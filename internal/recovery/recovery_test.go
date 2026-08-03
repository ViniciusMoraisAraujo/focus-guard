package recovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeBinary creates a file at path with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// touchBackup creates a *.bak.* file with an mtime matching age (ago) and
// returns its path.
func touchBackup(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	writeFile(t, p, "backup-content")
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatalf("chtimes %s: %v", p, err)
	}
	return p
}

// ---------------------------------------------------------------------------
// FindRecentBackup
// ---------------------------------------------------------------------------

func TestFindRecentBackup_ReturnsNewestWithinWindow(t *testing.T) {
	dir := t.TempDir()
	touchBackup(t, dir, "focusguard-daemon.bak.20260801100000", 25*time.Hour)
	touchBackup(t, dir, "focusguard-daemon.bak.20260802090000", 2*time.Hour)
	newest := touchBackup(t, dir, "focusguard-daemon.bak.20260802100000", 30*time.Minute)

	got, err := FindRecentBackup(filepath.Join(dir, "focusguard-daemon"), 24*time.Hour)
	if err != nil {
		t.Fatalf("FindRecentBackup erro: %v", err)
	}
	if got != newest {
		t.Errorf("FindRecentBackup = %q, want %q (o backup de 30min, mais recente dentro da janela)", got, newest)
	}
}

func TestFindRecentBackup_NoBackupWithinWindow(t *testing.T) {
	dir := t.TempDir()
	touchBackup(t, dir, "focusguard-daemon.bak.20260801000000", 48*time.Hour)

	got, err := FindRecentBackup(filepath.Join(dir, "focusguard-daemon"), 24*time.Hour)
	if err != nil {
		t.Fatalf("FindRecentBackup erro: %v", err)
	}
	if got != "" {
		t.Errorf("expected no backup within window, got %q", got)
	}
}

func TestFindRecentBackup_NoBackupAtAll(t *testing.T) {
	dir := t.TempDir()

	got, err := FindRecentBackup(filepath.Join(dir, "focusguard-daemon"), 24*time.Hour)
	if err != nil {
		t.Fatalf("FindRecentBackup erro: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty when no backup exists, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ShouldRollBack
// ---------------------------------------------------------------------------

// TestShouldRollBack_CrashLoopAfterUpdate covers the primary case: the update
// replaced the binary (backupTime) AFTER the daemon's last confirmed health and
// the daemon has been down past the restart grace — the new version never
// booted, so we roll back.
func TestShouldRollBack_CrashLoopAfterUpdate(t *testing.T) {
	now := time.Now()
	backupTime := now.Add(-90 * time.Second) // update aplicado 90s atrás
	lastHealthy := now.Add(-2 * time.Minute) // última saúde confirmada ANTES do update

	if !ShouldRollBack(backupTime, lastHealthy, now, 60*time.Second, 30*time.Second) {
		t.Error("expected rollback when the new binary was never confirmed healthy")
	}
}

// TestShouldRollBack_LegitRestartNotReverted is the anti-regression guard: right
// after a successful update the daemon exits on purpose and the service manager
// restarts it within seconds. The binary was replaced (backup newer than
// lastHealthy) but the daemon has been down for less than minDowntime — a good
// update must NOT be reverted.
func TestShouldRollBack_LegitRestartNotReverted(t *testing.T) {
	now := time.Now()
	backupTime := now.Add(-40 * time.Second)  // update aplicado 40s atrás
	lastHealthy := now.Add(-50 * time.Second) // saudável até o update

	if ShouldRollBack(backupTime, lastHealthy, now, 60*time.Second, 30*time.Second) {
		t.Error("no rollback during the post-update restart grace period")
	}
}

// TestShouldRollBack_CrashAfterBriefHealth covers a release that boots and serves
// IPC briefly, then dies within the crash window right after the update.
func TestShouldRollBack_CrashAfterBriefHealth(t *testing.T) {
	now := time.Now()
	backupTime := now.Add(-40 * time.Second)  // update aplicado 40s atrás
	lastHealthy := now.Add(-20 * time.Second) // o novo binário chegou a responder

	if !ShouldRollBack(backupTime, lastHealthy, now, 60*time.Second, 30*time.Second) {
		t.Error("expected rollback when the new binary crashed shortly after its first health")
	}
}

// TestShouldRollBack_RoutineDeathOfConfirmedBinary: the current binary was
// replaced long ago and has been running healthy for minutes — a routine death
// must never be reverted.
func TestShouldRollBack_RoutineDeathOfConfirmedBinary(t *testing.T) {
	now := time.Now()
	backupTime := now.Add(-10 * time.Minute)
	lastHealthy := now.Add(-5 * time.Minute) // saudável bem depois do update

	if ShouldRollBack(backupTime, lastHealthy, now, 60*time.Second, 30*time.Second) {
		t.Error("no rollback for a routine death of a long-running daemon")
	}
}

// TestShouldRollBack_NoBackup: a zero backupTime means no backup exists — the
// watchdog has nothing to restore from, so it never decides to roll back.
func TestShouldRollBack_NoBackup(t *testing.T) {
	now := time.Now()
	lastHealthy := now.Add(-20 * time.Second)

	if ShouldRollBack(time.Time{}, lastHealthy, now, 60*time.Second, 30*time.Second) {
		t.Error("no rollback without a backup file")
	}
}

func TestShouldRollBack_ZeroHealthyTime(t *testing.T) {
	now := time.Now()
	backupTime := now.Add(-90 * time.Second)
	// Nenhuma saúde confirmada (zero value) → não decide rollback (ex.: boot
	// inicial sem histórico — não deve reverter uma instalação limpa).
	if ShouldRollBack(backupTime, time.Time{}, now, 60*time.Second, 30*time.Second) {
		t.Error("no rollback without a recorded healthy time")
	}
}

// ---------------------------------------------------------------------------
// RestoreFromBackup
// ---------------------------------------------------------------------------

func TestRestoreFromBackup_CopiesContent(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	backup := filepath.Join(dir, "focusguard-daemon.bak.20260802100000")
	writeFile(t, binary, "broken-new-version")
	writeFile(t, backup, "good-old-version")

	if err := RestoreFromBackup(backup, binary); err != nil {
		t.Fatalf("RestoreFromBackup erro: %v", err)
	}

	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if string(data) != "good-old-version" {
		t.Errorf("binary = %q, want good-old-version", string(data))
	}
}

func TestRestoreFromBackup_MissingBackup(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	writeFile(t, binary, "content")

	if err := RestoreFromBackup(filepath.Join(dir, "does-not-exist.bak"), binary); err == nil {
		t.Fatal("expected error when the backup is missing")
	}
}

// TestRestoreFromBackup_WindowsRenamesBeforeCopy exercises the Windows branch
// on any platform: the running binary must be renamed aside (liberating the
// file lock) before os.WriteFile creates a fresh copy, and the .trash file is
// cleaned up afterwards.
func TestRestoreFromBackup_WindowsRenamesBeforeCopy(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	backup := filepath.Join(dir, "focusguard-daemon.bak.20260802100000")
	writeFile(t, binary, "broken-new-version")
	writeFile(t, backup, "good-old-version")

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	var renamed []string
	origRename := osRename
	osRename = func(old, new string) error {
		renamed = append(renamed, old)
		return os.Rename(old, new)
	}
	defer func() { osRename = origRename }()

	if err := RestoreFromBackup(backup, binary); err != nil {
		t.Fatalf("RestoreFromBackup erro: %v", err)
	}

	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if string(data) != "good-old-version" {
		t.Errorf("binary = %q, want good-old-version", string(data))
	}
	if len(renamed) != 1 || renamed[0] != binary {
		t.Errorf("esperava rename de %q antes do write, got %v", binary, renamed)
	}
	if _, err := os.Stat(binary + ".trash"); !os.IsNotExist(err) {
		t.Error(".trash deveria ter sido removido após o restore")
	}
}

// ---------------------------------------------------------------------------
// RecoverIfNeeded (entry point usado pelo watchdog)
// ---------------------------------------------------------------------------

func TestRecoverIfNeeded_CrashLoopRestoresBackup(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	writeFile(t, binary, "broken-new-version")
	// Backup recente, criado DEPOIS da última saúde confirmada.
	touchBackup(t, dir, "focusguard-daemon.bak.20260802100000", 90*time.Second)

	now := time.Now()
	restored, err := RecoverIfNeeded(binary, now.Add(-2*time.Minute), now, 60*time.Second, 30*time.Second, 24*time.Hour)
	if err != nil {
		t.Fatalf("RecoverIfNeeded erro: %v", err)
	}
	if !restored {
		t.Fatal("expected rollback to happen on a post-update crash-loop")
	}

	data, _ := os.ReadFile(binary)
	if string(data) != "backup-content" {
		t.Errorf("binary deveria ter sido restaurado do backup, got %q", string(data))
	}
}

// TestRecoverIfNeeded_LegitRestartNotReverted: update aplicado há 40s, daemon
// fora há 50s (dentro da graça de 60s) — restart legítimo em andamento, o
// binário novo NÃO pode ser revertido.
func TestRecoverIfNeeded_LegitRestartNotReverted(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	writeFile(t, binary, "good-new-version")
	touchBackup(t, dir, "focusguard-daemon.bak.20260802100000", 40*time.Second)

	now := time.Now()
	restored, err := RecoverIfNeeded(binary, now.Add(-50*time.Second), now, 60*time.Second, 30*time.Second, 24*time.Hour)
	if err != nil {
		t.Fatalf("RecoverIfNeeded erro: %v", err)
	}
	if restored {
		t.Error("não pode haver rollback durante a janela de restart pós-update")
	}

	data, _ := os.ReadFile(binary)
	if string(data) != "good-new-version" {
		t.Errorf("binário novo não deveria ter sido tocado, got %q", string(data))
	}
}

func TestRecoverIfNeeded_StableDaemonNoRollback(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon")
	writeFile(t, binary, "working-version")
	touchBackup(t, dir, "focusguard-daemon.bak.20260802100000", 30*time.Minute)

	now := time.Now()
	// Daemon rodou 10min antes de cair — morte normal, sem rollback.
	restored, err := RecoverIfNeeded(binary, now.Add(-10*time.Minute), now, 60*time.Second, 30*time.Second, 24*time.Hour)
	if err != nil {
		t.Fatalf("RecoverIfNeeded erro: %v", err)
	}
	if restored {
		t.Error("não deveria haver rollback para uma morte normal do daemon")
	}

	data, _ := os.ReadFile(binary)
	if string(data) != "working-version" {
		t.Errorf("binário não deveria ter sido tocado, got %q", string(data))
	}
}
