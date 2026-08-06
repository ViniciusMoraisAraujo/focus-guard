package ipc_test

// ---------------------------------------------------------------------------
// Wiring test (package ipc_test, externo): compõe os handlers REAIS dos
// pacotes de domínio com o roteador do ipc.Server, exatamente como o daemon
// os registra no composition root (Fase 5). Fecha o gap de drift: os testes
// internos do ipc (package ipc) usam os adapters de referência
// (handlers_ref_test.go), e este exercita o código de PRODUÇÃO dos pacotes de
// domínio contra a superfície do wire — incluindo o fechamento specs↔registry
// que o daemon verifica no boot (ValidateRegistry).
// ---------------------------------------------------------------------------

import (
	"context"
	"strings"
	"testing"
	"time"

	"focusguard/internal/apps"
	"focusguard/internal/blocks"
	"focusguard/internal/dns"
	"focusguard/internal/dnsserver"
	"focusguard/internal/goal"
	"focusguard/internal/ipc"
	"focusguard/internal/policy"
	"focusguard/internal/preset"
	"focusguard/internal/presets"
	"focusguard/internal/user"
	"focusguard/internal/users"
)

type fakeBlocker struct {
	block  *policy.Block
	active *policy.Block
	blocks []policy.Block
	err    error
}

func (f *fakeBlocker) Block(domain string, d time.Duration) (*policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.block, nil
}

func (f *fakeBlocker) BlockDomains(domains []string, d time.Duration) ([]policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.blocks, nil
}

func (f *fakeBlocker) ExtendBlock(domain string, d time.Duration) (*policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.block, nil
}

func (f *fakeBlocker) ActiveBlock(domain string) *policy.Block { return f.active }

func (f *fakeBlocker) BlockAllInternet(allowlist []string, d time.Duration) (*policy.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.block, nil
}

type fakeDNSController struct {
	started  bool
	upstream string
	err      error
}

func (f *fakeDNSController) Start() error {
	if f.err != nil {
		return f.err
	}
	f.started = true
	return nil
}

func (f *fakeDNSController) Stop() error {
	f.started = false
	return nil
}

func (f *fakeDNSController) SetUpstream(u string) error {
	if f.err != nil {
		return f.err
	}
	f.upstream = u
	return nil
}

func (f *fakeDNSController) Status() dnsserver.Status {
	return dnsserver.Status{Listening: f.started, Upstream: f.upstream}
}

type fakeDNSPersister struct {
	enabled  bool
	upstream string
	err      error
}

func (f *fakeDNSPersister) SetDNSEnabled(v bool) error {
	if f.err != nil {
		return f.err
	}
	f.enabled = v
	return nil
}

func (f *fakeDNSPersister) SetDNSUpstream(u string) error {
	if f.err != nil {
		return f.err
	}
	f.upstream = u
	return nil
}

func (f *fakeDNSPersister) DNSEnabled() bool { return f.enabled }

// composeTestServer mounts the 19 domain handlers (como o daemon faz) sobre o
// NewServer (que registra os 15 de nível servidor) — o conjunto completo de 34
// ações que o ValidateRegistry exige no boot.
func composeTestServer(t *testing.T) (*ipc.Server, *fakeBlocker, *fakeDNSPersister) {
	t.Helper()
	s := ipc.NewServer(nil)

	cat := preset.NewStore(t.TempDir() + "/presets.json")
	goalStore := goal.NewStore(t.TempDir() + "/goal.json")
	userStore := user.NewStore(t.TempDir() + "/user.json")
	appsStore := apps.NewStore(t.TempDir() + "/apps.json")
	blk := &fakeBlocker{}
	dc := &fakeDNSController{upstream: dnsserver.DefaultUpstream}
	dp := &fakeDNSPersister{}

	s.Register(blocks.New(blk, cat))
	s.Register(blocks.NewBlockAll(blk))
	s.Register(presets.NewList(cat))
	s.Register(presets.NewAdd(cat))
	s.Register(presets.NewRemove(cat))
	s.Register(goal.NewGet(goalStore))
	s.Register(goal.NewSet(goalStore))
	s.Register(dns.NewStart(dc, dp, nil))
	s.Register(dns.NewStop(dc, dp))
	s.Register(dns.NewStatus(dc, dp))
	s.Register(dns.NewSetUpstream(dc, dp))
	s.Register(users.NewList(userStore))
	s.Register(users.NewVerify(userStore))
	s.Register(users.NewAdd(userStore))
	s.Register(users.NewRemove(userStore))
	s.Register(users.NewSetPassword(userStore))
	s.Register(apps.NewList(appsStore))
	s.Register(apps.NewAdd(appsStore))
	s.Register(apps.NewRemove(appsStore))
	return s, blk, dp
}

