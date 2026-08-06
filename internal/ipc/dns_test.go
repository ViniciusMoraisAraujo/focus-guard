package ipc

import (
	"errors"
	"strings"
	"testing"

	"focusguard/internal/infrastructure/dnsserver"
)

// fakeDNSController is a scriptable stand-in for *dnsserver.Controller.
type fakeDNSController struct {
	startErr    error
	stopErr     error
	setErr      error
	started     bool
	setUpstream string
	st          dnsserver.Status
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

func (f *fakeDNSController) SetUpstream(upstream string) error {
	f.setUpstream = upstream
	if f.setErr != nil {
		return f.setErr
	}
	f.st.Upstream = upstream
	return nil
}

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

func TestDNSSetUpstream_PersistsAndApplies(t *testing.T) {
	server := setupTestServer(t)
	dns := newFakeDNS()
	server.SetDNS(dns)

	// Sem porta explícita: o normalize acrescenta :53.
	resp := executeRequest(t, server, Request{Action: "dns-set-upstream", Upstream: "9.9.9.9"})
	if !resp.Success {
		t.Fatalf("dns-set-upstream falhou: %s", resp.Message)
	}
	if got := server.scheduler.DNSUpstream(); got != "9.9.9.9:53" {
		t.Errorf("scheduler.DNSUpstream = %q, want 9.9.9.9:53", got)
	}
	if dns.setUpstream != "9.9.9.9:53" {
		t.Errorf("controller.SetUpstream = %q, want 9.9.9.9:53", dns.setUpstream)
	}
	if resp.DNSUpstream != "9.9.9.9:53" {
		t.Errorf("resp.DNSUpstream = %q, want 9.9.9.9:53", resp.DNSUpstream)
	}
}

func TestDNSSetUpstream_ValidationErrors(t *testing.T) {
	server := setupTestServer(t)
	server.SetDNS(newFakeDNS())

	cases := []struct {
		name     string
		upstream string
	}{
		{name: "empty", upstream: ""},
		{name: "port zero", upstream: "1.1.1.2:0"},
		{name: "port too big", upstream: "1.1.1.2:99999"},
		{name: "non-numeric port", upstream: "host:abc"},
		{name: "missing host", upstream: ":53"},
		{name: "too many colons", upstream: "1.1.1.2:53:53"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := executeRequest(t, server, Request{Action: "dns-set-upstream", Upstream: tc.upstream})
			if resp.Success {
				t.Errorf("dns-set-upstream(%q) deveria falhar", tc.upstream)
			}
			if server.scheduler.DNSUpstream() != "" {
				t.Errorf("upstream inválido não deveria persistir, got %q", server.scheduler.DNSUpstream())
			}
		})
	}
}

func TestDNSSetUpstream_Unconfigured(t *testing.T) {
	server := setupTestServer(t)
	resp := executeRequest(t, server, Request{Action: "dns-set-upstream", Upstream: "9.9.9.9"})
	if resp.Success {
		t.Fatal("dns-set-upstream sem controller deveria falhar")
	}
	if !strings.Contains(resp.Message, "não configurado") {
		t.Errorf("mensagem = %q, esperava 'não configurado'", resp.Message)
	}
}

func TestDNSSetUpstream_ApplyFailureSurfacesError(t *testing.T) {
	server := setupTestServer(t)
	server.SetDNS(&fakeDNSController{setErr: errors.New("restart falhou")})

	resp := executeRequest(t, server, Request{Action: "dns-set-upstream", Upstream: "9.9.9.9"})
	if resp.Success {
		t.Fatal("dns-set-upstream deveria falhar quando o controller rejeita")
	}
	if !strings.Contains(resp.Message, "restart falhou") {
		t.Errorf("mensagem = %q, esperava o erro do controller", resp.Message)
	}
	// O valor persistiu mesmo com o apply falhando — o próximo boot o usa
	// (mesmo padrão do dns-start com bind ocupado).
	if server.scheduler.DNSUpstream() != "9.9.9.9:53" {
		t.Errorf("scheduler.DNSUpstream = %q, esperava persistido", server.scheduler.DNSUpstream())
	}
}

func TestNormalizeUpstream(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "1.1.1.2", want: "1.1.1.2:53"},
		{in: "9.9.9.9:53", want: "9.9.9.9:53"},
		{in: "dns.google", want: "dns.google:53"},
		{in: "  8.8.8.8  ", want: "8.8.8.8:53"},
		{in: "[::1]:53", want: "[::1]:53"},
		{in: "", wantErr: true},
		{in: ":53", wantErr: true},
		{in: "1.1.1.2:0", wantErr: true},
		{in: "1.1.1.2:99999", wantErr: true},
		{in: "host:abc", wantErr: true},
		{in: "1.1.1.2:53:53", wantErr: true},
	}
	for _, tc := range tests {
		got, err := normalizeUpstream(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("normalizeUpstream(%q) deveria falhar, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeUpstream(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeUpstream(%q) = %q, want %q", tc.in, got, tc.want)
		}
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

func TestDNSStart_CallsOnDNSStartedHookAfterPersist(t *testing.T) {
	var hookCalled bool
	server := setupTestServerWithDeps(t, &refDeps{onDNSStarted: func() { hookCalled = true }})
	server.SetDNS(newFakeDNS())

	resp := executeRequest(t, server, Request{Action: "dns-start"})
	if !resp.Success {
		t.Fatalf("dns-start falhou: %v", resp.Message)
	}
	if !hookCalled {
		t.Error("SetOnDNSStarted hook não foi chamado após dns-start bem-sucedido")
	}
	if !server.scheduler.DNSEnabled() {
		t.Error("hook foi chamado antes de persistir DNSEnabled")
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
