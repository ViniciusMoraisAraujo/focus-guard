//go:build linux

package enforcer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestIptablesBinFor(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		wantBin string
		wantErr bool
	}{
		{
			name:    "Valid IPv4 address",
			ip:      "192.168.1.1",
			wantBin: "iptables",
			wantErr: false,
		},
		{
			name:    "Valid IPv6 address",
			ip:      "2001:db8::1",
			wantBin: "ip6tables",
			wantErr: false,
		},
		{
			name:    "Valid IPv6 Loopback",
			ip:      "::1",
			wantBin: "ip6tables",
			wantErr: false,
		},
		{
			name:    "Invalid IP address",
			ip:      "256.256.256.256",
			wantBin: "",
			wantErr: true,
		},
		{
			name:    "Arbitrary text string",
			ip:      "not-an-ip",
			wantBin: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBin, err := iptablesBinFor(tt.ip)
			if (err != nil) != tt.wantErr {
				t.Errorf("iptablesBinFor(%q) error = %v, wantErr %v", tt.ip, err, tt.wantErr)
				return
			}
			if gotBin != tt.wantBin {
				t.Errorf("iptablesBinFor(%q) = %q, expected %q", tt.ip, gotBin, tt.wantBin)
			}
		})
	}
}

func TestParseIptablesBlockedIPs(t *testing.T) {
	output := `-P OUTPUT ACCEPT
-A OUTPUT -d 1.1.1.1/32 -j DROP
-A OUTPUT -d 8.8.8.8/32 -p tcp --dport 443 -j DROP
-A OUTPUT -d 2001:db8::1/128 -j DROP
-A OUTPUT -p tcp --dport 853 -j DROP
-A OUTPUT -d 10.0.0.1/32 -j ACCEPT
`

	blocked := parseIptablesBlockedIPs(output)

	if !blocked["1.1.1.1"] {
		t.Error("expected 1.1.1.1 to be parsed as blocked")
	}
	if !blocked["2001:db8::1"] {
		t.Error("expected 2001:db8::1 to be parsed as blocked")
	}
	if blocked["8.8.8.8"] {
		t.Error("DoH rule (--dport) should not be counted as an IP block")
	}
	if blocked["10.0.0.1"] {
		t.Error("ACCEPT rule should not be counted")
	}
	if len(blocked) != 2 {
		t.Errorf("expected 2 blocked IPs, got %d: %v", len(blocked), blocked)
	}
}

