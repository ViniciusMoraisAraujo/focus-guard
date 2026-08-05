package ipc

import (
	"strings"
	"testing"

	"focusguard/internal/dnsserver"
)

// fakeDNSController is a scriptable stand-in for *dnsserver.Controller.
type fakeDNSController struct {
	startErr error
	stopErr  error
	started  bool
	st       dnsserver.Status
}

func newFakeDNS() *fakeDNSController {
	return &fakeDNSController{
		st: dnsserver.Status{Upstream: dnsserver.DefaultUpstream},
	}
}

func (f *fakeDNSController) Start() error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	f.st.Listening = true
	f.st.Addr = "0.0.0.0:53"
	return nil
}

func (f *fakeDNSController) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.started = false
	f.st.Listening = false
	f.st.Addr = ""
	return nil
}

func (f *fakeDNSController) Status() dnsserver.Status { return f.st }

func TestDNSStart_StartsAndPersistsEnabled(t *testing.T) {
	server := setupTestServer(t)
	dns := newFakeDNS()
	server.SetDNS(dns)

	resp := executeRequest(t, server, Request{Action: "dns-start"})
	if !resp.Success {
		t.Fatalf("dns-start falhou: %v", resp.Message)
	}
	if !dns.started {
		t.Error("controller não foi iniciado")
	}
	if !resp.DNSListening || resp.DNSAddr == "" {
		t.Errorf("response DNS = listening=%v addr=%q, want listening=true addr preenchido", resp.DNSListening, resp.DNSAddr)
	}
	if !resp.DNSEnabled {
		t.Error("DNSEnabled = false após dns-start")
	}
	if !server.scheduler.DNSEnabled() {
		t.Error("scheduler não persistiu DNSEnabled=true")
	}
}

func TestDNSStop_StopsAndPersistsDisabled(t *testing.T) {
	server := setupTestServer(t)
	dns := newFakeDNS()
	server.SetDNS(dns)

	if resp := executeRequest(t, server, Request{Action: "dns-start"}); !resp.Success {
		t.Fatalf("dns-start falhou: %v", resp.Message)
	}

	resp := executeRequest(t, server, Request{Action: "dns-stop"})
	if !resp.Success {
		t.Fatalf("dns-stop falhou: %v", resp.Message)
	}
	if dns.started {
		t.Error("controller ainda marcado como iniciado após dns-stop")
	}
	if resp.DNSListening {
		t.Error("DNSListening = true após dns-stop")
	}
	if resp.DNSEnabled {
		t.Error("DNSEnabled = true após dns-stop")
	}
	if server.scheduler.DNSEnabled() {
		t.Error("scheduler manteve DNSEnabled=true após dns-stop")
	}
}

func TestDNSActions_Unconfigured(t *testing.T) {
	server := setupTestServer(t)

	for _, action := range []string{"dns-start", "dns-stop", "dns-status"} {
		resp := executeRequest(t, server, Request{Action: action})
		if resp.Success {
			t.Errorf("%s deveria falhar sem controller configurado", action)
		}
		if !strings.Contains(resp.Message, "não configurado") {
			t.Errorf("%s: mensagem = %q, esperava 'não configurado'", action, resp.Message)
		}
	}
}

func TestDNSStatus_ShowsCountersAndUpstream(t *testing.T) {
	server := setupTestServer(t)
	dns := newFakeDNS()
	dns.st.Listening = true
	dns.st.Addr = "0.0.0.0:53"
	dns.st.Upstream = "9.9.9.9:53"
	dns.st.Queries = 42
	dns.st.Blocked = 7
	server.SetDNS(dns)
	if err := server.scheduler.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled: %v", err)
	}

	resp := executeRequest(t, server, Request{Action: "dns-status"})
	if !resp.Success {
		t.Fatalf("dns-status falhou: %v", resp.Message)
	}
	if !resp.DNSEnabled || !resp.DNSListening {
		t.Errorf("dns-status: enabled=%v listening=%v", resp.DNSEnabled, resp.DNSListening)
	}
	if resp.DNSAddr != "0.0.0.0:53" || resp.DNSUpstream != "9.9.9.9:53" {
		t.Errorf("dns-status: addr=%q upstream=%q", resp.DNSAddr, resp.DNSUpstream)
	}
	if resp.DNSQueries != 42 || resp.DNSBlocked != 7 {
		t.Errorf("dns-status: queries=%d blocked=%d", resp.DNSQueries, resp.DNSBlocked)
	}
}

func TestStatusAction_IncludesDNSFields(t *testing.T) {
	server := setupTestServer(t)
	dns := newFakeDNS()
	server.SetDNS(dns)

	// Desligado mas habilitado (persistido): status deve refletir os dois.
	if err := server.scheduler.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled: %v", err)
	}
	resp := executeRequest(t, server, Request{Action: "status"})
	if !resp.Success {
		t.Fatalf("status falhou: %v", resp.Message)
	}
	if !resp.DNSEnabled {
		t.Error("status: DNSEnabled = false, esperava true (persistido)")
	}
	if resp.DNSListening {
		t.Error("status: DNSListening = true, esperava false (desligado)")
	}
	if resp.DNSUpstream != dnsserver.DefaultUpstream {
		t.Errorf("status: DNSUpstream = %q, esperava padrão %q", resp.DNSUpstream, dnsserver.DefaultUpstream)
	}
}

func TestStatusAction_DNSEnabledPersistedWithoutController(t *testing.T) {
	server := setupTestServer(t)
	if err := server.scheduler.SetDNSEnabled(true); err != nil {
		t.Fatalf("SetDNSEnabled: %v", err)
	}

	resp := executeRequest(t, server, Request{Action: "status"})
	if !resp.DNSEnabled {
		t.Error("status sem controller: DNSEnabled = false, esperava true")
	}
}

func TestDNSStart_BindFailureSurfacesError(t *testing.T) {
	server := setupTestServer(t)
	server.SetDNS(&fakeDNSController{startErr: &bindErr{}})

	resp := executeRequest(t, server, Request{Action: "dns-start"})
	if resp.Success {
		t.Fatal("dns-start deveria falhar com bind error")
	}
	if !strings.Contains(resp.Message, "permission denied") {
		t.Errorf("mensagem = %q, esperava o erro de bind", resp.Message)
	}
	if server.scheduler.DNSEnabled() {
		t.Error("scheduler persistiu DNSEnabled=true apesar do bind falhar")
	}
}

type bindErr struct{}

func (bindErr) Error() string { return "dns: udp bind 0.0.0.0:53: bind: permission denied" }
