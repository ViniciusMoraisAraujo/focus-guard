package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"

	"focusguard/internal/domain/recovery"
)

func TestNewUpdater(t *testing.T) {
	u := NewUpdater("testowner", "testrepo")
	if u == nil {
		t.Fatal("NewUpdater returned nil")
	}
	if u.owner != "testowner" {
		t.Errorf("expected owner testowner, got %s", u.owner)
	}
	if u.repo != "testrepo" {
		t.Errorf("expected repo testrepo, got %s", u.repo)
	}
}

func TestNewUpdaterWithVersion(t *testing.T) {
	u := NewUpdater("owner", "repo", WithVersion("1.0.0"))
	if u.currentVersion != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", u.currentVersion)
	}
}

func TestWithVersionEmpty(t *testing.T) {
	u := NewUpdater("o", "r", WithVersion(""))
	if u.currentVersion != "" {
		t.Errorf("expected empty version when empty string given, got %s", u.currentVersion)
	}
}

func TestCheckForUpdate_NoNewVersion(t *testing.T) {
	t.Parallel()

	server := newTestGitHubServer(t, "v1.0.0", false)
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result (no update), got %+v", result)
	}
}

func TestCheckForUpdate_NewVersion(t *testing.T) {
	t.Parallel()

	server := newTestGitHubServer(t, "v1.1.0", false)
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a result (new version available), got nil")
	}
	if result.Version != "1.1.0" {
		t.Errorf("expected version 1.1.0, got %s", result.Version)
	}
}

func TestCheckForUpdate_NoReleases(t *testing.T) {
	t.Parallel()

	server := newTestGitHubServer(t, "", true)
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result (no releases), got %+v", result)
	}
}

func TestCheckForUpdate_DevVersion(t *testing.T) {
	t.Parallel()

	server := newTestGitHubServer(t, "v1.0.0", false)
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("0.0.0-dev"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for dev version, got %+v", result)
	}
}

func TestApplyUpdate_InvalidPath(t *testing.T) {
	u := NewUpdater("o", "r")
	backupPath, err := u.ApplyUpdate("/nonexistent/path/to/binary")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
	if backupPath != "" {
		t.Errorf("expected empty backup path on error, got %s", backupPath)
	}
}

func TestApplyUpdate_Success(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "focusguard-daemon")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	initialContent := []byte("old-binary-content")
	if err := os.WriteFile(binaryPath, initialContent, 0755); err != nil {
		t.Fatalf("failed to write initial binary: %v", err)
	}

	u := NewUpdater("o", "r")

	backupPath, err := u.ApplyUpdate(binaryPath)
	if err != nil {
		t.Fatalf("ApplyUpdate returned error: %v", err)
	}

	if backupPath == "" {
		t.Fatal("expected backup path, got empty")
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Errorf("backup file does not exist at %s", backupPath)
	}

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Errorf("original binary was removed unexpectedly")
	}
}

func TestCheckForUpdate_HTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Logf("CheckForUpdate returned error (acceptable): %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on HTTP error, got %+v", result)
	}
}

func TestCheckForUpdate_PreRelease(t *testing.T) {
	t.Parallel()

	server := newTestGitHubServer(t, "v1.1.0-rc1", false)
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a result even for pre-release, got nil")
	}
	if result.Version != "1.1.0-rc1" {
		t.Errorf("expected version 1.1.0-rc1, got %s", result.Version)
	}
}

// ---------------------------------------------------------------------------
// Update channels (beta vs. stable)
// ---------------------------------------------------------------------------

