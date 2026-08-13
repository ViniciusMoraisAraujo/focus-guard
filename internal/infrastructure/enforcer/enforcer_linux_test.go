//go:build linux

package enforcer

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
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
	// Mistura regras legadas (-j DROP), atuais (-j REJECT --reject-with
	// tcp-reset) e a companion rule de não-TCP (-j REJECT --reject-with
	// icmp-port-unreachable): todas devem ser reconhecidas como IPs bloqueados.
	output := `-P OUTPUT ACCEPT
-A OUTPUT -d 1.1.1.1/32 -j DROP
-A OUTPUT -d 8.8.8.8/32 -p tcp --dport 443 -j DROP
-A OUTPUT -d 2001:db8::1/128 -j DROP
-A OUTPUT -d 9.9.9.9/32 -p tcp -j REJECT --reject-with tcp-reset
-A OUTPUT -d 9.9.9.9/32 -j REJECT --reject-with icmp-port-unreachable
-A OUTPUT -d 2001:db8::2/128 -j REJECT --reject-with tcp-reset
-A OUTPUT -d 2001:db8::2/128 -j REJECT --reject-with icmp-port-unreachable
-A OUTPUT -p tcp --dport 853 -j DROP
-A OUTPUT -d 10.0.0.1/32 -j ACCEPT
`

	blocked := parseIptablesBlockedIPs([]byte(output))

	if !blocked["1.1.1.1"] {
		t.Error("expected 1.1.1.1 (legacy DROP) to be parsed as blocked")
	}
	if !blocked["2001:db8::1"] {
		t.Error("expected 2001:db8::1 (legacy DROP) to be parsed as blocked")
	}
	if !blocked["9.9.9.9"] {
		t.Error("expected 9.9.9.9 (REJECT tcp-reset + icmp-port-unreachable) to be parsed as blocked")
	}
	if !blocked["2001:db8::2"] {
		t.Error("expected 2001:db8::2 (REJECT tcp-reset + icmp-port-unreachable) to be parsed as blocked")
	}
	if blocked["8.8.8.8"] {
		t.Error("DoH rule (--dport) should not be counted as an IP block")
	}
	if blocked["10.0.0.1"] {
		t.Error("ACCEPT rule should not be counted")
	}
	if len(blocked) != 4 {
		t.Errorf("expected 4 blocked IPs, got %d: %v", len(blocked), blocked)
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
	args := doHRuleArgs("-A", "1.1.1.1", 443, "tcp")

	want := []string{"-A", "OUTPUT", "-d", "1.1.1.1", "-p", "tcp", "--dport", "443", "-j", "DROP"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("doHRuleArgs() = %v, want %v", args, want)
	}
}

func TestDoHRuleArgs_UDP(t *testing.T) {
	// DoH sobre QUIC/HTTP/3 (UDP:443) precisa de uma regra própria — sem ela
	// o Firefox contorna o bloqueio TCP-only.
	args := doHRuleArgs("-A", "1.1.1.1", 443, "udp")

	want := []string{"-A", "OUTPUT", "-d", "1.1.1.1", "-p", "udp", "--dport", "443", "-j", "DROP"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("doHRuleArgs(udp) = %v, want %v", args, want)
	}
}

func TestCountIptablesRules(t *testing.T) {
	// Regras atuais (REJECT tcp-reset + companion icmp-port-unreachable),
	// DoH por porta (tcp e udp) e legadas (DROP) contam para o Status.
	output := `-P OUTPUT ACCEPT
-A OUTPUT -d 1.1.1.1/32 -j DROP
-A OUTPUT -d 8.8.8.8/32 -p tcp --dport 443 -j DROP
-A OUTPUT -d 8.8.8.8/32 -p udp --dport 443 -j DROP
-A OUTPUT -d 9.9.9.9/32 -p tcp -j REJECT --reject-with tcp-reset
-A OUTPUT -d 9.9.9.9/32 -j REJECT --reject-with icmp-port-unreachable
-A OUTPUT -p tcp --dport 853 -j DROP
-A OUTPUT -d 10.0.0.1/32 -j ACCEPT
`

	status := countIptablesRules([]byte(output))

	if status.FirewallRules != 6 {
		t.Errorf("FirewallRules = %d, want 6", status.FirewallRules)
	}
	if !status.DoHActive {
		t.Error("DoHActive deve ser true com regras de porta 443/853")
	}
}

