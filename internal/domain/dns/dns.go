// Package dns implements the domain service and use cases for the DNS Sinkhole.
// It orchestrates the lifecycle of the local DNS server, local network adapter
// DNS configuration (Wi-Fi/Ethernet) and persistent settings.
package dns

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"focusguard/internal/infrastructure/dnsserver"
	"focusguard/internal/infrastructure/netdns"
)

// Controller drives the DNS sinkhole server lifecycle.
type Controller interface {
	Start() error
	Stop() error
	SetUpstream(upstream string) error
	Status() dnsserver.Status
}

// AdapterConfigurator manages the system network adapters DNS settings (Wi-Fi, Ethernet).
type AdapterConfigurator interface {
	SetLocalDNS() ([]string, error)
	RestoreDNS() ([]string, error)
	Status() netdns.AdapterStatus
}

// Persister is the persisted DNS setting surface (satisfied by *scheduler.Scheduler).
type Persister interface {
	SetDNSEnabled(enabled bool) error
	SetDNSUpstream(upstream string) error
	DNSEnabled() bool
}

// FirewallEnforcer handles firewall rules and DNS cache flushing.
type FirewallEnforcer interface {
	AllowDNSInbound() error
	BlockDoH() error
	FlushDNSCache() error
}

// NoInput is the empty input payload for parameterless actions.
type NoInput struct{}

// Status aggregates the live server controller state, network adapter status,
// and the persisted enabled flag.
type Status struct {
	Enabled            bool     `json:"enabled"`
	Listening          bool     `json:"listening"`
	Addr               string   `json:"addr,omitempty"`
	Upstream           string   `json:"upstream,omitempty"`
	Queries            uint64   `json:"queries,omitempty"`
	Blocked            uint64   `json:"blocked,omitempty"`
	BindError          string   `json:"bind_error,omitempty"`
	AdapterConnected   bool     `json:"adapter_connected"`
	ConfiguredAdapters []string `json:"configured_adapters,omitempty"`
}

type StartResult struct {
	Message string `json:"message"`
	Status  Status `json:"status"`
}

type StopResult struct {
	Message string `json:"message"`
	Status  Status `json:"status"`
}

type StatusResult struct {
	Status Status `json:"status"`
}

type SetUpstreamInput struct {
	Upstream string `json:"upstream"`
}

type SetUpstreamResult struct {
	Message string `json:"message"`
	Status  Status `json:"status"`
}

// MergeDNS copies the live DNS controller state and adapter status into the result Status.
func MergeDNS(st *Status, s dnsserver.Status, enabled bool, adapterSt *netdns.AdapterStatus) {
	st.Enabled = enabled
	st.Listening = s.Listening
	st.Addr = s.Addr
	st.Upstream = s.Upstream
	st.Queries = s.Queries
	st.Blocked = s.Blocked
	st.BindError = s.BindError
	if adapterSt != nil {
		st.AdapterConnected = adapterSt.Connected
		st.ConfiguredAdapters = adapterSt.ConfiguredAdapters
	}
}

// NormalizeUpstream validates a user-supplied upstream resolver and returns it
// in host:port form (a bare host gets the DNS default port 53).
func NormalizeUpstream(in string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", errors.New("informe um upstream (ex: 1.1.1.2, 9.9.9.9:53)")
	}
	host, port, err := net.SplitHostPort(in)
	if err != nil {
		// Sem porta explícita (ex: "1.1.1.2", "dns.google") → porta 53.
		if !strings.Contains(in, ":") {
			return net.JoinHostPort(in, "53"), nil
		}
		return "", fmt.Errorf("upstream inválido %q (use host ou host:porta)", in)
	}
	if host == "" || port == "" {
		return "", fmt.Errorf("upstream inválido %q (use host ou host:porta)", in)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("porta de upstream inválida %q", port)
	}
	return net.JoinHostPort(host, port), nil
}
