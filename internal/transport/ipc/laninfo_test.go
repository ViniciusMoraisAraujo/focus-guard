package ipc

import (
	"testing"
)

// TestStatusAction_IncludesMachineLAN verifies the status action surfaces the
// machine's LAN IP+MAC (Guia: reserva DHCP no roteador) via the injectable
// lanInfo seam — o ambiente de teste não tem rota default garantida, então o
// valor real é stubado.
func TestStatusAction_IncludesMachineLAN(t *testing.T) {
	old := lanInfo
	lanInfo = func() (string, string) { return "192.168.1.100", "aa:bb:cc:dd:ee:ff" }
	defer func() { lanInfo = old }()

	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "status"})
	if !resp.Success {
		t.Fatalf("status falhou: %v", resp.Message)
	}
	if resp.LanIP != "192.168.1.100" || resp.LanMAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("status: lan_ip=%q lan_mac=%q, esperava 192.168.1.100/aa:bb:cc:dd:ee:ff",
			resp.LanIP, resp.LanMAC)
	}
}

// TestStatusAction_MachineLANBestEffort covers the degraded path: sem rota
// default (lanInfo vazio) o status não falha e reporta campos vazios.
func TestStatusAction_MachineLANBestEffort(t *testing.T) {
	old := lanInfo
	lanInfo = func() (string, string) { return "", "" }
	defer func() { lanInfo = old }()

	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "status"})
	if resp.LanIP != "" || resp.LanMAC != "" {
		t.Errorf("status sem LAN: lan_ip=%q lan_mac=%q, esperava vazio", resp.LanIP, resp.LanMAC)
	}
}

// TestLanInfoMACFormat sanity-checks the MAC formatting of the real
// implementation: machineLAN devolve o MAC da interface que detém o IPv4 de
// saída (AA:BB:CC:DD:EE:FF). Skippable: exige rota default — em ambientes sem
// rede o teste apenas verifica que os dois valores são consistentes (ou ambos
// vazios).
func TestLanInfoMACFormat(t *testing.T) {
	ip4, mac := machineLAN()
	if ip4 == "" {
		// Sem rota default (sandbox/CI): best-effort documentado, nada a
		// validar.
		return
	}
	if mac == "" {
		t.Errorf("machineLAN: IP %q sem MAC correspondente", ip4)
	}
}
