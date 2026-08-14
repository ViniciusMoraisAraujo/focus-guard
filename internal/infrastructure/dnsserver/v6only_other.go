//go:build !unix && !windows

package dnsserver

import "errors"

// setV6Only reports unsupported on platforms without the IPV6_V6ONLY socket
// option: the IPv6 wildcard bind then fails and the server falls back to the
// best-effort IPv4-only path instead of risking an EADDRINUSE between the two
// wildcard listeners.
func setV6Only(_ uintptr) error {
	return errors.New("IPV6_V6ONLY não suportado nesta plataforma")
}
