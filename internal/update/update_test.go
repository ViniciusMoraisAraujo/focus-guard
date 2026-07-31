package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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

// --- Test helpers ---

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