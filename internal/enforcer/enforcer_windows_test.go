//go:build windows

package enforcer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
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

	names := parseFocusGuardRuleNames([]byte(output))

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

	names := parseFocusGuardRuleNames([]byte(output))
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
	args := addDoHRuleArgs("FocusGuard_DoH_1_1_1_1_tcp", "1.1.1.1", 443, "tcp")

	want := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=FocusGuard_DoH_1_1_1_1_tcp",
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

func TestAddDoHRuleArgs_UDP(t *testing.T) {
	// DoH sobre QUIC/HTTP/3 (UDP:443) precisa de uma regra própria — sem ela
	// o Firefox contorna o bloqueio TCP-only.
	args := addDoHRuleArgs("FocusGuard_DoH_1_1_1_1_udp", "1.1.1.1", 443, "udp")

	want := []string{
		"advfirewall", "firewall", "add", "rule",
		"name=FocusGuard_DoH_1_1_1_1_udp",
		"dir=out",
		"action=block",
		"remoteip=1.1.1.1",
		"remoteport=443",
		"protocol=udp",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("addDoHRuleArgs(udp) = %v, want %v", args, want)
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

	status := countFocusGuardRules([]byte(output))

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

	status := countFocusGuardRules([]byte(output))

	if status.FirewallRules != 0 {
		t.Errorf("FirewallRules = %d, want 0", status.FirewallRules)
	}
	if status.DoHActive {
		t.Error("DoHActive deve ser false sem regras DoH/DoT")
	}
}

// batchStubWin replaces execCommandContext for the Windows batch/Status tests:
// it records every invocation (name+args) and the scripts fed via stdin, and
// returns a canned netsh dump for show-rule queries.
type batchStubWin struct {
	calls  [][]string
	stdins []string
	// showCount returns the netsh dump for each show rule call, in order.
	showDumps []string
}

func (b *batchStubWin) SetStdin(r io.Reader) {
	data, _ := io.ReadAll(r)
	b.stdins = append(b.stdins, string(data))
}

func (b *batchStubWin) CombinedOutput() ([]byte, error) {
	if len(b.calls) == 0 {
		return nil, nil
	}
	last := b.calls[len(b.calls)-1]
	if slices.Contains(last, "show") {
		idx := 0
		for _, c := range b.calls {
			if slices.Contains(c, "show") {
				idx++
			}
		}
		if idx-1 < len(b.showDumps) {
			return []byte(b.showDumps[idx-1]), nil
		}
	}
	return nil, nil
}

func stubBatchExecWin(t *testing.T, b *batchStubWin) {
	t.Helper()
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		b.calls = append(b.calls, append([]string{name}, args...))
		return b
	}
}

// TestAddFirewallRulesBatch_SingleNetshProcess verifies that N rules are
// applied with a single netsh process fed a script via stdin (no PowerShell,
// no one exec per IP), and that a verification query confirms the result.
func TestAddFirewallRulesBatch_SingleNetshProcess(t *testing.T) {
	b := &batchStubWin{
		showDumps: []string{
			"", // first query: no rules exist yet
			"Regra:\n    Nome da regra:    FocusGuard_1.1.1.1\n\nRegra:\n    Nome da regra:    FocusGuard_8.8.8.8\n",
		},
	}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	if err := e.addFirewallRulesBatch([]string{"1.1.1.1", "8.8.8.8"}); err != nil {
		t.Fatalf("addFirewallRulesBatch: %v", err)
	}

	// Expect: 1 show query (existing), 1 netsh add via stdin, 1 verification
	// show + 1 ipconfig /flushdns (derrubar conexões via cache de DNS).
	if len(b.calls) != 4 {
		t.Fatalf("expected 4 invocations (show + netsh add + verify + flushdns), got %d: %v", len(b.calls), b.calls)
	}

	addCall := b.calls[1]
	if addCall[0] != "netsh" {
		t.Errorf("expected netsh add invocation, got %v", addCall)
	}
	if len(b.stdins) != 1 {
		t.Fatalf("expected 1 stdin script, got %d", len(b.stdins))
	}
	for _, ip := range []string{"1.1.1.1", "8.8.8.8"} {
		if !strings.Contains(b.stdins[0], "add rule name=FocusGuard_"+ip+" dir=out action=block remoteip="+ip) {
			t.Errorf("script missing rule for %s:\n%s", ip, b.stdins[0])
		}
	}
	if !strings.Contains(b.stdins[0], "exit") {
		t.Errorf("script must end with exit:\n%s", b.stdins[0])
	}

	// A última invocação é o flush do cache de DNS.
	last := b.calls[3]
	if last[0] != "ipconfig" || !slices.Contains(last, "/flushdns") {
		t.Errorf("expected ipconfig /flushdns after successful batch, got %v", last)
	}
}

