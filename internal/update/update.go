package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// UpdateResult contains information about a found update.
type UpdateResult struct {
	Version string
	Release *selfupdate.Release
}

// Updater handles checking for and applying updates from GitHub releases.
type Updater struct {
	owner          string
	repo           string
	currentVersion string
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

// WithGitHubAPI overrides the GitHub API base URL (useful for testing).
func WithGitHubAPI(apiURL string) Option {
	return func(u *Updater) {
		source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{
			EnterpriseBaseURL: apiURL,
		})
		if err != nil {
			return
		}
		updater, err := selfupdate.NewUpdater(selfupdate.Config{
			Source: source,
		})
		if err != nil {
			return
		}
		u.updater = updater
	}
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

	latest, found, err := u.updater.DetectLatest(ctx, u.repository())
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

	_, err := u.updater.UpdateCommand(ctx, binaryPath, u.currentVersion, u.repository())
	if err != nil {
		_ = u.RestoreBackup(backupPath, binaryPath)
		return "", fmt.Errorf("failed to update binary: %w", err)
	}

	return backupPath, nil
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

// IsNewVersionAvailable checks if the latest release version is greater than the current.
func IsNewVersionAvailable(current, latest string) bool {
	if current == "" || latest == "" {
		return false
	}
	rel := &selfupdate.Release{
		AssetName: fmt.Sprintf("dummy_%s_linux_amd64.tar.gz", latest),
		Name:      latest,
	}
	return rel.GreaterThan(current)
}