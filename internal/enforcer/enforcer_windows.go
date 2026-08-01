//go:build windows

package enforcer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type windowsEnforcer struct {
	mu           sync.Mutex
	hostsPath    string
	adminOnce    sync.Once
	adminErr     error
	onHostsWrite func()
}

func (e *windowsEnforcer) SetOnHostsWrite(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onHostsWrite = fn
}

func NewEnforcer() Enforcer {
	return &windowsEnforcer{
		hostsPath: `C:\Windows\System32\drivers\etc\hosts`,
	}
}

func (e *windowsEnforcer) checkAdmin() error {
	e.adminOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "net", "session")
		e.adminErr = cmd.Run()
	})
	if e.adminErr != nil {
		return fmt.Errorf("checkAdmin: %w", e.adminErr)
	}
	return nil
}

func (e *windowsEnforcer) BlockDomain(domain string, ips []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
		return err
	}

	return e.blockDomainLocked(domain, ips)
}

// blockDomainLocked applies a block while holding the lock. If any firewall
// rule fails, the whole operation is rolled back (host entry plus the rules
// already applied) so the system never keeps a partial/zombie block.
func (e *windowsEnforcer) blockDomainLocked(domain string, ips []string) error {
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

func (e *windowsEnforcer) UnblockDomain(domain string, ips []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
		return err
	}

	return e.unblockDomainLocked(domain, ips)
}

func (e *windowsEnforcer) unblockDomainLocked(domain string, ips []string) error {
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

func (e *windowsEnforcer) Sync(activeBlocks map[string][]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
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

	existing := e.existingFocusGuardRules()

	for domain, ips := range activeBlocks {
		if err := e.addHostEntry(domain); err != nil {
			return fmt.Errorf("enforcer: failed to sync host entry for %s: %w", domain, err)
		}

		for _, ip := range dedupeIPs(ips) {
			if existing[fmt.Sprintf("FocusGuard_%s", ip)] {
				continue
			}
			if err := e.addFirewallRuleUnchecked(ip); err != nil {
				return fmt.Errorf("enforcer: failed to sync firewall rule for %s: %w", ip, err)
			}
		}
	}
	return nil
}

// existingFocusGuardRules consults the firewall once and returns the names of
// every FocusGuard rule already present, so Sync can add only the missing ones.
func (e *windowsEnforcer) existingFocusGuardRules() map[string]bool {
	names := make(map[string]bool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all")
	out, err := cmd.Output()
	if err != nil {
		return names
	}
	return parseFocusGuardRuleNames(string(out))
}

func parseFocusGuardRuleNames(output string) map[string]bool {
	names := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, "FocusGuard")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[idx:])
		if name != "" {
			names[name] = true
		}
	}
	return names
}

func (e *windowsEnforcer) addHostEntry(domain string) error {
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

	lines = append(lines,
		fmt.Sprintf("127.0.0.1 %s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("::1 %s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("127.0.0.1 www.%s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("::1 www.%s # FOCUSGUARD: %s", domain, domain),
	)

	return e.writeHostsLines(lines)
}

func (e *windowsEnforcer) removeHostEntry(domain string) error {
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

func (e *windowsEnforcer) readHostsLines() ([]string, error) {
	file, err := os.Open(e.hostsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// The hosts file was deleted (e.g. by an admin) — recreate it with a
		// baseline so localhost keeps resolving, then continue reading.
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
		"# Copyright (c) 1993-2009 Microsoft Corp.",
		"#",
		"# This is a sample HOSTS file used by Microsoft TCP/IP for Windows.",
		"#",
		"127.0.0.1       localhost",
		"::1             localhost",
	}
}

func (e *windowsEnforcer) writeHostsLines(lines []string) error {
	content := strings.Join(lines, "\r\n") + "\r\n"
	if err := os.WriteFile(e.hostsPath, []byte(content), 0644); err != nil {
		return err
	}
	if e.onHostsWrite != nil {
		e.onHostsWrite()
	}
	return nil
}

func (e *windowsEnforcer) addFirewallRule(ip string) error {
	ruleName := fmt.Sprintf("FocusGuard_%s", ip)
	if e.ruleExists(ruleName) {
		return nil
	}
	return e.addFirewallRuleUnchecked(ip)
}

func (e *windowsEnforcer) ruleExists(ruleName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "netsh", showRuleArgs(ruleName)...).Run() == nil
}

func (e *windowsEnforcer) addFirewallRuleUnchecked(ip string) error {
	ruleName := fmt.Sprintf("FocusGuard_%s", ip)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	addCmd := exec.CommandContext(ctx, "netsh", "advfirewall", "firewall", "add", "rule",
		"name="+ruleName,
		"dir=out",
		"action=block",
		"remoteip="+ip)

	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netsh firewall add falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}

func (e *windowsEnforcer) removeFirewallRule(ip string) error {
	ruleName := fmt.Sprintf("FocusGuard_%s", ip)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "netsh", deleteRuleArgs(ruleName)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "No rules match") || strings.Contains(outStr, "Nenhuma regra") {
			return nil
		}
		return fmt.Errorf("netsh firewall delete falhou: %w (%s)", err, strings.TrimSpace(outStr))
	}

	return nil
}