// TestAddFirewallRulesBatch_IPv6_MigratesLegacyRule verifies the IPv6 block
// path: a legacy raw-':' rule present in the firewall is removed with a
// failure-tolerant delete BEFORE the batch, so the netsh add script only
// carries add lines — a delete of a non-existent rule inside the script
// aborts the whole batch with exit status 1 (netsh propagates a failing line
// to the process exit code).
func TestAddFirewallRulesBatch_IPv6_MigratesLegacyRule(t *testing.T) {
	b := &batchStubWin{
		showDumps: []string{
			"Regra:\n    Nome da regra:    FocusGuard_2606:4700:4700::1111\n", // regra legada existe
			"Regra:\n    Nome da regra:    FocusGuard_2606_4700_4700__1111\n", // pós-add normalizado
		},
	}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	if err := e.addFirewallRulesBatch([]string{"2606:4700:4700::1111"}); err != nil {
		t.Fatalf("addFirewallRulesBatch: %v", err)
	}

	// show (existing) + delete legada + delete normalizado + netsh add + verify + flushdns
	if len(b.calls) != 6 {
		t.Fatalf("expected 6 invocations, got %d: %v", len(b.calls), b.calls)
	}

	del := b.calls[2]
	if del[0] != "netsh" || !slices.Contains(del, "delete") ||
		!strings.Contains(strings.Join(del, " "), legacyDomainRuleName("2606:4700:4700::1111")) {
		t.Errorf("expected a failure-tolerant delete of the legacy rule, got %v", del)
	}

	if len(b.stdins) != 1 {
		t.Fatalf("expected 1 stdin script, got %d", len(b.stdins))
	}
	if strings.Contains(b.stdins[0], "delete rule") {
		t.Errorf("script must not carry delete lines (netsh exits 1 on a failing line):\n%s", b.stdins[0])
	}
	if !strings.Contains(b.stdins[0], "add rule name=FocusGuard_2606_4700_4700__1111") {
		t.Errorf("script missing the normalized IPv6 rule:\n%s", b.stdins[0])
	}
}

// TestAddFirewallRulesBatch_MissingRuleAfterAdd verifies that when netsh exits
// 0 but a rule is not actually present after the batch, an error is returned
// (netsh can mask internal failures with exit code 0).
func TestAddFirewallRulesBatch_MissingRuleAfterAdd(t *testing.T) {
	b := &batchStubWin{
		showDumps: []string{
			"", // first query: no rules exist yet
			"Regra:\n    Nome da regra:    FocusGuard_1.1.1.1\n", // 8.8.8.8 missing
		},
	}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	err := e.addFirewallRulesBatch([]string{"1.1.1.1", "8.8.8.8"})
	if err == nil {
		t.Fatal("expected error when a rule is missing after the batch")
	}
	if !strings.Contains(err.Error(), "8.8.8.8") {
		t.Errorf("error should mention the missing rule, got: %v", err)
	}

	// Sem flush de DNS em falha: a regra não foi aplicada de fato.
	for _, c := range b.calls {
		if c[0] == "ipconfig" {
			t.Errorf("flushdns must not run when the batch failed, got %v", c)
		}
	}
}

