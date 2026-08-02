package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	"golang.org/x/mod/semver"
)

// UpdateResult contains information about a found update.
type UpdateResult struct {
	Version string
	Release *selfupdate.Release
}

// Updater handles checking for and applying updates from GitHub releases.
type Updater struct {
	mu             sync.Mutex // protege channel/apiBaseURL/updater (SetChannel é chamado por request)
	owner          string
	repo           string
	currentVersion string
	channel        string // "" | "stable" | "beta"
	apiBaseURL     string // override p/ GitHub Enterprise ou testes
	updater        *selfupdate.Updater
}

// Option configures the Updater.
type Option func(*Updater)

// WithVersion sets the current version of the application.
func WithVersion(version string) Option {
	return func(u *Updater) {
		u.currentVersion = version
	}
}

// WithChannel selects the release channel: "stable" (default) skips
// prereleases; "beta" opts in to prereleases for early access.
func WithChannel(channel string) Option {
	return func(u *Updater) {
		u.channel = channel
		u.rebuildUpdater()
	}
}

// WithGitHubAPI overrides the GitHub API base URL (useful for testing).
func WithGitHubAPI(apiURL string) Option {
	return func(u *Updater) {
		u.apiBaseURL = apiURL
		u.rebuildUpdater()
	}
}

// getUpdater returns the current underlying go-selfupdate updater under lock,
// so a SetChannel from another goroutine cannot swap it mid-operation. Each
// Check/Update call works on a consistent snapshot.
func (u *Updater) getUpdater() *selfupdate.Updater {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.updater
}

// rebuildUpdater (re)creates the underlying go-selfupdate updater honoring the
// configured source (GitHub API override) and the release channel. Prereleases
// are only included when the channel is "beta".
//
// Callers must hold u.mu (construction-time options run single-threaded;
// SetChannel takes the lock itself).
func (u *Updater) rebuildUpdater() {
	cfg := selfupdate.Config{
		Prerelease: u.channel == "beta",
	}
	if u.apiBaseURL != "" {
		source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{
			EnterpriseBaseURL: u.apiBaseURL,
		})
		if err != nil {
			return
		}
		cfg.Source = source
	}
	updater, err := selfupdate.NewUpdater(cfg)
	if err != nil {
		return
	}
	u.updater = updater
}

// NewUpdater creates a new Updater for the given GitHub owner and repository.
func NewUpdater(owner, repo string, opts ...Option) *Updater {
	u := &Updater{
		owner: owner,
		repo:  repo,
	}
	for _, opt := range opts {
		opt(u)
	}
	if u.updater == nil {
		u.updater = selfupdate.DefaultUpdater()
	}
	return u
}

// SetChannel reconfigures an existing Updater for a new release channel
// ("" | "stable" | "beta") without rebuilding the whole object — used by the
// daemon to honor a per-request --channel flag. The daemon serves each IPC
// connection in its own goroutine, so the switch is mutex-protected to avoid
// racing on the underlying updater.
func (u *Updater) SetChannel(channel string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.channel == channel {
		return
	}
	u.channel = channel
	u.rebuildUpdater()
}

// repository returns a selfupdate.Repository for the configured GitHub owner/repo.
func (u *Updater) repository() selfupdate.Repository {
	return selfupdate.NewRepositorySlug(u.owner, u.repo)
}

// CheckForUpdate checks if a newer version is available on GitHub releases.
// Returns nil if no update is needed.
func (u *Updater) CheckForUpdate(ctx context.Context) (*UpdateResult, error) {
	if u.currentVersion == "" || strings.HasSuffix(u.currentVersion, "-dev") || u.currentVersion == "0.0.0-dev" {
		return nil, nil
	}

	latest, found, err := u.getUpdater().DetectLatest(ctx, u.repository())
	if err != nil {
		return nil, fmt.Errorf("failed to detect latest release: %w", err)
	}
	if !found {
		return nil, nil
	}

	if latest.LessOrEqual(u.currentVersion) {
		return nil, nil
	}

	return &UpdateResult{
		Version: latest.Version(),
		Release: latest,
	}, nil
}

