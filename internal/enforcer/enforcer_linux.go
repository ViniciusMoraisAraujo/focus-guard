//go:build linux

package enforcer

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type linuxEnforcer struct {
	mu           sync.Mutex
	hostsPath    string
	onHostsWrite func()
}

func NewEnforcer() Enforcer {
	return &linuxEnforcer{
		hostsPath: "/etc/hosts",
	}
}

func (e *linuxEnforcer) SetOnHostsWrite(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onHostsWrite = fn
}

func checkRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("enforcer: privilégios insuficientes (uid=%d); é necessário rodar como root (sudo) para gerenciar regras de firewall e editar %s", os.Geteuid(), "/etc/hosts")
	}
	return nil
}

func (e *linuxEnforcer) BlockDomain(domain string, ips []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkRoot(); err != nil {
		return err
	}

	return e.blockDomainLocked(domain, ips)
}

// blockDomainLocked applies a block while holding the lock. If the batched
// firewall application fails, the whole operation is rolled back (host entry
// plus every attempted rule) so the system never keeps a partial/zombie block.
func (e *linuxEnforcer) blockDomainLocked(domain string, ips []string) error {
	if err := e.addHostEntry(domain); err != nil {
		return fmt.Errorf("enforcer: failed to add host entry: %w", err)
	}

	allIPs := dedupeIPs(ips)
	if err := e.addFirewallRulesBatch(allIPs); err != nil {
		for _, ip := range allIPs {
			_ = e.removeFirewallRule(ip)
		}
		_ = e.removeHostEntry(domain)
		return fmt.Errorf("enforcer: failed to add firewall rule: %w", err)
	}
	return nil
}

func (e *linuxEnforcer) UnblockDomain(domain string, ips []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkRoot(); err != nil {
		return err
	}

	return e.unblockDomainLocked(domain, ips)
}

func (e *linuxEnforcer) unblockDomainLocked(domain string, ips []string) error {
	if err := e.removeHostEntry(domain); err != nil {
		return fmt.Errorf("enforcer: failed to remove host entry: %w", err)
	}

	allIPs := dedupeIPs(ips)

	var firstErr error
	for _, ip := range allIPs {
		if err := e.removeFirewallRule(ip); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("enforcer: failed to remove firewall rule for %s: %w", ip, err)
			}
		}
	}

	return firstErr
}

func (e *linuxEnforcer) Sync(activeBlocks map[string][]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkRoot(); err != nil {
		return err
	}

	return e.syncLocked(activeBlocks)
}

// syncLocked applies a batch of active blocks with a single hosts-file rewrite
// (one read-modify-write for N domains instead of one write per domain) and a
// single batched firewall application. The caller must hold e.mu.
func (e *linuxEnforcer) syncLocked(activeBlocks map[string][]string) error {
	lines, err := e.readHostsLines()
	if err != nil {
		return fmt.Errorf("enforcer: failed to read hosts file: %w", err)
	}

	// Strip stale FOCUSGUARD markers and append the batch's entries in one pass.
	var newLines []string
	for _, line := range lines {
		if !strings.Contains(line, "# FOCUSGUARD:") {
			newLines = append(newLines, line)
		}
	}
	seen := make(map[string]bool, len(activeBlocks))
	for domain := range activeBlocks {
		d, err := sanitizeDomain(domain)
		if err != nil {
			return err
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		newLines = append(newLines, hostEntryLines(d)...)
	}
	if err := e.writeHostsLines(newLines); err != nil {
		return fmt.Errorf("enforcer: failed to sync hosts file: %w", err)
	}

	existing := e.existingBlockedIPs()

	var missing []string
	for _, ips := range activeBlocks {
		for _, ip := range dedupeIPs(ips) {
			if existing[ip] {
				continue
			}
			missing = append(missing, ip)
		}
	}

	if len(missing) > 0 {
		if err := e.restoreFirewallRulesBatch(missing); err != nil {
			return fmt.Errorf("enforcer: failed to sync firewall rules: %w", err)
		}
	}

	return nil
}

// existingBlockedIPs consults the firewall once and returns the IPs with an
// existing DROP rule, so Sync/block can add only the missing ones. Both
// families are always probed; a missing binary just fails the exec and is
// skipped, which keeps the query deterministic in tests.
func (e *linuxEnforcer) existingBlockedIPs() map[string]bool {
	blocked := make(map[string]bool)
	for _, bin := range []string{"iptables", "ip6tables"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := execCommandContext(ctx, bin, "-S", "OUTPUT")
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		for ip := range parseIptablesBlockedIPs(out) {
			blocked[ip] = true
		}
	}
	return blocked
}

// addFirewallRulesBatch applies a DROP rule for every IP with a single
// iptables-restore/ip6tables-restore --noflush invocation per address family
// (1 exec for N IPs instead of one exec per IP). IPs already blocked are
// skipped, so re-blocking after a periodic refresh never duplicates rules.
func (e *linuxEnforcer) addFirewallRulesBatch(ips []string) error {
	allIPs := dedupeIPs(ips)
	if len(allIPs) == 0 {
		return nil
	}

	existing := e.existingBlockedIPs()
	var missing []string
	for _, ip := range allIPs {
		if !existing[ip] {
			missing = append(missing, ip)
		}
	}
	return e.restoreFirewallRulesBatch(missing)
}

// restoreFirewallRulesBatch applies DROP rules for already-computed missing
// IPs with one restore invocation per family.
func (e *linuxEnforcer) restoreFirewallRulesBatch(ips []string) error {
	v4, v6 := groupIPsByFamily(ips)
	if len(v4) > 0 {
		if err := e.restoreFirewallRules("iptables-restore", v4, "/32"); err != nil {
			return err
		}
	}
	if len(v6) > 0 {
		if err := e.restoreFirewallRules("ip6tables-restore", v6, "/128"); err != nil {
			return err
		}
	}
	return nil
}

// restoreFirewallRules feeds a restore script to a single iptables-restore
// --noflush process via stdin.
func (e *linuxEnforcer) restoreFirewallRules(bin string, ips []string, mask string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, bin, "--noflush")
	cmd.SetStdin(strings.NewReader(buildRestoreScript(ips, mask)))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s restore falhou: %w (%s)", bin, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func parseIptablesBlockedIPs(output []byte) map[string]bool {
	blocked := make(map[string]bool)
	for _, line := range bytes.Split(output, []byte("\n")) {
		if !bytes.Contains(line, []byte("-j DROP")) || bytes.Contains(line, []byte("--dport")) {
			continue
		}
		fields := bytes.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if bytes.Equal(fields[i], []byte("-d")) {
				ip := bytes.TrimSuffix(fields[i+1], []byte("/32"))
				ip = bytes.TrimSuffix(ip, []byte("/128"))
				blocked[string(ip)] = true
			}
		}
	}
	return blocked
}