// TestSyncLocked_WritesHostsOnce verifies the batched Windows Sync rewrites
// the hosts file exactly once for N domains (single read-modify-write).
func TestSyncLocked_WritesHostsOnce(t *testing.T) {
	b := &batchStubWin{
		showDumps: []string{
			"", // first query: no rules exist yet
			"Regra:\n    Nome da regra:    FocusGuard_1.1.1.1\n\nRegra:\n    Nome da regra:    FocusGuard_2.2.2.2\n",
		},
	}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	var writes int32
	e.SetOnHostsWrite(func() { atomic.AddInt32(&writes, 1) })

	active := map[string][]string{
		"a.com": {"1.1.1.1"},
		"b.com": {"2.2.2.2"},
	}
	if err := e.syncLocked(active); err != nil {
		t.Fatalf("syncLocked: %v", err)
	}

	if got := atomic.LoadInt32(&writes); got != 1 {
		t.Errorf("expected exactly 1 hosts write for 2 domains, got %d", got)
	}

	content := readRawHosts(t, e.hostsPath)
	for _, d := range []string{"a.com", "b.com"} {
		if !strings.Contains(content, "# FOCUSGUARD: "+d) {
			t.Errorf("hosts missing marker for %s:\n%s", d, content)
		}
	}
	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Errorf("original hosts lines must be preserved:\n%s", content)
	}
}

// TestSyncLocked_SweepsOrphanRules verifies Sync removes domain block rules
// that are no longer in activeBlocks (crashed-before-Unblock leftovers) while
// preserving DoH/DoT/allow/catch-all rules.
func TestSyncLocked_SweepsOrphanRules(t *testing.T) {
	// 1.1.1.1 pertence a um bloco ativo (mantido); 3.3.3.3 é órfão (removido);
	// DoH/Allow/AllInternet e o catch-all nunca podem ser tocados.
	dump := "Regra:\n    Nome da regra:    FocusGuard_1.1.1.1\n\n" +
		"Regra:\n    Nome da regra:    FocusGuard_3.3.3.3\n\n" +
		"Regra:\n    Nome da regra:    FocusGuard_DoH_8_8_8_8_tcp\n\n" +
		"Regra:\n    Nome da regra:    FocusGuard_Allow_2001_db8__1\n\n" +
		"Regra:\n    Nome da regra:    FocusGuard_AllInternet\n"
	b := &batchStubWin{showDumps: []string{dump}}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	active := map[string][]string{
		"a.com": {"1.1.1.1"},
	}
	if err := e.syncLocked(active); err != nil {
		t.Fatalf("syncLocked: %v", err)
	}

	var deleted [][]string
	for _, c := range b.calls {
		if c[0] == "netsh" && slices.Contains(c, "delete") {
			deleted = append(deleted, c)
		}
	}

	var delOrphan, delActive, delOther bool
	for _, d := range deleted {
		joined := strings.Join(d, " ")
		switch {
		case strings.Contains(joined, "name=FocusGuard_3.3.3.3"):
			delOrphan = true
		case strings.Contains(joined, "name=FocusGuard_1.1.1.1"):
			delActive = true
		case strings.Contains(joined, "DoH_") || strings.Contains(joined, "Allow_") || strings.Contains(joined, "AllInternet"):
			delOther = true
		}
	}
	if !delOrphan {
		t.Errorf("esperava remover a regra órfã FocusGuard_3.3.3.3, got deletes: %v", deleted)
	}
	if delActive {
		t.Errorf("regra de bloco ativo FocusGuard_1.1.1.1 NÃO pode ser removida: %v", deleted)
	}
	if delOther {
		t.Errorf("regras DoH/Allow/AllInternet NÃO podem ser removidas pelo sweep: %v", deleted)
	}
}