// newPrereleaseGitHubServer serves a release list with the latest tag marked
// as prerelease, so tests can prove the beta channel opts in to prereleases
// while the stable channel skips them.
func newPrereleaseGitHubServer(t *testing.T, stableTag, betaTag string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		// O asset precisa terminar com o sufixo OS_arch do SO atual, senão o
		// DetectLatest nunca o encontra (assetMatchSuffixes). Linux + Windows.
		linux := `{
			"name": "focusguard_` + betaTag + `_linux_amd64.tar.gz",
			"browser_download_url": "` + baseURL + `/focusguard_` + betaTag + `_linux_amd64.tar.gz"
		},{
			"name": "focusguard_` + stableTag + `_linux_amd64.tar.gz",
			"browser_download_url": "` + baseURL + `/focusguard_` + stableTag + `_linux_amd64.tar.gz"
		},`
		windows := `{
			"name": "focusguard_` + betaTag + `_windows_amd64.zip",
			"browser_download_url": "` + baseURL + `/focusguard_` + betaTag + `_windows_amd64.zip"
		},{
			"name": "focusguard_` + stableTag + `_windows_amd64.zip",
			"browser_download_url": "` + baseURL + `/focusguard_` + stableTag + `_windows_amd64.zip"
		},`
		body := `[{
			"tag_name": "` + betaTag + `",
			"name": "` + betaTag + `",
			"prerelease": true,
			"assets": [` + linux + windows + `
				{
					"name": "checksums.txt",
					"browser_download_url": "` + baseURL + `/checksums.txt"
				}
			]
		},{
			"tag_name": "` + stableTag + `",
			"name": "` + stableTag + `",
			"prerelease": false,
			"assets": [` + linux + windows + `
				{
					"name": "checksums.txt",
					"browser_download_url": "` + baseURL + `/checksums.txt"
				}
			]
		}]`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// TestWithChannel_Beta_OptsIntoPrerelease verifies the beta channel sees a
// prerelease that is newer than the current version, while the default (stable)
// updater skips prereleases entirely.
func TestWithChannel_Beta_OptsIntoPrerelease(t *testing.T) {
	server := newPrereleaseGitHubServer(t, "v1.0.0", "v1.1.0-rc1")
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithChannel("beta"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate erro: %v", err)
	}
	if result == nil {
		t.Fatal("beta channel deveria detectar a prerelease v1.1.0-rc1")
	}
	if result.Version != "1.1.0-rc1" {
		t.Errorf("esperava v1.1.0-rc1, got %s", result.Version)
	}
}

// TestStableChannel_SkipsPrerelease verifies the stable updater does NOT report
// a prerelease-only update — prereleases are opt-in via the beta channel.
func TestStableChannel_SkipsPrerelease(t *testing.T) {
	server := newPrereleaseGitHubServer(t, "v1.0.0", "v1.1.0-rc1")
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate erro: %v", err)
	}
	if result != nil {
		t.Errorf("stable channel não deveria detectar a prerelease, got %+v", result)
	}
}

// TestSetChannel_SwitchesStableToBeta verifies SetChannel reconfigures an
// existing updater so the daemon can honor a per-request channel without
// rebuilding the whole Updater.
func TestSetChannel_SwitchesStableToBeta(t *testing.T) {
	server := newPrereleaseGitHubServer(t, "v1.0.0", "v1.1.0-rc1")
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	// Estável primeiro: sem update.
	if res, _ := u.CheckForUpdate(context.Background()); res != nil {
		t.Fatalf("stable não deveria ver a prerelease, got %+v", res)
	}

	u.SetChannel("beta")

	res, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate após SetChannel: %v", err)
	}
	if res == nil || res.Version != "1.1.0-rc1" {
		t.Errorf("SetChannel(beta) deveria habilitar prereleases, got %+v", res)
	}
}

// TestSetChannel_ConcurrentRequests_NoRace simulates the daemon serving IPC
// update requests from multiple goroutines at once (each connection runs in its
// own goroutine) while they flip between channels. Under -race this proves
// SetChannel + CheckForUpdate are safe to call concurrently.
func TestSetChannel_ConcurrentRequests_NoRace(t *testing.T) {
	server := newPrereleaseGitHubServer(t, "v1.0.0", "v1.1.0-rc1")
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	const goroutines = 8
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if (i+j)%2 == 0 {
					u.SetChannel("beta")
				} else {
					u.SetChannel("stable")
				}
				// Resultado pode variar conforme o snapshot — o que importa
				// aqui é não haver data race.
				_, _ = u.CheckForUpdate(context.Background())
			}
		}(i)
	}
	wg.Wait()

	// Após o stress, o updater deve permanecer íntegro (não-nil).
	if u.getUpdater() == nil {
		t.Error("updater ficou nil após concorrência")
	}
}

