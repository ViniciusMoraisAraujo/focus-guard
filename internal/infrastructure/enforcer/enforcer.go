package enforcer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"
)

type Enforcer interface {
	BlockDomain(domain string, ips []string) error
	UnblockDomain(domain string, ips []string) error
	Sync(activeBlocks map[string][]string) error
	BlockDoH() error
	UnblockDoH() error
	// BlockAll cuts off ALL outbound internet (panic mode). allowlistIPs, when
	// non-empty, are the only destinations still reachable (deep-focus mode:
	// block everything except the allowed sites). Idempotent.
	BlockAll(allowlistIPs []string) error
	// UnblockAll removes the all-internet block and every allowlist exception.
	UnblockAll() error
	Status() (EnforcerStatus, error)
}

type EnforcerStatus struct {
	DoHActive     bool
	FirewallRules int
	AllBlocked    bool
}

// AllInternetDomain is the sentinel scheduler block key for the all-internet
// block (panic/deep-focus mode). It never touches the hosts file; the enforcer
// applies a catch-all firewall rule instead. Kept as a stable identifier used
// by the scheduler, server and CLI.
const AllInternetDomain = "*all-internet*"

const (
	HeaderMarker = "# FOCUS GUARD BLOCKS - DO NOT EDIT MANUALLY"
)

type DoHProvider struct {
	Name     string
	IPs      []string
	Port     int
	IsDoT    bool
	Protocol string
}

var DoHProviders = []DoHProvider{
	{Name: "Cloudflare", IPs: []string{"1.1.1.1", "1.0.0.1", "2606:4700:4700::1111", "2606:4700:4700::1001"}, Port: 443},
	{Name: "Google", IPs: []string{"8.8.8.8", "8.8.4.4", "2001:4860:4860::8888", "2001:4860:4860::8844"}, Port: 443},
	{Name: "Quad9", IPs: []string{"9.9.9.9", "149.112.112.112", "2620:fe::fe", "2620:fe::9"}, Port: 443},
	{Name: "OpenDNS", IPs: []string{"208.67.222.222", "208.67.220.220"}, Port: 443},
	{Name: "Comodo", IPs: []string{"8.26.56.26", "8.20.247.20"}, Port: 443},
	{Name: "DoT_TCP", IPs: nil, Port: 853, IsDoT: true, Protocol: "tcp"},
	{Name: "DoT_UDP", IPs: nil, Port: 853, IsDoT: true, Protocol: "udp"},
}

// dohProtocols is the transport coverage for port-443 DoH (DNS-over-HTTPS):
// TCP carries the classic HTTP/2 DoH, UDP carries DoH over QUIC/HTTP/3 (RFC
// 9250-style), which Firefox uses by default — blocking only TCP leaks.
var dohProtocols = []string{"tcp", "udp"}

type lookupFunc func(host string) ([]net.IP, error)
type lookupFuncCtx func(ctx context.Context, host string) ([]net.IP, error)

func ResolveIPs(domain string) ([]string, error) {
	return resolveIPs(domain, net.LookupIP)
}

// ResolveIPsContext resolves a domain honoring a caller-provided context, so
// DNS lookups can be cancelled or bounded by a timeout (net.LookupIP has no
// context variant). The DefaultResolver.LookupIP method is wrapped because it
// takes a network argument.
func ResolveIPsContext(ctx context.Context, domain string) ([]string, error) {
	return resolveIPsCtx(ctx, domain, func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	})
}

func resolveIPs(domain string, lookup lookupFunc) ([]string, error) {
	return resolveIPsCtx(context.Background(), domain, func(_ context.Context, host string) ([]net.IP, error) {
		return lookup(host)
	})
}

func resolveIPsCtx(ctx context.Context, domain string, lookup lookupFuncCtx) ([]string, error) {
	cleaned := strings.TrimPrefix(domain, "http://")
	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.Split(cleaned, "/")[0]

	ips, err := lookup(ctx, cleaned)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve IPs for domain %s: %v", domain, err)
	}

	var ipStrings []string

	seen := make(map[string]bool)
	for _, ip := range ips {
		ipStr := ip.String()
		if !seen[ipStr] {
			seen[ipStr] = true
			ipStrings = append(ipStrings, ipStr)
		}
	}

	return ipStrings, nil
}

