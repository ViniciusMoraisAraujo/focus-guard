//go:build unix

package dnsserver

import "syscall"

// setV6Only marks the socket fd with IPV6_V6ONLY=1, so the [::] wildcard
// listener does not capture IPv4-mapped addresses and can coexist with the
// separate 0.0.0.0 listener on the same port.
func setV6Only(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1)
}
