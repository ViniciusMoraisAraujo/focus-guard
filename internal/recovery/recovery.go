// Package recovery implements the Smart Recovery mechanism (Feature 4): when a
// freshly updated daemon crashes within a short window after starting, the
// external watchdog restores the previous binary from the .bak files that
// UpdateToAll leaves behind, so a broken release cannot leave the user's
// blocking protection dead.
package recovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FindRecentBackup returns the newest backup file for binaryPath whose mtime
// is within maxAge. Returns "" when there is no eligible backup.
func FindRecentBackup(binaryPath string, maxAge time.Duration) (string, error) {
	matches, err := filepath.Glob(binaryPath + ".bak.*")
	if err != nil {
		return "", err
	}

	now := time.Now()
	var candidates []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue // sumiu/lock no meio do caminho — pula
		}
		if now.Sub(info.ModTime()) > maxAge {
			continue // backup velho demais — não é deste update
		}
		candidates = append(candidates, m)
	}

	if len(candidates) == 0 {
		return "", nil
	}

	// Mais recente primeiro (mtime), com o nome como desempate estável.
	sort.Slice(candidates, func(i, j int) bool {
		mi, _ := os.Stat(candidates[i])
		mj, _ := os.Stat(candidates[j])
		if mi.ModTime().Equal(mj.ModTime()) {
			return candidates[i] > candidates[j]
		}
		return mi.ModTime().After(mj.ModTime())
	})

	return candidates[0], nil
}

// ShouldRollBack decides whether the watchdog must restore the previous
// binary. The signal is the relationship between the backup file (the moment
// the current binary was replaced by an update) and the daemon's last
// confirmed-healthy moment:
//
//   - Backup NEWER than lastHealthy → the current binary was never confirmed
//     healthy after the update (crash-loop on boot). Roll back, but only once
//     the daemon has been down for at least minDowntime — a legitimate
//     post-update restart (the daemon exits on purpose and the service manager
//     brings it back within seconds) must never be reverted.
//   - Backup OLDER than lastHealthy → the current binary HAS run fine. Roll
//     back only when the daemon died within crashWindow of that healthy run AND
//     that run happened within crashWindow of the replacement — i.e. a crash
//     right after (re)starting with the new binary. A routine death of a
//     long-running daemon is never reverted.
func ShouldRollBack(backupTime, lastHealthy, now time.Time, minDowntime, crashWindow time.Duration) bool {
	if lastHealthy.IsZero() {
		return false // nunca vimos o daemon saudável — não decide nada
	}
	if backupTime.After(lastHealthy) {
		return now.Sub(lastHealthy) >= minDowntime
	}
	return now.Sub(lastHealthy) < crashWindow && lastHealthy.Sub(backupTime) < crashWindow
}

// RestoreFromBackup copies the backup over the broken binary, preserving the
// executable permissions from the backup.
func RestoreFromBackup(backupPath, binaryPath string) error {
	if backupPath == "" || binaryPath == "" {
		return fmt.Errorf("recovery: backup e binário não podem ser vazios")
	}
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("recovery: backup não encontrado em %s: %w", backupPath, err)
	}

	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("recovery: leitura do backup %s: %w", backupPath, err)
	}
	if err := os.WriteFile(binaryPath, data, 0755); err != nil {
		return fmt.Errorf("recovery: restauração de %s: %w", binaryPath, err)
	}

	// Preserva as permissões de execução do backup (o WriteFile força 0755,
	// mas o backup original pode ter sido um .exe com outro modo).
	info, err := os.Stat(backupPath)
	if err == nil {
		_ = os.Chmod(binaryPath, info.Mode())
	}
	return nil
}

// RecoverIfNeeded is the high-level entry point used by the watchdog: given
// the daemon binary path, the time it was last confirmed healthy, the grace
// period for post-update restarts and the crash window, it restores the
// newest recent backup when a crash-loop after an update is detected. Returns
// whether a rollback happened.
func RecoverIfNeeded(binaryPath string, lastHealthy, now time.Time, minDowntime, crashWindow, backupMaxAge time.Duration) (bool, error) {
	backup, err := FindRecentBackup(binaryPath, backupMaxAge)
	if err != nil {
		return false, err
	}
	if backup == "" {
		return false, nil
	}
	info, err := os.Stat(backup)
	if err != nil {
		return false, fmt.Errorf("recovery: stat do backup %s: %w", backup, err)
	}
	if !ShouldRollBack(info.ModTime(), lastHealthy, now, minDowntime, crashWindow) {
		return false, nil
	}
	if err := RestoreFromBackup(backup, binaryPath); err != nil {
		return false, err
	}
	return true, nil
}
