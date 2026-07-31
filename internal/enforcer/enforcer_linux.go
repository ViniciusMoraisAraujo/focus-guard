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
	mu        sync.Mutex
	hostsPath string
}

func NewEnforcer() Enforcer {
	return &linuxEnforcer{
		hostsPath: "/etc/hosts",
	}
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

	if err := e.addHostEntry(domain); err != nil {
		return fmt.Errorf("enforcer: failed to add host entry: %w", err)
	}

	allIPs := e.collectAllIPs(domain, ips)

	var firstErr error
	for _, ip := range allIPs {
		if err := e.addFirewallRule(ip); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("enforcer: failed to add firewall rule for %s: %w", ip, err)
			}
		}
	}

	return firstErr
}

func (e *linuxEnforcer) UnblockDomain(domain string, ips []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkRoot(); err != nil {
		return err
	}

	if err := e.removeHostEntry(domain); err != nil {
		return fmt.Errorf("enforcer: failed to remove host entry: %w", err)
	}

	allIPs := e.collectAllIPs(domain, ips)

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

	for domain, ips := range activeBlocks {
		if err := e.addHostEntry(domain); err != nil {
			return fmt.Errorf("enforcer: failed to sync host entry for %s: %w", domain, err)
		}

		allIPs := e.collectAllIPs(domain, ips)
		for _, ip := range allIPs {
			if err := e.addFirewallRule(ip); err != nil {
				return fmt.Errorf("enforcer: failed to sync firewall rule for %s: %w", ip, err)
			}
		}
	}

	return nil
}

func (e *linuxEnforcer) collectAllIPs(domain string, extra []string) []string {
	seen := make(map[string]struct{})
	var result []string

	add := func(ip string) {
		if ip == "" {
			return
		}
		if _, ok := seen[ip]; ok {
			return
		}
		seen[ip] = struct{}{}
		result = append(result, ip)
	}

	for _, ip := range extra {
		add(ip)
	}

	for _, host := range []string{domain, "www." + domain} {
		resolved, err := net.LookupIP(host)
		if err != nil {
			continue
		}
		for _, ip := range resolved {
			add(ip.String())
		}
	}

	return result
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
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

func (e *linuxEnforcer) writeHostsLines(lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(e.hostsPath, []byte(content), 0644)
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

// Status consulta o firewall para reportar se a proteção DoH/DoT está ativa
// e quantas regras do FocusGuard existem na chain OUTPUT.
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

	// Só reportar erro se nenhuma consulta teve sucesso; uma consulta bem-sucedida
	// com 0 regras (firewall limpo) é um resultado válido.
	if queried == 0 {
		return EnforcerStatus{}, fmt.Errorf("falha ao consultar as regras de firewall (iptables/ip6tables)")
	}

	return status, nil
}

// countIptablesRules analisa a saída de "iptables -S OUTPUT" (ou ip6tables) e
// conta regras DROP do FocusGuard e se há bloqueio de porta DoH/DoT.
// Nota: conta TODAS as regras "-j DROP" da chain, incluindo quaisquer outras
// regras de terceiros — heurística aceitável para exibição de status.
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

// ─── DoH / DoT Blocking ──────────────────────────────────────────────────────

// doTRuleArgs monta os argumentos para bloquear a porta remota 853 (DoT),
// aplicável a iptables (IPv4) e ip6tables (IPv6). jump é "-C", "-A" ou "-D".
func doTRuleArgs(jump, protocol string, port int) []string {
	return []string{jump, "OUTPUT", "-p", protocol, "--dport", fmt.Sprintf("%d", port), "-j", "DROP"}
}

// availableDoTBins retorna os binários de firewall disponíveis para bloquear
// a porta DoT (IPv4 e IPv6). Sistemas sem ip6tables seguem apenas com iptables.
func availableDoTBins() []string {
	var bins []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		if _, err := exec.LookPath(bin); err == nil {
			bins = append(bins, bin)
		}
	}
	return bins
}

// doHRuleArgs monta os argumentos para bloquear IP+porta 443 (DoH).
// jump é "-C", "-A" ou "-D".
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
		// Bloqueio global da porta remota (DoT) — IPv4 (iptables) e IPv6 (ip6tables)
		for _, bin := range availableDoTBins() {
			checkCmd := exec.CommandContext(ctx, bin, doTRuleArgs("-C", provider.Protocol, provider.Port)...)
			if err := checkCmd.Run(); err == nil {
				continue // já existe
			}

			addCmd := exec.CommandContext(ctx, bin, doTRuleArgs("-A", provider.Protocol, provider.Port)...)
			if out, err := addCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s DoT block falhou para %s: %w (%s)",
					bin, provider.Name, err, strings.TrimSpace(string(out)))
			}
		}
		return nil
	}

	// Bloqueio de IPs específicos na porta 443 (DoH)
	for _, ip := range provider.IPs {
		bin, err := iptablesBinFor(ip)
		if err != nil {
			continue
		}

		// Check if rule exists
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
		// Remove o bloqueio da porta remota (DoT) — IPv4 (iptables) e IPv6 (ip6tables)
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
