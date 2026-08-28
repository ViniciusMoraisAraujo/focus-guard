//go:build windows

package netdns

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseIPv4ShowInterfaces(t *testing.T) {
	sample := `
Índ     Met         MTU          Estado                Nome
---  ----------  ----------  ------------  ---------------------------
  1          75  4294967295  connected     Loopback Pseudo-Interface 1
  3          65        1500  disconnected  Conexão de Rede Bluetooth
  4          55        1500  connected     Wi-Fi
 17          25        1500  disconnected  Conexão Local* 1
 18          25        1500  connected     Ethernet 2
`
	got := parseIPv4ShowInterfaces([]byte(sample))
	want := []string{"Wi-Fi", "Ethernet 2"}

	if len(got) != len(want) {
		t.Fatalf("parseIPv4ShowInterfaces: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("adapter[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseShowInterface(t *testing.T) {
	sample := `
Estado adm.    Estado         Tipo             Nome da interface
-------------------------------------------------------------------------
Habilitado     Conectado      Dedicado         Wi-Fi
Habilitado     Desconectado   Dedicado         Ethernet
Habilitado     Conectado      Dedicado         Rede Sem Fio
`
	got := parseShowInterface([]byte(sample))
	want := []string{"Wi-Fi", "Rede Sem Fio"}

	if len(got) != len(want) {
		t.Fatalf("parseShowInterface: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("adapter[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestHelperProcess is used to mock os/exec commands in tests.
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
	if strings.Contains(cmdStr, "interface ipv4 show interfaces") {
		_, _ = os.Stdout.WriteString(`
Índ     Met         MTU          Estado                Nome
---  ----------  ----------  ------------  ---------------------------
  1          75  4294967295  connected     Loopback Pseudo-Interface 1
  4          55        1500  connected     Wi-Fi
`)
		os.Exit(0)
	}

	if strings.Contains(cmdStr, "set dnsservers") {
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

func TestWindowsConfigurator_SetAndRestore(t *testing.T) {
	mockExec(t)

	cfg := newPlatformConfigurator()
	st := cfg.Status()
	if st.Connected {
		t.Errorf("inicialmente não deveria estar conectado")
	}

	configured, err := cfg.SetLocalDNS()
	if err != nil {
		t.Fatalf("SetLocalDNS: %v", err)
	}
	if len(configured) != 1 || configured[0] != "Wi-Fi" {
		t.Errorf("configured = %v, want [Wi-Fi]", configured)
	}

	st = cfg.Status()
	if !st.Connected || len(st.ConfiguredAdapters) != 1 || st.ConfiguredAdapters[0] != "Wi-Fi" {
		t.Errorf("status após set: %+v", st)
	}

	restored, err := cfg.RestoreDNS()
	if err != nil {
		t.Fatalf("RestoreDNS: %v", err)
	}
	if len(restored) != 1 || restored[0] != "Wi-Fi" {
		t.Errorf("restored = %v, want [Wi-Fi]", restored)
	}

	st = cfg.Status()
	if st.Connected || len(st.ConfiguredAdapters) != 0 {
		t.Errorf("status após restore: %+v", st)
	}
}