func TestCountIptablesRules_NoRules(t *testing.T) {
	output := `-P OUTPUT ACCEPT
-A OUTPUT -d 10.0.0.1/32 -j ACCEPT
`

	status := countIptablesRules([]byte(output))

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

func (f *fakeCmdRunner) SetStdin(r io.Reader) {
	// Not used by -D sweep commands; present to satisfy the cmdRunner interface.
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

	// Fase 1: sweep REJECT tcp (3 remoções + 1 probe no-match). Fase 2: sweep
	// REJECT icmp-port-unreachable (1 probe no-match). Fase 3: sweep legado
	// DROP (1 probe no-match). Total = 6 invocations.
	if len(runner.calls) != 6 {
		t.Errorf("expected 6 iptables invocations (3 REJECT removals + 3 no-match probes), got %d", len(runner.calls))
	}
	for i, call := range runner.calls {
		if len(call) < 5 || call[0] != "iptables" {
			t.Errorf("call %d should be an iptables invocation, got %v", i, call)
		}
		if !slices.Contains(call, "-D") || !slices.Contains(call, "1.2.3.4") {
			t.Errorf("call %d should be iptables -D ... 1.2.3.4, got %v", i, call)
		}
	}
	// Os 4 primeiros usam a spec REJECT tcp; o 5º é o probe da spec REJECT
	// icmp-port-unreachable; o 6º é o probe da spec DROP legada.
	for i := 0; i < 4; i++ {
		if !slices.Contains(runner.calls[i], "REJECT") {
			t.Errorf("call %d should be the REJECT sweep, got %v", i, runner.calls[i])
		}
	}
	if !slices.Contains(runner.calls[4], "icmp-port-unreachable") {
		t.Errorf("call 4 should be the icmp-port-unreachable probe, got %v", runner.calls[4])
	}
	if !slices.Contains(runner.calls[5], "DROP") {
		t.Errorf("call 5 should be the legacy DROP probe, got %v", runner.calls[5])
	}
}

// TestRemoveFirewallRule_V6UsesICMPv6RejectType: o sweep de um IP IPv6 deve
// sondar o tipo icmp6-port-unreachable (não o v4) — o nome ICMPv4 é rejeitado
// pelo ip6tables com backend nft e faria o unblock de um domínio com IPs v6
// falhar.
func TestRemoveFirewallRule_V6UsesICMPv6RejectType(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	runner := &fakeCmdRunner{successes: 0} // todos os probes no-match
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		runner.calls = append(runner.calls, append([]string{name}, args...))
		return runner
	}

	enf := &linuxEnforcer{}
	if err := enf.removeFirewallRule("2001:db8::1"); err != nil {
		t.Fatalf("removeFirewallRule: %v", err)
	}

	if len(runner.calls) != 3 {
		t.Fatalf("expected 3 probes (REJECT tcp + REJECT icmp6 + DROP), got %d: %v", len(runner.calls), runner.calls)
	}
	for _, call := range runner.calls {
		if call[0] != "ip6tables" {
			t.Errorf("v6 remove deve usar ip6tables, got %v", call)
		}
	}
	if !slices.Contains(runner.calls[1], "icmp6-port-unreachable") {
		t.Errorf("probe 1 deve usar icmp6-port-unreachable, got %v", runner.calls[1])
	}
	if slices.Contains(runner.calls[1], "icmp-port-unreachable") {
		t.Errorf("probe 1 não deve usar o tipo ICMPv4, got %v", runner.calls[1])
	}
	if !slices.Contains(runner.calls[2], "DROP") {
		t.Errorf("probe 2 deve ser o sweep DROP legado, got %v", runner.calls[2])
	}
}

// seqResult is one canned outcome for the scripted seqRunner.
type seqResult struct {
	out []byte
	err error
}

func successResult() seqResult { return seqResult{} }
func noMatchResult() seqResult {
	return seqResult{out: []byte("iptables: Bad rule (does a matching rule exist in that chain)."), err: errors.New("exit status 1")}
}

// seqRunner returns a fixed sequence of results (one per invocation), so a
// test can script exactly which removals succeed and which probe no-match —
// unlike fakeCmdRunner, whose success counter is global across both sweep
// phases.
type seqRunner struct {
	results []seqResult
	idx     int
	calls   [][]string
}

func (s *seqRunner) SetStdin(r io.Reader) {}

func (s *seqRunner) CombinedOutput() ([]byte, error) {
	if s.idx >= len(s.results) {
		return nil, errors.New("seqRunner: resultados esgotados")
	}
	r := s.results[s.idx]
	s.idx++
	return r.out, r.err
}

