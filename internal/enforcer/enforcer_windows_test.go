//go:build windows

package enforcer

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestParseFocusGuardRuleNames(t *testing.T) {
	output := `Regra:
    Nome da regra:    FocusGuard_1.1.1.1

Regra:
    Nome da regra:    FocusGuard_DoH_8_8_8_8

Regra:
    Nome da regra:    Windows Defender Firewall - default
`

	names := parseFocusGuardRuleNames(output)

	if !names["FocusGuard_1.1.1.1"] {
		t.Error("expected FocusGuard_1.1.1.1 to be parsed")
	}
	if !names["FocusGuard_DoH_8_8_8_8"] {
		t.Error("expected FocusGuard_DoH_8_8_8_8 to be parsed")
	}
	if len(names) != 2 {
		t.Errorf("expected exactly 2 FocusGuard rules, got %d: %v", len(names), names)
	}
}

func TestParseFocusGuardRuleNames_NoRules(t *testing.T) {
	output := `Regra:
    Nome da regra:    Windows Defender Firewall - default
`

	names := parseFocusGuardRuleNames(output)
	if len(names) != 0 {
		t.Errorf("expected no FocusGuard rules, got %v", names)
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

func TestReadHostsLines_RecreatesMissingHostsFile(t *testing.T) {
	e := newTestEnforcer(t)
	if err := os.Remove(e.hostsPath); err != nil {
		t.Fatalf("failed to remove hosts file: %v", err)
	}

	lines, err := e.readHostsLines()
	if err != nil {
		t.Fatalf("readHostsLines on missing file returned error: %v", err)
	}

	if _, err := os.Stat(e.hostsPath); err != nil {
		t.Fatalf("expected hosts file to be recreated on disk: %v", err)
	}

	joined := strings.Join(lines, "\r\n")
	if !strings.Contains(joined, "localhost") {
		t.Errorf("expected localhost baseline to be written, got:\n%s", joined)
	}
}

func TestAddHostEntry_RecreatesMissingHostsFile(t *testing.T) {
	e := newTestEnforcer(t)
	if err := os.Remove(e.hostsPath); err != nil {
		t.Fatalf("failed to remove hosts file: %v", err)
	}

	if err := e.addHostEntry("example.com"); err != nil {
		t.Fatalf("addHostEntry on missing hosts file returned error: %v", err)
	}

	content := readRawHosts(t, e.hostsPath)

	if !strings.Contains(content, "localhost") {
		t.Errorf("expected localhost baseline in recreated hosts, got:\n%s", content)
	}
	if !strings.Contains(content, "127.0.0.1 example.com # FOCUSGUARD: example.com") {
		t.Errorf("expected block entry in recreated hosts, got:\n%s", content)
	}
}

func TestBlockDomainLocked_RollbackCleansHosts(t *testing.T) {
	e := newTestEnforcer(t)

	if err := e.checkAdmin(); err == nil {
		t.Skip("Skipping: running as admin, firewall rules may succeed")
	}

	err := e.blockDomainLocked("example.com", []string{"1.1.1.1", "2.2.2.2"})
	if err == nil {
		t.Fatal("expected blockDomainLocked to fail without admin privileges")
	}

	content := readRawHosts(t, e.hostsPath)
	if strings.Contains(content, "FOCUSGUARD") {
		t.Errorf("expected hosts file to be rolled back (no FOCUSGUARD markers), got:\n%s", content)
	}
	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Errorf("expected original hosts content to be preserved, got:\n%s", content)
	}
}

func TestBlockDoH_Windows(t *testing.T) {
	e := newTestEnforcer(t)

	err := e.BlockDoH()
	if err != nil {
		t.Logf("BlockDoH retornou (esperado se não admin): %v", err)
	}

	err2 := e.BlockDoH()
	if err2 != nil {
		t.Logf("BlockDoH (2ª chamada) retornou: %v", err2)
	}
}

func TestUnblockDoH_Windows(t *testing.T) {
	e := newTestEnforcer(t)

	err := e.UnblockDoH()
	if err != nil {
		t.Logf("UnblockDoH retornou (esperado se não admin): %v", err)
	}

	err2 := e.UnblockDoH()
	if err2 != nil {
		t.Logf("UnblockDoH (2ª chamada) retornou: %v", err2)
	}
}

func TestAddDoTRuleArgs_UsesRemotePort(t *testing.T) {
	args := addDoTRuleArgs("FocusGuard_DoT_TCP", "tcp", 853)

	want := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=FocusGuard_DoT_TCP",
		"dir=out",
		"action=block",
		"protocol=tcp",
		"remoteport=853",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("addDoTRuleArgs() = %v, want %v", args, want)
	}

	for _, a := range args {
		if strings.HasPrefix(a, "localport=") {
			t.Errorf("DoT rule must use remoteport, not localport: got %q", a)
		}
	}
}

