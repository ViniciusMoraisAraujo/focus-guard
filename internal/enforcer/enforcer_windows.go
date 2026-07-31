//go:build windows

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

type windowsEnforcer struct {
	mu        sync.Mutex
	hostsPath string
}

func NewEnforcer() Enforcer {
	return &windowsEnforcer{
		hostsPath: `C:\Windows\System32\drivers\etc\hosts`,
	}
}

func checkAdmin() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "net", "session")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("checkAdmin: %w", err)
	}
	return nil
}

func (e *windowsEnforcer) BlockDomain(domain string, ips []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkAdmin(); err != nil {
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

func (e *windowsEnforcer) UnblockDomain(domain string, ips []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkAdmin(); err != nil {
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

func (e *windowsEnforcer) Sync(activeBlocks map[string][]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := checkAdmin(); err != nil {
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

func (e *windowsEnforcer) collectAllIPs(domain string, extra []string) []string {
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

func (e *windowsEnforcer) addHostEntry(domain string) error {
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

func (e *windowsEnforcer) writeHostsLines(lines []string) error {
	content := strings.Join(lines, "\r\n") + "\r\n"
	return os.WriteFile(e.hostsPath, []byte(content), 0644)
}

func (e *windowsEnforcer) addFirewallRule(ip string) error {
	ruleName := fmt.Sprintf("FocusGuard_%s", ip)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	checkCmd := exec.CommandContext(ctx, "netsh", showRuleArgs(ruleName)...)
	if err := checkCmd.Run(); err == nil {
		return nil
	}

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

	if err := checkAdmin(); err != nil {
		return EnforcerStatus{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all")
	out, err := cmd.Output()
	if err != nil {
		return EnforcerStatus{}, fmt.Errorf("netsh show rules falhou: %w", err)
	}

	return countFocusGuardRules(string(out)), nil
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

	if err := checkAdmin(); err != nil {
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

	if err := checkAdmin(); err != nil {
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
