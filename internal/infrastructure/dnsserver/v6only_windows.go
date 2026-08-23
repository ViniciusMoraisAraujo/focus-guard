//go:build windows

package dnsserver

import "syscall"

// setV6Only marks the socket fd with IPV6_V6ONLY=1. On Windows the raw socket
// handle is a syscall.Handle, unlike Unix's int fd.
func setV6Only(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1)
}
