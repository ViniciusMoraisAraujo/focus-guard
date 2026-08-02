//go:build windows

package enforcer

import (
	"bytes"
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

		cmd := execCommandContext(ctx, "net", "session")
		_, e.adminErr = cmd.CombinedOutput()
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

// blockDomainLocked applies a block while holding the lock. If the batched
// firewall application fails, the whole operation is rolled back (host entry
// plus every attempted rule) so the system never keeps a partial/zombie block.
func (e *windowsEnforcer) blockDomainLocked(domain string, ips []string) error {
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

	return e.syncLocked(activeBlocks)
}

// syncLocked applies a batch of active blocks with a single hosts-file rewrite
// (one read-modify-write for N domains instead of one write per domain) and a
// single batched netsh application. The caller must hold e.mu.
func (e *windowsEnforcer) syncLocked(activeBlocks map[string][]string) error {
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

	existing := e.existingFocusGuardRules()

	var missing []string
	for _, ips := range activeBlocks {
		for _, ip := range dedupeIPs(ips) {
			if existing[fmt.Sprintf("FocusGuard_%s", ip)] {
				continue
			}
			missing = append(missing, ip)
		}
	}

	if len(missing) > 0 {
		if err := e.runNetshAddBatch(missing); err != nil {
			return fmt.Errorf("enforcer: failed to sync firewall rules: %w", err)
		}
	}
	return nil
}

// existingFocusGuardRules consults the firewall once and returns the names of
// every FocusGuard rule already present, so Sync/block can add only the missing
// ones.
func (e *windowsEnforcer) existingFocusGuardRules() map[string]bool {
	names := make(map[string]bool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return names
	}
	return parseFocusGuardRuleNames(out)
}

// addFirewallRulesBatch applies one FocusGuard rule per IP with a single netsh
// process fed a script via stdin (1 exec for N IPs instead of one exec per
// IP). IPs already blocked are skipped.
func (e *windowsEnforcer) addFirewallRulesBatch(ips []string) error {
	allIPs := dedupeIPs(ips)
	if len(allIPs) == 0 {
		return nil
	}

	existing := e.existingFocusGuardRules()
	var missing []string
	for _, ip := range allIPs {
		if !existing[fmt.Sprintf("FocusGuard_%s", ip)] {
			missing = append(missing, ip)
		}
	}
	return e.runNetshAddBatch(missing)
}

// runNetshAddBatch feeds an add-rule script to a single netsh process via
// stdin and then verifies the rules actually exist, because netsh can exit 0
// even when an inner command of the script failed.
func (e *windowsEnforcer) runNetshAddBatch(ips []string) error {
	if len(ips) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "netsh")
	cmd.SetStdin(strings.NewReader(buildNetshAddScript(ips)))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netsh firewall add em lote falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// netsh pode mascarar falha interna com exit 0: confirma a aplicação real.
	after := e.existingFocusGuardRules()
	for _, ip := range ips {
		if !after[fmt.Sprintf("FocusGuard_%s", ip)] {
			return fmt.Errorf("netsh firewall add em lote incompleto: regra FocusGuard_%s ausente", ip)
		}
	}

	// Derruba conexões Keep-Alive ativas: o flush do cache DNS impede que
	// resoluções antigas mantenham fluxos para os IPs recém-bloqueados.
	e.flushDNS()
	return nil
}

// flushDNS clears the resolver cache (ipconfig /flushdns) so stale DNS
// resolutions don't keep active Keep-Alive connections pointing at freshly
// blocked IPs. Best-effort and void: a failure (ipconfig ausente, sem
// privilégio) is ignored — the netsh block rule already stops new flows.
func (e *windowsEnforcer) flushDNS() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execCommandContext(ctx, "ipconfig", "/flushdns")
	_, _ = cmd.CombinedOutput()
}

func parseFocusGuardRuleNames(output []byte) map[string]bool {
	names := make(map[string]bool)
	for _, line := range bytes.Split(output, []byte("\n")) {
		idx := bytes.Index(line, []byte("FocusGuard"))
		if idx < 0 {
			continue
		}
		name := string(bytes.TrimSpace(line[idx:]))
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

	lines = append(lines, hostEntryLines(domain)...)

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
	data, err := os.ReadFile(e.hostsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// The hosts file was deleted (e.g. by an admin) — recreate it with a
		// baseline so localhost keeps resolving, then continue reading.
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

func (e *windowsEnforcer) removeFirewallRule(ip string) error {
	ruleName := fmt.Sprintf("FocusGuard_%s", ip)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "netsh", deleteRuleArgs(ruleName)...)
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

	// Consulta nativa via netsh (sem PowerShell): todas as regras FocusGuard
	// são dir=out, então o filtro por direção reduz o dump a ser parseado.
	cmd := execCommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all", "dir=out")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return EnforcerStatus{}, fmt.Errorf("netsh show rules falhou: %w", err)
	}
	return countFocusGuardRules(out), nil
}

func countFocusGuardRules(output []byte) EnforcerStatus {
	status := EnforcerStatus{}

	for _, line := range bytes.Split(output, []byte("\n")) {
		if bytes.Contains(line, []byte("FocusGuard")) {
			status.FirewallRules++
			if bytes.Contains(line, []byte("FocusGuard_DoH_")) || bytes.Contains(line, []byte("FocusGuard_DoT_")) {
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
