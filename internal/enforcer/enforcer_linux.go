//go:build linux

package enforcer

import (
	"bufio"
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

// blockDomainLocked applies a block while holding the lock. If any firewall
// rule fails, the whole operation is rolled back (host entry plus the rules
// already applied) so the system never keeps a partial/zombie block.
func (e *linuxEnforcer) blockDomainLocked(domain string, ips []string) error {
	if err := e.addHostEntry(domain); err != nil {
		return fmt.Errorf("enforcer: failed to add host entry: %w", err)
	}

	allIPs := dedupeIPs(ips)
	if err := applyBlockRules(allIPs, e.addFirewallRule, e.removeFirewallRule); err != nil {
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

	lines, err := e.readHostsLines()
	if err == nil {
		var cleanLines []string
		for _, line := range lines {
			if !strings.Contains(line, "# FOCUSGUARD:") {
				cleanLines = append(cleanLines, line)
			}
		}
		if err := e.writeHostsLines(cleanLines); err != nil {
			return fmt.Errorf("enforcer: failed to clean hosts file: %w", err)
		}
	}

	existing := e.existingBlockedIPs()

	for domain, ips := range activeBlocks {
		if err := e.addHostEntry(domain); err != nil {
			return fmt.Errorf("enforcer: failed to sync host entry for %s: %w", domain, err)
		}

		for _, ip := range dedupeIPs(ips) {
			if existing[ip] {
				continue
			}
			if err := e.addFirewallRuleUnchecked(ip); err != nil {
				return fmt.Errorf("enforcer: failed to sync firewall rule for %s: %w", ip, err)
			}
		}
	}

	return nil
}

// existingBlockedIPs consults the firewall once and returns the IPs with an
// existing DROP rule, so Sync can add only the missing ones.
func (e *linuxEnforcer) existingBlockedIPs() map[string]bool {
	blocked := make(map[string]bool)
	for _, bin := range availableDoTBins() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, bin, "-S", "OUTPUT").Output()
		cancel()
		if err != nil {
			continue
		}
		for ip := range parseIptablesBlockedIPs(string(out)) {
			blocked[ip] = true
		}
	}
	return blocked
}

func parseIptablesBlockedIPs(output string) map[string]bool {
	blocked := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "-j DROP") || strings.Contains(line, "--dport") {
			continue
		}
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "-d" {
				ip := strings.TrimSuffix(fields[i+1], "/32")
				ip = strings.TrimSuffix(ip, "/128")
				blocked[ip] = true
			}
		}
	}
	return blocked
}

func (e *linuxEnforcer) addHostEntry(domain string) error {
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

	lines = append(lines,
		fmt.Sprintf("127.0.0.1 %s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("::1 %s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("127.0.0.1 www.%s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("::1 www.%s # FOCUSGUARD: %s", domain, domain),
	)

	return e.writeHostsLines(lines)
}

func (e *linuxEnforcer) removeHostEntry(domain string) error {
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
	file, err := os.Open(e.hostsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// The hosts file was deleted — recreate it with a baseline so localhost
		// keeps resolving, then continue reading.
		if werr := e.writeHostsLines(defaultHostsLines()); werr != nil {
			return nil, werr
		}
		file, err = os.Open(e.hostsPath)
		if err != nil {
			return nil, err
		}
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
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

func (e *linuxEnforcer) addFirewallRule(ip string) error {
	bin, err := iptablesBinFor(ip)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	checkCmd := exec.CommandContext(ctx, bin, "-C", "OUTPUT", "-d", ip, "-j", "DROP")
	if err := checkCmd.Run(); err == nil {
		return nil
	}

	return e.addFirewallRuleUnchecked(ip)
}

func (e *linuxEnforcer) addFirewallRuleUnchecked(ip string) error {
	bin, err := iptablesBinFor(ip)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	addCmd := exec.CommandContext(ctx, bin, "-A", "OUTPUT", "-d", ip, "-j", "DROP")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s -A falhou: %w (%s)", bin, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *linuxEnforcer) removeFirewallRule(ip string) error {
	bin, err := iptablesBinFor(ip)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-D", "OUTPUT", "-d", ip, "-j", "DROP")
	if out, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "does a matching rule exist") {
			return nil
		}
		return fmt.Errorf("%s -D falhou: %w (%s)", bin, err, strings.TrimSpace(string(out)))
	}
	return nil
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
		st := countIptablesRules(string(out))
		status.FirewallRules += st.FirewallRules
		status.DoHActive = status.DoHActive || st.DoHActive
	}

	// Erro apenas se nenhuma consulta teve sucesso; 0 regras de um firewall limpo é válido.
	if queried == 0 {
		return EnforcerStatus{}, fmt.Errorf("falha ao consultar as regras de firewall (iptables/ip6tables)")
	}

	return status, nil
}

func countIptablesRules(output string) EnforcerStatus {
	status := EnforcerStatus{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasSuffix(line, "-j DROP") {
			continue
		}
		status.FirewallRules++
		if strings.Contains(line, "--dport 853") || strings.Contains(line, "--dport 443") {
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
