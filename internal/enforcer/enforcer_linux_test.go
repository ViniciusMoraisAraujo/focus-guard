//go:build linux

package enforcer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIptablesBinFor(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		wantBin string
		wantErr bool
	}{
		{
			name:    "Valid IPv4 address",
			ip:      "192.168.1.1",
			wantBin: "iptables",
			wantErr: false,
		},
		{
			name:    "Valid IPv6 address",
			ip:      "2001:db8::1",
			wantBin: "ip6tables",
			wantErr: false,
		},
		{
			name:    "Valid IPv6 Loopback",
			ip:      "::1",
			wantBin: "ip6tables",
			wantErr: false,
		},
		{
			name:    "Invalid IP address",
			ip:      "256.256.256.256",
			wantBin: "",
			wantErr: true,
		},
		{
			name:    "Arbitrary text string",
			ip:      "not-an-ip",
			wantBin: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBin, err := iptablesBinFor(tt.ip)
			if (err != nil) != tt.wantErr {
				t.Errorf("iptablesBinFor(%q) error = %v, wantErr %v", tt.ip, err, tt.wantErr)
				return
			}
			if gotBin != tt.wantBin {
				t.Errorf("iptablesBinFor(%q) = %q, expected %q", tt.ip, gotBin, tt.wantBin)
			}
		})
	}
}

func TestCollectAllIPs(t *testing.T) {
	enf := &linuxEnforcer{}

	extraIPs := []string{
		"1.1.1.1",
		"1.1.1.1",
		"",
		"8.8.8.8",
	}

	ips := enf.collectAllIPs("domain.local.invalid", extraIPs)

	seen := make(map[string]int)
	for _, ip := range ips {
		seen[ip]++
	}

	if seen["1.1.1.1"] != 1 {
		t.Errorf("expected exactly 1 occurrence of 1.1.1.1, got: %d", seen["1.1.1.1"])
	}

	if seen["8.8.8.8"] != 1 {
		t.Errorf("expected exactly 1 occurrence of 8.8.8.8, got: %d", seen["8.8.8.8"])
	}

	if _, exists := seen[""]; exists {
		t.Errorf("empty string should not be present in collectAllIPs result")
	}
}

func TestHostsFileOperations(t *testing.T) {
	tempDir := t.TempDir()
	tempHostsPath := filepath.Join(tempDir, "hosts")

	initialContent := "127.0.0.1 localhost\n::1 localhost\n"
	err := os.WriteFile(tempHostsPath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("failed to create temporary hosts file: %v", err)
	}

	enf := &linuxEnforcer{
		hostsPath: tempHostsPath,
	}

	domain := "example.com"

	t.Run("Add entry to hosts", func(t *testing.T) {
		err := enf.addHostEntry(domain)
		if err != nil {
			t.Fatalf("addHostEntry failed: %v", err)
		}

		lines, err := enf.readHostsLines()
		if err != nil {
			t.Fatalf("readHostsLines failed: %v", err)
		}

		foundMarker := false
		for _, line := range lines {
			if strings.Contains(line, "# FOCUSGUARD: example.com") {
				foundMarker = true
				break
			}
		}

		if !foundMarker {
			t.Errorf("marker # FOCUSGUARD: example.com was not found in hosts file")
		}
	})

	t.Run("Idempotency on re-adding the same entry", func(t *testing.T) {
		linesBefore, _ := enf.readHostsLines()

		err := enf.addHostEntry(domain)
		if err != nil {
			t.Fatalf("addHostEntry failed on second execution: %v", err)
		}

		linesAfter, _ := enf.readHostsLines()

		if len(linesBefore) != len(linesAfter) {
			t.Errorf("addHostEntry duplicated lines. Before: %d, After: %d", len(linesBefore), len(linesAfter))
		}
	})

	t.Run("Remove entry from hosts", func(t *testing.T) {
		err := enf.removeHostEntry(domain)
		if err != nil {
			t.Fatalf("removeHostEntry failed: %v", err)
		}

		lines, err := enf.readHostsLines()
		if err != nil {
			t.Fatalf("readHostsLines failed: %v", err)
		}

		for _, line := range lines {
			if strings.Contains(line, "# FOCUSGUARD: example.com") {
				t.Errorf("line with marker still found after removal: %s", line)
			}
		}
	})
}

func TestUnprivilegedCheck(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("Skipping unprivileged check as test is running with root permissions (sudo)")
	}

	enf := NewEnforcer()

	errBlock := enf.BlockDomain("example.com", []string{"1.1.1.1"})
	if errBlock == nil {
		t.Error("expected error due to lack of root privileges in BlockDomain, got nil")
	}

	errUnblock := enf.UnblockDomain("example.com", []string{"1.1.1.1"})
	if errUnblock == nil {
		t.Error("expected error due to lack of root privileges in UnblockDomain, got nil")
	}

	errSync := enf.Sync(map[string][]string{"example.com": {"1.1.1.1"}})
	if errSync == nil {
		t.Error("expected error due to lack of root privileges in Sync, got nil")
	}
}