// TestAddHostEntry_SanitizesWWW tests that a domain with a www. prefix is
// normalized so the hosts entries never become the redundant www.www.site.com.
func TestAddHostEntry_SanitizesWWW(t *testing.T) {
	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")
	if err := os.WriteFile(tempHostsPath, []byte("127.0.0.1 localhost\n::1 localhost\n"), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	enf := &linuxEnforcer{hostsPath: tempHostsPath}

	if err := enf.addHostEntry("www.site.com"); err != nil {
		t.Fatalf("addHostEntry: %v", err)
	}

	data, err := os.ReadFile(tempHostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "www.www.site.com") {
		t.Errorf("expected no www.www.site.com redundancy, got:\n%s", content)
	}
	if !strings.Contains(content, "127.0.0.1 site.com # FOCUSGUARD: site.com") {
		t.Errorf("expected sanitized site.com entry, got:\n%s", content)
	}
	if !strings.Contains(content, "127.0.0.1 www.site.com # FOCUSGUARD: site.com") {
		t.Errorf("expected www.site.com entry, got:\n%s", content)
	}
}

// TestAddHostEntry_RejectsInjection tests that a domain containing newlines or
// spaces (a /etc/hosts injection attempt) never reaches the hosts file raw.
func TestAddHostEntry_RejectsInjection(t *testing.T) {
	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")
	initial := "127.0.0.1 localhost\n::1 localhost\n"
	if err := os.WriteFile(tempHostsPath, []byte(initial), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	enf := &linuxEnforcer{hostsPath: tempHostsPath}

	// A CRLF in the domain must not inject a second hosts line.
	if err := enf.addHostEntry("evil.com\r\n127.0.0.1 attacker.com"); err != nil {
		t.Fatalf("addHostEntry should sanitize, not error: %v", err)
	}

	data, err := os.ReadFile(tempHostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	content := string(data)

	// A CRLF injection would create a NEW line mapping 127.0.0.1 attacker.com.
	// After sanitization the tokens are fused into one hostname, so no such
	// mapping line may exist.
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "127.0.0.1 attacker.com") ||
			strings.HasPrefix(trimmed, "::1 attacker.com") {
			t.Errorf("injection created a mapping for attacker.com: %q", line)
		}
	}
	if strings.Contains(content, "127.0.0.1 attacker.com") && !strings.Contains(content, "evil.com127.0.0.1attacker.com") {
		t.Errorf("expected injected tokens fused into the domain, got:\n%s", content)
	}
}

// TestRemoveHostEntry_SanitizesWWW tests that removal uses the same
// normalization, so blocking www.site.com and unblocking site.com (or vice
// versa) hit the same marker.
func TestRemoveHostEntry_SanitizesWWW(t *testing.T) {
	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")
	if err := os.WriteFile(tempHostsPath, []byte("127.0.0.1 localhost\n::1 localhost\n"), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	enf := &linuxEnforcer{hostsPath: tempHostsPath}

	if err := enf.addHostEntry("www.site.com"); err != nil {
		t.Fatalf("addHostEntry: %v", err)
	}
	if err := enf.removeHostEntry("site.com"); err != nil {
		t.Fatalf("removeHostEntry: %v", err)
	}

	data, err := os.ReadFile(tempHostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	if strings.Contains(string(data), "FOCUSGUARD") {
		t.Errorf("expected all FOCUSGUARD markers removed, got:\n%s", data)
	}
}

func TestHostsFileOperations(t *testing.T) {
	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")

	initialContent := "127.0.0.1 localhost\n::1 localhost\n"
	err := os.WriteFile(tempHostsPath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("failed to create temporary hosts file: %v", err)
	}

	enf := &linuxEnforcer{
		hostsPath: tempHostsPath,
	}

	domain := "example.com"

	t.Run("Add entry to hosts", func(t *testing.T) {
		err := enf.addHostEntry(domain)
		if err != nil {
			t.Fatalf("addHostEntry failed: %v", err)
		}

		lines, err := enf.readHostsLines()
		if err != nil {
			t.Fatalf("readHostsLines failed: %v", err)
		}

		foundMarker := false
		for _, line := range lines {
			if strings.Contains(line, "# FOCUSGUARD: example.com") {
				foundMarker = true
				break
			}
		}

		if !foundMarker {
			t.Errorf("marker # FOCUSGUARD: example.com was not found in hosts file")
		}
	})

	t.Run("Idempotency on re-adding the same entry", func(t *testing.T) {
		linesBefore, _ := enf.readHostsLines()

		err := enf.addHostEntry(domain)
		if err != nil {
			t.Fatalf("addHostEntry failed on second execution: %v", err)
		}

		linesAfter, _ := enf.readHostsLines()

		if len(linesBefore) != len(linesAfter) {
			t.Errorf("addHostEntry duplicated lines. Before: %d, After: %d", len(linesBefore), len(linesAfter))
		}
	})

	t.Run("Remove entry from hosts", func(t *testing.T) {
		err := enf.removeHostEntry(domain)
		if err != nil {
			t.Fatalf("removeHostEntry failed: %v", err)
		}

		lines, err := enf.readHostsLines()
		if err != nil {
			t.Fatalf("readHostsLines failed: %v", err)
		}

		for _, line := range lines {
			if strings.Contains(line, "# FOCUSGUARD: example.com") {
				t.Errorf("line with marker still found after removal: %s", line)
			}
		}
	})
}

func TestHostsFile_RecreatedWhenMissing(t *testing.T) {
	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")

	enf := &linuxEnforcer{hostsPath: tempHostsPath}

	if err := enf.addHostEntry("example.com"); err != nil {
		t.Fatalf("addHostEntry on missing hosts file failed: %v", err)
	}

	data, err := os.ReadFile(tempHostsPath)
	if err != nil {
		t.Fatalf("hosts file was not recreated: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Errorf("expected localhost baseline in recreated hosts, got:\n%s", content)
	}
	if !strings.Contains(content, "# FOCUSGUARD: example.com") {
		t.Errorf("expected block marker in recreated hosts, got:\n%s", content)
	}
}

func TestReadHostsLines_RecreatesMissingHostsFile(t *testing.T) {
	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")

	enf := &linuxEnforcer{hostsPath: tempHostsPath}

	lines, err := enf.readHostsLines()
	if err != nil {
		t.Fatalf("readHostsLines on missing file returned error: %v", err)
	}

	if _, err := os.Stat(tempHostsPath); err != nil {
		t.Fatalf("expected hosts file to be recreated on disk: %v", err)
	}

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "127.0.0.1 localhost") {
		t.Errorf("expected localhost baseline to be written, got:\n%s", joined)
	}
}

func TestBlockDoH_Linux(t *testing.T) {
	enf := &linuxEnforcer{}

	err := enf.BlockDoH()
	if err != nil {
		t.Logf("BlockDoH retornou (esperado se não root): %v", err)
	}

	err2 := enf.BlockDoH()
	if err2 != nil {
		t.Logf("BlockDoH (2ª chamada) retornou: %v", err2)
	}
}

func TestUnblockDoH_Linux(t *testing.T) {
	enf := &linuxEnforcer{}

	err := enf.UnblockDoH()
	if err != nil {
		t.Logf("UnblockDoH retornou (esperado se não root): %v", err)
	}

	err2 := enf.UnblockDoH()
	if err2 != nil {
		t.Logf("UnblockDoH (2ª chamada) retornou: %v", err2)
	}
}

func TestDoTRuleArgs_UsesDport(t *testing.T) {
	args := doTRuleArgs("-A", "tcp", 853)

	want := []string{"-A", "OUTPUT", "-p", "tcp", "--dport", "853", "-j", "DROP"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("doTRuleArgs() = %v, want %v", args, want)
	}

	for _, a := range args {
		if strings.HasPrefix(a, "--sport") {
			t.Errorf("DoT rule must use --dport, not --sport: got %q", a)
		}
	}
}

func TestDoTRuleArgs_UDP(t *testing.T) {
	args := doTRuleArgs("-A", "udp", 853)
	want := []string{"-A", "OUTPUT", "-p", "udp", "--dport", "853", "-j", "DROP"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("doTRuleArgs(udp) = %v, want %v", args, want)
	}
}

func TestDoHRuleArgs(t *testing.T) {
	args := doHRuleArgs("-A", "1.1.1.1", 443)

	want := []string{"-A", "OUTPUT", "-d", "1.1.1.1", "-p", "tcp", "--dport", "443", "-j", "DROP"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("doHRuleArgs() = %v, want %v", args, want)
	}
}

func TestCountIptablesRules(t *testing.T) {
	output := `-P OUTPUT ACCEPT
-A OUTPUT -d 1.1.1.1/32 -j DROP
-A OUTPUT -d 8.8.8.8/32 -p tcp --dport 443 -j DROP
-A OUTPUT -p tcp --dport 853 -j DROP
-A OUTPUT -d 10.0.0.1/32 -j ACCEPT
`

	status := countIptablesRules(output)

	if status.FirewallRules != 3 {
		t.Errorf("FirewallRules = %d, want 3", status.FirewallRules)
	}
	if !status.DoHActive {
		t.Error("DoHActive deve ser true com regras de porta 443/853")
	}
}

func TestCountIptablesRules_NoRules(t *testing.T) {
	output := `-P OUTPUT ACCEPT
-A OUTPUT -d 10.0.0.1/32 -j ACCEPT
`

	status := countIptablesRules(output)

	if status.FirewallRules != 0 {
		t.Errorf("FirewallRules = %d, want 0", status.FirewallRules)
	}
	if status.DoHActive {
		t.Error("DoHActive deve ser false sem regras DROP")
	}
}

func TestAvailableDoTBins(t *testing.T) {
	bins := availableDoTBins()

	known := map[string]bool{"iptables": true, "ip6tables": true}
	if len(bins) == 0 {
		t.Skip("iptables/ip6tables não disponíveis neste ambiente; nada a verificar")
	}

	for _, bin := range bins {
		if !known[bin] {
			t.Errorf("availableDoTBins() retornou binário inesperado %q", bin)
		}
		if _, err := exec.LookPath(bin); err != nil {
			t.Errorf("availableDoTBins() retornou %q, mas LookPath falha: %v", bin, err)
		}
	}
}

func TestUnprivilegedCheck(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("Skipping unprivileged check as test is running with root permissions (sudo)")
	}

	enf := NewEnforcer()

	errBlock := enf.BlockDomain("example.com", []string{"1.1.1.1"})
	if errBlock == nil {
		t.Error("expected error due to lack of root privileges in BlockDomain, got nil")
	}

	errUnblock := enf.UnblockDomain("example.com", []string{"1.1.1.1"})
	if errUnblock == nil {
		t.Error("expected error due to lack of root privileges in UnblockDomain, got nil")
	}

	errSync := enf.Sync(map[string][]string{"example.com": {"1.1.1.1"}})
	if errSync == nil {
		t.Error("expected error due to lack of root privileges in Sync, got nil")
	}
}

// fakeCmdRunner simulates an iptables -D invocation: it succeeds a configurable
// number of times (removing one orphan rule each) and then reports the
// "does a matching rule exist" no-match error, like the real iptables.
type fakeCmdRunner struct {
	successes int
	calls     [][]string // every command line run (one entry per invocation)
}

func (f *fakeCmdRunner) CombinedOutput() ([]byte, error) {
	if f.successes > 0 {
		f.successes--
		return nil, nil
	}
	return []byte("iptables: Bad rule (does a matching rule exist in that chain)."), errors.New("exit status 1")
}

func TestRemoveFirewallRule_RemovesAllDuplicateRules(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	runner := &fakeCmdRunner{successes: 3}
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		runner.calls = append(runner.calls, append([]string{name}, args...))
		return runner
	}

	enf := &linuxEnforcer{}
	if err := enf.removeFirewallRule("1.2.3.4"); err != nil {
		t.Fatalf("removeFirewallRule: %v", err)
	}

	// Every invocation must be the same -D command for the same IP.
	for i, call := range runner.calls {
		if len(call) < 5 || call[0] != "iptables" {
			t.Errorf("call %d should be an iptables invocation, got %v", i, call)
		}
		if !slices.Contains(call, "-D") || !slices.Contains(call, "1.2.3.4") {
			t.Errorf("call %d should be iptables -D ... 1.2.3.4, got %v", i, call)
		}
	}

	// 3 orphan rules removed + 1 final no-match probe = 4 invocations.
	if len(runner.calls) != 4 {
		t.Errorf("expected 4 iptables invocations (3 removals + 1 probe), got %d", len(runner.calls))
	}
}

func TestRemoveFirewallRule_NoRulesIsNoOp(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	runner := &fakeCmdRunner{successes: 0}
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		runner.calls = append(runner.calls, append([]string{name}, args...))
		return runner
	}

	enf := &linuxEnforcer{}
	if err := enf.removeFirewallRule("1.2.3.4"); err != nil {
		t.Fatalf("removeFirewallRule with no rules should not error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Errorf("expected exactly 1 no-match probe, got %d calls", len(runner.calls))
	}
}

func TestRemoveFirewallRule_PropagatesRealErrors(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		return &failRunner{msg: "iptables: Permission denied"}
	}

	enf := &linuxEnforcer{}
	err := enf.removeFirewallRule("1.2.3.4")
	if err == nil {
		t.Fatal("expected error for non-no-match failure")
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("expected permission error surfaced, got %v", err)
	}
}

