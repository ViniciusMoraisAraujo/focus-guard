package enforcer

import (
	"fmt"
	"net"
	"strings"
)

type Enforcer interface {
	BlockDomain(domain string, ips []string) error
	UnblockDomain(domain string, ips []string) error
	Sync(activeBlocks map[string][]string) error
	BlockDoH() error
	UnblockDoH() error
	Status() (EnforcerStatus, error)
}

type EnforcerStatus struct {
	DoHActive     bool
	FirewallRules int
}

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

type lookupFunc func(host string) ([]net.IP, error)

func ResolveIPs(domain string) ([]string, error) {
	return resolveIPs(domain, net.LookupIP)
}

func resolveIPs(domain string, lookup lookupFunc) ([]string, error) {
	cleaned := strings.TrimPrefix(domain, "http://")
	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.Split(cleaned, "/")[0]

	ips, err := lookup(cleaned)
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

// applyBlockRules applies addRule to each IP, tracking the ones already applied.
// On the first failure it best-effort removes every rule applied so far and
// returns the error, so a partially failed block never leaves zombie firewall
// rules behind.
func applyBlockRules(ips []string, addRule, removeRule func(string) error) error {
	added := make([]string, 0, len(ips))
	for _, ip := range ips {
		if err := addRule(ip); err != nil {
			for _, done := range added {
				_ = removeRule(done)
			}
			return err
		}
		added = append(added, ip)
	}
	return nil
}
