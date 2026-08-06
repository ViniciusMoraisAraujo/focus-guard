package dns

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/infrastructure/dnsserver"
	"focusguard/internal/ipc"
)

type fakeCtrl struct {
	started    bool
	stopped    bool
	upstream   string
	status     dnsserver.Status
	startErr   error
	stopErr    error
	upstreamErr error
}

func (f *fakeCtrl) Start() error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	f.status = dnsserver.Status{Listening: true, Addr: "127.0.0.1:53"}
	return nil
}

func (f *fakeCtrl) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stopped = true
	f.status = dnsserver.Status{}
	return nil
}

func (f *fakeCtrl) SetUpstream(upstream string) error {
	if f.upstreamErr != nil {
		return f.upstreamErr
	}
	f.upstream = upstream
	return nil
}

func (f *fakeCtrl) Status() dnsserver.Status { return f.status }

type fakePersist struct {
	enabled  bool
	upstream string
	err      error
}

func (f *fakePersist) SetDNSEnabled(enabled bool) error {
	if f.err != nil {
		return f.err
	}
	f.enabled = enabled
	return nil
}

func (f *fakePersist) SetDNSUpstream(upstream string) error {
	if f.err != nil {
		return f.err
	}
	f.upstream = upstream
	return nil
}

func (f *fakePersist) DNSEnabled() bool { return f.enabled }

func TestDNSStart_OKComHook(t *testing.T) {
	c := &fakeCtrl{}
	p := &fakePersist{}
	hooked := false
	h := NewStart(c, p, func() { hooked = true })

	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "dns-start"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("esperava sucesso, got %+v", resp)
	}
	if !c.started {
		t.Fatal("Start não foi chamado")
	}
	if !p.enabled {
		t.Fatal("flag persistido deveria estar true")
	}
	if !hooked {
		t.Fatal("hook onStarted não disparado")
	}
	if !resp.DNSListening {
		t.Fatalf("resposta deveria refletir o estado vivo, got %+v", resp)
	}
}

func TestDNSStart_FalhaAoPersistirParaOServidor(t *testing.T) {
	c := &fakeCtrl{}
	p := &fakePersist{err: errors.New("write falhou")}
	h := NewStart(c, p, nil)

	_, err := h.Handle(context.Background(), &ipc.Request{Action: "dns-start"})
	if err == nil {
		t.Fatal("esperava o erro de persistência")
	}
	if !c.stopped {
		t.Fatal("servidor deveria ter sido desligado após falha de persistência")
	}
}

func TestDNSStop_OK(t *testing.T) {
	c := &fakeCtrl{status: dnsserver.Status{Listening: true}}
	p := &fakePersist{enabled: true}
	h := NewStop(c, p)

	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "dns-stop"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !c.stopped || p.enabled {
		t.Fatalf("esperava sucesso + parado + flag false, got resp=%+v", resp)
	}
}

func TestDNSStatus_OK(t *testing.T) {
	c := &fakeCtrl{status: dnsserver.Status{Listening: true, Addr: "127.0.0.1:53", Queries: 10}}
	p := &fakePersist{enabled: true}
	h := NewStatus(c, p)

	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "dns-status"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.DNSEnabled || !resp.DNSListening || resp.DNSQueries != 10 {
		t.Fatalf("esperava estado combinado, got %+v", resp)
	}
}

func TestDNSSetUpstream_SemController(t *testing.T) {
	h := NewSetUpstream(nil, &fakePersist{})
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "dns-set-upstream", Upstream: "1.1.1.2"})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeNotConfigured {
		t.Fatalf("esperava ERR_NOT_CONFIGURED, got %v", err)
	}
}

func TestDNSSetUpstream_UpstreamInvalido(t *testing.T) {
	h := NewSetUpstream(&fakeCtrl{}, &fakePersist{})
	_, err := h.Handle(context.Background(), &ipc.Request{Action: "dns-set-upstream", Upstream: ""})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeInvalid {
		t.Fatalf("esperava ERR_INVALID, got %v", err)
	}
}

func TestDNSSetUpstream_OK(t *testing.T) {
	c := &fakeCtrl{}
	p := &fakePersist{}
	h := NewSetUpstream(c, p)

	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "dns-set-upstream", Upstream: "9.9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("esperava sucesso, got %+v", resp)
	}
	if c.upstream != "9.9.9.9:53" {
		t.Fatalf("upstream deveria ganhar porta 53, got %q", c.upstream)
	}
	if p.upstream != "9.9.9.9:53" {
		t.Fatalf("persist deveria receber o upstream normalizado, got %q", p.upstream)
	}
}

func TestNormalizeUpstream(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"1.1.1.2", "1.1.1.2:53", false},
		{"dns.google", "dns.google:53", false},
		{"9.9.9.9:53", "9.9.9.9:53", false},
		{"", "", true},
		{"1.1.1.2:99999", "", true},
		{"host:porta", "", true},
	}
	for _, c := range cases {
		got, err := normalizeUpstream(c.in)
		if c.err != (err != nil) {
			t.Fatalf("normalizeUpstream(%q): err=%v, queria err=%v", c.in, err, c.err)
		}
		if got != c.want {
			t.Fatalf("normalizeUpstream(%q) = %q, queria %q", c.in, got, c.want)
		}
	}
}
