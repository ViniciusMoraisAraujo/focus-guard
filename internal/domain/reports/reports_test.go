package reports

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"focusguard/internal/domain/analytics"
)

func TestConfig_Valid(t *testing.T) {
	if err := DefaultConfig().Valid(); err != nil {
		t.Fatalf("default deveria ser válida: %v", err)
	}
	bad := []Config{
		{DayOfWeek: 7},
		{DayOfWeek: -1},
		{Hour: 24},
		{Hour: -1},
		{Minute: 60},
	}
	for _, c := range bad {
		if err := c.Valid(); err == nil {
			t.Errorf("%+v deveria ser inválida", c)
		}
	}
}

func TestStore_PersistsConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/reports.json"
	s := NewStore(path)
	cfg := Config{Enabled: true, DayOfWeek: 0, Hour: 23, Minute: 59, ExportPath: "/tmp/reports"}
	if err := s.Set(cfg); err != nil {
		t.Fatal(err)
	}
	re := NewStore(path)
	got := re.Get()
	if !got.Enabled || got.DayOfWeek != 0 || got.Hour != 23 || got.Minute != 59 || got.ExportPath != "/tmp/reports" {
		t.Fatalf("config não persistiu: %+v", got)
	}
}

func TestStore_InvalidConfigRejected(t *testing.T) {
	s := NewStore("")
	if err := s.Set(Config{Hour: 99}); err == nil {
		t.Fatal("hora 99 deveria ser rejeitada")
	}
	if got := s.Get(); got.Hour != 23 {
		t.Fatalf("config não deveria mudar após rejeição: %+v", got)
	}
}

func TestStore_CorruptFile_DegradesToDefault(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/reports.json"
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if s.Get().Enabled {
		t.Fatal("arquivo corrompido deveria degradar para default desativado")
	}
}

func TestNextRun_SameDayLater(t *testing.T) {
	cfg := Config{DayOfWeek: int(time.Wednesday), Hour: 20, Minute: 0}
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local) // quarta
	next := cfg.NextRun(now)
	if next.Weekday() != time.Wednesday || next.Hour() != 20 || next.Minute() != 0 {
		t.Fatalf("NextRun = %v, want quarta 20:00", next)
	}
	if !next.After(now) {
		t.Fatal("NextRun deveria ser no futuro")
	}
}

func TestNextRun_NextWeekWhenPassed(t *testing.T) {
	cfg := Config{DayOfWeek: int(time.Wednesday), Hour: 20, Minute: 0}
	now := time.Date(2026, 8, 12, 21, 0, 0, 0, time.Local) // quarta, depois das 20h
	next := cfg.NextRun(now)
	if !next.After(now) || next.Day() != 19 {
		t.Fatalf("NextRun = %v, want próxima quarta (dia 19)", next)
	}
}

// TestMissedToday_* cobrem o catch-up do boot (fix v0.18.1): o worker gera o
// relatório no boot quando o horário agendado de HOJE já passou — sem isso o
// NextRun pularia para a semana seguinte e a semana ficaria sem relatório.
func TestMissedToday_FalseBeforeTime(t *testing.T) {
	cfg := Config{DayOfWeek: int(time.Wednesday), Hour: 20, Minute: 0}
	now := time.Date(2026, 8, 12, 19, 59, 0, 0, time.Local) // quarta 19:59
	if cfg.MissedToday(now) {
		t.Error("antes do horário agendado não é atraso")
	}
}

func TestMissedToday_TrueAfterTimeSameDay(t *testing.T) {
	cfg := Config{DayOfWeek: int(time.Wednesday), Hour: 20, Minute: 0}
	now := time.Date(2026, 8, 12, 20, 1, 0, 0, time.Local) // quarta 20:01
	if !cfg.MissedToday(now) {
		t.Error("depois do horário agendado no mesmo dia deveria ser atraso")
	}
}

func TestMissedToday_TrueAtExactTime(t *testing.T) {
	cfg := Config{DayOfWeek: int(time.Wednesday), Hour: 20, Minute: 0}
	now := time.Date(2026, 8, 12, 20, 0, 0, 0, time.Local) // exatamente 20:00
	if !cfg.MissedToday(now) {
		t.Error("boot no minuto exato deveria ser atraso (senão o NextRun pula a semana)")
	}
}

func TestMissedToday_FalseOtherDay(t *testing.T) {
	cfg := Config{DayOfWeek: int(time.Wednesday), Hour: 20, Minute: 0}
	now := time.Date(2026, 8, 13, 21, 0, 0, 0, time.Local) // quinta 21:00
	if cfg.MissedToday(now) {
		t.Error("outro dia não é atraso do mesmo dia")
	}
}