// failRunner always fails with a fixed message that is NOT the no-match case.
type failRunner struct {
	msg string
}

func (f *failRunner) CombinedOutput() ([]byte, error) {
	return []byte(f.msg), errors.New("exit status 1")
}

// TestRemoveFirewallRule_StopsAtCap verifies the defensive guard against a
// pathological firewall state where iptables never reports "does a matching
// rule exist": the loop must stop after maxFirewallRuleRemovals attempts with
// an error instead of spinning forever.
func TestRemoveFirewallRule_StopsAtCap(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	runner := &fakeCmdRunner{successes: 1 << 30} // always succeeds, never no-match
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		runner.calls = append(runner.calls, append([]string{name}, args...))
		return runner
	}

	enf := &linuxEnforcer{}
	err := enf.removeFirewallRule("1.2.3.4")
	if err == nil {
		t.Fatal("expected error when the sweep never reaches no-match")
	}
	if !strings.Contains(err.Error(), "limite") {
		t.Errorf("expected cap-limit error, got: %v", err)
	}
	if len(runner.calls) != maxFirewallRuleRemovals {
		t.Errorf("expected exactly %d invocations at the cap, got %d", maxFirewallRuleRemovals, len(runner.calls))
	}
}

func TestBlockDomainLocked_RollbackCleansHosts(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("Skipping: running as root, firewall rules may succeed")
	}

	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")
	initial := "127.0.0.1 localhost\n::1 localhost\n"
	if err := os.WriteFile(tempHostsPath, []byte(initial), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}

	enf := &linuxEnforcer{hostsPath: tempHostsPath}

	err := enf.blockDomainLocked("example.com", []string{"1.1.1.1", "2.2.2.2"})
	if err == nil {
		t.Fatal("expected blockDomainLocked to fail without root privileges")
	}

	data, err := os.ReadFile(tempHostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	if strings.Contains(string(data), "FOCUSGUARD") {
		t.Errorf("expected hosts file to be rolled back (no FOCUSGUARD markers), got:\n%s", data)
	}
	if !strings.Contains(string(data), "127.0.0.1 localhost") {
		t.Errorf("expected original hosts content to be preserved, got:\n%s", data)
	}
}
