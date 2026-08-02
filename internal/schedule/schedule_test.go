package schedule

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"focusguard/internal/policy"
)

func TestManager_AddGeneratesIDAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	m := NewManager(path)

	r, err := m.Add(Rule{
		Label:   "Estudo matinal",
		Preset:  "social",
		Days:    []int{1, 2, 3, 4, 5},
		Start:   "08:00",
		End:     "12:00",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if r.ID == "" {
		t.Fatal("Add deve gerar um ID")
	}

	// um novo manager no mesmo arquivo deve enxergar a regra persistida
	m2 := NewManager(path)
	list := m2.List()
	if len(list) != 1 {
		t.Fatalf("persistência: esperava 1 regra após reload, got %d", len(list))
	}
	if list[0].ID != r.ID || list[0].Preset != "social" {
		t.Errorf("regra recarregada inesperada: %+v", list[0])
	}
}

func TestManager_AddValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	m := NewManager(path)

	base := Rule{Preset: "social", Days: []int{1}, Start: "08:00", End: "12:00", Enabled: true}

	cases := []struct {
		name string
		mut  func(*Rule)
	}{
		{"preset vazio", func(r *Rule) { r.Preset = "" }},
		{"dias vazios", func(r *Rule) { r.Days = nil }},
		{"dia inválido", func(r *Rule) { r.Days = []int{7} }},
		{"dia negativo", func(r *Rule) { r.Days = []int{-1} }},
		{"start inválido", func(r *Rule) { r.Start = "25:00" }},
		{"start malformado", func(r *Rule) { r.Start = "oito" }},
		{"end inválido", func(r *Rule) { r.End = "12:60" }}, {"end igual ao start", func(r *Rule) { r.End = "08:00" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mut(&r)
			if _, err := m.Add(r); err == nil {
				t.Errorf("esperava erro de validação para %q", tc.name)
			}
		})
	}
}