// TestStatus_UsesNetshNotPowerShell verifies Status() queries the firewall
// with a single native netsh invocation (no PowerShell runtime) and reuses the
// existing countFocusGuardRules parser.
// flushStub records exec invocations so the DNS flush can be asserted.
type flushStub struct {
	calls [][]string
	err   error
}

func (f *flushStub) SetStdin(r io.Reader) {}

func (f *flushStub) CombinedOutput() ([]byte, error) {
	return nil, f.err
}

func stubFlushExec(t *testing.T, f *flushStub) {
	t.Helper()
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		f.calls = append(f.calls, append([]string{name}, args...))
		return f
	}
}

// TestFlushDNS_RunsIpconfig verifies flushDNS executes ipconfig /flushdns to
// clear the resolver cache so stale resolutions don't keep Keep-Alive
// connections pointing at freshly blocked IPs.
func TestFlushDNS_RunsIpconfig(t *testing.T) {
	f := &flushStub{}
	stubFlushExec(t, f)

	e := newTestEnforcer(t)
	e.flushDNS()

	if len(f.calls) != 1 {
		t.Fatalf("expected 1 ipconfig invocation, got %d: %v", len(f.calls), f.calls)
	}
	call := f.calls[0]
	if call[0] != "ipconfig" || !slices.Contains(call, "/flushdns") {
		t.Errorf("expected ipconfig /flushdns, got %v", call)
	}
}

// TestFlushDNS_BestEffort verifies a flush failure (ipconfig ausente, sem
// privilégio) is silently ignored — the netsh block rule already stops new
// flows.
func TestFlushDNS_BestEffort(t *testing.T) {
	f := &flushStub{err: errors.New("ipconfig: command not found")}
	stubFlushExec(t, f)

	e := newTestEnforcer(t)
	e.flushDNS()
	if len(f.calls) != 1 {
		t.Errorf("expected 1 ipconfig invocation, got %d", len(f.calls))
	}
}

// ---------------------------------------------------------------------------
// BlockAll / UnblockAll (modo pânico + allowlist deep-focus) — Windows
// ---------------------------------------------------------------------------

func TestAllBlockRuleName_Stable(t *testing.T) {
	if allBlockRuleName() != "FocusGuard_AllInternet" {
		t.Errorf("allBlockRuleName() = %q, want FocusGuard_AllInternet", allBlockRuleName())
	}
	if allowRuleName("1.1.1.1") != "FocusGuard_Allow_1.1.1.1" {
		t.Errorf("allowRuleName(1.1.1.1) = %q", allowRuleName("1.1.1.1"))
	}
	if allowRuleName("2001:db8::1") != "FocusGuard_Allow_2001_db8__1" {
		t.Errorf("allowRuleName(v6) = %q, want colons replaced", allowRuleName("2001:db8::1"))
	}
}

func TestDomainRuleName_NormalizesIPv6(t *testing.T) {
	if got := domainRuleName("1.1.1.1"); got != "FocusGuard_1.1.1.1" {
		t.Errorf("domainRuleName(v4) = %q", got)
	}
	if got := domainRuleName("2606:4700:4700::1111"); got != "FocusGuard_2606_4700_4700__1111" {
		t.Errorf("domainRuleName(v6) = %q, want colons replaced", got)
	}
	if got := legacyDomainRuleName("2606:4700:4700::1111"); got != "FocusGuard_2606:4700:4700::1111" {
		t.Errorf("legacyDomainRuleName(v6) = %q, want raw colons kept", got)
	}
}

