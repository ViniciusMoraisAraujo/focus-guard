package dns

import (
	"context"
	"errors"
	"strings"
	"testing"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/infrastructure/dnsserver"
	"focusguard/internal/infrastructure/netdns"
)

type fakeCtrl struct {
	started     bool
	startErr    error
	stopErr     error
	upstreamErr error
	upstream    string
	st          dnsserver.Status
}

func (f *fakeCtrl) Start() error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	f.st.Listening = true
	f.st.Addr = "0.0.0.0:53"
	return nil
}

func (f *fakeCtrl) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.started = false
	f.st.Listening = false
	f.st.Addr = ""
	return nil
}

func (f *fakeCtrl) SetUpstream(u string) error {
	if f.upstreamErr != nil {
		return f.upstreamErr
	}
	f.upstream = u
	f.st.Upstream = u
	return nil
}

func (f *fakeCtrl) Status() dnsserver.Status {
	return f.st
}

type fakePersist struct {
	enabled     bool
	upstream    string
	persistErr  error
	upstreamErr error
}

func (f *fakePersist) SetDNSEnabled(v bool) error {
	if f.persistErr != nil {
		return f.persistErr
	}
	f.enabled = v
	return nil
}

func (f *fakePersist) SetDNSUpstream(u string) error {
	if f.upstreamErr != nil {
		return f.upstreamErr
	}
	f.upstream = u
	return nil
}

func (f *fakePersist) DNSEnabled() bool {
	return f.enabled
}

type fakeAdapter struct {
	setErr     error
	restoreErr error
	adapters   []string
	connected  bool
}

func (f *fakeAdapter) SetLocalDNS() ([]string, error) {
	if f.setErr != nil {
		return nil, f.setErr
	}
	f.connected = true
	f.adapters = []string{"Wi-Fi"}
	return f.adapters, nil
}

func (f *fakeAdapter) RestoreDNS() ([]string, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	f.connected = false
	res := f.adapters
	f.adapters = nil
	return res, nil
}

func (f *fakeAdapter) Status() netdns.AdapterStatus {
	return netdns.AdapterStatus{
		Connected:          f.connected,
		ConfiguredAdapters: f.adapters,
	}
}

func TestStartHandler_Success(t *testing.T) {
	ctrl := &fakeCtrl{st: dnsserver.Status{Upstream: "1.1.1.2:53"}}
	persist := &fakePersist{}
	adapter := &fakeAdapter{}
	var hookCalled bool

	h := NewStart(ctrl, persist, adapter, func() { hookCalled = true })
	if h.Action() != "dns-start" {
		t.Errorf("action = %s, want dns-start", h.Action())
	}

	res, err := h.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !ctrl.started || !ctrl.st.Listening {
		t.Error("server controller não foi iniciado")
	}
	if !adapter.connected || len(adapter.adapters) != 1 || adapter.adapters[0] != "Wi-Fi" {
		t.Errorf("adaptadores não foram configurados: %+v", adapter)
	}
	if !persist.enabled {
		t.Error("persistência não foi marcada como enabled")
	}
	if !hookCalled {
		t.Error("hook onStarted não foi chamado")
	}
	if !res.Status.Listening || !res.Status.Enabled || !res.Status.AdapterConnected {
		t.Errorf("status inesperado: %+v", res.Status)
	}
}

func TestStartHandler_RollbackOnPersistFailure(t *testing.T) {
	ctrl := &fakeCtrl{st: dnsserver.Status{Upstream: "1.1.1.2:53"}}
	persist := &fakePersist{persistErr: errors.New("falha no disco")}
	adapter := &fakeAdapter{}

	h := NewStart(ctrl, persist, adapter, nil)
	_, err := h.Handle(context.Background(), &NoInput{})
	if err == nil {
		t.Fatal("esperava erro ao falhar persistência")
	}

	if ctrl.started {
		t.Error("controller deveria ter sido parado no rollback")
	}
	if adapter.connected {
		t.Error("adaptador deveria ter sido restaurado no rollback")
	}
}