func TestManager_Remove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	m := NewManager(path)

	r, err := m.Add(Rule{Preset: "video", Days: []int{6}, Start: "20:00", End: "23:00", Enabled: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := m.Remove(r.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := len(m.List()); got != 0 {
		t.Errorf("esperava 0 regras após remover, got %d", got)
	}
	if err := m.Remove(r.ID); err == nil {
		t.Error("remover regra inexistente deve falhar")
	}
}

func TestManager_DisabledRuleNotActive(t *testing.T) {
	m := NewManager("")
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC) // segunda-feira 10:00
	r, err := m.Add(Rule{Preset: "social", Days: []int{1}, Start: "08:00", End: "12:00", Enabled: false})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	_ = r
	if got := m.ActiveRules(now); len(got) != 0 {
		t.Errorf("regra desabilitada não deve estar ativa, got %d", len(got))
	}
}

func TestManager_ActiveRules_DayAndWindow(t *testing.T) {
	m := NewManager("")
	_, _ = m.Add(Rule{Preset: "social", Days: []int{1, 2}, Start: "08:00", End: "12:00", Enabled: true})

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"segunda 10h ativa", time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC), true},
		{"segunda 07:59 inativa", time.Date(2026, 8, 3, 7, 59, 0, 0, time.UTC), false},
		{"segunda 08:00 ativa (inclusive)", time.Date(2026, 8, 3, 8, 0, 0, 0, time.UTC), true},
		{"segunda 12:00 inativa (exclusive)", time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC), false},
		{"terça 09h ativa", time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC), true},
		{"quarta 10h inativa (dia fora)", time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC), false},
		{"domingo 10h inativa", time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(m.ActiveRules(tc.now)) == 1
			if got != tc.want {
				t.Errorf("ActiveRules(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestManager_ActiveRules_Overnight(t *testing.T) {
	m := NewManager("")
	// 22:00 → 02:00: vira de noite. Dias = {1} (segunda) = noite de seg p/ ter.
	_, _ = m.Add(Rule{Preset: "games", Days: []int{1}, Start: "22:00", End: "02:00", Enabled: true})

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"seg 23h ativa", time.Date(2026, 8, 3, 23, 0, 0, 0, time.UTC), true},
		{"seg 21:59 inativa", time.Date(2026, 8, 3, 21, 59, 0, 0, time.UTC), false},
		{"ter 01h ativa (virada)", time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC), true},
		{"ter 02:00 inativa (exclusive)", time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC), false},
		{"qua 01h inativa (dia errado)", time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(m.ActiveRules(tc.now)) == 1
			if got != tc.want {
				t.Errorf("ActiveRules(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestManager_CorruptFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	if err := os.WriteFile(path, []byte("{corrompido"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(path)
	if got := len(m.List()); got != 0 {
		t.Errorf("arquivo corrompido deve resultar em catálogo vazio, got %d", got)
	}
}

func TestManager_ListReturnsCopies(t *testing.T) {
	m := NewManager("")
	_, _ = m.Add(Rule{Preset: "news", Days: []int{1}, Start: "08:00", End: "12:00", Enabled: true})

	list := m.List()
	if len(list) != 1 {
		t.Fatalf("esperava 1 regra, got %d", len(list))
	}
	// mutar a cópia não pode afetar o manager
	list[0].Preset = "hacked"
	if got := m.List()[0].Preset; got != "news" {
		t.Errorf("List deve retornar cópias, preset = %q", got)
	}
}

// ---------------------------------------------------------------------------
// Múltiplas janelas por regra (--windows)
// ---------------------------------------------------------------------------

func TestManager_ActiveRules_MultipleWindows(t *testing.T) {
	m := NewManager("")
	_, _ = m.Add(Rule{
		Preset:  "social",
		Days:    []int{1},
		Windows: []string{"08:00-12:00", "14:00-18:00"},
		Enabled: true,
	})

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"seg 10h ativa (1ª janela)", time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC), true},
		{"seg 13h inativa (entre janelas)", time.Date(2026, 8, 3, 13, 0, 0, 0, time.UTC), false},
		{"seg 15h ativa (2ª janela)", time.Date(2026, 8, 3, 15, 0, 0, 0, time.UTC), true},
		{"seg 12:00 inativa (exclusive)", time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC), false},
		{"seg 18:00 inativa (exclusive)", time.Date(2026, 8, 3, 18, 0, 0, 0, time.UTC), false},
		{"ter 10h inativa (dia fora)", time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(m.ActiveRules(tc.now)) == 1
			if got != tc.want {
				t.Errorf("ActiveRules(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestManager_ActiveRules_WindowsOvernight(t *testing.T) {
	m := NewManager("")
	// janela overnight dentro das Windows
	_, _ = m.Add(Rule{Preset: "games", Days: []int{1}, Windows: []string{"22:00-06:00"}, Enabled: true})

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"seg 23h ativa", time.Date(2026, 8, 3, 23, 0, 0, 0, time.UTC), true},
		{"ter 01h ativa (virada)", time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC), true},
		{"ter 06:00 inativa (exclusive)", time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC), false},
		{"qua 01h inativa (dia errado)", time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(m.ActiveRules(tc.now)) == 1
			if got != tc.want {
				t.Errorf("ActiveRules(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestManager_Add_WindowsValidation(t *testing.T) {
	m := NewManager("")
	cases := []struct {
		name    string
		windows []string
	}{
		{"janela malformada", []string{"oito"}},
		{"janela sem fim", []string{"08:00"}},
		{"hora inválida", []string{"25:00-12:00"}},
		{"minuto inválido", []string{"08:60-12:00"}},
		{"start igual ao end", []string{"08:00-08:00"}},
		{"uma boa e uma ruim", []string{"08:00-12:00", "oito"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Add(Rule{Preset: "social", Days: []int{1}, Windows: tc.windows, Enabled: true})
			if err == nil {
				t.Errorf("esperava erro de validação para %q", tc.name)
			}
		})
	}
}

func TestManager_Add_WindowsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	m := NewManager(path)
	r, err := m.Add(Rule{Preset: "social", Days: []int{1}, Windows: []string{"08:00-12:00", "14:00-18:00"}, Enabled: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m2 := NewManager(path)
	list := m2.List()
	if len(list) != 1 {
		t.Fatalf("persistência: got %d regras", len(list))
	}
	if len(list[0].Windows) != 2 || list[0].Windows[0] != r.Windows[0] {
		t.Errorf("Windows não persistidas: %+v", list[0].Windows)
	}
}

func TestApplyActiveRules_MultipleWindows_DurationToWindowEnd(t *testing.T) {
	m := NewManager("")
	_, _ = m.Add(Rule{Preset: "social", Days: []int{1}, Windows: []string{"08:00-12:00", "14:00-18:00"}, Enabled: true})
	now := time.Date(2026, 8, 3, 15, 30, 0, 0, time.UTC) // seg 15:30 (2ª janela)

	b := &fakeBlocker{}
	n, err := ApplyActiveRules(m, fakeResolver{map[string][]string{"social": {"twitter.com"}}}, b, now)
	if err != nil {
		t.Fatalf("ApplyActiveRules: %v", err)
	}
	if n != 1 || len(b.calls) != 1 {
		t.Fatalf("esperava 1 aplicação, got n=%d calls=%d", n, len(b.calls))
	}
	// 15:30 → 18:00 = 2h30m
	if b.calls[0].duration != 2*time.Hour+30*time.Minute {
		t.Errorf("duration = %v, want 2h30m (até o fim da janela atual)", b.calls[0].duration)
	}
}

// --- worker (ApplyActiveRules) ---

type fakeBlocker struct {
	blocks []string // domínios considerados já bloqueados
	calls  []blockCall
}

type blockCall struct {
	domains  []string
	duration time.Duration
}

func (f *fakeBlocker) ListBlocks() ([]policy.Block, error) {
	out := make([]policy.Block, 0, len(f.blocks))
	for _, d := range f.blocks {
		out = append(out, policy.Block{Domain: d})
	}
	return out, nil
}

func (f *fakeBlocker) BlockDomains(domains []string, duration time.Duration) ([]policy.Block, error) {
	f.calls = append(f.calls, blockCall{domains: append([]string(nil), domains...), duration: duration})
	return nil, nil
}

type fakeResolver struct {
	m map[string][]string
}

func (f fakeResolver) Resolve(name string) ([]string, error) {
	return f.m[name], nil
}

func TestApplyActiveRules_BlocksDueRules(t *testing.T) {
	m := NewManager("")
	_, _ = m.Add(Rule{Preset: "social", Days: []int{1}, Start: "08:00", End: "12:00", Enabled: true})
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC) // seg 09h

	b := &fakeBlocker{}
	n, err := ApplyActiveRules(m, fakeResolver{map[string][]string{"social": {"twitter.com", "facebook.com"}}}, b, now)
	if err != nil {
		t.Fatalf("ApplyActiveRules: %v", err)
	}
	if n != 1 {
		t.Errorf("esperava 1 regra aplicada, got %d", n)
	}
	if len(b.calls) != 1 {
		t.Fatalf("esperava 1 BlockDomains, got %d", len(b.calls))
	}
	call := b.calls[0]
	if len(call.domains) != 2 || call.domains[0] != "twitter.com" || call.domains[1] != "facebook.com" {
		t.Errorf("domains inesperados: %v", call.domains)
	}
	// 09:00 → 12:00 = 3h
	if call.duration != 3*time.Hour {
		t.Errorf("duration = %v, want 3h", call.duration)
	}
}

func TestApplyActiveRules_SkipsAlreadyBlocked(t *testing.T) {
	m := NewManager("")
	_, _ = m.Add(Rule{Preset: "social", Days: []int{1}, Start: "08:00", End: "12:00", Enabled: true})
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	b := &fakeBlocker{blocks: []string{"twitter.com", "facebook.com"}} // já bloqueados
	n, err := ApplyActiveRules(m, fakeResolver{map[string][]string{"social": {"twitter.com", "facebook.com"}}}, b, now)
	if err != nil {
		t.Fatalf("ApplyActiveRules: %v", err)
	}
	if n != 0 {
		t.Errorf("regra com domínios já bloqueados não deve ser reaplicada, got %d", n)
	}
	if len(b.calls) != 0 {
		t.Errorf("não deve chamar BlockDomains, got %d chamadas", len(b.calls))
	}
}

func TestApplyActiveRules_DeduplicatesAcrossRules(t *testing.T) {
	m := NewManager("")
	_, _ = m.Add(Rule{Preset: "social", Days: []int{1}, Start: "08:00", End: "12:00", Enabled: true})
	_, _ = m.Add(Rule{Preset: "social", Days: []int{1}, Start: "09:00", End: "13:00", Enabled: true})
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)

	b := &fakeBlocker{}
	n, err := ApplyActiveRules(m, fakeResolver{map[string][]string{"social": {"twitter.com"}}}, b, now)
	if err != nil {
		t.Fatalf("ApplyActiveRules: %v", err)
	}
	if n != 1 {
		t.Errorf("duas regras com o mesmo preset devem resultar em 1 aplicação, got %d", n)
	}
	if len(b.calls) != 1 {
		t.Errorf("esperava 1 BlockDomains deduplicado, got %d", len(b.calls))
	}
}

func TestApplyActiveRules_NoRulesNoCalls(t *testing.T) {
	m := NewManager("")
	b := &fakeBlocker{}
	n, err := ApplyActiveRules(m, fakeResolver{map[string][]string{}}, b, time.Now())
	if err != nil {
		t.Fatalf("ApplyActiveRules: %v", err)
	}
	if n != 0 || len(b.calls) != 0 {
		t.Errorf("sem regras ativas não deve bloquear nada (n=%d, calls=%d)", n, len(b.calls))
	}
}