// TestDomainWiring_ComposesWithRouter cobre o caminho de produção dos handlers
// de domínio pelo roteador real: mensagens, códigos estáveis e o fechamento
// specs↔registry (boot check do daemon).
func TestDomainWiring_ComposesWithRouter(t *testing.T) {
	s, blk, dp := composeTestServer(t)

	if err := s.ValidateRegistry(); err != nil {
		t.Fatalf("ValidateRegistry: %v", err)
	}

	now := time.Now()
	blk.block = &policy.Block{Domain: "x.com", StartedAt: now, ExpiresAt: now.Add(time.Hour)}

	// block → mensagem de sucesso do handler de domínio.
	resp := s.Dispatch(&ipc.Request{Action: "block", Domain: "x.com", Duration: "1h"})
	if !resp.Success || !strings.Contains(resp.Message, "Domain x.com blocked") {
		t.Fatalf("block: success=%v msg=%q", resp.Success, resp.Message)
	}

	// Conflito ask-first (domínio já ativo) → CodeDomainConflict + ConflictBlock.
	blk.active = &policy.Block{Domain: "x.com", StartedAt: now, ExpiresAt: now.Add(time.Hour)}
	resp = s.Dispatch(&ipc.Request{Action: "block", Domain: "x.com", Duration: "1h"})
	if resp.Success || resp.Code != ipc.CodeDomainConflict || !resp.Conflict {
		t.Fatalf("conflito: success=%v code=%q conflict=%v", resp.Success, resp.Code, resp.Conflict)
	}

	// Duração inválida → código estável (mesmo do switch legado).
	resp = s.Dispatch(&ipc.Request{Action: "block", Domain: "x.com", Duration: "bad"})
	if resp.Success || resp.Code != ipc.CodeDurationInvalid {
		t.Fatalf("duração inválida: code=%q", resp.Code)
	}

	// user-set-password → fail-fast do handler de domínio (senha curta).
	resp = s.Dispatch(&ipc.Request{Action: "user-set-password", UserName: "maria", UserPassword: "curta"})
	if resp.Success || resp.Code != ipc.CodeInvalid {
		t.Fatalf("user-set-password curta: code=%q msg=%q", resp.Code, resp.Message)
	}

	// dns-start → persiste o flag e devolve a mensagem de sucesso.
	resp = s.Dispatch(&ipc.Request{Action: "dns-start"})
	if !resp.Success || !dp.enabled || !strings.Contains(resp.Message, "Servidor DNS iniciado") {
		t.Fatalf("dns-start: success=%v enabled=%v msg=%q", resp.Success, dp.enabled, resp.Message)
	}

	// goal-set com store real → meta refletida na resposta.
	resp = s.Dispatch(&ipc.Request{Action: "goal-set", GoalMinutes: 120})
	if !resp.Success || resp.Goal != 120*time.Minute {
		t.Fatalf("goal-set: success=%v goal=%v", resp.Success, resp.Goal)
	}

	// Ação desconhecida → CodeUnknownAction + mensagem legada preservada.
	resp = s.Dispatch(&ipc.Request{Action: "nope"})
	if resp.Success || resp.Code != ipc.CodeUnknownAction || resp.Message != "Not suported action: nope" {
		t.Fatalf("desconhecida: success=%v code=%q msg=%q", resp.Success, resp.Code, resp.Message)
	}
}