func (e *linuxEnforcer) addHostEntry(domain string) error {
	domain, err := sanitizeDomain(domain)
	if err != nil {
		return err
	}

	lines, err := e.readHostsLines()
	if err != nil {
		return err
	}

	marker := fmt.Sprintf("# FOCUSGUARD: %s", domain)
	for _, line := range lines {
		if strings.Contains(line, marker) {
			return nil
		}
	}

	lines = append(lines, hostEntryLines(domain)...)

	return e.writeHostsLines(lines)
}

func (e *linuxEnforcer) removeHostEntry(domain string) error {
	domain, err := sanitizeDomain(domain)
	if err != nil {
		return err
	}

	lines, err := e.readHostsLines()
	if err != nil {
		return err
	}

	marker := fmt.Sprintf("# FOCUSGUARD: %s", domain)
	var newLines []string
	for _, line := range lines {
		if !strings.Contains(line, marker) {
			newLines = append(newLines, line)
		}
	}

	return e.writeHostsLines(newLines)
}

func (e *linuxEnforcer) readHostsLines() ([]string, error) {
	data, err := os.ReadFile(e.hostsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// The hosts file was deleted — recreate it with a baseline so localhost
		// keeps resolving, then continue reading.
		if werr := e.writeHostsLines(defaultHostsLines()); werr != nil {
			return nil, werr
		}
		data, err = os.ReadFile(e.hostsPath)
		if err != nil {
			return nil, err
		}
	}

	return splitHostsLines(data), nil
}

// defaultHostsLines is the baseline written when the hosts file is missing.
func defaultHostsLines() []string {
	return []string{
		"127.0.0.1 localhost",
		"::1 localhost",
	}
}

func (e *linuxEnforcer) writeHostsLines(lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(e.hostsPath, []byte(content), 0644); err != nil {
		return err
	}
	if e.onHostsWrite != nil {
		e.onHostsWrite()
	}
	return nil
}

func iptablesBinFor(ip string) (string, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", fmt.Errorf("IP inválido: %q", ip)
	}

	if parsed.To4() != nil {
		return "iptables", nil
	}
	return "ip6tables", nil
}

// maxFirewallRuleRemovals caps the orphan-rule sweep: in a pathological
// firewall state where iptables never reports "does a matching rule exist"
// (e.g. a broken driver that accepts but never applies -D), the loop must stop
// instead of spinning forever.
const maxFirewallRuleRemovals = 100

// removeFirewallRule removes every matching DROP rule for ip, looping until
// iptables reports "does a matching rule exist". This sweeps orphan rules
// accumulated from previous crashes/races instead of removing just one.
func (e *linuxEnforcer) removeFirewallRule(ip string) error {
	bin, err := iptablesBinFor(ip)
	if err != nil {
		return err
	}

	for attempts := 0; attempts < maxFirewallRuleRemovals; attempts++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := execCommandContext(ctx, bin, "-D", "OUTPUT", "-d", ip, "-j", "DROP")
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			if strings.Contains(string(out), "does a matching rule exist") {
				return nil
			}
			return fmt.Errorf("%s -D falhou: %w (%s)", bin, err, strings.TrimSpace(string(out)))
		}
	}

	return fmt.Errorf("%s: limite de %d remoções atingido para %s (regras órfãs podem restar)", bin, maxFirewallRuleRemovals, ip)
}

