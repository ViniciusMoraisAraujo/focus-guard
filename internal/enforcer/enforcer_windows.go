//go:build windows

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

type windowsEnforcer struct {
	mu           sync.Mutex
	hostsPath    string
	adminOnce    sync.Once
	adminErr     error
	onHostsWrite func()

	statusMu       sync.Mutex
	lastStatus     EnforcerStatus
	lastStatusTime time.Time

	// rulesMu guards the cached FocusGuard rule inventory. A full
	// `netsh show rule name=all` dump is the daemon's hottest syscall
	// (existingFocusGuardRules runs on every Sync), so the inventory is cached
	// and only re-enumerated after a mutation or the TTL expiry.
	rulesMu       sync.Mutex
	lastRules     map[string]bool
	lastRulesTime time.Time
}

// rulesCacheTTL bounds how long the cached FocusGuard rule inventory is reused
// before the firewall is re-enumerated. Mutations invalidate the cache
// immediately, so the TTL only delays reflecting external changes.
const rulesCacheTTL = 10 * time.Second

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

	if err := e.blockDomainLocked(domain, ips); err != nil {
		return err
	}
	e.invalidateStatusCache()
	return nil
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

	if err := e.unblockDomainLocked(domain, ips); err != nil {
		return err
	}
	e.invalidateStatusCache()
	return nil
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

	if err := e.syncLocked(activeBlocks); err != nil {
		return err
	}
	e.invalidateStatusCache()
	return nil
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
			if existing[domainRuleName(ip)] {
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

	// Sweep de regras órfãs de domínio (restos de crash/sigkill antes de um
	// UnblockDomain): remove o que está no firewall mas não pertence a nenhum
	// bloco ativo. domainIPFromRuleName garante que regras DoH/DoT/allow e o
	// catch-all do BlockAll nunca sejam tocados por aqui.
	return e.sweepOrphanRules(existing, expectedBlockedIPs(activeBlocks))
}