func TestCheckForUpdate_InvalidSemver(t *testing.T) {
	t.Parallel()

	server := newTestGitHubServer(t, "not-a-valid-version", false)
	defer server.Close()

	u := NewUpdater("testowner", "testrepo",
		WithVersion("1.0.0"),
		WithGitHubAPI(server.URL+"/api/v3/"),
	)

	result, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for invalid semver tag, got %+v", result)
	}
}

// ---------------------------------------------------------------------------
// IsNewVersionAvailable (semver, sem hack do dummy release)
// ---------------------------------------------------------------------------

func TestIsNewVersionAvailable(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"newer", "1.0.0", "1.1.0", true},
		{"older", "1.1.0", "1.0.0", false},
		{"equal", "1.0.0", "1.0.0", false},
		{"empty current", "", "1.0.0", false},
		{"empty latest", "1.0.0", "", false},
		{"both empty", "", "", false},
		{"v prefix", "v1.0.0", "v1.1.0", true},
		{"mixed v prefix", "1.0.0", "v1.1.0", true},
		{"pre-release newer", "1.0.0", "1.1.0-rc1", true},
		{"pre-release current", "1.0.0-rc1", "1.0.0", true},
		{"invalid current", "not-a-version", "1.0.0", false},
		{"invalid latest", "1.0.0", "not-a-version", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewVersionAvailable(tt.current, tt.latest); got != tt.want {
				t.Errorf("IsNewVersionAvailable(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// UpdateToAll (multi-binary update com rollback atômico)
// ---------------------------------------------------------------------------

// writeBinary creates a binary file with the given content for the tests.
func writeBinary(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func readBinary(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(data)
}

func TestUpdateToAll_UpdatesEveryBinary(t *testing.T) {
	dir := t.TempDir()
	bins := []string{
		writeBinary(t, dir, "focusguard", "old-focusguard"),
		writeBinary(t, dir, "focusguard-daemon", "old-daemon"),
		writeBinary(t, dir, "focusguard-tray", "old-tray"),
	}
	origContent := make(map[string]string, len(bins))
	for _, b := range bins {
		origContent[b] = readBinary(t, b)
	}

	// Arquivo de release com os binários novos, embrulhado em um diretório
	// (como o GoReleaser faz com wrap_in_directory) — prova que o UpdateToAll
	// acha os binários recursivamente.
	archive := filepath.Join(t.TempDir(), "focusguard_1.1.0.zip")
	wantContent := map[string]string{
		"focusguard":        "new-focusguard",
		"focusguard-daemon": "new-daemon",
		"focusguard-tray":   "new-tray",
	}
	makeTestZip(t, archive, "focusguard_1.1.0", wantContent)
	server := serveZip(t, archive)
	defer server.Close()

	u := NewUpdater("o", "r")
	result := &UpdateResult{Version: "1.1.0", Release: &selfupdate.Release{
		AssetURL:  server.URL + "/focusguard_1.1.0.zip",
		AssetName: "focusguard_1.1.0.zip",
	}}

	backups, err := u.UpdateToAll(context.Background(), result, bins)
	if err != nil {
		t.Fatalf("UpdateToAll erro: %v", err)
	}
	if len(backups) != len(bins) {
		t.Fatalf("backups = %d, want %d", len(backups), len(bins))
	}
	for _, b := range bins {
		if got := readBinary(t, b); got != wantContent[filepath.Base(b)] {
			t.Errorf("%s = %q, want %q", b, got, wantContent[filepath.Base(b)])
		}
	}
	// Backups preservam o conteúdo antigo (rollback possível).
	for i, b := range bins {
		if got := readBinary(t, backups[i]); got != origContent[b] {
			t.Errorf("backup %s = %q, want %q", backups[i], got, origContent[b])
		}
	}
}

func TestUpdateToAll_RollsBackAllOnFailure(t *testing.T) {
	origReplace := replaceOneBinary
	defer func() { replaceOneBinary = origReplace }()

	var replaceCalls int
	replaceOneBinary = func(_ string, targetPath string) error {
		replaceCalls++
		if replaceCalls == 2 { // segundo binário falha
			return errors.New("file locked")
		}
		return os.WriteFile(targetPath, []byte("new-version"), 0755)
	}

	dir := t.TempDir()
	bins := []string{
		writeBinary(t, dir, "focusguard", "old-focusguard"),
		writeBinary(t, dir, "focusguard-daemon", "old-daemon"),
		writeBinary(t, dir, "focusguard-tray", "old-tray"),
	}

	archive := filepath.Join(t.TempDir(), "focusguard.zip")
	makeTestZip(t, archive, "", map[string]string{
		"focusguard":        "new-focusguard",
		"focusguard-daemon": "new-daemon",
		"focusguard-tray":   "new-tray",
	})
	server := serveZip(t, archive)
	defer server.Close()

	u := NewUpdater("o", "r")
	result := &UpdateResult{Version: "1.1.0", Release: &selfupdate.Release{
		AssetURL:  server.URL + "/focusguard.zip",
		AssetName: "focusguard.zip",
	}}

	if _, err := u.UpdateToAll(context.Background(), result, bins); err == nil {
		t.Fatal("expected error when a binary fails to update")
	}

	// O primeiro binário já atualizado deve ter sido restaurado ao original.
	if got := readBinary(t, bins[0]); got != "old-focusguard" {
		t.Errorf("binário já atualizado deveria ser restaurado, got %q", got)
	}
	// O binário que falhou nunca foi tocado.
	if got := readBinary(t, bins[1]); got != "old-daemon" {
		t.Errorf("binário que falhou deveria permanecer original, got %q", got)
	}
}

// TestUpdateToAll_PrevalidatesMissingBinary verifies the fail-fast step: when a
// local binary has no match in the release archive, the update aborts BEFORE
// any backup/replace happens — nothing is modified, no rollback needed.
func TestUpdateToAll_PrevalidatesMissingBinary(t *testing.T) {
	dir := t.TempDir()
	bins := []string{
		writeBinary(t, dir, "focusguard", "old-focusguard"),
		writeBinary(t, dir, "focusguard-daemon", "old-daemon"),
	}

	// focusguard-daemon ausente do arquivo de release.
	archive := filepath.Join(t.TempDir(), "focusguard.zip")
	makeTestZip(t, archive, "", map[string]string{"focusguard": "new-focusguard"})
	server := serveZip(t, archive)
	defer server.Close()

	u := NewUpdater("o", "r")
	result := &UpdateResult{Version: "1.1.0", Release: &selfupdate.Release{
		AssetURL:  server.URL + "/focusguard.zip",
		AssetName: "focusguard.zip",
	}}

	if _, err := u.UpdateToAll(context.Background(), result, bins); err == nil {
		t.Fatal("expected error when a local binary is missing from the archive")
	}
	origContent := map[string]string{
		bins[0]: "old-focusguard",
		bins[1]: "old-daemon",
	}
	for _, b := range bins {
		if got := readBinary(t, b); got != origContent[b] {
			t.Errorf("%s deveria permanecer original (fail-fast), got %q", b, got)
		}
	}
}

func TestUpdateToAll_NilResult(t *testing.T) {
	u := NewUpdater("o", "r")
	if _, err := u.UpdateToAll(context.Background(), nil, []string{"/tmp/x"}); err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestUpdateToAll_NoBinaries(t *testing.T) {
	u := NewUpdater("o", "r")
	result := &UpdateResult{Version: "1.1.0", Release: &selfupdate.Release{}}
	backups, err := u.UpdateToAll(context.Background(), result, nil)
	if err != nil {
		t.Fatalf("UpdateToAll com lista vazia não deve errar: %v", err)
	}
	if backups != nil {
		t.Errorf("expected nil backups for empty list, got %v", backups)
	}
}

// ---------------------------------------------------------------------------
// downloadFile, extractArchive, findFileByName, replaceOneBinary, RestoreBackup
// ---------------------------------------------------------------------------

func TestDownloadFile_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("archive-bytes"))
	}))
	defer server.Close()

	dst := filepath.Join(t.TempDir(), "downloaded")
	if err := downloadFile(context.Background(), server.URL+"/asset.zip", dst); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "archive-bytes" {
		t.Errorf("content = %q, want archive-bytes", string(data))
	}
}