// TestRemoveFirewallRule_SweepsLegacyDropAfterReject verifies the migration
// path: rules created by versions before REJECT tcp-reset (legacy -j DROP)
// are still removed after the REJECT sweep finds nothing.
func TestRemoveFirewallRule_SweepsLegacyDropAfterReject(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	// Sequência: 1 remoção REJECT tcp, probe no-match, probe icmp no-match,
	// 1 remoção DROP legada, probe no-match.
	runner := &seqRunner{results: []seqResult{successResult(), noMatchResult(), noMatchResult(), successResult(), noMatchResult()}}
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		runner.calls = append(runner.calls, append([]string{name}, args...))
		return runner
	}

	enf := &linuxEnforcer{}
	if err := enf.removeFirewallRule("5.6.7.8"); err != nil {
		t.Fatalf("removeFirewallRule: %v", err)
	}

	if len(runner.calls) != 5 {
		t.Fatalf("expected 5 invocations, got %d: %v", len(runner.calls), runner.calls)
	}
	if !slices.Contains(runner.calls[0], "REJECT") {
		t.Errorf("call 0 should be REJECT removal, got %v", runner.calls[0])
	}
	if !slices.Contains(runner.calls[2], "icmp-port-unreachable") {
		t.Errorf("call 2 should be the icmp-port-unreachable probe, got %v", runner.calls[2])
	}
	if !slices.Contains(runner.calls[3], "DROP") {
		t.Errorf("call 3 should be legacy DROP removal, got %v", runner.calls[3])
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

	// 3 probes no-match: REJECT tcp + REJECT icmp-port-unreachable + DROP legada.
	if len(runner.calls) != 3 {
		t.Errorf("expected 3 no-match probes (REJECT tcp + icmp + DROP), got %d calls", len(runner.calls))
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

func (f *failRunner) SetStdin(r io.Reader) {
	// Not used by -D sweep commands; present to satisfy the cmdRunner interface.
}

func (f *failRunner) CombinedOutput() ([]byte, error) {
	return []byte(f.msg), errors.New("exit status 1")
}

// killStub records exec invocations and returns a canned result, so socket
// killing can be asserted without root privileges.
type killStub struct {
	calls [][]string
	err   error
}

func (k *killStub) SetStdin(r io.Reader) {}

func (k *killStub) CombinedOutput() ([]byte, error) {
	return nil, k.err
}

func stubKillExec(t *testing.T, k *killStub) {
	t.Helper()
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		k.calls = append(k.calls, append([]string{name}, args...))
		return k
	}
}

// TestKillSockets_RunsSSPerIP verifies ss -K dst <ip> is invoked once per IP
// to tear down active Keep-Alive connections in the kernel.
func TestKillSockets_RunsSSPerIP(t *testing.T) {
	k := &killStub{}
	stubKillExec(t, k)

	enf := &linuxEnforcer{}
	enf.killSockets([]string{"1.1.1.1", "2.2.2.2", "2001:db8::1"})

	if len(k.calls) != 3 {
		t.Fatalf("expected 3 ss invocations, got %d: %v", len(k.calls), k.calls)
	}
	for i, ip := range []string{"1.1.1.1", "2.2.2.2", "2001:db8::1"} {
		call := k.calls[i]
		if call[0] != "ss" || !slices.Contains(call, "-K") || !slices.Contains(call, "dst") || !slices.Contains(call, ip) {
			t.Errorf("call %d should be ss -K dst %s, got %v", i, ip, call)
		}
	}
}

// TestKillSockets_EmptyIsNoOp verifies killSockets skips when there are no IPs.
func TestKillSockets_EmptyIsNoOp(t *testing.T) {
	k := &killStub{}
	stubKillExec(t, k)

	enf := &linuxEnforcer{}
	enf.killSockets(nil)
	enf.killSockets([]string{})

	if len(k.calls) != 0 {
		t.Errorf("expected no ss invocations for empty input, got %d", len(k.calls))
	}
}

// TestKillSockets_BestEffort verifies socket-kill failures (ss ausente, sem
// suporte -K, sem privilégio) are silently ignored — the REJECT rule already
// kills the connection on the next packet.
func TestKillSockets_BestEffort(t *testing.T) {
	k := &killStub{err: errors.New("ss: option -K not supported")}
	stubKillExec(t, k)

	enf := &linuxEnforcer{}
	enf.killSockets([]string{"1.1.1.1"})
	// Sem pânico e sem propagar erro: a função é void/best-effort.
	if len(k.calls) != 1 {
		t.Errorf("expected 1 ss invocation, got %d", len(k.calls))
	}
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

// ---------------------------------------------------------------------------
// BlockAll / UnblockAll (modo pânico + allowlist deep-focus)
// ---------------------------------------------------------------------------

func TestBuildBlockAllScript_AllowlistFirstThenCatchAll(t *testing.T) {
	script := buildBlockAllScript([]string{"1.1.1.1", "8.8.8.8"}, "/32")

	if !strings.HasPrefix(script, "*filter\n") || !strings.HasSuffix(script, "COMMIT\n") {
		t.Errorf("script deve ter header *filter e footer COMMIT:\n%s", script)
	}
	// ACCEPT da allowlist ANTES do catch-all (ordem de avaliação do iptables)
	allowIdx := strings.Index(script, "-A OUTPUT -d 1.1.1.1/32 -j ACCEPT -m comment --comment \""+AllowMarker)
	catchIdx := strings.Index(script, "-A OUTPUT -p tcp -j REJECT --reject-with tcp-reset -m comment --comment \""+AllBlockMarker)
	nonTCPIdx := strings.Index(script, "-A OUTPUT -j REJECT --reject-with icmp-port-unreachable -m comment --comment \""+AllBlockMarker)
	if allowIdx < 0 {
		t.Errorf("script deve conter ACCEPT da allowlist com marker:\n%s", script)
	}
	if catchIdx < 0 {
		t.Errorf("script deve conter catch-all REJECT tcp com marker:\n%s", script)
	}
	if nonTCPIdx < 0 {
		t.Errorf("script deve conter catch-all REJECT não-TCP (UDP/QUIC) com marker:\n%s", script)
	}
	if allowIdx > catchIdx {
		t.Errorf("ACCEPT da allowlist deve vir antes do catch-all:\n%s", script)
	}
}

func TestBuildBlockAllScript_NoAllowlistOnlyCatchAll(t *testing.T) {
	script := buildBlockAllScript(nil, "/32")
	if !strings.Contains(script, AllBlockMarker) {
		t.Errorf("script sem allowlist deve ter só o catch-all:\n%s", script)
	}
	if strings.Contains(script, AllowMarker) {
		t.Errorf("script sem allowlist não deve conter ACCEPT rules:\n%s", script)
	}
}

// allBlockStub records exec invocations; -S OUTPUT returns the canned rule
// dump and restore calls succeed, so BlockAll/UnblockAll can be exercised
// without root.
type allBlockStub struct {
	calls  [][]string
	stdins []string
	dump   map[string]string // bin -> -S OUTPUT output
}

func (a *allBlockStub) SetStdin(r io.Reader) {
	data, _ := io.ReadAll(r)
	a.stdins = append(a.stdins, string(data))
}

func (a *allBlockStub) CombinedOutput() ([]byte, error) {
	if len(a.calls) == 0 {
		return nil, errors.New("allBlockStub: sem chamadas registradas")
	}
	call := a.calls[len(a.calls)-1]
	if call[0] == "iptables" || call[0] == "ip6tables" {
		if len(call) > 1 && call[1] == "-S" {
			return []byte(a.dump[call[0]]), nil
		}
	}
	return nil, nil
}

func stubAllBlockExec(t *testing.T, a *allBlockStub) {
	t.Helper()
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		a.calls = append(a.calls, append([]string{name}, args...))
		return a
	}
}

func TestBlockAll_AppliesBothFamilies(t *testing.T) {
	a := &allBlockStub{dump: map[string]string{"iptables": "", "ip6tables": ""}}
	stubAllBlockExec(t, a)

	enf := &linuxEnforcer{}
	if err := enf.blockAllLocked([]string{"1.1.1.1", "2001:db8::1"}); err != nil {
		t.Fatalf("BlockAll: %v", err)
	}

	// UnblockAll inicial (2 -S) + 2 restores (iptables + ip6tables) + 2 ss
	if len(a.calls) < 4 {
		t.Fatalf("esperava >= 4 invocações, got %d: %v", len(a.calls), a.calls)
	}
	restores := 0
	for _, c := range a.calls {
		if c[0] == "iptables-restore" || c[0] == "ip6tables-restore" {
			restores++
		}
	}
	if restores != 2 {
		t.Errorf("esperava 2 restores (v4+v6), got %d", restores)
	}
	if len(a.stdins) != 2 {
		t.Fatalf("esperava 2 stdin scripts, got %d", len(a.stdins))
	}
	// v4 script: allowlist 1.1.1.1 + catch-all
	if !strings.Contains(a.stdins[0], "1.1.1.1/32") || !strings.Contains(a.stdins[0], AllBlockMarker) {
		t.Errorf("v4 script inesperado:\n%s", a.stdins[0])
	}
	if !strings.Contains(a.stdins[1], "2001:db8::1/128") {
		t.Errorf("v6 script deveria conter 2001:db8::1:\n%s", a.stdins[1])
	}
}

func TestBlockAll_NoAllowlistOnlyCatchAll(t *testing.T) {
	a := &allBlockStub{dump: map[string]string{"iptables": "", "ip6tables": ""}}
	stubAllBlockExec(t, a)

	enf := &linuxEnforcer{}
	if err := enf.blockAllLocked(nil); err != nil {
		t.Fatalf("BlockAll: %v", err)
	}
	if len(a.stdins) != 2 {
		t.Fatalf("esperava 2 scripts (v4+v6 catch-all), got %d", len(a.stdins))
	}
	for _, s := range a.stdins {
		if !strings.Contains(s, AllBlockMarker) {
			t.Errorf("script deve conter catch-all:\n%s", s)
		}
		if strings.Contains(s, AllowMarker) {
			t.Errorf("script sem allowlist não deve ter ACCEPT:\n%s", s)
		}
	}
}

func TestUnblockAll_SweepsMarkedRules(t *testing.T) {
	a := &allBlockStub{dump: map[string]string{
		"iptables":  "-A OUTPUT -d 1.1.1.1/32 -j ACCEPT -m comment --comment \"FOCUSGUARD_ALLOW\"\n-A OUTPUT -p tcp -j REJECT --reject-with tcp-reset -m comment --comment \"FOCUSGUARD_ALL\"\n-A OUTPUT -j REJECT --reject-with icmp-port-unreachable -m comment --comment \"FOCUSGUARD_ALL\"\n-A OUTPUT -d 9.9.9.9/32 -p tcp -j REJECT --reject-with tcp-reset\n",
		"ip6tables": "",
	}}
	stubAllBlockExec(t, a)

	enf := &linuxEnforcer{}
	if err := enf.unblockAllLocked(); err != nil {
		t.Fatalf("UnblockAll: %v", err)
	}

	// 2 probes -S + 3 deletes (allow + catch-all tcp + catch-all não-TCP). A
	// regra de domínio normal (9.9.9.9, sem marker) não pode ser tocada.
	delCalls := 0
	for _, c := range a.calls {
		if len(c) > 1 && c[1] == "-D" {
			delCalls++
			if strings.Contains(strings.Join(c, " "), "9.9.9.9") {
				t.Errorf("regra de domínio sem marker não pode ser removida: %v", c)
			}
		}
	}
	if delCalls != 3 {
		t.Errorf("esperava 3 deletes (allow + 2 catch-all), got %d: %v", delCalls, a.calls)
	}
}

func TestAllBlockedByStatus_DetectsMarker(t *testing.T) {
	a := &allBlockStub{dump: map[string]string{
		"iptables":  "-A OUTPUT -p tcp -j REJECT --reject-with tcp-reset -m comment --comment \"FOCUSGUARD_ALL\"\n",
		"ip6tables": "",
	}}
	stubAllBlockExec(t, a)

	enf := &linuxEnforcer{}
	if !enf.allBlockedByStatus() {
		t.Error("allBlockedByStatus deveria detectar o catch-all")
	}
}

func TestAllBlockedByStatus_NoMarkerFalse(t *testing.T) {
	a := &allBlockStub{dump: map[string]string{"iptables": "-A OUTPUT -d 9.9.9.9/32 -j REJECT --reject-with tcp-reset\n", "ip6tables": ""}}
	stubAllBlockExec(t, a)

	enf := &linuxEnforcer{}
	if enf.allBlockedByStatus() {
		t.Error("allBlockedByStatus não deveria detectar regra sem marker")
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

// batchStub replaces execCommandContext for the batch tests: it records every
// invocation (name+args) and the scripts fed via stdin, returns the canned
// -S OUTPUT dump for existence queries and a canned error for restore calls.
type batchStub struct {
	calls      [][]string
	stdins     []string
	query      map[string]string // bin -> -S OUTPUT output
	restoreErr error
}

func (b *batchStub) SetStdin(r io.Reader) {
	data, _ := io.ReadAll(r)
	b.stdins = append(b.stdins, string(data))
}

func (b *batchStub) CombinedOutput() ([]byte, error) {
	if len(b.calls) == 0 {
		return nil, b.restoreErr
	}
	call := b.calls[len(b.calls)-1]
	switch call[0] {
	case "iptables", "ip6tables":
		return []byte(b.query[call[0]]), nil
	default:
		return nil, b.restoreErr
	}
}

func stubBatchExec(t *testing.T, b *batchStub) {
	t.Helper()
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		b.calls = append(b.calls, append([]string{name}, args...))
		return b
	}
}

// TestAddFirewallRulesBatch_V4SingleRestore verifies that N IPv4 rules are
// applied with a single iptables-restore --noflush invocation (1 exec for all
// IPs) instead of one iptables -A per IP.
func TestAddFirewallRulesBatch_V4SingleRestore(t *testing.T) {
	b := &batchStub{query: map[string]string{"iptables": "", "ip6tables": ""}}
	stubBatchExec(t, b)

	enf := &linuxEnforcer{}
	if err := enf.addFirewallRulesBatch([]string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}); err != nil {
		t.Fatalf("addFirewallRulesBatch: %v", err)
	}

	// 2 existence queries (-S) + 1 restore + 3 socket kills (ss -K).
	if len(b.calls) != 6 {
		t.Fatalf("expected 6 invocations (2 -S queries + 1 restore + 3 ss), got %d: %v", len(b.calls), b.calls)
	}

	restore := b.calls[2]
	if restore[0] != "iptables-restore" {
		t.Errorf("expected iptables-restore, got %v", restore)
	}
	if !slices.Contains(restore, "--noflush") {
		t.Errorf("restore must use --noflush to avoid flushing the table: %v", restore)
	}

	if len(b.stdins) != 1 {
		t.Fatalf("expected 1 stdin script, got %d", len(b.stdins))
	}
	script := b.stdins[0]
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"} {
		if !strings.Contains(script, "-A OUTPUT -d "+ip+"/32 -p tcp -j REJECT --reject-with tcp-reset") {
			t.Errorf("script missing TCP REJECT rule for %s:\n%s", ip, script)
		}
		if !strings.Contains(script, "-A OUTPUT -d "+ip+"/32 -j REJECT --reject-with icmp-port-unreachable") {
			t.Errorf("script missing non-TCP REJECT rule for %s (UDP/QUIC leak):\n%s", ip, script)
		}
		if strings.Contains(script, "-j DROP") {
			t.Errorf("script must use REJECT tcp-reset, not DROP:\n%s", script)
		}
	}
	if !strings.HasPrefix(script, "*filter\n") || !strings.HasSuffix(script, "COMMIT\n") {
		t.Errorf("script must have *filter header and COMMIT footer:\n%s", script)
	}

	// No per-IP iptables -A invocations must have happened.
	for _, c := range b.calls {
		if c[0] == "iptables" && slices.Contains(c, "-A") {
			t.Errorf("per-IP -A invocation found: %v", c)
		}
	}

	// Cada IP recém-adicionado deve ter um ss -K dst <ip> para matar
	// conexões Keep-Alive ativas.
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"} {
		found := false
		for _, c := range b.calls {
			if c[0] == "ss" && slices.Contains(c, "-K") && slices.Contains(c, "dst") && slices.Contains(c, ip) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected ss -K dst %s socket-kill invocation", ip)
		}
	}
}