// sweepOrphanRules deletes every domain block rule present in the firewall
// whose IP is not in the expected set, preserving DoH/DoT/allow/catch-all
// rules. Rules are removed by replaying their exact firewall name (legacy or
// normalized) via removeFirewallRule, so both naming conventions are swept.
func (e *windowsEnforcer) sweepOrphanRules(existing map[string]bool, expected map[string]bool) error {
	var firstErr error
	for name := range existing {
		ip, ok := domainIPFromRuleName(name)
		if !ok || expected[ip] {
			continue
		}
		if err := e.removeFirewallRule(ip); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// existingFocusGuardRules consults the firewall once and returns the names of
// every FocusGuard rule already present, so Sync/block can add only the missing
// ones. The result is cached (rulesCacheTTL) because the enumeration dominates
// the daemon's CPU when Sync runs on every tamper or hosts change; mutations
// invalidate the cache. The caller must hold e.mu.
func (e *windowsEnforcer) existingFocusGuardRules() map[string]bool {
	return e.focusGuardRules(false)
}

// refreshFocusGuardRules forces a fresh firewall enumeration and replaces the
// cache with the current rule inventory. Used by runNetshAddBatch's post-add
// verification, which must observe the rules it just created. The caller must
// hold e.mu.
func (e *windowsEnforcer) refreshFocusGuardRules() map[string]bool {
	return e.focusGuardRules(true)
}

func (e *windowsEnforcer) focusGuardRules(force bool) map[string]bool {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()

	if !force && e.lastRules != nil && time.Since(e.lastRulesTime) < rulesCacheTTL {
		return e.lastRules
	}

	names := make(map[string]bool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Não atualiza o cache em falha: um netsh indisponível deve ser
		// re-tentado na próxima chamada, não mascarado por 10s de vazio.
		return names
	}
	names = parseFocusGuardRuleNames(out)

	e.lastRules = names
	e.lastRulesTime = time.Now()
	return names
}

// invalidateRuleCache drops the cached rule inventory so the next enumeration
// re-queries the firewall. Called after every mutation so a subsequent Sync
// re-reads the real state instead of the stale snapshot.
func (e *windowsEnforcer) invalidateRuleCache() {
	e.rulesMu.Lock()
	e.lastRules = nil
	e.rulesMu.Unlock()
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
		if !existing[domainRuleName(ip)] {
			missing = append(missing, ip)
		}
	}
	return e.runNetshAddBatch(missing)
}

// runNetshAddBatch feeds an add-rule script to a single netsh process via
// stdin and then verifies the rules actually exist, because netsh can exit 0
// even when an inner command of the script failed.
func (e *windowsEnforcer) runNetshAddBatch(ips []string) error {
	ips = validateIPs(ips)
	if len(ips) == 0 {
		return nil
	}

	// Migração legada tolerante: remove as regras antigas (nome cru com ':'
	// para IPv6) que ainda existirem ANTES do batch. Um delete de regra
	// inexistente DENTRO do script faz o netsh sair com exit 1 em modo lote
	// (cada linha que falha propaga ao exit code do processo — confirmado por
	// "Nenhuma regra correspondente..." → exit=1), e o add de qualquer domínio
	// com IPv6 falhava o batch inteiro com "exit status 1". removeFirewallRule
	// tolera "No rules match" e ainda varre o nome normalizado.
	existing := e.existingFocusGuardRules()
	for _, ip := range ips {
		if strings.Contains(ip, ":") && existing[legacyDomainRuleName(ip)] {
			if err := e.removeFirewallRule(ip); err != nil {
				return fmt.Errorf("enforcer: migração da regra legada %s: %w", ip, err)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "netsh")
	cmd.SetStdin(strings.NewReader(buildNetshAddScript(ips)))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netsh firewall add em lote falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// netsh pode mascarar falha interna com exit 0: confirma a aplicação real.
	// A verificação força uma nova enumeração (refreshFocusGuardRules) e
	// atualiza o cache com o inventário pós-add — um cache hit aqui esconderia
	// as regras recém-criadas e invalidaria a checagem.
	after := e.refreshFocusGuardRules()
	for _, ip := range ips {
		if !after[domainRuleName(ip)] {
			return fmt.Errorf("netsh firewall add em lote incompleto: regra %s ausente", domainRuleName(ip))
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
	// Sweep do nome normalizado (atual) e do legado cru (IPv6 com ':'), para a
	// migração nunca deixar regras antigas para trás.
	names := []string{domainRuleName(ip), legacyDomainRuleName(ip)}
	for _, ruleName := range names {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		cmd := execCommandContext(ctx, "netsh", deleteRuleArgs(ruleName)...)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			outStr := string(out)
			if strings.Contains(outStr, "No rules match") || strings.Contains(outStr, "Nenhuma regra") {
				continue
			}
			return fmt.Errorf("netsh firewall delete falhou: %w (%s)", err, strings.TrimSpace(outStr))
		}
	}

	// A remoção mudou o firewall: invalida o cache de regras para o próximo
	// Sync re-enumerar e não tentar adicionar/limpar com base no estado antigo.
	e.invalidateRuleCache()
	return nil
}

// allBlockRuleName is the stable name of the catch-all outbound block rule.
func allBlockRuleName() string { return "FocusGuard_AllInternet" }

// allowRuleName builds the per-IP allow rule name, replacing ':' so IPv6
// addresses produce a valid rule name.
func allowRuleName(ip string) string {
	return "FocusGuard_Allow_" + strings.ReplaceAll(ip, ":", "_")
}

// domainRuleName builds the (normalized) name of a domain block rule for an
// IP, replacing ':' so IPv6 addresses produce a valid, consistent rule name —
// the same convention DoH and Allow rules already use.
func domainRuleName(ip string) string {
	return "FocusGuard_" + strings.ReplaceAll(ip, ":", "_")
}

// legacyDomainRuleName is the pre-normalization domain rule name (raw IP, with
// ':' for IPv6). Kept for migration: batch add deletes it before adding the
// normalized rule, and removeFirewallRule sweeps both names.
func legacyDomainRuleName(ip string) string {
	return "FocusGuard_" + ip
}

// domainIPFromRuleName extracts the blocked IP from a domain rule name in
// either the legacy (raw ':' IPv6) or normalized ('_' for ':') form, keyed by
// canonical net.IP.String(). The bool reports whether the name is a domain
// rule at all — DoH/DoT/allow/catch-all names (FocusGuard_DoH_*, DoT_*,
// Allow_*, AllInternet) never parse as an IP and are therefore never swept.
func domainIPFromRuleName(name string) (string, bool) {
	const prefix = "FocusGuard_"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	suffix := name[len(prefix):]
	for _, candidate := range []string{suffix, strings.ReplaceAll(suffix, "_", ":")} {
		if ip := net.ParseIP(candidate); ip != nil {
			return ip.String(), true
		}
	}
	return "", false
}

// BlockAll cuts off ALL outbound internet (panic mode) with a single catch-all
// netsh block rule. allowlistIPs, when non-empty, get per-IP ALLOW rules
// (deep-focus mode): Windows Firewall gives more specific rules (remoteip)
// precedence over the broad block, so only the allowed destinations remain
// reachable. Idempotent — previous all/allow rules are swept first.
func (e *windowsEnforcer) BlockAll(allowlistIPs []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
		return err
	}
	if err := e.unblockAllLocked(); err != nil {
		return err
	}

	for _, ip := range validateIPs(allowlistIPs) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		args := []string{"advfirewall", "firewall", "add", "rule",
			"name=" + allowRuleName(ip),
			"dir=out",
			"action=allow",
			"remoteip=" + ip,
		}
		cmd := execCommandContext(ctx, "netsh", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			cancel()
			return fmt.Errorf("netsh allow rule falhou para %s: %w (%s)", ip, err, strings.TrimSpace(string(out)))
		}
		cancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := execCommandContext(ctx, "netsh", []string{"advfirewall", "firewall", "add", "rule",
		"name=" + allBlockRuleName(),
		"dir=out",
		"action=block",
	}...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netsh block-all falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	e.invalidateStatusCache()
	e.invalidateRuleCache()
	return nil
}

// UnblockAll removes the catch-all block and every FocusGuard_Allow_* rule
// (panic/deep-focus exit), leaving domain/DoH rules untouched.
func (e *windowsEnforcer) UnblockAll() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkAdmin(); err != nil {
		return err
	}
	if err := e.unblockAllLocked(); err != nil {
		return err
	}
	e.invalidateStatusCache()
	e.invalidateRuleCache()
	return nil
}

// unblockAllLocked removes the catch-all block rule and sweeps every allow
// rule (FocusGuard_Allow_*) present in the firewall. The allow names are
// enumerated BEFORE any deletion — the name=all query is the slow part, and
// moving it before the deletes shrinks the window in which an interruption
// could leave allow rules orphaned. The caller must hold e.mu.
func (e *windowsEnforcer) unblockAllLocked() error {
	// Enumerar as allow rules primeiro (best-effort: se a consulta falhar,
	// ainda seguimos e removemos o catch-all abaixo).
	var allows map[string]bool
	listCtx, listCancel := context.WithTimeout(context.Background(), 5*time.Second)
	listCmd := execCommandContext(listCtx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all")
	out, err := listCmd.CombinedOutput()
	listCancel()
	if err == nil {
		allows = parseFocusGuardAllowRuleNames(out)
	}

	// Catch-all primeiro: se o processo for interrompido no meio da sequência,
	// a internet volta a funcionar e sobram apenas allow rules inertes (cruft,
	// limpo no próximo BlockAll) — jamais uma internet bloqueada por um
	// catch-all órfão.
	delCtx, delCancel := context.WithTimeout(context.Background(), 3*time.Second)
	cmd := execCommandContext(delCtx, "netsh", deleteRuleArgs(allBlockRuleName())...)
	if out, err := cmd.CombinedOutput(); err != nil {
		delCancel()
		outStr := string(out)
		if !strings.Contains(outStr, "No rules match") && !strings.Contains(outStr, "Nenhuma regra") {
			return fmt.Errorf("netsh delete do block-all falhou: %w (%s)", err, strings.TrimSpace(outStr))
		}
	}
	delCancel()

	// Deletes já enumerados, em sequência rápida.
	for name := range allows {
		dctx, dcancel := context.WithTimeout(context.Background(), 3*time.Second)
		dcmd := execCommandContext(dctx, "netsh", deleteRuleArgs(name)...)
		if dout, derr := dcmd.CombinedOutput(); derr != nil {
			dcancel()
			doutStr := string(dout)
			if !strings.Contains(doutStr, "No rules match") && !strings.Contains(doutStr, "Nenhuma regra") {
				return fmt.Errorf("netsh delete da allow rule %s falhou: %w (%s)", name, derr, strings.TrimSpace(doutStr))
			}
		}
		dcancel()
	}
	return nil
}

// parseFocusGuardAllowRuleNames extracts only the FocusGuard_Allow_* rule
// names from a netsh show output, so UnblockAll never touches domain/DoH rules.
func parseFocusGuardAllowRuleNames(output []byte) map[string]bool {
	names := make(map[string]bool)
	for _, line := range bytes.Split(output, []byte("\n")) {
		idx := bytes.Index(line, []byte("FocusGuard_Allow_"))
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

func (e *windowsEnforcer) Status() (EnforcerStatus, error) {
	// Cache com mutex próprio: Status nunca segura e.mu, então uma consulta
	// lenta de netsh (show rule name=all pode levar segundos) não bloqueia
	// BlockDomain/UnblockDomain/Sync. Mutações invalidam o cache.
	e.statusMu.Lock()
	defer e.statusMu.Unlock()

	if time.Since(e.lastStatusTime) < statusCacheTTL {
		return e.lastStatus, nil
	}

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

	e.lastStatus = countFocusGuardRules(out)
	e.lastStatusTime = time.Now()
	return e.lastStatus, nil
}

// invalidateStatusCache drops the cached Status snapshot so the next call
// re-queries the firewall. Called after every successful mutation so the tray
// and CLI reflect the change immediately instead of waiting out the TTL.
func (e *windowsEnforcer) invalidateStatusCache() {
	e.statusMu.Lock()
	e.lastStatusTime = time.Time{}
	e.statusMu.Unlock()
}

func countFocusGuardRules(output []byte) EnforcerStatus {
	status := EnforcerStatus{}

	for _, line := range bytes.Split(output, []byte("\n")) {
		if bytes.Contains(line, []byte("FocusGuard")) {
			status.FirewallRules++
			if bytes.Contains(line, []byte("FocusGuard_DoH_")) || bytes.Contains(line, []byte("FocusGuard_DoT_")) {
				status.DoHActive = true
			}
			if bytes.Contains(line, []byte(allBlockRuleName())) {
				status.AllBlocked = true
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

// doHRuleName returns the per-protocol DoH rule name for an IP. The protocol
// suffix lets TCP and UDP (QUIC/HTTP/3) rules coexist on the same resolver
// address.
func doHRuleName(ip, protocol string) string {
	return fmt.Sprintf("FocusGuard_DoH_%s_%s", strings.ReplaceAll(ip, ":", "_"), protocol)
}

// legacyDoHRuleName is the pre-QUIC rule name (single TCP-only rule per IP).
// Kept for migration: upgrading must delete these before adding new rules.
func legacyDoHRuleName(ip string) string {
	return fmt.Sprintf("FocusGuard_DoH_%s", strings.ReplaceAll(ip, ":", "_"))
}

func addDoHRuleArgs(ruleName, ip string, port int, protocol string) []string {
	return []string{
		"advfirewall", "firewall", "add", "rule",
		"name=" + ruleName,
		"dir=out",
		"action=block",
		"remoteip=" + ip,
		fmt.Sprintf("remoteport=%d", port),
		"protocol=" + protocol,
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
	e.invalidateStatusCache()
	e.invalidateRuleCache()
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
	e.invalidateStatusCache()
	e.invalidateRuleCache()
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
		// delete-before-add (migração): a regra antiga (tcp-only, sem sufixo
		// de protocolo) precisa ser substituída pelas novas regras
		// por-protocolo — o check de existência pularia a regra antiga.
		legacyName := legacyDoHRuleName(ip)
		delCtx, delCancel := context.WithTimeout(context.Background(), 3*time.Second)
		delCmd := exec.CommandContext(delCtx, "netsh", deleteRuleArgs(legacyName)...)
		out, err := delCmd.CombinedOutput()
		delCancel()
		if err != nil {
			outStr := string(out)
			if !strings.Contains(outStr, "No rules match") && !strings.Contains(outStr, "Nenhuma regra") {
				return fmt.Errorf("netsh DoH remove prévio falhou para %s (%s): %w (%s)", provider.Name, ip, err, strings.TrimSpace(outStr))
			}
		}

		for _, proto := range dohProtocols {
			ruleName := doHRuleName(ip, proto)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

			checkCmd := exec.CommandContext(ctx, "netsh", showRuleArgs(ruleName)...)
			if err := checkCmd.Run(); err == nil {
				cancel()
				continue
			}

			addCmd := exec.CommandContext(ctx, "netsh", addDoHRuleArgs(ruleName, ip, provider.Port, proto)...)
			if out, err := addCmd.CombinedOutput(); err != nil {
				cancel()
				return fmt.Errorf("netsh DoH block falhou para %s (%s/%s): %w (%s)", provider.Name, ip, proto, err, strings.TrimSpace(string(out)))
			}
			cancel()
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

	// Removes the current per-protocol rules plus the legacy pre-QUIC rule
	// name (tcp-only), so an upgrade that replaced a legacy rule never leaves
	// a stale one behind.
	for _, ip := range provider.IPs {
		names := []string{legacyDoHRuleName(ip)}
		for _, proto := range dohProtocols {
			names = append(names, doHRuleName(ip, proto))
		}
		for _, ruleName := range names {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

			cmd := exec.CommandContext(ctx, "netsh", deleteRuleArgs(ruleName)...)
			out, err := cmd.CombinedOutput()
			cancel()
			if err != nil {
				outStr := string(out)
				if strings.Contains(outStr, "No rules match") || strings.Contains(outStr, "Nenhuma regra") {
					continue
				}
				return fmt.Errorf("netsh DoH remove falhou para %s (%s): %w (%s)", provider.Name, ruleName, err, strings.TrimSpace(outStr))
			}
		}
	}
	return nil
}
