//go:build linux

package netdns

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseResolvectlStatus(t *testing.T) {
	sample := `
Global
       Protocols: -LLMNR -mDNS -DNSOverTLS DNSSEC=no/unsupported
resolv.conf mode: stub

Link 2 (wlan0)
    Current Scopes: DNS
         Protocols: +DefaultRoute +LLMNR -mDNS -DNSOverTLS DNSSEC=no/unsupported
Current DNS Server: 192.168.1.1
       DNS Servers: 192.168.1.1

Link 3 (eth0)
    Current Scopes: none
`
	got := parseResolvectlStatus([]byte(sample))
	want := []string{"wlan0", "eth0"}

	if len(got) != len(want) {
		t.Fatalf("parseResolvectlStatus: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("iface[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseIPLinkShow(t *testing.T) {
	sample := `
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP mode DEFAULT group default qlen 1000
3: wlan0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP mode DEFAULT group default qlen 1000
`
	got := parseIPLinkShow([]byte(sample))
	want := []string{"eth0", "wlan0"}

	if len(got) != len(want) {
		t.Fatalf("parseIPLinkShow: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("iface[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(0)
	}

	cmdStr := strings.Join(args, " ")
	if strings.Contains(cmdStr, "resolvectl status") {
		_, _ = os.Stdout.WriteString("Link 2 (wlan0)\n")
		os.Exit(0)
	}
	os.Exit(0)
}

func mockExec(t *testing.T) {
	oldExec := execCommandContext
	t.Cleanup(func() { execCommandContext = oldExec })

	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
}

func TestLinuxConfigurator_SetAndRestore(t *testing.T) {
	mockExec(t)

	cfg := newPlatformConfigurator()
	configured, err := cfg.SetLocalDNS()
	if err != nil {
		t.Fatalf("SetLocalDNS: %v", err)
	}
	if len(configured) != 1 || configured[0] != "wlan0" {
		t.Errorf("configured = %v, want [wlan0]", configured)
	}

	restored, err := cfg.RestoreDNS()
	if err != nil {
		t.Fatalf("RestoreDNS: %v", err)
	}
	if len(restored) != 1 || restored[0] != "wlan0" {
		t.Errorf("restored = %v, want [wlan0]", restored)
	}
}