// TestAddFirewallRulesBatch_SkipsExisting verifies that IPs already blocked
// (present in the -S dump) are not re-added by the batch.
func TestAddFirewallRulesBatch_SkipsExisting(t *testing.T) {
	b := &batchStub{query: map[string]string{
		"iptables":  "-A OUTPUT -d 1.1.1.1/32 -j DROP\n",
		"ip6tables": "",
	}}
	stubBatchExec(t, b)

	enf := &linuxEnforcer{}
	if err := enf.addFirewallRulesBatch([]string{"1.1.1.1", "2.2.2.2"}); err != nil {
		t.Fatalf("addFirewallRulesBatch: %v", err)
	}

	if len(b.stdins) != 1 {
		t.Fatalf("expected 1 stdin script, got %d", len(b.stdins))
	}
	if strings.Contains(b.stdins[0], "1.1.1.1") {
		t.Errorf("already-blocked IP must not be re-added:\n%s", b.stdins[0])
	}
	if !strings.Contains(b.stdins[0], "-A OUTPUT -d 2.2.2.2/32 -p tcp -j REJECT --reject-with tcp-reset") {
		t.Errorf("missing IP 2.2.2.2 should be added:\n%s", b.stdins[0])
	}

	// Socket kill só para os IPs efetivamente adicionados, nunca para os já
	// bloqueados.
	for _, c := range b.calls {
		if c[0] == "ss" {
			if slices.Contains(c, "1.1.1.1") {
				t.Errorf("already-blocked IP must not be socket-killed: %v", c)
			}
		}
	}
	if !slices.Contains(b.calls[len(b.calls)-1], "2.2.2.2") {
		t.Errorf("expected socket kill for newly added 2.2.2.2, last call: %v", b.calls[len(b.calls)-1])
	}
}

