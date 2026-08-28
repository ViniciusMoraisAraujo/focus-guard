//go:build linux

package netdns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// execCommandContext is mockable for tests.
var execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

type linuxConfigurator struct {
	mu         sync.RWMutex
	configured []string
	connected  bool
	lastError  string
}

func newPlatformConfigurator() Configurator {
	return &linuxConfigurator{}
}

func (c *linuxConfigurator) SetLocalDNS() ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ifaces, err := c.detectActiveInterfaces()
	if err != nil {
		c.lastError = err.Error()
		return nil, fmt.Errorf("netdns: falha ao detectar interfaces no Linux: %w", err)
	}

	if len(ifaces) == 0 {
		c.lastError = "nenhuma interface ativa encontrada"
		return nil, errors.New("netdns: nenhuma interface ativa encontrada")
	}

	var successful []string
	var errs []error

	for _, iface := range ifaces {
		if err := c.setInterfaceDNS(iface, "127.0.0.1", "::1"); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", iface, err))
		} else {
			successful = append(successful, iface)
		}
	}

	if len(successful) == 0 {
		c.connected = false
		c.configured = nil
		c.lastError = errors.Join(errs...).Error()
		return nil, fmt.Errorf("netdns: falha ao configurar DNS no Linux: %w", errors.Join(errs...))
	}

	c.configured = successful
	c.connected = true
	c.lastError = ""
	return successful, nil
}

func (c *linuxConfigurator) RestoreDNS() ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targetIfaces := c.configured
	if len(targetIfaces) == 0 {
		if active, err := c.detectActiveInterfaces(); err == nil {
			targetIfaces = active
		}
	}

	var restored []string
	var errs []error

	for _, iface := range targetIfaces {
		if err := c.restoreInterfaceDNS(iface); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", iface, err))
		} else {
			restored = append(restored, iface)
		}
	}

	c.configured = nil
	c.connected = false
	if len(errs) > 0 {
		c.lastError = errors.Join(errs...).Error()
		return restored, errors.Join(errs...)
	}

	c.lastError = ""
	return restored, nil
}

func (c *linuxConfigurator) Status() AdapterStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return AdapterStatus{
		Connected:          c.connected,
		ConfiguredAdapters: append([]string(nil), c.configured...),
		Detail:             c.lastError,
	}
}

func (c *linuxConfigurator) detectActiveInterfaces() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "resolvectl", "status")
	out, err := cmd.CombinedOutput()
	if err == nil {
		if ifaces := parseResolvectlStatus(out); len(ifaces) > 0 {
			return ifaces, nil
		}
	}

	// Fallback para ip -o link
	cmdIP := execCommandContext(ctx, "ip", "-o", "link", "show", "up")
	outIP, errIP := cmdIP.CombinedOutput()
	if errIP != nil {
		return nil, fmt.Errorf("detectar interfaces falhou: %w", errIP)
	}

	return parseIPLinkShow(outIP), nil
}

func parseResolvectlStatus(output []byte) []string {
	var ifaces []string
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		l := strings.TrimSpace(string(line))
		if strings.HasPrefix(l, "Link ") {
			// Exemplo: Link 2 (wlan0) ou Link 3 (eth0)
			parts := strings.Split(l, "(")
			if len(parts) >= 2 {
				name := strings.TrimSuffix(parts[1], ")")
				name = strings.TrimSpace(name)
				if name != "" && name != "lo" {
					ifaces = append(ifaces, name)
				}
			}
		}
	}
	return ifaces
}

func parseIPLinkShow(output []byte) []string {
	var ifaces []string
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		l := strings.TrimSpace(string(line))
		if l == "" {
			continue
		}
		parts := strings.Split(l, ": ")
		if len(parts) >= 2 {
			name := strings.TrimSpace(parts[1])
			if name != "" && name != "lo" && !strings.HasPrefix(name, "docker") && !strings.HasPrefix(name, "veth") {
				ifaces = append(ifaces, name)
			}
		}
	}
	return ifaces
}

func (c *linuxConfigurator) setInterfaceDNS(iface, ipv4Addr, ipv6Addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs := []string{"dns", iface, ipv4Addr}
	if ipv6Addr != "" {
		addrs = append(addrs, ipv6Addr)
	}
	cmd := execCommandContext(ctx, "resolvectl", addrs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("resolvectl dns %s falhou: %w (%s)", iface, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *linuxConfigurator) restoreInterfaceDNS(iface string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "resolvectl", "revert", iface)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("resolvectl revert %s falhou: %w (%s)", iface, err, strings.TrimSpace(string(out)))
	}
	return nil
}