func TestDownloadFile_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if err := downloadFile(context.Background(), server.URL+"/asset.zip", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestExtractArchive_Zip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "focusguard.zip")
	makeTestZip(t, archive, "wrapped", map[string]string{
		"focusguard":  "bin-content",
		"install.txt": "readme",
	})

	dest := filepath.Join(dir, "extracted")
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "wrapped", "focusguard"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(got) != "bin-content" {
		t.Errorf("extracted = %q, want bin-content", string(got))
	}
}

func TestExtractArchive_TarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "focusguard.tar.gz")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{"focusguard": "tar-bin"}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write tar.gz: %v", err)
	}

	dest := filepath.Join(dir, "extracted")
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "focusguard"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(got) != "tar-bin" {
		t.Errorf("extracted = %q, want tar-bin", string(got))
	}
}

func TestExtractArchive_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	w, _ := zw.Create("../../evil.txt")
	_, _ = w.Write([]byte("evil"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	if err := extractArchive(archive, filepath.Join(dir, "dest")); err == nil {
		t.Fatal("expected error for traversal entry name")
	}
}

func TestFindFileByName(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "wrap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wrap", "focusguard-daemon"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findFileByName(dir, "focusguard-daemon")
	if err != nil {
		t.Fatalf("findFileByName: %v", err)
	}
	if want := filepath.Join(dir, "wrap", "focusguard-daemon"); got != want {
		t.Errorf("found = %q, want %q", got, want)
	}

	if _, err := findFileByName(dir, "missing"); err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestReplaceBinary_SwapsAndPreservesExec(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "new")
	target := filepath.Join(dir, "focusguard")
	if err := os.WriteFile(newPath, []byte("new-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replaceOneBinary(newPath, target); err != nil {
		t.Fatalf("replaceOneBinary: %v", err)
	}
	if got := readBinary(t, target); got != "new-content" {
		t.Errorf("target = %q, want new-content", got)
	}
	// O modo deve ter sido garantido como executável (o Windows não reporta
	// os bits de execução — o write bit 0o200 é que reflete a permissão real).
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("target deveria ser executável, mode=%v", info.Mode())
		}
	}
}

