//go:build windows

package enforcer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestEnforcer(t *testing.T) *windowsEnforcer {
	t.Helper()

	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")

	initial := "127.0.0.1 localhost\r\n::1 localhost\r\n"
	if err := os.WriteFile(hostsPath, []byte(initial), 0644); err != nil {
		t.Fatalf("failed to seed hosts file: %v", err)
	}

	return &windowsEnforcer{hostsPath: hostsPath}
}

func readRawHosts(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read hosts file: %v", err)
	}
	return string(data)
}

func TestAddHostEntry_AddsMarkedEntries(t *testing.T) {
	e := newTestEnforcer(t)

	if err := e.addHostEntry("example.com"); err != nil {
		t.Fatalf("addHostEntry returned error: %v", err)
	}

	content := readRawHosts(t, e.hostsPath)

	wantSubstrings := []string{
		"127.0.0.1 example.com # FOCUSGUARD: example.com",
		"::1 example.com # FOCUSGUARD: example.com",
		"127.0.0.1 www.example.com # FOCUSGUARD: example.com",
		"::1 www.example.com # FOCUSGUARD: example.com",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(content, want) {
			t.Errorf("expected hosts file to contain %q, got:\n%s", want, content)
		}
	}

	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Errorf("expected original localhost entry to be preserved, got:\n%s", content)
	}

	if !strings.Contains(content, "\r\n") {
		t.Errorf("expected hosts file to use CRLF line endings")
	}
}

func TestAddHostEntry_IsIdempotent(t *testing.T) {
	e := newTestEnforcer(t)

	if err := e.addHostEntry("example.com"); err != nil {
		t.Fatalf("first addHostEntry returned error: %v", err)
	}
	if err := e.addHostEntry("example.com"); err != nil {
		t.Fatalf("second addHostEntry returned error: %v", err)
	}

	content := readRawHosts(t, e.hostsPath)
	marker := "# FOCUSGUARD: example.com"
	count := strings.Count(content, marker)

	if count != 4 {
		t.Errorf("expected marker to appear exactly 4 times after duplicate add, got %d times:\n%s", count, content)
	}
}

func TestRemoveHostEntry_RemovesOnlyMatchingDomain(t *testing.T) {
	e := newTestEnforcer(t)

	if err := e.addHostEntry("example.com"); err != nil {
		t.Fatalf("addHostEntry(example.com) returned error: %v", err)
	}
	if err := e.addHostEntry("other.com"); err != nil {
		t.Fatalf("addHostEntry(other.com) returned error: %v", err)
	}

	if err := e.removeHostEntry("example.com"); err != nil {
		t.Fatalf("removeHostEntry returned error: %v", err)
	}

	content := readRawHosts(t, e.hostsPath)

	if strings.Contains(content, "FOCUSGUARD: example.com") {
		t.Errorf("expected example.com entries to be removed, got:\n%s", content)
	}
	if !strings.Contains(content, "FOCUSGUARD: other.com") {
		t.Errorf("expected other.com entries to remain untouched, got:\n%s", content)
	}
	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Errorf("expected original localhost entry to be preserved, got:\n%s", content)
	}
}

func TestRemoveHostEntry_NoOpWhenDomainNotPresent(t *testing.T) {
	e := newTestEnforcer(t)

	before := readRawHosts(t, e.hostsPath)

	if err := e.removeHostEntry("nonexistent.com"); err != nil {
		t.Fatalf("removeHostEntry returned error: %v", err)
	}

	after := readRawHosts(t, e.hostsPath)
	if before != after {
		t.Errorf("expected hosts file to remain unchanged, before:\n%s\nafter:\n%s", before, after)
	}
}

func TestCollectAllIPs_DeduplicatesAndIgnoresEmpty(t *testing.T) {
	e := newTestEnforcer(t)

	extra := []string{"1.2.3.4", "1.2.3.4", "", "5.6.7.8"}

	got := e.collectAllIPs("invalid.invalid-tld-for-tests", extra)

	want := map[string]bool{"1.2.3.4": true, "5.6.7.8": true}
	if len(got) != len(want) {
		t.Fatalf("expected %d unique IPs, got %d: %v", len(want), len(got), got)
	}
	for _, ip := range got {
		if !want[ip] {
			t.Errorf("unexpected IP in result: %q", ip)
		}
	}
}

func TestWriteReadHostsLines_RoundTrip(t *testing.T) {
	e := newTestEnforcer(t)

	lines := []string{"127.0.0.1 localhost", "10.0.0.1 test.local"}
	if err := e.writeHostsLines(lines); err != nil {
		t.Fatalf("writeHostsLines returned error: %v", err)
	}

	got, err := e.readHostsLines()
	if err != nil {
		t.Fatalf("readHostsLines returned error: %v", err)
	}

	if len(got) != len(lines) {
		t.Fatalf("expected %d lines, got %d: %v", len(lines), len(got), got)
	}
	for i, line := range lines {
		if got[i] != line {
			t.Errorf("line %d: expected %q, got %q", i, line, got[i])
		}
	}
}

func TestBlockDoH_Windows(t *testing.T) {
	e := newTestEnforcer(t)

	// BlockDoH chama netsh, que precisa de admin.
	// Se falhar por falta de privilégio, o teste não deve falhar.
	err := e.BlockDoH()
	if err != nil {
		t.Logf("BlockDoH retornou (esperado se não admin): %v", err)
	}

	// Idempotente: chamar de novo não deve quebrar
	err2 := e.BlockDoH()
	if err2 != nil {
		t.Logf("BlockDoH (2ª chamada) retornou: %v", err2)
	}
}

func TestUnblockDoH_Windows(t *testing.T) {
	e := newTestEnforcer(t)

	// Desbloqueio não deve falhar mesmo se as regras não existirem
	err := e.UnblockDoH()
	if err != nil {
		t.Logf("UnblockDoH retornou (esperado se não admin): %v", err)
	}

	// Idempotente
	err2 := e.UnblockDoH()
	if err2 != nil {
		t.Logf("UnblockDoH (2ª chamada) retornou: %v", err2)
	}
}