func TestMissedToday_FalseEarlierWeekday(t *testing.T) {
	cfg := DefaultConfig()                                // domingo 23:59
	now := time.Date(2026, 8, 10, 0, 1, 0, 0, time.Local) // segunda 00:01
	if cfg.MissedToday(now) {
		t.Error("dia seguinte não é atraso do mesmo dia")
	}
}

func TestNextRun_SundayDefault(t *testing.T) {
	cfg := DefaultConfig() // domingo 23:59
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	next := cfg.NextRun(now)
	if next.Weekday() != time.Sunday {
		t.Fatalf("NextRun = %v, want domingo", next)
	}
}

// fakeProvider devolve sessões fixas (uma sessão na semana de referência).
type fakeProvider struct{ sessions []analytics.Session }

func (f *fakeProvider) Sessions() ([]analytics.Session, error) { return f.sessions, nil }

func TestGenerate_WritesHTMLAndJSON(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	p := &fakeProvider{sessions: []analytics.Session{
		{Start: now.Add(-2 * time.Hour), End: now.Add(-1 * time.Hour), Focus: time.Hour, Domains: []string{"youtube.com"}},
	}}
	cfg := Config{ExportPath: dir}

	htmlPath, jsonPath, err := Generate(p, cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	if htmlPath == "" || jsonPath == "" {
		t.Fatal("Generate deveria devolver os caminhos")
	}
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(html)[:15] != "<!DOCTYPE html>" {
		t.Error("relatório HTML deveria ser self-contained")
	}
	js, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(js) == "" {
		t.Error("JSON vazio")
	}
	// A semana de referência entra no nome do arquivo.
	year, week := now.ISOWeek()
	want := filepath.Join(dir, "focusguard-2026-W33.html")
	if year != 2026 || week != 33 {
		t.Skipf("semana ISO diferente (%d-W%02d) — nome esperado muda", year, week)
	}
	if htmlPath != want {
		t.Errorf("htmlPath = %s, want %s", htmlPath, want)
	}
}

func TestGenerate_NoSessionsStillWrites(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Generate(&fakeProvider{}, Config{ExportPath: dir}, time.Now())
	if err != nil {
		t.Fatalf("relatório sem sessões deveria escrever mesmo assim: %v", err)
	}
}

func TestGenerate_NilProvider(t *testing.T) {
	if _, _, err := Generate(nil, Config{ExportPath: t.TempDir()}, time.Now()); err == nil {
		t.Fatal("provider nil deveria falhar")
	}
}

func TestGenerate_ExpandsHome(t *testing.T) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		got := expandHome("~/FocusGuardReports")
		if got == "~/FocusGuardReports" {
			t.Fatal("~ deveria ser expandido")
		}
		if filepath.Dir(got) != home {
			t.Errorf("expandHome = %s, want sob %s", got, home)
		}
	}
}

func TestHandlers_ConfigGetSetGenerate(t *testing.T) {
	s := NewStore(t.TempDir() + "/reports.json")
	dir := t.TempDir()

	hGet := NewConfigGet(s)
	res, err := hGet.Handle(context.Background(), &NoInput{})
	if err != nil || res.Config.Enabled {
		t.Fatalf("config inicial: %+v err=%v", res, err)
	}

	hSet := NewConfigSet(s)
	set, err := hSet.Handle(context.Background(), &ConfigInput{Config: Config{Enabled: true, DayOfWeek: 5, Hour: 8, Minute: 0, ExportPath: dir}})
	if err != nil {
		t.Fatal(err)
	}
	if !set.Config.Enabled || set.Config.DayOfWeek != 5 {
		t.Fatalf("config set não refletida: %+v", set.Config)
	}

	hGen := NewGenerate(s, &fakeProvider{})
	gen, err := hGen.Handle(context.Background(), &GenerateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if gen.HTMLPath == "" || gen.Message == "" {
		t.Fatalf("generate: %+v", gen)
	}
	if _, err := os.Stat(gen.HTMLPath); err != nil {
		t.Fatalf("arquivo gerado não existe: %v", err)
	}
}

func TestHandler_NoStore_NotConfigured(t *testing.T) {
	if _, err := NewConfigGet(nil).Handle(context.Background(), &NoInput{}); err == nil {
		t.Fatal("reports-config-get sem store deveria falhar")
	}
	if _, err := NewGenerate(nil, &fakeProvider{}).Handle(context.Background(), &GenerateInput{}); err == nil {
		t.Fatal("reports-generate sem store deveria falhar")
	}
}