// TestAddFirewallRulesBatch_MixedFamilies verifies v4 and v6 are applied with
// one restore invocation per family, each with the right binary and mask.
func TestAddFirewallRulesBatch_MixedFamilies(t *testing.T) {
	b := &batchStub{query: map[string]string{"iptables": "", "ip6tables": ""}}
	stubBatchExec(t, b)

	enf := &linuxEnforcer{}
	if err := enf.addFirewallRulesBatch([]string{"1.1.1.1", "2001:db8::1"}); err != nil {
		t.Fatalf("addFirewallRulesBatch: %v", err)
	}

	if len(b.calls) != 6 {
		t.Fatalf("expected 6 invocations (2 -S queries + 2 restores + 2 ss), got %d: %v", len(b.calls), b.calls)
	}
	if b.calls[2][0] != "iptables-restore" {
		t.Errorf("expected iptables-restore first, got %v", b.calls[2])
	}
	if b.calls[3][0] != "ip6tables-restore" {
		t.Errorf("expected ip6tables-restore second, got %v", b.calls[3])
	}
	if len(b.stdins) != 2 {
		t.Fatalf("expected 2 stdin scripts, got %d", len(b.stdins))
	}
	if !strings.Contains(b.stdins[0], "-d 1.1.1.1/32") {
		t.Errorf("v4 script should use /32 mask:\n%s", b.stdins[0])
	}
	if !strings.Contains(b.stdins[1], "-d 2001:db8::1/128") {
		t.Errorf("v6 script should use /128 mask:\n%s", b.stdins[1])
	}
	// Regressão da Etapa 6 real: o script v6 deve rejeitar com o tipo ICMPv6
	// (icmp6-port-unreachable) — o nome ICMPv4 é rejeitado pelo backend nft do
	// ip6tables e quebrava o bloqueio de domínios com IPs IPv6.
	if !strings.Contains(b.stdins[1], "icmp6-port-unreachable") {
		t.Errorf("v6 script deve usar icmp6-port-unreachable:\n%s", b.stdins[1])
	}
	if strings.Contains(b.stdins[1], "icmp-port-unreachable") {
		t.Errorf("v6 script não deve conter o tipo ICMPv4 icmp-port-unreachable:\n%s", b.stdins[1])
	}
	// Socket kills para ambas as famílias (v4 e v6).
	for _, ip := range []string{"1.1.1.1", "2001:db8::1"} {
		found := false
		for _, c := range b.calls {
			if c[0] == "ss" && slices.Contains(c, "dst") && slices.Contains(c, ip) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected ss -K dst %s socket-kill invocation", ip)
		}
	}
}