func (e *linuxEnforcer) Status() (EnforcerStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkRoot(); err != nil {
		return EnforcerStatus{}, err
	}

	bins := availableDoTBins()
	if len(bins) == 0 {
		return EnforcerStatus{}, fmt.Errorf("nenhum binário de firewall disponível (iptables/ip6tables não encontrados)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status := EnforcerStatus{}
	queried := 0
	for _, bin := range bins {
		cmd := exec.CommandContext(ctx, bin, "-S", "OUTPUT")
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		queried++
		st := countIptablesRules(out)
		status.FirewallRules += st.FirewallRules
		status.DoHActive = status.DoHActive || st.DoHActive
	}

	// Erro apenas se nenhuma consulta teve sucesso; 0 regras de um firewall limpo é válido.
	if queried == 0 {
		return EnforcerStatus{}, fmt.Errorf("falha ao consultar as regras de firewall (iptables/ip6tables)")
	}

	return status, nil
}

func countIptablesRules(output []byte) EnforcerStatus {
	status := EnforcerStatus{}

	for _, line := range bytes.Split(output, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasSuffix(line, []byte("-j DROP")) {
			continue
		}
		status.FirewallRules++
		if bytes.Contains(line, []byte("--dport 853")) || bytes.Contains(line, []byte("--dport 443")) {
			status.DoHActive = true
		}
	}

	return status
}

func doTRuleArgs(jump, protocol string, port int) []string {
	return []string{jump, "OUTPUT", "-p", protocol, "--dport", fmt.Sprintf("%d", port), "-j", "DROP"}
}

func availableDoTBins() []string {
	var bins []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		if _, err := exec.LookPath(bin); err == nil {
			bins = append(bins, bin)
		}
	}
	return bins
}

func doHRuleArgs(jump, ip string, port int) []string {
	return []string{jump, "OUTPUT", "-d", ip, "-p", "tcp", "--dport", fmt.Sprintf("%d", port), "-j", "DROP"}
}

func (e *linuxEnforcer) BlockDoH() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkRoot(); err != nil {
		return err
	}

	for _, provider := range DoHProviders {
		if err := e.addIptablesDoHRule(provider); err != nil {
			return err
		}
	}
	return nil
}

func (e *linuxEnforcer) UnblockDoH() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkRoot(); err != nil {
		return err
	}

	for _, provider := range DoHProviders {
		if err := e.removeIptablesDoHRule(provider); err != nil {
			return err
		}
	}
	return nil
}

func (e *linuxEnforcer) addIptablesDoHRule(provider DoHProvider) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if provider.IsDoT {
		for _, bin := range availableDoTBins() {
			checkCmd := exec.CommandContext(ctx, bin, doTRuleArgs("-C", provider.Protocol, provider.Port)...)
			if err := checkCmd.Run(); err == nil {
				continue
			}

			addCmd := exec.CommandContext(ctx, bin, doTRuleArgs("-A", provider.Protocol, provider.Port)...)
			if out, err := addCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s DoT block falhou para %s: %w (%s)",
					bin, provider.Name, err, strings.TrimSpace(string(out)))
			}
		}
		return nil
	}

	for _, ip := range provider.IPs {
		bin, err := iptablesBinFor(ip)
		if err != nil {
			continue
		}

		checkCmd := exec.CommandContext(ctx, bin, doHRuleArgs("-C", ip, provider.Port)...)
		if err := checkCmd.Run(); err == nil {
			continue
		}

		addCmd := exec.CommandContext(ctx, bin, doHRuleArgs("-A", ip, provider.Port)...)
		if out, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s DoH block falhou para %s (%s): %w (%s)",
				bin, provider.Name, ip, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (e *linuxEnforcer) removeIptablesDoHRule(provider DoHProvider) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if provider.IsDoT {
		for _, bin := range availableDoTBins() {
			cmd := exec.CommandContext(ctx, bin, doTRuleArgs("-D", provider.Protocol, provider.Port)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				if strings.Contains(string(out), "does a matching rule exist") {
					continue
				}
				return fmt.Errorf("%s DoT remove falhou para %s: %w (%s)",
					bin, provider.Name, err, strings.TrimSpace(string(out)))
			}
		}
		return nil
	}

	for _, ip := range provider.IPs {
		bin, err := iptablesBinFor(ip)
		if err != nil {
			continue
		}

		cmd := exec.CommandContext(ctx, bin, doHRuleArgs("-D", ip, provider.Port)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			if strings.Contains(string(out), "does a matching rule exist") {
				continue
			}
			return fmt.Errorf("%s DoH remove falhou para %s (%s): %w (%s)",
				bin, provider.Name, ip, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