// UpdateTo downloads and applies the update from the given release.
// Returns the backup path if successful.
func (u *Updater) UpdateTo(ctx context.Context, result *UpdateResult, binaryPath string) (string, error) {
	if result == nil || result.Release == nil {
		return "", fmt.Errorf("no update result provided")
	}

	backupPath := binaryPath + ".bak." + time.Now().Format("20060102150405")
	if err := copyFile(binaryPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	_, err := u.getUpdater().UpdateCommand(ctx, binaryPath, u.currentVersion, u.repository())
	if err != nil {
		_ = u.RestoreBackup(backupPath, binaryPath)
		return "", fmt.Errorf("failed to update binary: %w", err)
	}

	return backupPath, nil
}

// applyOneBinary is the per-binary replace step, stubbable in tests so the
// multi-binary rollback flow can be exercised without hitting GitHub.
var applyOneBinary = func(u *Updater, ctx context.Context, binaryPath string) error {
	_, err := u.getUpdater().UpdateCommand(ctx, binaryPath, u.currentVersion, u.repository())
	return err
}

// UpdateToAll downloads the release and replaces every given binary (daemon,
// CLI, tray, watchdog) — go-selfupdate is single-binary, so each path is
// updated in turn. It is all-or-nothing: every binary is backed up first and,
// if any replace fails, all binaries already replaced are restored from their
// backups. This prevents the FocusGuard suite from ending up half-updated
// (e.g. a new daemon talking IPC with an old CLI, which would break the
// protocol). Returns the backup paths on success.
func (u *Updater) UpdateToAll(ctx context.Context, result *UpdateResult, binaries []string) ([]string, error) {
	if result == nil || result.Release == nil {
		return nil, fmt.Errorf("no update result provided")
	}
	if len(binaries) == 0 {
		return nil, nil
	}

	// Fase 1 — backup de todos antes de tocar em qualquer binário.
	backups := make([]string, 0, len(binaries))
	for _, b := range binaries {
		if _, err := os.Stat(b); os.IsNotExist(err) {
			continue // binário não instalado nesta máquina (ex.: tray opcional)
		}
		backupPath := b + ".bak." + time.Now().Format("20060102150405")
		if err := copyFile(b, backupPath); err != nil {
			u.cleanupBackups(backups)
			return nil, fmt.Errorf("failed to back up %s: %w", b, err)
		}
		backups = append(backups, backupPath)
	}

	// Fase 2 — aplica a atualização em cada binário presente.
	// okPaths acompanha a mesma ordem de backups (ambos só contêm binários
	// existentes), então okPaths[i] ↔ backups[i] no rollback.
	var okPaths []string
	for _, b := range binaries {
		if _, err := os.Stat(b); os.IsNotExist(err) {
			continue
		}
		if err := applyOneBinary(u, ctx, b); err != nil {
			// Fase 3 — rollback atômico: restaura todos os já atualizados.
			for i, bk := range backups {
				if i < len(okPaths) {
					if rerr := u.RestoreBackup(bk, okPaths[i]); rerr != nil {
						return nil, fmt.Errorf("%v; além disso, falha ao restaurar %s: %w", err, okPaths[i], rerr)
					}
				}
			}
			u.cleanupBackups(backups)
			return nil, fmt.Errorf("failed to update binary %s: %w", b, err)
		}
		okPaths = append(okPaths, b)
	}

	return backups, nil
}

// cleanupBackups removes backup files (best-effort) after a successful update
// or a failed one that already restored the originals.
func (u *Updater) cleanupBackups(backups []string) {
	for _, bk := range backups {
		_ = u.CleanupBackup(bk)
	}
}

// ApplyUpdate creates a backup copy of the binary.
func (u *Updater) ApplyUpdate(binaryPath string) (string, error) {
	if binaryPath == "" {
		return "", fmt.Errorf("binary path cannot be empty")
	}
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("binary not found at %s", binaryPath)
	}

	backupPath := binaryPath + ".bak." + time.Now().Format("20060102150405")
	if err := copyFile(binaryPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}
	return backupPath, nil
}

// RestoreBackup restores a backup binary to the original path.
func (u *Updater) RestoreBackup(backupPath, originalPath string) error {
	if backupPath == "" || originalPath == "" {
		return fmt.Errorf("backup and original paths must not be empty")
	}
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found at %s", backupPath)
	}
	return copyFile(backupPath, originalPath)
}

// CleanupBackup removes the backup file.
func (u *Updater) CleanupBackup(backupPath string) error {
	if backupPath == "" {
		return nil
	}
	return os.Remove(backupPath)
}

// NewVersionDownloadedPath returns the path where the new version binary is expected.
func (u *Updater) NewVersionDownloadedPath(originalPath string) string {
	dir := filepath.Dir(originalPath)
	ext := filepath.Ext(originalPath)
	base := "focusguard-daemon-new"
	return filepath.Join(dir, base+ext)
}

// copyFile copies a file from src to dst preserving permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

// IsNewVersionAvailable checks if the latest release version is greater than
// the current one using strict semver comparison (golang.org/x/mod/semver).
// It does NOT depend on go-selfupdate's internal asset-name parsing — the old
// implementation built a "dummy" Release and called GreaterThan, which would
// silently break if the library ever changed how it parses AssetName.
func IsNewVersionAvailable(current, latest string) bool {
	cur := canonicalSemver(current)
	lat := canonicalSemver(latest)
	if cur == "" || lat == "" {
		return false // versão inválida — nunca decide "há update"
	}
	return semver.Compare(lat, cur) > 0
}

// canonicalSemver normalizes a version for x/mod/semver: that package requires
// the "v" prefix (semver.IsValid("1.0.0") is false), while FocusGuard versions
// may come with or without it ("0.2.6" from ldflags, "v0.2.6" from tags).
// Invalid versions yield "".
func canonicalSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		if !semver.IsValid("v" + v) {
			return ""
		}
		v = "v" + v
	}
	return semver.Canonical(v)
}