// TestBuildRestoreScript_V6UsesICMPv6RejectType: o tipo --reject-with da
// regra protocol-agnostic depende da família — icmp-port-unreachable (ICMPv4)
// no v4, icmp6-port-unreachable (ICMPv6) no v6. O backend nft do ip6tables
// rejeita o nome ICMPv4 ("unknown reject type") — achado da Etapa 6 real no
// Ubuntu 26.04 (block youtube.com falhava por causa dos IPs IPv6).
func TestBuildRestoreScript_V6UsesICMPv6RejectType(t *testing.T) {
	script := buildRestoreScript([]string{"2001:db8::1"}, "/128")
	if !strings.Contains(script, "-j REJECT --reject-with icmp6-port-unreachable") {
		t.Errorf("v6 script deve usar icmp6-port-unreachable (o nome ICMPv4 é rejeitado pelo nft):\n%s", script)
	}
	if strings.Contains(script, "icmp-port-unreachable") {
		t.Errorf("v6 script não deve conter o tipo ICMPv4 icmp-port-unreachable:\n%s", script)
	}
	if !strings.Contains(script, "-p tcp -j REJECT --reject-with tcp-reset") {
		t.Errorf("v6 script deve manter a regra TCP tcp-reset:\n%s", script)
	}
}

// TestBuildBlockAllScript_V6UsesICMPv6RejectType: o catch-all do modo pânico
// também usa o tipo ICMPv6 no v6 — mesmo bug de família do buildRestoreScript.
func TestBuildBlockAllScript_V6UsesICMPv6RejectType(t *testing.T) {
	script := buildBlockAllScript(nil, "/128")
	if !strings.Contains(script, "-j REJECT --reject-with icmp6-port-unreachable") {
		t.Errorf("v6 catch-all deve usar icmp6-port-unreachable:\n%s", script)
	}
	if strings.Contains(script, "icmp-port-unreachable") {
		t.Errorf("v6 catch-all não deve conter o tipo ICMPv4 icmp-port-unreachable:\n%s", script)
	}
	if !strings.Contains(script, "-p tcp -j REJECT --reject-with tcp-reset") {
		t.Errorf("v6 catch-all deve manter a regra TCP tcp-reset:\n%s", script)
	}
}

