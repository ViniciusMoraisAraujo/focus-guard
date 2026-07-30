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
}

const (
	HeaderMarker = "# FOCUS GUARD BLOCKS - DO NOT EDIT MANUALLY"
)

// DoHProvider representa um provedor de DNS criptografado conhecido.
type DoHProvider struct {
	Name     string
	IPs      []string
	Port     int
	IsDoT    bool   // true = bloquear a porta globalmente (sem IP específico)
	Protocol string // "tcp" ou "udp" para DoT; vazio para DoH (usa remoteip:port)
}

// DoHProviders é a lista de provedores DoH/DoT conhecidos que serão bloqueados.
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
