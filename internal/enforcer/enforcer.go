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
}

type lookupFunc func(host string) ([]net.IP, error)

const (
	HeaderMarker = "# FOCUS GUARD BLOCKS - DO NOT EDIT MANUALLY"
)

func resolveIPs(domain string, lookup lookupFunc) ([]string, error) {
	cleaned := strings.TrimPrefix(domain, "http://")
	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.Split(cleaned, "/")[0]

	ips, err := net.LookupIP(cleaned)
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

func ResolveIPs(domain string) ([]string, error) {
	return resolveIPs(domain, net.LookupIP)
}