// TestAddFirewallRulesBatch_PropagatesError verifies a restore failure is
// surfaced with the failing binary in the message.
func TestAddFirewallRulesBatch_PropagatesError(t *testing.T) {
	b := &batchStub{
		query:      map[string]string{"iptables": "", "ip6tables": ""},
		restoreErr: errors.New("iptables-restore: Permission denied (you must be root)"),
	}
	stubBatchExec(t, b)

	enf := &linuxEnforcer{}
	err := enf.addFirewallRulesBatch([]string{"1.1.1.1"})
	if err == nil {
		t.Fatal("expected error when restore fails")
	}
	if !strings.Contains(err.Error(), "iptables-restore") {
		t.Errorf("error should mention iptables-restore, got: %v", err)
	}
}

// TestSyncLocked_WritesHostsOnce verifies the batched Sync rewrites the hosts
// file exactly once for N domains (single read-modify-write) instead of once
// per domain (1 clean write + N addHostEntry writes).
func TestSyncLocked_WritesHostsOnce(t *testing.T) {
	b := &batchStub{query: map[string]string{"iptables": "", "ip6tables": ""}}
	stubBatchExec(t, b)

	tempDir := t.TempDir()
	hostsPath := filepath.Join(tempDir, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n::1 localhost\n"), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	enf := &linuxEnforcer{hostsPath: hostsPath}

	var writes int32
	enf.SetOnHostsWrite(func() { atomic.AddInt32(&writes, 1) })

	active := map[string][]string{
		"a.com": {"1.1.1.1"},
		"b.com": {"2.2.2.2"},
	}
	if err := enf.syncLocked(active); err != nil {
		t.Fatalf("syncLocked: %v", err)
	}

	if got := atomic.LoadInt32(&writes); got != 1 {
		t.Errorf("expected exactly 1 hosts write for 2 domains, got %d", got)
	}

	data, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	content := string(data)
	for _, d := range []string{"a.com", "b.com"} {
		if !strings.Contains(content, "# FOCUSGUARD: "+d) {
			t.Errorf("hosts missing marker for %s:\n%s", d, content)
		}
	}
	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Errorf("original hosts lines must be preserved:\n%s", content)
	}
}