// TestDomainWiring_AllActionsDispatch dispara TODAS as 19 ações de domínio
// contra o roteador real (handlers de produção), prendendo o shape do wire de
// cada família — a rede de segurança contra drift entre os adapters de
// referência (testes internos) e os handlers reais (daemon).
func TestDomainWiring_AllActionsDispatch(t *testing.T) {
	s, blk, _ := composeTestServer(t)

	now := time.Now()
	blk.block = &policy.Block{Domain: "x.com", StartedAt: now, ExpiresAt: now.Add(time.Hour)}

	cases := []struct {
		name   string
		req    ipc.Request
		wantOK bool
	}{
		{name: "block", req: ipc.Request{Action: "block", Domain: "x.com", Duration: "1h"}, wantOK: true},
		{name: "block-all", req: ipc.Request{Action: "block-all", Duration: "1h"}, wantOK: true},
		{name: "presets", req: ipc.Request{Action: "presets"}, wantOK: true},
		{name: "preset-add", req: ipc.Request{Action: "preset-add", PresetName: "meu", PresetDomains: []string{"a.com"}}, wantOK: true},
		{name: "preset-remove", req: ipc.Request{Action: "preset-remove", PresetName: "meu"}, wantOK: true},
		{name: "apps-list", req: ipc.Request{Action: "apps-list"}, wantOK: true},
		{name: "apps-add", req: ipc.Request{Action: "apps-add", AppName: "steam.exe"}, wantOK: true},
		{name: "apps-remove", req: ipc.Request{Action: "apps-remove", AppName: "steam.exe"}, wantOK: true},
		{name: "goal-get", req: ipc.Request{Action: "goal-get"}, wantOK: true},
		{name: "goal-set", req: ipc.Request{Action: "goal-set", GoalMinutes: 60}, wantOK: true},
		{name: "user-list", req: ipc.Request{Action: "user-list"}, wantOK: true},
		{name: "user-add", req: ipc.Request{Action: "user-add", UserName: "maria", UserPassword: "senha-forte-1"}, wantOK: true},
		{name: "user-verify-ok", req: ipc.Request{Action: "user-verify", UserName: "maria", UserPassword: "senha-forte-1"}, wantOK: true},
		{name: "user-set-password", req: ipc.Request{Action: "user-set-password", UserName: "maria", UserPassword: "nova-senha-123"}, wantOK: true},
		{name: "user-remove", req: ipc.Request{Action: "user-remove", UserName: "maria"}, wantOK: true},
		{name: "user-verify-fail", req: ipc.Request{Action: "user-verify", UserName: "maria", UserPassword: "senha-forte-1"}, wantOK: false},
		{name: "dns-start", req: ipc.Request{Action: "dns-start"}, wantOK: true},
		{name: "dns-stop", req: ipc.Request{Action: "dns-stop"}, wantOK: true},
		{name: "dns-status", req: ipc.Request{Action: "dns-status"}, wantOK: true},
		{name: "dns-set-upstream", req: ipc.Request{Action: "dns-set-upstream", Upstream: "9.9.9.9"}, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.Dispatch(&tc.req)
			if resp.Success != tc.wantOK {
				t.Fatalf("%s: success=%v msg=%q", tc.name, resp.Success, resp.Message)
			}
		})
	}
}

// TestDomainWiring_NoStoreFailsAsLegacy verifica que os handlers de domínio
// reproduzem os erros "não configurado" do switch legado (store ausente),
// fechando o comportamento que os adapters de referência também testam.
func TestDomainWiring_NoStoreFailsAsLegacy(t *testing.T) {
	// users.NewList(nil): mesmo CodeNotConfigured do adaptador legado.
	h := users.NewList(nil)
	resp, err := h.Handle(context.Background(), &ipc.Request{Action: "user-list"})
	if err == nil {
		t.Fatal("user-list sem store deveria falhar")
	}
	if resp != nil || err.Error() != "usuários não configurados" {
		t.Fatalf("user-list nil: resp=%v err=%v", resp, err)
	}
}
