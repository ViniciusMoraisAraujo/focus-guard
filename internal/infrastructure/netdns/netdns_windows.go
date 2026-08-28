//go:build windows

package netdns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// execCommandContext is mockable for tests.
var execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

type windowsConfigurator struct {
	mu         sync.RWMutex
	configured []string
	connected  bool
	lastError  string
}

func newPlatformConfigurator() Configurator {
	return &windowsConfigurator{}
}

// SetLocalDNS discovers active connected network adapters (such as Wi-Fi and
// Ethernet) and configures their DNS servers to point to the local DNS sinkhole
// (127.0.0.1 for IPv4 and ::1 for IPv6).
func (c *windowsConfigurator) SetLocalDNS() ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	adapters, err := c.detectActiveAdapters()
	if err != nil {
		c.lastError = err.Error()
		return nil, fmt.Errorf("netdns: falha ao detectar interfaces de rede: %w", err)
	}

	if len(adapters) == 0 {
		c.lastError = "nenhuma interface ativa conectada encontrada"
		return nil, errors.New("netdns: nenhuma interface ativa conectada encontrada")
	}

	var successful []string
	var errs []error

	for _, name := range adapters {
		if err := c.setInterfaceDNS(name, "127.0.0.1", "::1"); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		} else {
			successful = append(successful, name)
		}
	}

	if len(successful) == 0 {
		c.connected = false
		c.configured = nil
		c.lastError = errors.Join(errs...).Error()
		return nil, fmt.Errorf("netdns: falha ao configurar DNS nos adaptadores: %w", errors.Join(errs...))
	}

	c.configured = successful
	c.connected = true
	c.lastError = ""
	return successful, nil
}

// RestoreDNS restores the DNS configuration for previously configured adapters
// (and all active adapters) back to DHCP/automatic.
func (c *windowsConfigurator) RestoreDNS() ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targetAdapters := c.configured
	if len(targetAdapters) == 0 {
		// Se não havia lista salva, busca as interfaces ativas atuais
		if active, err := c.detectActiveAdapters(); err == nil {
			targetAdapters = active
		}
	}

	var restored []string
	var errs []error

	for _, name := range targetAdapters {
		if err := c.restoreInterfaceDNS(name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		} else {
			restored = append(restored, name)
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

// Status returns the current adapter configuration status.
func (c *windowsConfigurator) Status() AdapterStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return AdapterStatus{
		Connected:          c.connected,
		ConfiguredAdapters: append([]string(nil), c.configured...),
		Detail:             c.lastError,
	}
}

// detectActiveAdapters lists connected network interface names (e.g. Wi-Fi, Ethernet)
// using netsh.
func (c *windowsConfigurator) detectActiveAdapters() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "netsh", "interface", "ipv4", "show", "interfaces")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback para netsh interface show interface
		return c.detectActiveAdaptersFallback()
	}

	adapters := parseIPv4ShowInterfaces(out)
	if len(adapters) > 0 {
		return adapters, nil
	}

	return c.detectActiveAdaptersFallback()
}

func (c *windowsConfigurator) detectActiveAdaptersFallback() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, "netsh", "interface", "show", "interface")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("netsh show interface falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return parseShowInterface(out), nil
}

// parseIPv4ShowInterfaces parses output of "netsh interface ipv4 show interfaces".
func parseIPv4ShowInterfaces(output []byte) []string {
	var adapters []string
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		l := strings.TrimSpace(string(line))
		if l == "" || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "Índ") || strings.HasPrefix(l, "Idx") {
			continue
		}

		fields := strings.Fields(l)
		if len(fields) < 5 {
			continue
		}

		state := strings.ToLower(fields[3])
		if state == "connected" || state == "conectado" {
			// O nome da interface pode conter espaços (ex: "Wi-Fi", "Conexão de Rede Bluetooth")
			name := strings.Join(fields[4:], " ")
			name = strings.TrimSpace(name)
			if name == "" || strings.HasPrefix(name, "Loopback") || strings.HasPrefix(name, "Conexão Local*") {
				continue
			}
			adapters = append(adapters, name)
		}
	}
	return adapters
}

// parseShowInterface parses output of "netsh interface show interface".
func parseShowInterface(output []byte) []string {
	var adapters []string
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		l := strings.TrimSpace(string(line))
		if l == "" || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "Estado") || strings.HasPrefix(l, "Admin") {
			continue
		}

		fields := strings.Fields(l)
		if len(fields) < 4 {
			continue
		}

		state := strings.ToLower(fields[1])
		if state == "connected" || state == "conectado" {
			name := strings.Join(fields[3:], " ")
			name = strings.TrimSpace(name)
			if name == "" || strings.HasPrefix(name, "Loopback") {
				continue
			}
			adapters = append(adapters, name)
		}
	}
	return adapters
}

// setInterfaceDNS applies IPv4 (127.0.0.1) and IPv6 (::1) static DNS to an interface.
func (c *windowsConfigurator) setInterfaceDNS(name, ipv4Addr, ipv6Addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// IPv4 DNS
	cmdV4 := execCommandContext(ctx, "netsh", "interface", "ipv4", "set", "dnsservers",
		fmt.Sprintf("name=%s", name), "source=static", fmt.Sprintf("address=%s", ipv4Addr),
		"register=none", "validate=no")
	if out, err := cmdV4.CombinedOutput(); err != nil {
		return fmt.Errorf("ipv4 DNS falhou: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// IPv6 DNS (best effort)
	if ipv6Addr != "" {
		ctxV6, cancelV6 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelV6()
		cmdV6 := execCommandContext(ctxV6, "netsh", "interface", "ipv6", "set", "dnsservers",
			fmt.Sprintf("name=%s", name), "source=static", fmt.Sprintf("address=%s", ipv6Addr),
			"register=none", "validate=no")
		if out, err := cmdV6.CombinedOutput(); err != nil {
			log.Printf("[FocusGuard NetDNS] aviso: IPv6 DNS não aplicado em %s: %v (%s)", name, err, strings.TrimSpace(string(out)))
		}
	}

	return nil
}

// restoreInterfaceDNS resets the interface DNS back to DHCP.
func (c *windowsConfigurator) restoreInterfaceDNS(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// IPv4 to DHCP
	cmdV4 := execCommandContext(ctx, "netsh", "interface", "ipv4", "set", "dnsservers",
		fmt.Sprintf("name=%s", name), "source=dhcp")
	outV4, errV4 := cmdV4.CombinedOutput()

	// IPv6 to DHCP (best effort)
	ctxV6, cancelV6 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelV6()
	cmdV6 := execCommandContext(ctxV6, "netsh", "interface", "ipv6", "set", "dnsservers",
		fmt.Sprintf("name=%s", name), "source=dhcp")
	_, _ = cmdV6.CombinedOutput()

	if errV4 != nil {
		return fmt.Errorf("ipv4 restauração DHCP falhou: %w (%s)", errV4, strings.TrimSpace(string(outV4)))
	}

	return nil
}