// TestSyncLocked_CleansStaleMarkers verifies a re-sync strips old FOCUSGUARD
// entries (e.g. a domain that was unblocked elsewhere) and keeps only the
// active batch, still with a single write.
func TestSyncLocked_CleansStaleMarkers(t *testing.T) {
	b := &batchStub{query: map[string]string{"iptables": "", "ip6tables": ""}}
	stubBatchExec(t, b)

	tempDir := t.TempDir()
	hostsPath := filepath.Join(tempDir, "hosts")
	seed := "127.0.0.1 localhost\n" +
		"127.0.0.1 stale.com # FOCUSGUARD: stale.com\n" +
		"::1 stale.com # FOCUSGUARD: stale.com\n"
	if err := os.WriteFile(hostsPath, []byte(seed), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	enf := &linuxEnforcer{hostsPath: hostsPath}

	if err := enf.syncLocked(map[string][]string{"a.com": {"1.1.1.1"}}); err != nil {
		t.Fatalf("syncLocked: %v", err)
	}

	data, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "stale.com") {
		t.Errorf("stale FOCUSGUARD marker must be cleaned:\n%s", content)
	}
	if !strings.Contains(content, "# FOCUSGUARD: a.com") {
		t.Errorf("active batch marker missing:\n%s", content)
	}
	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Errorf("original hosts lines must be preserved:\n%s", content)
	}
}

// TestSyncLocked_SweepsOrphans verifies Sync removes domain block rules that
// are no longer in activeBlocks (crashed-before-Unblock leftovers) while
// preserving DoH/DoT rules and the catch-all.
func TestSyncLocked_SweepsOrphans(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	dump := "-A OUTPUT -d 1.1.1.1/32 -j REJECT --reject-with tcp-reset\n" +
		"-A OUTPUT -d 3.3.3.3/32 -j REJECT --reject-with tcp-reset\n" +
		"-A OUTPUT -d 8.8.8.8/32 -p udp --dport 853 -j REJECT --reject-with tcp-reset\n" +
		"-A OUTPUT -j REJECT --reject-with tcp-reset\n"
	// Consulta iptables, consulta ip6tables, depois o sweep do órfão 3.3.3.3:
	// -D REJECT tcp (ok), -D REJECT tcp (no-match), -D icmp (no-match),
	// -D DROP legado (no-match).
	runner := &seqRunner{results: []seqResult{
		{out: []byte(dump)},
		{out: nil},
		successResult(), noMatchResult(), noMatchResult(), noMatchResult(),
	}}
	execCommandContext = func(_ context.Context, name string, args ...string) cmdRunner {
		runner.calls = append(runner.calls, append([]string{name}, args...))
		return runner
	}

	tempDir := t.TempDir()
	hostsPath := filepath.Join(tempDir, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n::1 localhost\n"), 0644); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	enf := &linuxEnforcer{hostsPath: hostsPath}

	if err := enf.syncLocked(map[string][]string{"a.com": {"1.1.1.1"}}); err != nil {
		t.Fatalf("syncLocked: %v", err)
	}

	// O sweep do órfão gera 4 invocações -D (1 remoção com sucesso na spec
	// tcp-reset + 3 probes no-match: tcp esgotada, icmp, DROP legado) — o
	// comentário do setup já descrevia os 4; a asserção original exigia 1 e
	// nunca rodou (a suíte Linux nunca foi executada). O que importa: apenas
	// UMA remoção tem sucesso e ela atinge 3.3.3.3 — nunca o DoH/ativo.
	var deletes [][]string
	successful := 0
	for i, call := range runner.calls {
		if len(call) > 0 && call[0] == "iptables" && slices.Contains(call, "-D") {
			deletes = append(deletes, call)
			if runner.results[i].err == nil {
				successful++
			}
		}
	}
	if len(deletes) != 4 {
		t.Fatalf("expected 4 -D invocations (1 remoção + 3 probes no-match), got %d: %v", len(deletes), runner.calls)
	}
	if successful != 1 {
		t.Fatalf("expected exactly 1 successful orphan -D removal, got %d: %v", successful, runner.calls)
	}
	if !slices.Contains(deletes[0], "3.3.3.3") {
		t.Errorf("orphan removal should target 3.3.3.3, got %v", deletes[0])
	}
	for _, bad := range []string{"8.8.8.8", "1.1.1.1"} {
		for _, d := range deletes {
			if slices.Contains(d, bad) {
				t.Errorf("DoH/active rule %s must not be swept, got %v", bad, d)
			}
		}
	}
}