func TestDomainIPFromRuleName(t *testing.T) {
	cases := []struct {
		name  string
		want  string
		isIP  bool
	}{
		{"FocusGuard_1.1.1.1", "1.1.1.1", true},
		{"FocusGuard_2606:4700:4700::1111", "2606:4700:4700::1111", true},  // legado cru
		{"FocusGuard_2606_4700_4700__1111", "2606:4700:4700::1111", true},   // normalizado
		{"FocusGuard_DoH_8_8_8_8_tcp", "", false},
		{"FocusGuard_DoT_TCP", "", false},
		{"FocusGuard_Allow_2001_db8__1", "", false},
		{"FocusGuard_AllInternet", "", false},
		{"Other_1.1.1.1", "", false},
	}
	for _, tt := range cases {
		got, ok := domainIPFromRuleName(tt.name)
		if ok != tt.isIP || got != tt.want {
			t.Errorf("domainIPFromRuleName(%q) = (%q, %v), want (%q, %v)", tt.name, got, ok, tt.want, tt.isIP)
		}
	}
}

// TestBlockAll_AddsCatchAllAndAllowRules verifies BlockAll adds the catch-all
// block rule plus one allow rule per allowlisted IP (specific rules take
// precedence over the broad block in Windows Firewall), after sweeping any
// previous all/allow rules (idempotent re-application).
func TestBlockAll_AddsCatchAllAndAllowRules(t *testing.T) {
	b := &batchStubWin{showDumps: []string{"", ""}} // dump vazio: nada a limpar
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	if err := e.BlockAll([]string{"1.1.1.1", "2001:db8::1"}); err != nil {
		t.Fatalf("BlockAll: %v", err)
	}

	// Busca as invocações netsh add.
	var adds [][]string
	for _, c := range b.calls {
		if c[0] == "netsh" && slices.Contains(c, "add") {
			adds = append(adds, c)
		}
	}
	if len(adds) != 3 {
		t.Fatalf("esperava 3 netsh add (2 allow + 1 catch-all), got %d: %v", len(adds), b.calls)
	}

	var hasCatch, hasAllow4, hasAllow6 bool
	for _, a := range adds {
		joined := strings.Join(a, " ")
		switch {
		case strings.Contains(joined, "name=FocusGuard_AllInternet") && strings.Contains(joined, "action=block"):
			hasCatch = true
		case strings.Contains(joined, "name=FocusGuard_Allow_1.1.1.1") && strings.Contains(joined, "action=allow") && strings.Contains(joined, "remoteip=1.1.1.1"):
			hasAllow4 = true
		case strings.Contains(joined, "name=FocusGuard_Allow_2001_db8__1") && strings.Contains(joined, "action=allow"):
			hasAllow6 = true
		}
	}
	if !hasCatch {
		t.Errorf("falta a regra catch-all FocusGuard_AllInternet")
	}
	if !hasAllow4 {
		t.Errorf("falta a allow rule para 1.1.1.1")
	}
	if !hasAllow6 {
		t.Errorf("falta a allow rule para 2001:db8::1")
	}
}

// TestBlockAll_NoAllowlistOnlyCatchAll verifies panic mode without an
// allowlist adds only the catch-all block rule.
func TestBlockAll_NoAllowlistOnlyCatchAll(t *testing.T) {
	b := &batchStubWin{showDumps: []string{"", ""}}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	if err := e.BlockAll(nil); err != nil {
		t.Fatalf("BlockAll: %v", err)
	}

	var adds [][]string
	for _, c := range b.calls {
		if c[0] == "netsh" && slices.Contains(c, "add") {
			adds = append(adds, c)
		}
	}
	if len(adds) != 1 || !strings.Contains(strings.Join(adds[0], " "), "FocusGuard_AllInternet") {
		t.Errorf("esperava apenas o catch-all, got %v", adds)
	}
}