func TestStopHandler_Success(t *testing.T) {
	ctrl := &fakeCtrl{started: true, st: dnsserver.Status{Listening: true, Addr: "0.0.0.0:53"}}
	persist := &fakePersist{enabled: true}
	adapter := &fakeAdapter{connected: true, adapters: []string{"Wi-Fi"}}

	h := NewStop(ctrl, persist, adapter)
	if h.Action() != "dns-stop" {
		t.Errorf("action = %s, want dns-stop", h.Action())
	}

	res, err := h.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if ctrl.started || ctrl.st.Listening {
		t.Error("controller deveria estar parado")
	}
	if adapter.connected {
		t.Error("adaptadores deveriam estar restaurados")
	}
	if persist.enabled {
		t.Error("persistência deveria estar desabilitada")
	}
	if res.Status.Listening || res.Status.Enabled || res.Status.AdapterConnected {
		t.Errorf("status inesperado após stop: %+v", res.Status)
	}
}

func TestStatusHandler_Success(t *testing.T) {
	ctrl := &fakeCtrl{st: dnsserver.Status{Listening: true, Addr: "0.0.0.0:53", Queries: 10, Blocked: 2}}
	persist := &fakePersist{enabled: true}
	adapter := &fakeAdapter{connected: true, adapters: []string{"Wi-Fi"}}

	h := NewStatus(ctrl, persist, adapter)
	if h.Action() != "dns-status" {
		t.Errorf("action = %s, want dns-status", h.Action())
	}

	res, err := h.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !res.Status.Listening || !res.Status.Enabled || !res.Status.AdapterConnected {
		t.Errorf("status inesperado: %+v", res.Status)
	}
	if res.Status.Queries != 10 || res.Status.Blocked != 2 {
		t.Errorf("contadores incorretos: queries=%d blocked=%d", res.Status.Queries, res.Status.Blocked)
	}
}

func TestSetUpstreamHandler_Success(t *testing.T) {
	ctrl := &fakeCtrl{st: dnsserver.Status{Listening: true}}
	persist := &fakePersist{}
	adapter := &fakeAdapter{}

	h := NewSetUpstream(ctrl, persist, adapter)
	if h.Action() != "dns-set-upstream" {
		t.Errorf("action = %s, want dns-set-upstream", h.Action())
	}

	res, err := h.Handle(context.Background(), &SetUpstreamInput{Upstream: "9.9.9.9"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if ctrl.upstream != "9.9.9.9:53" || persist.upstream != "9.9.9.9:53" {
		t.Errorf("upstream não aplicado: ctrl=%s, persist=%s", ctrl.upstream, persist.upstream)
	}
	if !strings.Contains(res.Message, "9.9.9.9:53") {
		t.Errorf("mensagem = %s, want contendo 9.9.9.9:53", res.Message)
	}
}

func TestHandlers_Unconfigured(t *testing.T) {
	assertNotConfigured := func(name string, err error) {
		t.Helper()
		var ipErr *ipcerr.Error
		if !errors.As(err, &ipErr) || ipErr.Code != ipcerr.CodeNotConfigured {
			t.Errorf("%s: got error %v, want CodeNotConfigured", name, err)
		}
	}

	startH := NewStart(nil, nil, nil, nil)
	_, err := startH.Handle(context.Background(), &NoInput{})
	assertNotConfigured("startHandler", err)

	stopH := NewStop(nil, nil, nil)
	_, err = stopH.Handle(context.Background(), &NoInput{})
	assertNotConfigured("stopHandler", err)

	statusH := NewStatus(nil, nil, nil)
	_, err = statusH.Handle(context.Background(), &NoInput{})
	assertNotConfigured("statusHandler", err)

	upstreamH := NewSetUpstream(nil, nil, nil)
	_, err = upstreamH.Handle(context.Background(), &SetUpstreamInput{Upstream: "1.1.1.1"})
	assertNotConfigured("setUpstreamHandler", err)
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
		got, err := NormalizeUpstream(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeUpstream(%q) deveria falhar, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeUpstream(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeUpstream(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