// splitHostsLines splits hosts content into lines without per-line string
// allocations before they are needed. It operates on []byte and strips a
// trailing \r from CRLF endings (matching bufio.Scanner.ScanLines), and drops
// the empty element a final \n would otherwise produce.
func splitHostsLines(data []byte) []string {
	raw := bytes.Split(data, []byte("\n"))
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = bytes.TrimSuffix(line, []byte("\r"))
		lines = append(lines, string(line))
	}
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// hostEntryLines returns the four hosts entries used to block a domain (IPv4
// and IPv6 for the domain and its www subdomain), each tagged with the
// FOCUSGUARD marker for idempotent removal.
func hostEntryLines(domain string) []string {
	return []string{
		fmt.Sprintf("127.0.0.1 %s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("::1 %s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("127.0.0.1 www.%s # FOCUSGUARD: %s", domain, domain),
		fmt.Sprintf("::1 www.%s # FOCUSGUARD: %s", domain, domain),
	}
}

// sanitizeDomain cleans a user-supplied domain before it is written to the
// hosts file or used as a marker: it strips the scheme and path, removes CR/LF
// and spaces (hosts-file injection vectors), collapses a leading "www." prefix
// (so www.site.com and www.www.site.com both normalize to site.com, avoiding
// redundant www.www.site.com entries) and rejects characters that cannot appear
// in a hostname.
func sanitizeDomain(domain string) (string, error) {
	// Hostnames and URL schemes are case-insensitive: lowercase first so both
	// "HTTP://example.com" and "HtTpS://example.com/path" are stripped the
	// same way as their lowercase equivalents.
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimPrefix(d, "https://")
	if i := strings.Index(d, "/"); i >= 0 {
		d = d[:i]
	}

	// Remove newlines, spaces and tabs — a domain containing them is an
	// injection attempt against /etc/hosts (a CRLF would start a second line).
	d = strings.NewReplacer("\r", "", "\n", "", " ", "", "\t", "").Replace(d)

	// Collapse leading www. prefixes so blocking www.site.com never produces
	// the redundant www.www.site.com entry (addHostEntry appends www. itself).
	for strings.HasPrefix(d, "www.") {
		d = d[len("www."):]
	}

	if d == "" {
		return "", errors.New("enforcer: domínio vazio após sanitização")
	}

	for _, r := range d {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
		if !valid {
			return "", fmt.Errorf("enforcer: domínio inválido %q", domain)
		}
	}

	return d, nil
}

// dedupeIPs removes empty strings and duplicates, preserving order.
func dedupeIPs(ips []string) []string {
	seen := make(map[string]struct{}, len(ips))
	var result []string
	for _, ip := range ips {
		if ip == "" {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		result = append(result, ip)
	}
	return result
}

// validateIPs returns the canonical form of every valid IP in ips, dropping
// empty and invalid entries and de-duplicating. A non-IP value (e.g. a FQDN)
// fed to netsh remoteip is rejected by Windows Firewall with "An IP address or
// address keyword specified is not valid" and exits the batch with status 1 —
// this is the last line of defense before any netsh invocation.
func validateIPs(ips []string) []string {
	seen := make(map[string]struct{}, len(ips))
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		p := net.ParseIP(strings.TrimSpace(ip))
		if p == nil {
			continue
		}
		canon := p.String()
		if _, ok := seen[canon]; ok {
			continue
		}
		seen[canon] = struct{}{}
		out = append(out, canon)
	}
	return out
}

// groupIPsByFamily splits IPs into IPv4 and IPv6 lists so each family can be
// handled by the right firewall binary in a single batched invocation.
func groupIPsByFamily(ips []string) (v4, v6 []string) {
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		if parsed.To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	return v4, v6
}

// statusCacheTTL bounds how long Status() may return a cached firewall
// snapshot before re-querying. The tray polls the daemon status every ~10s; a
// TTL above that skips most netsh/iptables invocations. Mutations invalidate
// the cache immediately, so the TTL only delays reflecting external changes.
const statusCacheTTL = 15 * time.Second

// AllBlockMarker is the iptables comment tagging the catch-all REJECT rule
// that implements the all-internet block. Removal sweeps by this marker.
const AllBlockMarker = "FOCUSGUARD_ALL"

// AllowMarker tags the per-IP ACCEPT exceptions of the deep-focus allowlist.
const AllowMarker = "FOCUSGUARD_ALLOW"

// icmpPortUnreachableType returns the --reject-with type of the
// protocol-agnostic REJECT for the address family of mask: ICMPv4
// icmp-port-unreachable on IPv4 (iptables) and ICMPv6 icmp6-port-unreachable
// on IPv6 (ip6tables). O nome ICMPv4 é rejeitado pelo backend nft do
// ip6tables ("unknown reject type") — usar o tipo v4 no v6 quebrava o
// bloqueio de domínios com IPs IPv6 no Ubuntu moderno (achado da Etapa 6
// real).
func icmpPortUnreachableType(mask string) string {
	if mask == "/128" {
		return "icmp6-port-unreachable"
	}
	return "icmp-port-unreachable"
}

// buildBlockAllScript renders the iptables-restore payload for BlockAll: one
// ACCEPT per allowlisted IP first (so exceptions are evaluated before the
// catch-all) and then a catch-all REJECT for the family mask. The catch-all is
// split in two: a TCP rule rejecting with tcp-reset (fast RST for TCP) and a
// protocol-agnostic rule rejecting everything else (UDP/QUIC included) with
// icmp-port-unreachable. tcp-reset is only valid on rules that match TCP, so a
// bare catch-all would be rejected by iptables. The markers let UnblockAll
// sweep both kinds of rule by comment.
func buildBlockAllScript(allowlistIPs []string, mask string) string {
	var b strings.Builder
	b.WriteString("*filter\n")
	for _, ip := range allowlistIPs {
		b.WriteString("-A OUTPUT -d ")
		b.WriteString(ip)
		b.WriteString(mask)
		b.WriteString(" -j ACCEPT -m comment --comment \"")
		b.WriteString(AllowMarker)
		b.WriteString("\"\n")
	}
	b.WriteString("-A OUTPUT -p tcp -j REJECT --reject-with tcp-reset -m comment --comment \"")
	b.WriteString(AllBlockMarker)
	b.WriteString("\"\n")
	b.WriteString("-A OUTPUT -j REJECT --reject-with ")
	b.WriteString(icmpPortUnreachableType(mask))
	b.WriteString(" -m comment --comment \"")
	b.WriteString(AllBlockMarker)
	b.WriteString("\"\n")
	b.WriteString("COMMIT\n")
	return b.String()
}

// buildRestoreScript renders the stdin payload for iptables-restore/ip6tables
// --noflush: a *filter header, two -A OUTPUT REJECT lines per IP (with the
// family mask, matching the iptables-save format) and a COMMIT footer.
//
// Each IP gets a TCP rule rejecting with tcp-reset and a protocol-agnostic
// rule rejecting everything else with icmp-port-unreachable. tcp-reset is only
// accepted by iptables on rules that match TCP explicitly, and it only covers
// TCP — the companion rule is what blocks UDP/QUIC (HTTP/3), otherwise a
// browser using DoH + QUIC would keep reaching the site over UDP.
//
// REJECT --reject-with tcp-reset is used instead of DROP so blocked sites
// fail fast with an RST instead of hanging on client-side timeouts — and the
// RST sent on the next packet of an existing flow tears down Keep-Alive
// connections at the packet level (ss -K does the same in the kernel).
//
// Chain policy lines (e.g. ":OUTPUT ACCEPT [0:0]") are intentionally NOT
// emitted: under --noflush they would reset the chain policy/counters, which
// could clobber a hardened OUTPUT DROP policy. Omitting them appends rules to
// the existing built-in chain without touching its policy.
func buildRestoreScript(ips []string, mask string) string {
	var b strings.Builder
	b.WriteString("*filter\n")
	for _, ip := range ips {
		b.WriteString("-A OUTPUT -d ")
		b.WriteString(ip)
		b.WriteString(mask)
		b.WriteString(" -p tcp -j REJECT --reject-with tcp-reset\n")
		b.WriteString("-A OUTPUT -d ")
		b.WriteString(ip)
		b.WriteString(mask)
		b.WriteString(" -j REJECT --reject-with ")
		b.WriteString(icmpPortUnreachableType(mask))
		b.WriteString("\n")
	}
	b.WriteString("COMMIT\n")
	return b.String()
}

// buildNetshAddScript renders the script fed to a single netsh process via
// stdin: one full-context add rule line per IP (netsh scripting needs the
// advfirewall context on every line) followed by exit. Rule names normalize
// ':' to '_' so IPv6 addresses produce valid names (the same
// convention DoH and Allow rules use). Input IPs are validated and
// canonicalized first: a non-IP value would make netsh reject the remoteip
// and exit the whole batch with status 1. The legacy raw-':' rule migration
// is handled by runNetshAddBatch with a failure-tolerant delete BEFORE this
// script runs — an inline delete of a non-existent rule aborts the batch the
// same way (netsh propagates a failing line to the process exit code).
func buildNetshAddScript(ips []string) string {
	var b strings.Builder
	for _, ip := range validateIPs(ips) {
		b.WriteString("advfirewall firewall add rule name=FocusGuard_")
		b.WriteString(strings.ReplaceAll(ip, ":", "_"))
		b.WriteString(" dir=out action=block remoteip=")
		b.WriteString(ip)
		b.WriteString("\r\n")
	}
	b.WriteString("exit\r\n")
	return b.String()
}

// expectedBlockedIPs returns the set of IPs that must be blocked per
// activeBlocks (the union of every domain's resolved IPs), keyed by the
// canonical net.IP.String() form so IPv4/IPv6 textual variants compare equal.
// Sync uses it both to compute missing rules and to sweep orphan rules.
func expectedBlockedIPs(activeBlocks map[string][]string) map[string]bool {
	expected := make(map[string]bool)
	for _, ips := range activeBlocks {
		for _, ip := range dedupeIPs(ips) {
			parsed := net.ParseIP(ip)
			if parsed == nil {
				continue
			}
			expected[parsed.String()] = true
		}
	}
	return expected
}

// execCommandContext is an indirection for firewall command execution so tests
// can stub iptables/netsh without root/admin privileges.
var execCommandContext = func(ctx context.Context, name string, args ...string) cmdRunner {
	return &execCmd{Cmd: exec.CommandContext(ctx, name, args...)}
}

// cmdRunner is the subset of *exec.Cmd used by the enforcer. SetStdin feeds a
// batch script (iptables-restore / netsh) to the process.
type cmdRunner interface {
	SetStdin(r io.Reader)
	CombinedOutput() ([]byte, error)
}

// execCmd adapts *exec.Cmd to the cmdRunner interface.
type execCmd struct {
	*exec.Cmd
}

func (c *execCmd) SetStdin(r io.Reader) {
	c.Cmd.Stdin = r
}