// TestUnblockAll_DeletesCatchAllAndAllowRules verifies UnblockAll removes the
// catch-all and every FocusGuard_Allow_* rule, leaving other FocusGuard rules
// (domain blocks, DoH) untouched.
func TestUnblockAll_DeletesCatchAllAndAllowRules(t *testing.T) {
	dump := "Regra:\n    Nome da regra:    FocusGuard_AllInternet\n\n" +
		"Regra:\n    Nome da regra:    FocusGuard_Allow_1.1.1.1\n\n" +
		"Regra:\n    Nome da regra:    FocusGuard_Allow_8_8_8_8\n\n" +
		"Regra:\n    Nome da regra:    FocusGuard_9.9.9.9\n"
	b := &batchStubWin{showDumps: []string{dump}}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	if err := e.UnblockAll(); err != nil {
		t.Fatalf("UnblockAll: %v", err)
	}

	var dels [][]string
	for _, c := range b.calls {
		if c[0] == "netsh" && slices.Contains(c, "delete") {
			dels = append(dels, c)
		}
	}

	var deletedAll, deletedAllow1, deletedAllow8, deletedDomain bool
	for _, d := range dels {
		joined := strings.Join(d, " ")
		switch {
		case strings.Contains(joined, "name=FocusGuard_AllInternet"):
			deletedAll = true
		case strings.Contains(joined, "name=FocusGuard_Allow_1.1.1.1"):
			deletedAllow1 = true
		case strings.Contains(joined, "name=FocusGuard_Allow_8_8_8_8"):
			deletedAllow8 = true
		case strings.Contains(joined, "name=FocusGuard_9.9.9.9"):
			deletedDomain = true
		}
	}
	if !deletedAll {
		t.Error("esperava deletar FocusGuard_AllInternet")
	}
	if !deletedAllow1 || !deletedAllow8 {
		t.Error("esperava deletar as allow rules")
	}
	if deletedDomain {
		t.Error("regra de domínio normal (FocusGuard_9.9.9.9) NÃO pode ser deletada")
	}

	// A enumeração das allow rules (show) deve acontecer ANTES de qualquer
	// delete: se um crash ocorrer no meio, o catch-all já foi removido e a
	// internet volta; regras allow sobram apenas como lixo inerte.
	firstShow := -1
	firstDelete := -1
	for i, c := range b.calls {
		if c[0] == "netsh" && slices.Contains(c, "name=all") && firstShow == -1 {
			firstShow = i
		}
		if c[0] == "netsh" && slices.Contains(c, "delete") && firstDelete == -1 {
			firstDelete = i
		}
	}
	if firstShow == -1 {
		t.Error("esperava consulta netsh show rule name=all para enumerar as allow rules")
	}
	if firstDelete != -1 && firstShow > firstDelete {
		t.Errorf("show de enumeração (#%d) deve preceder os deletes (#%d)", firstShow, firstDelete)
	}
}

func TestCountFocusGuardRules_DetectsAllBlocked(t *testing.T) {
	output := "\nRegra:\n    Nome da regra:    FocusGuard_AllInternet\n\nRegra:\n    Nome da regra:    FocusGuard_DoT_TCP\n"
	status := countFocusGuardRules([]byte(output))
	if !status.AllBlocked {
		t.Error("AllBlocked deve ser true quando FocusGuard_AllInternet existe")
	}
	if status.FirewallRules != 2 {
		t.Errorf("FirewallRules = %d, want 2", status.FirewallRules)
	}
}

func TestCountFocusGuardRules_AllBlockedFalse(t *testing.T) {
	output := "\nRegra:\n    Nome da regra:    FocusGuard_1.1.1.1\n"
	status := countFocusGuardRules([]byte(output))
	if status.AllBlocked {
		t.Error("AllBlocked deve ser false sem FocusGuard_AllInternet")
	}
}