func TestRestoreBackup_MissingBackup(t *testing.T) {
	u := NewUpdater("o", "r")
	if err := u.RestoreBackup("/nonexistent.bak", "/whatever"); err == nil {
		t.Fatal("expected error for missing backup")
	}
}

// TestReplaceOneBinary_RetriesTransientRenameLock simula o lock transitório
// (ex.: antivírus) no Windows: o primeiro rename-aside falha e o retry conclui
// a troca. Fora do Windows o retry é desligado (tentativa única, comportamento
// histórico).
func TestReplaceOneBinary_RetriesTransientRenameLock(t *testing.T) {
	origGoos, origRetry, origRename := goos, renameRetryEnabled, osRename
	goos = "windows"
	renameRetryEnabled = true
	defer func() { goos, renameRetryEnabled, osRename = origGoos, origRetry, origRename }()

	dir := t.TempDir()
	newPath := filepath.Join(dir, "new")
	target := filepath.Join(dir, "focusguard")
	if err := os.WriteFile(newPath, []byte("new-content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	var renameCalls int
	osRename = func(old, new string) error {
		renameCalls++
		if renameCalls == 1 && strings.HasSuffix(old, "focusguard") {
			return errors.New("Acesso negado.") // lock transitório
		}
		return os.Rename(old, new)
	}

	if err := replaceOneBinary(newPath, target); err != nil {
		t.Fatalf("replaceOneBinary: %v", err)
	}
	if got := readBinary(t, target); got != "new-content" {
		t.Errorf("target = %q, want new-content", got)
	}
	if renameCalls < 2 {
		t.Errorf("esperava retry no rename-aside, renameCalls = %d", renameCalls)
	}
}

// TestUpdateToAll_SchedulesRebootWhenFirstBinaryLocked verifica o fallback
// move-on-reboot: quando o PRIMEIRO binário (o daemon, que não pode ser
// parado) falha no rename-aside mesmo com retry e nada foi trocado ainda, a
// suíte inteira é agendada para o próximo boot e o UpdateToAll retorna
// ErrScheduledOnReboot — sem rollback e sem meia-atualização.
func TestUpdateToAll_SchedulesRebootWhenFirstBinaryLocked(t *testing.T) {
	origGoos, origRetry, origReplace, origSchedule := goos, renameRetryEnabled, replaceOneBinary, scheduleReplaceOnReboot
	goos = "windows"
	renameRetryEnabled = true
	defer func() {
		goos, renameRetryEnabled, replaceOneBinary, scheduleReplaceOnReboot = origGoos, origRetry, origReplace, origSchedule
	}()

	dir := t.TempDir()
	bins := []string{
		writeBinary(t, dir, "focusguard-daemon", "old-daemon"),
		writeBinary(t, dir, "focusguard-tray", "old-tray"),
	}

	replaceOneBinary = func(_, _ string) error {
		return errors.New("rename ... Acesso negado.") // sempre falha
	}

	var scheduled []string
	scheduleReplaceOnReboot = func(targetPath, _ string) error {
		scheduled = append(scheduled, targetPath)
		return nil
	}

	archive := filepath.Join(t.TempDir(), "focusguard.zip")
	makeTestZip(t, archive, "", map[string]string{
		"focusguard-daemon": "new-daemon",
		"focusguard-tray":   "new-tray",
	})
	server := serveZip(t, archive)
	defer server.Close()

	u := NewUpdater("o", "r")
	result := &UpdateResult{Version: "1.1.0", Release: &selfupdate.Release{
		AssetURL:  server.URL + "/focusguard.zip",
		AssetName: "focusguard.zip",
	}}

	backups, err := u.UpdateToAll(context.Background(), result, bins)
	if !errors.Is(err, ErrScheduledOnReboot) {
		t.Fatalf("esperava ErrScheduledOnReboot, got %v", err)
	}
	// Os .bak ficam para o smart recovery do watchdog pós-reboot.
	if len(backups) != len(bins) {
		t.Errorf("backups = %d, want %d", len(backups), len(bins))
	}
	// Todos os binários existentes foram agendados para o boot.
	if len(scheduled) != len(bins) {
		t.Fatalf("agendados = %v, want %d binários", scheduled, len(bins))
	}
	// Nada foi trocado no disco — tudo continua na versão antiga.
	if got := readBinary(t, bins[0]); got != "old-daemon" {
		t.Errorf("daemon deveria permanecer antigo, got %q", got)
	}
	if got := readBinary(t, bins[1]); got != "old-tray" {
		t.Errorf("tray deveria permanecer antigo, got %q", got)
	}
	// Os estágios .new ficam ao lado dos binários para o boot consumir.
	for _, b := range bins {
		staged := filepath.Join(dir, "."+filepath.Base(b)+".new")
		if _, err := os.Stat(staged); err != nil {
			t.Errorf("estágio %s deveria existir: %v", staged, err)
		}
	}
}

// TestCleanupStale_KeepsOnlyNewestBackupPerBinary verifies the core of Bug 1:
// each update leaves one .bak per binary and, without a sweep, they accumulate
// forever. CleanupStale must keep ONLY the newest .bak per binary.
func TestCleanupStale_KeepsOnlyNewestBackupPerBinary(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon.exe")
	for _, ts := range []string{"20260801100000", "20260802090000", "20260802100000"} {
		if err := os.WriteFile(daemon+".bak."+ts, []byte("backup "+ts), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cli := filepath.Join(dir, "focusguard.exe")
	if err := os.WriteFile(cli+".bak.20260801100000", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	u := NewUpdater("o", "r")
	u.CleanupStale(dir)

	remaining, _ := filepath.Glob(daemon + ".bak.*")
	if len(remaining) != 1 || !strings.Contains(remaining[0], "20260802100000") {
		t.Errorf("expected only the newest daemon backup, got %v", remaining)
	}
	cliBaks, _ := filepath.Glob(cli + ".bak.*")
	if len(cliBaks) != 1 {
		t.Errorf("expected the single cli backup kept, got %v", cliBaks)
	}
}

// TestCleanupStale_RemovesOrphans verifies the .old/.trash/stale-download
// cleanup: artifacts of crashed updates or failed restores must not linger.
func TestCleanupStale_RemovesOrphans(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		".focusguard-daemon.exe.old",  // replaceOneBinary move-aside
		"focusguard-daemon.exe.trash", // RestoreBackup move-aside
		"focusguard-daemon-new.exe",   // download antigo nunca trocado
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	u := NewUpdater("o", "r")
	u.CleanupStale(dir)

	for _, name := range []string{
		".focusguard-daemon.exe.old",
		"focusguard-daemon.exe.trash",
		"focusguard-daemon-new.exe",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s deveria ter sido removido", name)
		}
	}
}

// TestCleanupStale_KeepsRealBinaries verifies the sweep never touches the
// actual installed binaries — only their backup/artifact suffixes.
func TestCleanupStale_KeepsRealBinaries(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "focusguard-daemon.exe")
	if err := os.WriteFile(binary, []byte("real"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := NewUpdater("o", "r")
	u.CleanupStale(dir)

	if _, err := os.Stat(binary); err != nil {
		t.Error("binário real não pode ser removido pela varredura")
	}
}

// TestCleanupStale_ExpiresBackupsPastRetentionWindow verifies the age-out of
// Bug 1: o .bak mais novo por binário também é removido quando passa da janela
// de retenção do smart recovery (recovery.BackupMaxAge) — o watchdog
// (FindRecentBackup) nunca consome backups mais velhos que isso, então um
// .bak expirado é só a "versão antiga" acumulada na pasta de instalação.
func TestCleanupStale_ExpiresBackupsPastRetentionWindow(t *testing.T) {
	// Só o backup antigo: mesmo sendo o único (o mais novo por binário), a
	// varredura o expira quando ele passa da janela.
	dir := t.TempDir()
	oldBak := filepath.Join(dir, "focusguard-daemon.exe.bak.20260801000000")
	if err := os.WriteFile(oldBak, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-(recovery.BackupMaxAge + time.Hour))
	if err := os.Chtimes(oldBak, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	u := NewUpdater("o", "r")
	u.CleanupStale(dir)

	if _, err := os.Stat(oldBak); !os.IsNotExist(err) {
		t.Error("backup além da janela de retenção deveria ser removido mesmo sendo o mais novo")
	}

	// Controle positivo: um backup fresco (dentro da janela) permanece — o
	// smart recovery ainda pode precisar dele.
	freshDir := t.TempDir()
	freshBak := filepath.Join(freshDir, "focusguard-daemon.exe.bak.20260811100000")
	if err := os.WriteFile(freshBak, []byte("fresh"), 0o644); err != nil {
		t.Fatal(err)
	}

	u.CleanupStale(freshDir)

	if _, err := os.Stat(freshBak); err != nil {
		t.Errorf("backup dentro da janela deveria permanecer: %v", err)
	}
}

func TestRestoreBackup_RestoresContent(t *testing.T) {
	dir := t.TempDir()
	backupPath := filepath.Join(dir, "focusguard.bak.1")
	originalPath := filepath.Join(dir, "focusguard")
	if err := os.WriteFile(backupPath, []byte("old-content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalPath, []byte("new-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := NewUpdater("o", "r")
	if err := u.RestoreBackup(backupPath, originalPath); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if got := readBinary(t, originalPath); got != "old-content" {
		t.Errorf("original = %q, want old-content", got)
	}
}

// TestRestoreBackup_WindowsRenamesBeforeCopy exercises the Windows branch on
// any platform: the original must be renamed aside (liberating the file lock)
// before copyFile creates a fresh file, and the .trash must be cleaned up.
func TestRestoreBackup_WindowsRenamesBeforeCopy(t *testing.T) {
	dir := t.TempDir()
	backupPath := filepath.Join(dir, "focusguard.bak.1")
	originalPath := filepath.Join(dir, "focusguard")
	if err := os.WriteFile(backupPath, []byte("old-content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalPath, []byte("new-content"), 0o755); err != nil {
		t.Fatal(err)
	}

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

	u := NewUpdater("o", "r")
	if err := u.RestoreBackup(backupPath, originalPath); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if got := readBinary(t, originalPath); got != "old-content" {
		t.Errorf("original = %q, want old-content", got)
	}
	if len(renamed) != 1 || renamed[0] != originalPath {
		t.Errorf("esperava rename de %q antes do copy, got %v", originalPath, renamed)
	}
	if _, err := os.Stat(originalPath + ".trash"); !os.IsNotExist(err) {
		t.Error(".trash deveria ter sido removido após o restore")
	}
}

// --- Test helpers ---

// makeTestZip writes a zip archive at path containing files (relative
// entry name → content). A non-empty wrap simulates GoReleaser's
// wrap_in_directory layout by prefixing every entry with a folder.
func makeTestZip(t *testing.T, path, wrap string, files map[string]string) {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, content := range files {
		entry := name
		if wrap != "" {
			entry = filepath.ToSlash(filepath.Join(wrap, name))
		}
		w, err := zw.Create(entry)
		if err != nil {
			t.Fatalf("zip.Create(%s): %v", entry, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip.Write(%s): %v", entry, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
}

// serveZip serves the bytes of an existing zip archive over HTTP.
func serveZip(t *testing.T, path string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(data)
	}))
}

func newTestGitHubServer(t *testing.T, tagName string, emptyReleases bool) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if emptyReleases {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}

		versionForAsset := tagName
		if tagName == "" {
			versionForAsset = "v1.0.0"
		}

		baseURL := "http://" + r.Host

		body := `[{
			"tag_name": "` + tagName + `",
			"name": "` + tagName + `",
			"prerelease": false,
			"assets": [
				{
					"name": "checksums.txt",
					"browser_download_url": "` + baseURL + `/checksums.txt"
				},
				{
					"name": "focusguard_` + versionForAsset + `_linux_amd64.tar.gz",
					"browser_download_url": "` + baseURL + `/focusguard_` + versionForAsset + `_linux_amd64.tar.gz"
				},
				{
					"name": "focusguard_` + versionForAsset + `_linux_arm64.tar.gz",
					"browser_download_url": "` + baseURL + `/focusguard_` + versionForAsset + `_linux_arm64.tar.gz"
				},
				{
					"name": "focusguard_` + versionForAsset + `_windows_amd64.zip",
					"browser_download_url": "` + baseURL + `/focusguard_` + versionForAsset + `_windows_amd64.zip"
				},
				{
					"name": "focusguard_` + versionForAsset + `_windows_arm64.zip",
					"browser_download_url": "` + baseURL + `/focusguard_` + versionForAsset + `_windows_arm64.zip"
				}
			]
		}]`

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func serverURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