func TestAddDoTRuleArgs_UDP(t *testing.T) {
	args := addDoTRuleArgs("FocusGuard_DoT_UDP", "udp", 853)
	want := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=FocusGuard_DoT_UDP",
		"dir=out",
		"action=block",
		"protocol=udp",
		"remoteport=853",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("addDoTRuleArgs(udp) = %v, want %v", args, want)
	}
}

func TestDoTRuleName_MigrationStable(t *testing.T) {
	want := map[string]string{
		"DoT_TCP": "FocusGuard_DoT_TCP",
		"DoT_UDP": "FocusGuard_DoT_UDP",
	}

	for _, p := range DoHProviders {
		if !p.IsDoT {
			continue
		}
		wantName, ok := want[p.Name]
		if !ok {
			t.Errorf("provedor DoT inesperado %q na lista", p.Name)
			continue
		}
		if got := doTRuleName(p); got != wantName {
			t.Errorf("doTRuleName(%s) = %q, want %q (migração quebraria)", p.Name, got, wantName)
		}
	}

	for name := range want {
		found := false
		for _, p := range DoHProviders {
			if p.IsDoT && p.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("provedor DoT %q ausente da lista", name)
		}
	}
}

func TestAddDoHRuleArgs(t *testing.T) {
	args := addDoHRuleArgs("FocusGuard_DoH_1_1_1_1", "1.1.1.1", 443)

	want := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=FocusGuard_DoH_1_1_1_1",
		"dir=out",
		"action=block",
		"remoteip=1.1.1.1",
		"remoteport=443",
		"protocol=tcp",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("addDoHRuleArgs() = %v, want %v", args, want)
	}
}

func TestShowAndDeleteRuleArgs(t *testing.T) {
	if got, want := showRuleArgs("FocusGuard_X"), []string{"advfirewall", "firewall", "show", "rule", "name=FocusGuard_X"}; !reflect.DeepEqual(got, want) {
		t.Errorf("showRuleArgs() = %v, want %v", got, want)
	}

	if got, want := deleteRuleArgs("FocusGuard_X"), []string{"advfirewall", "firewall", "delete", "rule", "name=FocusGuard_X"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deleteRuleArgs() = %v, want %v", got, want)
	}
}

func TestCountFocusGuardRules(t *testing.T) {
	output := `
Regra:
    Nome da regra:    FocusGuard_1.1.1.1

Regra:
    Nome da regra:    FocusGuard_DoH_8_8_8_8

Regra:
    Nome da regra:    FocusGuard_DoT_TCP

Regra:
    Nome da regra:    Windows Defender Firewall - xxx
`

	status := countFocusGuardRules(output)

	if status.FirewallRules != 3 {
		t.Errorf("FirewallRules = %d, want 3", status.FirewallRules)
	}
	if !status.DoHActive {
		t.Error("DoHActive deve ser true quando existem regras DoH/DoT")
	}
}

func TestCountFocusGuardRules_NoRules(t *testing.T) {
	output := `
Regra:
    Nome da regra:    Windows Defender Firewall - foo

Regra:
    Nome da regra:    Outra regra qualquer
`

	status := countFocusGuardRules(output)

	if status.FirewallRules != 0 {
		t.Errorf("FirewallRules = %d, want 0", status.FirewallRules)
	}
	if status.DoHActive {
		t.Error("DoHActive deve ser false sem regras DoH/DoT")
	}
}

func TestCountFocusGuardNames(t *testing.T) {
	output := "FocusGuard_1.1.1.1\nFocusGuard_DoH_8_8_8_8\nFocusGuard_DoT_TCP\nWindows Defender Firewall - default\n"

	status := countFocusGuardNames(output)

	if status.FirewallRules != 3 {
		t.Errorf("FirewallRules = %d, want 3", status.FirewallRules)
	}
	if !status.DoHActive {
		t.Error("DoHActive deve ser true quando existem regras DoH/DoT")
	}
}

func TestCountFocusGuardNames_NoRules(t *testing.T) {
	output := "Windows Defender Firewall - default\nOutra regra qualquer\n"

	status := countFocusGuardNames(output)

	if status.FirewallRules != 0 {
		t.Errorf("FirewallRules = %d, want 0", status.FirewallRules)
	}
	if status.DoHActive {
		t.Error("DoHActive deve ser false sem regras FocusGuard")
	}
}
