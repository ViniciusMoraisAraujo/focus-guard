package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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

	if !IsNewVersionAvailable(u.currentVersion, latest.Version()) {
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

// UpdateToAll downloads the release archive ONCE and replaces every given
// binary (daemon, CLI, tray, watchdog, web). The old implementation called
// go-selfupdate's UpdateCommand once per binary, which re-downloaded the same
// archive N times and hammered the GitHub API. The new flow downloads the asset
// a single time, extracts it to a temp dir, and swaps each binary from the
// extracted tree. It is all-or-nothing: every binary is backed up first and,
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

	// Fase 1 — baixa o arquivo UMA vez e extrai para um diretório temporário.
	tmpDir, err := os.MkdirTemp("", "focusguard-update-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir) // limpa a sujeira ao final

	// Mantém a extensão do AssetName (.zip/.tar.gz) — extractArchive decide o
	// formato pela extensão do arquivo baixado.
	archiveName := filepath.Base(result.Release.AssetName)
	if archiveName == "" || archiveName == "." {
		archiveName = "update_archive"
	}
	archivePath := filepath.Join(tmpDir, archiveName)
	if err := downloadFile(ctx, result.Release.AssetURL, archivePath); err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", result.Release.AssetName, err)
	}
	if err := extractArchive(archivePath, tmpDir); err != nil {
		return nil, fmt.Errorf("failed to extract %s: %w", result.Release.AssetName, err)
	}

	// Fase 2 — fail-fast antes de tocar em qualquer binário: todo binário local
	// presente precisa existir no arquivo extraído (all-or-nothing sem ter que
	// desfazer backup).
	for _, b := range binaries {
		if _, err := os.Stat(b); os.IsNotExist(err) {
			continue // binário não instalado nesta máquina (ex.: tray opcional)
		}
		if _, err := findFileByName(tmpDir, filepath.Base(b)); err != nil {
			return nil, fmt.Errorf("failed to update binary %s: %w", b, err)
		}
	}

	// Fase 3 — backup de todos antes de tocar em qualquer binário. A convenção
	// .bak.<timestamp> é consumida pelo internal/recovery (watchdog) — não
	// mudar o formato.
	backups := make([]string, 0, len(binaries))
	for _, b := range binaries {
		if _, err := os.Stat(b); os.IsNotExist(err) {
			continue
		}
		backupPath := b + ".bak." + time.Now().Format("20060102150405")
		if err := copyFile(b, backupPath); err != nil {
			u.cleanupBackups(backups)
			return nil, fmt.Errorf("failed to back up %s: %w", b, err)
		}
		backups = append(backups, backupPath)
	}

	// Fase 4 — aplica a atualização em cada binário presente, com rollback
	// atômico em qualquer falha. okPaths acompanha a mesma ordem de backups
	// (ambos só contêm binários existentes), então okPaths[i] ↔ backups[i].
	var okPaths []string
	rollback := func(cause error) ([]string, error) {
		for i, bk := range backups {
			if i < len(okPaths) {
				if rerr := u.RestoreBackup(bk, okPaths[i]); rerr != nil {
					return nil, fmt.Errorf("%v; além disso, falha ao restaurar %s: %w", cause, okPaths[i], rerr)
				}
			}
		}
		u.cleanupBackups(backups)
		return nil, cause
	}

	for _, b := range binaries {
		if _, err := os.Stat(b); os.IsNotExist(err) {
			continue
		}
		extracted, err := findFileByName(tmpDir, filepath.Base(b))
		if err != nil {
			return rollback(fmt.Errorf("failed to update binary %s: %w", b, err))
		}
		if err := replaceOneBinary(extracted, b); err != nil {
			return rollback(fmt.Errorf("failed to update binary %s: %w", b, err))
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

// osRename é stubbable nos testes (o main real usa os.Rename).
var osRename = os.Rename

// goos é stubbable nos testes para exercitar os caminhos do Windows em
// qualquer plataforma (espelha o var goos do daemon).
var goos = runtime.GOOS

// RestoreBackup restores a backup binary to the original path. On Windows a
// running executable cannot be overwritten or deleted, but it CAN be renamed:
// the original is moved aside first so copyFile can create a fresh file, then
// the moved-aside file is removed best-effort.
func (u *Updater) RestoreBackup(backupPath, originalPath string) error {
	if backupPath == "" || originalPath == "" {
		return fmt.Errorf("backup and original paths must not be empty")
	}
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found at %s", backupPath)
	}

	// Windows não permite sobrescrever um executável em execução com os.Create:
	// renomear o destino para um .trash libera o file lock do SO.
	if goos == "windows" {
		if _, err := os.Stat(originalPath); err == nil {
			if err := osRename(originalPath, originalPath+".trash"); err == nil {
				defer func() { _ = os.Remove(originalPath + ".trash") }()
			}
		}
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

// downloadFile streams the given URL into destPath, honoring ctx (the daemon
// passes the IPC request timeout). Non-200 responses are treated as errors.
func downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// extractArchive extracts a release archive (.zip or .tar.gz/.tgz, matching
// what GoReleaser publishes) into destDir, preserving the directory structure
// so the update works whether the archive wraps its files in a directory or
// not. Entry names are sanitized against path traversal (zip-slip).
func extractArchive(archivePath, destDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZip(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".tar.gz"), strings.HasSuffix(archivePath, ".tgz"):
		return extractTarGz(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", archivePath)
	}
}

func extractZip(archivePath, destDir string) error {
	z, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		target, err := safeArchivePath(destDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if err := writeArchiveFile(rc, target); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeArchivePath(destDir, hdr.Name)
		if err != nil {
			return err
		}
		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := writeArchiveFile(io.NopCloser(tr), target); err != nil {
			return err
		}
	}
}

// safeArchivePath joins an archive entry name under destDir, rejecting
// absolute paths and traversal (zip-slip) attempts.
func safeArchivePath(destDir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive entry name: %q", name)
	}
	return filepath.Join(destDir, clean), nil
}

// writeArchiveFile writes an archive entry into target, creating parent
// directories as needed.
func writeArchiveFile(src io.ReadCloser, target string) error {
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// findFileByName returns the path of the first regular file under root whose
// basename matches name. The release archive may or may not wrap its files in
// a directory, so a recursive search is required.
func findFileByName(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == name {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("binary %q not found in extracted archive", name)
	}
	return found, nil
}

// replaceOneBinary swaps the binary at targetPath with the newly downloaded
// one at newPath using the rename technique (the same one go-selfupdate's
// apply uses): the new file is copied into the target directory first so the
// renames stay on a single volume, the current binary is renamed aside —
// Windows locks a running executable against overwrite/delete but allows
// rename — and the new file is renamed into place. It is a package-level var
// so tests can stub the per-binary step for the rollback flow.
var replaceOneBinary = func(newPath, targetPath string) error {
	dir := filepath.Dir(targetPath)
	name := filepath.Base(targetPath)

	newLocal := filepath.Join(dir, "."+name+".new")
	if err := copyFile(newPath, newLocal); err != nil {
		return fmt.Errorf("failed to stage new binary: %w", err)
	}
	defer os.Remove(newLocal)
	// Garante execução mesmo se a entrada extraída não carregar o bit.
	if err := os.Chmod(newLocal, 0o755); err != nil {
		return err
	}

	oldPath := filepath.Join(dir, "."+name+".old")
	_ = os.Remove(oldPath) // remove .old órfão de um update anterior
	if err := os.Rename(targetPath, oldPath); err != nil {
		return fmt.Errorf("failed to move current binary aside: %w", err)
	}
	if err := os.Rename(newLocal, targetPath); err != nil {
		_ = os.Rename(oldPath, targetPath) // reverte a troca deste binário
		return fmt.Errorf("failed to move new binary into place: %w", err)
	}
	_ = os.Remove(oldPath) // best-effort: falha no Windows com o binário em uso
	return nil
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