func (e *windowsEnforcer) Status() (EnforcerStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
		return EnforcerStatus{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// G3: consulta focada — apenas as regras FocusGuard*, em vez de despejar
	// todas as regras do firewall e filtrar o dump inteiro em Go.
	ps := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"Get-NetFirewallRule -Name 'FocusGuard*' | Select-Object -ExpandProperty Name")
	if out, err := ps.Output(); err == nil {
		return countFocusGuardNames(string(out)), nil
	}

	// Fallback para ambientes sem PowerShell: dump completo via netsh.
	fallback := exec.CommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all")
	out, err := fallback.Output()
	if err != nil {
		return EnforcerStatus{}, fmt.Errorf("netsh show rules falhou: %w", err)
	}
	return countFocusGuardRules(string(out)), nil
}

// countFocusGuardNames conta as regras FocusGuard a partir da saída do
// PowerShell (um nome por linha).
func countFocusGuardNames(output string) EnforcerStatus {
	status := EnforcerStatus{}
	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasPrefix(name, "FocusGuard_") {
			continue
		}
		status.FirewallRules++
		if strings.Contains(name, "FocusGuard_DoH_") || strings.Contains(name, "FocusGuard_DoT_") {
			status.DoHActive = true
		}
	}
	return status
}

func countFocusGuardRules(output string) EnforcerStatus {
	status := EnforcerStatus{}

	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "FocusGuard") {
			status.FirewallRules++
			if strings.Contains(line, "FocusGuard_DoH_") || strings.Contains(line, "FocusGuard_DoT_") {
				status.DoHActive = true
			}
		}
	}

	return status
}

func showRuleArgs(ruleName string) []string {
	return []string{"advfirewall", "firewall", "show", "rule", "name=" + ruleName}
}

// Nome estável — não alterar: a migração (delete-before-add) depende dele para
// substituir as regras antigas ineficazes (localport).
func doTRuleName(provider DoHProvider) string {
	return fmt.Sprintf("FocusGuard_%s", provider.Name)
}

func addDoTRuleArgs(ruleName, protocol string, port int) []string {
	return []string{
		"advfirewall", "firewall", "add", "rule",
		"name=" + ruleName,
		"dir=out",
		"action=block",
		"protocol=" + protocol,
		fmt.Sprintf("remoteport=%d", port),
	}
}

func addDoHRuleArgs(ruleName, ip string, port int) []string {
	return []string{
		"advfirewall", "firewall", "add", "rule",
		"name=" + ruleName,
		"dir=out",
		"action=block",
		"remoteip=" + ip,
		fmt.Sprintf("remoteport=%d", port),
		"protocol=tcp",
	}
}

func deleteRuleArgs(ruleName string) []string {
	return []string{"advfirewall", "firewall", "delete", "rule", "name=" + ruleName}
}

func (e *windowsEnforcer) BlockDoH() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
		return err
	}

	for _, provider := range DoHProviders {
		if err := e.addDoHRule(provider); err != nil {
			return err
		}
	}
	return nil
}

func (e *windowsEnforcer) UnblockDoH() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
		return err
	}

	for _, provider := range DoHProviders {
		if err := e.removeDoHRule(provider); err != nil {
			return err
		}
	}
	return nil
}

func (e *windowsEnforcer) addDoHRule(provider DoHProvider) error {
	if provider.IsDoT {
		// delete-before-add: a regra antiga (localport, ineficaz) precisa ser substituída
		// pela correta (remoteport) — o check de existência pularia a regra antiga.
		ruleName := doTRuleName(provider)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		delCmd := exec.CommandContext(ctx, "netsh", deleteRuleArgs(ruleName)...)
		if out, err := delCmd.CombinedOutput(); err != nil {
			outStr := string(out)
			if !strings.Contains(outStr, "No rules match") && !strings.Contains(outStr, "Nenhuma regra") {
				return fmt.Errorf("netsh DoT remove prévio falhou para %s: %w (%s)", provider.Name, err, strings.TrimSpace(outStr))
			}
		}

		addCmd := exec.CommandContext(ctx, "netsh", addDoTRuleArgs(ruleName, provider.Protocol, provider.Port)...)
		if out, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("netsh DoT block falhou para %s: %w (%s)", provider.Name, err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	for _, ip := range provider.IPs {
		ruleName := fmt.Sprintf("FocusGuard_DoH_%s", strings.ReplaceAll(ip, ":", "_"))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		checkCmd := exec.CommandContext(ctx, "netsh", showRuleArgs(ruleName)...)
		if err := checkCmd.Run(); err == nil {
			continue
		}

		addCmd := exec.CommandContext(ctx, "netsh", addDoHRuleArgs(ruleName, ip, provider.Port)...)
		if out, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("netsh DoH block falhou para %s (%s): %w (%s)", provider.Name, ip, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (e *windowsEnforcer) removeDoHRule(provider DoHProvider) error {
	if provider.IsDoT {
		ruleName := doTRuleName(provider)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "netsh", deleteRuleArgs(ruleName)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			outStr := string(out)
			if strings.Contains(outStr, "No rules match") || strings.Contains(outStr, "Nenhuma regra") {
				return nil
			}
			return fmt.Errorf("netsh DoT remove falhou para %s: %w (%s)", provider.Name, err, strings.TrimSpace(outStr))
		}
		return nil
	}

	for _, ip := range provider.IPs {
		ruleName := fmt.Sprintf("FocusGuard_DoH_%s", strings.ReplaceAll(ip, ":", "_"))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "netsh", deleteRuleArgs(ruleName)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			outStr := string(out)
			if strings.Contains(outStr, "No rules match") || strings.Contains(outStr, "Nenhuma regra") {
				continue
			}
			return fmt.Errorf("netsh DoH remove falhou para %s (%s): %w (%s)", provider.Name, ip, err, strings.TrimSpace(outStr))
		}
	}
	return nil
}
