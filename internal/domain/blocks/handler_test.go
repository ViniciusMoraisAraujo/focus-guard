package blocks

import (
	"context"
	"errors"
	"testing"
	"time"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/domain/policy"
	"focusguard/internal/domain/preset"
)

type fakeBlocker struct {
	active  *policy.Block
	blocked map[string]time.Time
}

func (f *fakeBlocker) Block(domain string, d time.Duration) (*policy.Block, error) {
	if f.blocked == nil {
		f.blocked = make(map[string]time.Time)
	}
	f.blocked[domain] = time.Now().Add(d)
	return &policy.Block{Domain: domain, StartedAt: time.Now(), ExpiresAt: time.Now().Add(d)}, nil
}

func (f *fakeBlocker) BlockDomains(domains []string, d time.Duration) ([]policy.Block, error) {
	var out []policy.Block
	for _, dom := range domains {
		if f.blocked == nil {
			f.blocked = make(map[string]time.Time)
		}
		if _, dup := f.blocked[dom]; dup {
			continue
		}
		f.blocked[dom] = time.Now().Add(d)
		out = append(out, policy.Block{Domain: dom, StartedAt: time.Now(), ExpiresAt: time.Now().Add(d)})
	}
	return out, nil
}

func (f *fakeBlocker) ExtendBlock(domain string, d time.Duration) (*policy.Block, error) {
	exp := time.Now().Add(d)
	if f.active != nil && f.active.Domain == domain {
		exp = f.active.ExpiresAt.Add(d)
	}
	return &policy.Block{Domain: domain, StartedAt: time.Now(), ExpiresAt: exp}, nil
}

func (f *fakeBlocker) ActiveBlock(domain string) *policy.Block {
	if f.active != nil && f.active.Domain == domain {
		return f.active
	}
	return nil
}

func (f *fakeBlocker) BlockAllInternet(allowlist []string, d time.Duration) (*policy.Block, error) {
	return &policy.Block{Domain: "0.0.0.0", StartedAt: time.Now(), ExpiresAt: time.Now().Add(d), Allowlist: allowlist}, nil
}

type fakeCatalog struct{ p preset.Preset }

func (f fakeCatalog) Resolve(name string) (preset.Preset, error) {
	if f.p.Name == "" && name != "" {
		return preset.Preset{}, errors.New("preset desconhecido \"foo\" (disponíveis: social, video)")
	}
	return f.p, nil
}

func assertActionError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var ae *ipcerr.Error
	if !errors.As(err, &ae) || ae.Code != wantCode {
		t.Fatalf("esperava código %q, got %v", wantCode, err)
	}
}

func TestBlockValidate_DurationInvalidoAntesDoAlvo(t *testing.T) {
	h := New(&fakeBlocker{}, fakeCatalog{})
	// Duração inválida E alvo ausente: o erro de duração vence (ordem do switch).
	err := h.Validate(&BlockInput{Duration: "lixo"})
	assertActionError(t, err, ipcerr.CodeDurationInvalid)
}

func TestBlockValidate_SemAlvo(t *testing.T) {
	h := New(&fakeBlocker{}, fakeCatalog{})
	err := h.Validate(&BlockInput{Duration: "4h"})
	assertActionError(t, err, ipcerr.CodeDomainRequired)
}

func TestBlockValidate_PresetSozinhoEhAlvoValido(t *testing.T) {
	h := New(&fakeBlocker{}, fakeCatalog{})
	if err := h.Validate(&BlockInput{Duration: "4h", Preset: "social"}); err != nil {
		t.Fatalf("preset sozinho deveria passar na validação, got %v", err)
	}
}

func TestBlock_ConflitoAskFirst(t *testing.T) {
	existing := &policy.Block{Domain: "youtube.com", ExpiresAt: time.Now().Add(time.Hour)}
	h := New(&fakeBlocker{active: existing}, fakeCatalog{})
	resp, err := h.Handle(context.Background(), &BlockInput{Domain: "youtube.com", Duration: "4h"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Conflict == false || resp.ConflictBlock == nil {
		t.Fatalf("esperava conflito ask-first, got %+v", resp)
	}
	if resp.Code != ipcerr.CodeDomainConflict {
		t.Fatalf("esperava código ERR_DOMAIN_CONFLICT, got %q", resp.Code)
	}
}

func TestBlock_ReplacePulaConflito(t *testing.T) {
	existing := &policy.Block{Domain: "youtube.com", ExpiresAt: time.Now().Add(time.Hour)}
	f := &fakeBlocker{active: existing}
	h := New(f, fakeCatalog{})
	resp, err := h.Handle(context.Background(), &BlockInput{Domain: "youtube.com", Duration: "4h", Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != "" || resp.Conflict {
		t.Fatalf("--replace deveria bloquear direto, got %+v", resp)
	}
	if _, ok := f.blocked["youtube.com"]; !ok {
		t.Fatal("Block não foi chamado")
	}
}

func TestBlock_PresetResolveErro(t *testing.T) {
	h := New(&fakeBlocker{}, fakeCatalog{})
	resp, err := h.Handle(context.Background(), &BlockInput{Preset: "foo", Duration: "4h"})
	if err == nil || resp != nil {
		t.Fatalf("esperava erro de preset desconhecido, got resp=%+v err=%v", resp, err)
	}
}

func TestBlock_PresetBloqueiaLote(t *testing.T) {
	h := New(&fakeBlocker{}, fakeCatalog{p: preset.Preset{Name: "social", Domains: []string{"a.com", "b.com"}}})
	resp, err := h.Handle(context.Background(), &BlockInput{Preset: "social", Duration: "4h"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message != "Preset social bloqueado (2 domínios) até "+time.Now().Add(4*time.Hour).Local().Format("15:04:05 02/01/2006") {
		t.Fatalf("mensagem inesperada: %q", resp.Message)
	}
}

func TestBlockAll_SucessoComAllowlist(t *testing.T) {
	h := NewBlockAll(&fakeBlocker{})
	resp, err := h.Handle(context.Background(), &BlockAllInput{Duration: "2h", Allowlist: []string{"gmail.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message != "Internet bloqueada até "+time.Now().Add(2*time.Hour).Local().Format("15:04:05 02/01/2006")+" (apenas gmail.com permitido)" {
		t.Fatalf("mensagem inesperada: %q", resp.Message)
	}
}

func TestBlockAll_DuracaoInvalida(t *testing.T) {
	h := NewBlockAll(&fakeBlocker{})
	err := h.Validate(&BlockAllInput{Duration: "0s"})
	assertActionError(t, err, ipcerr.CodeDurationInvalid)
}