func TestStatus_UsesNetshNotPowerShell(t *testing.T) {
	b := &batchStubWin{
		showDumps: []string{
			"Regra:\n    Nome da regra:    FocusGuard_1.1.1.1\n\nRegra:\n    Nome da regra:    FocusGuard_DoH_8_8_8_8\n\nRegra:\n    Nome da regra:    FocusGuard_DoT_TCP\n\nRegra:\n    Nome da regra:    Windows Defender Firewall - default\n",
		},
	}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	st, err := e.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.FirewallRules != 3 {
		t.Errorf("FirewallRules = %d, want 3", st.FirewallRules)
	}
	if !st.DoHActive {
		t.Error("DoHActive deve ser true quando existem regras DoH/DoT")
	}

	for _, c := range b.calls {
		if c[0] == "powershell" {
			t.Errorf("Status must not invoke PowerShell, got %v", c)
		}
	}

	// checkAdmin usa um subprocesso net session; o Status em si deve fazer
	// exatamente uma consulta netsh show rule name=all dir=out.
	var netshCalls [][]string
	for _, c := range b.calls {
		if c[0] == "netsh" {
			netshCalls = append(netshCalls, c)
		}
	}
	if len(netshCalls) != 1 {
		t.Errorf("expected exactly 1 netsh invocation, got %d: %v", len(netshCalls), b.calls)
	}
	if len(netshCalls) == 1 {
		if !slices.Contains(netshCalls[0], "name=all") {
			t.Errorf("expected netsh show rule name=all, got %v", netshCalls[0])
		}
		if !slices.Contains(netshCalls[0], "dir=out") {
			t.Errorf("expected netsh show to filter dir=out, got %v", netshCalls[0])
		}
	}
}

// TestStatus_CacheWithinTTL verifies a second Status() within the TTL does not
// re-query netsh (keeps the mutation lock free).
func TestStatus_CacheWithinTTL(t *testing.T) {
	b := &batchStubWin{showDumps: []string{
		"Regra:\n    Nome da regra:    FocusGuard_1.1.1.1\n",
	}}
	stubBatchExecWin(t, b)

	e := newTestEnforcer(t)
	for i := 0; i < 3; i++ {
		if _, err := e.Status(); err != nil {
			t.Fatalf("Status #%d: %v", i+1, err)
		}
	}

	var netshCalls [][]string
	for _, c := range b.calls {
		if c[0] == "netsh" {
			netshCalls = append(netshCalls, c)
		}
	}
	if len(netshCalls) != 1 {
		t.Errorf("Status 3x dentro do TTL deve consultar netsh apenas 1x, got %d calls: %v", len(netshCalls), b.calls)
	}
}

// TestStatus_MutationInvalidatesCache verifies mutating methods refresh the
// cache so the next Status() reflects the change (no stale 15s window).
func TestStatus_MutationInvalidatesCache(t *testing.T) {
	first := batchStubWin{showDumps: []string{
		"Regra:\n    Nome da regra:    FocusGuard_1.1.1.1\n",
	}}
	stubBatchExecWin(t, &first)

	e := newTestEnforcer(t)
	if _, err := e.Status(); err != nil {
		t.Fatalf("Status: %v", err)
	}

	// Agora o bloco é removido: Status seguinte deve re-consultar e ver 0 regras.
	second := batchStubWin{showDumps: []string{
		"",
	}}
	stubBatchExecWin(t, &second)

	if err := e.UnblockDomain("a.com", []string{"1.1.1.1"}); err != nil {
		t.Fatalf("UnblockDomain: %v", err)
	}
	st, err := e.Status()
	if err != nil {
		t.Fatalf("Status pós-mutação: %v", err)
	}
	if st.FirewallRules != 0 {
		t.Errorf("após invalidar cache, FirewallRules = %d, want 0", st.FirewallRules)
	}

	var netshShows [][]string
	for _, c := range second.calls {
		if c[0] == "netsh" && slices.Contains(c, "show") {
			netshShows = append(netshShows, c)
		}
	}
	if len(netshShows) != 1 {
		t.Errorf("mudança deve invalidar o cache e gerar nova consulta, got %d netsh show calls: %v", len(netshShows), second.calls)
	}
}
