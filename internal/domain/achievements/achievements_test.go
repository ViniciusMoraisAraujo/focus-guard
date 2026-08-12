package achievements

import (
	"context"
	"testing"
	"time"

	"focusguard/internal/domain/analytics"
)

func TestCalculate_EmptyStats_NoBadges(t *testing.T) {
	got := Calculate(&analytics.Stats{}, nil, time.Now())
	if len(got) == 0 {
		t.Fatal("catálogo vazio? as badges são derivadas, devem sempre existir (locked)")
	}
	for _, a := range got {
		if a.Unlocked {
			t.Errorf("%s não deveria estar desbloqueada sem dados", a.ID)
		}
	}
}

func TestCalculate_FirstStepUnlocks(t *testing.T) {
	st := &analytics.Stats{TotalSessions: 1, TotalFocus: 30 * time.Minute}
	got := Calculate(st, nil, time.Now())
	byID := map[string]Achievement{}
	for _, a := range got {
		byID[a.ID] = a
	}
	if !byID["first-step"].Unlocked {
		t.Error("first-step deveria desbloquear com 1 sessão")
	}
	if byID["first-step"].Progress != 100 {
		t.Errorf("first-step progress = %d, want 100", byID["first-step"].Progress)
	}
}

func TestCalculate_SteelFocusBoundary(t *testing.T) {
	st := &analytics.Stats{TotalFocus: 9 * time.Hour}
	if got := Calculate(st, nil, time.Now()); find(got, "steel-focus").Unlocked {
		t.Error("steel-focus não deveria desbloquear com 9h")
	}
	st = &analytics.Stats{TotalFocus: 10 * time.Hour}
	if got := Calculate(st, nil, time.Now()); !find(got, "steel-focus").Unlocked {
		t.Error("steel-focus deveria desbloquear com 10h")
	}
	if p := find(Calculate(st, nil, time.Now()), "steel-focus").Progress; p != 100 {
		t.Errorf("steel-focus progress = %d, want 100", p)
	}
}

func TestCalculate_ProgressPartial(t *testing.T) {
	st := &analytics.Stats{TotalFocus: 5 * time.Hour}
	a := find(Calculate(st, nil, time.Now()), "steel-focus")
	if a.Unlocked {
		t.Fatal("5h não desbloqueia steel-focus")
	}
	if a.Progress != 50 {
		t.Errorf("steel-focus progress = %d, want 50", a.Progress)
	}
}

func TestCalculate_UnstoppableStreak(t *testing.T) {
	now := time.Now()
	var sessions []analytics.Session
	for i := 0; i < 7; i++ {
		sessions = append(sessions, analytics.Session{
			Start: now.AddDate(0, 0, -i).Add(-time.Hour),
			End:   now.AddDate(0, 0, -i),
			Focus: time.Hour,
		})
	}
	st := analytics.Summarize(sessions, 7, now)
	got := Calculate(st, sessions, now)
	if !find(got, "unstoppable").Unlocked {
		t.Error("unstoppable deveria desbloquear com raia 7")
	}
	if !find(got, "week-warrior").Unlocked {
		t.Error("week-warrior deveria desbloquear com 7 dias ativos")
	}
}

func TestCalculate_NightGuardian(t *testing.T) {
	now := time.Now()
	var sessions []analytics.Session
	for i := 0; i < 5; i++ {
		sessions = append(sessions, analytics.Session{
			Start: time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, time.Local).AddDate(0, 0, -i),
			End:   time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, time.Local).AddDate(0, 0, -i),
			Focus: time.Hour,
		})
	}
	st := analytics.Summarize(sessions, 7, now)
	if !find(Calculate(st, sessions, now), "night-guardian").Unlocked {
		t.Error("night-guardian deveria desbloquear com 5 sessões de madrugada")
	}
}

func TestCalculate_MissionMaster(t *testing.T) {
	now := time.Now()
	sessions := []analytics.Session{
		{Start: now.Add(-time.Hour), End: now, Label: "ENEM", Focus: time.Hour},
		{Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour), Label: "TCC", Focus: time.Hour},
		{Start: now.Add(-3 * time.Hour), End: now.Add(-2 * time.Hour), Label: "Curso", Focus: time.Hour},
	}
	st := analytics.Summarize(sessions, 7, now)
	if !find(Calculate(st, sessions, now), "mission-master").Unlocked {
		t.Error("mission-master deveria desbloquear com 3 missões")
	}
}

func TestCalculate_Marathoner(t *testing.T) {
	now := time.Now()
	sessions := []analytics.Session{
		{Start: now.Add(-4 * time.Hour), End: now.Add(-3 * time.Hour), Focus: time.Hour},
		{Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour), Focus: time.Hour},
	}
	st := analytics.Summarize(sessions, 7, now)
	// Uma sessão de 1h + outra de 1h no mesmo dia = 2h no dia.
	if !find(Calculate(st, sessions, now), "marathoner").Unlocked {
		t.Error("marathoner deveria desbloquear com 2h num único dia")
	}
}

func TestCalculate_DeepDiver(t *testing.T) {
	now := time.Now()
	sessions := []analytics.Session{{Start: now.Add(-2 * time.Hour), End: now, Focus: 90 * time.Minute}}
	st := analytics.Summarize(sessions, 7, now)
	if !find(Calculate(st, sessions, now), "deep-diver").Unlocked {
		t.Error("deep-diver deveria desbloquear com sessão de 90min")
	}
}

func TestHandler_WiresProvider(t *testing.T) {
	now := time.Now()
	sessions := []analytics.Session{{Start: now.Add(-time.Hour), End: now, Focus: time.Hour}}
	h := New(&fakeProvider{sessions: sessions})
	res, err := h.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Achievements) == 0 {
		t.Fatal("handler deveria devolver o catálogo")
	}
	if !find(res.Achievements, "first-step").Unlocked {
		t.Error("first-step deveria desbloquear com a sessão do fake")
	}
}

func TestHandler_NoProvider_NotConfigured(t *testing.T) {
	h := New(nil)
	if _, err := h.Handle(context.Background(), &NoInput{}); err == nil {
		t.Fatal("achievements-get sem provider deveria falhar")
	}
}

func TestCalculate_NilStats(t *testing.T) {
	if got := Calculate(nil, nil, time.Now()); len(got) != 0 {
		t.Fatalf("nil stats deveria devolver lista vazia, got %d", len(got))
	}
}

// fakeProvider devolve sessões fixas.
type fakeProvider struct{ sessions []analytics.Session }

func (f *fakeProvider) Sessions() ([]analytics.Session, error) { return f.sessions, nil }

// TestCalculate_FocusedMonth_Last30Days: o "Mês de Foco" mede o foco dos
// últimos 30 dias (janela real de um mês), NÃO o total histórico — antes
// desbloqueava cedo com 40h acumuladas de sempre (pendência INFO do
// docs/verification-plan.md).
func TestCalculate_FocusedMonth_Last30Days(t *testing.T) {
	now := time.Now()
	// 20h há 20 dias + 20h há 5 dias = 40h DENTRO dos últimos 30 dias.
	inWindow := []analytics.Session{
		{Start: now.AddDate(0, 0, -20).Add(-time.Hour), End: now.AddDate(0, 0, -20), Focus: 20 * time.Hour},
		{Start: now.AddDate(0, 0, -5).Add(-time.Hour), End: now.AddDate(0, 0, -5), Focus: 20 * time.Hour},
	}
	st := analytics.Summarize(inWindow, 7, now)
	if !find(Calculate(st, inWindow, now), "focused-month").Unlocked {
		t.Error("focused-month deveria desbloquear com 40h nos últimos 30 dias")
	}
	if p := find(Calculate(st, inWindow, now), "focused-month").Progress; p != 100 {
		t.Errorf("focused-month progress = %d, want 100", p)
	}
}

func TestCalculate_FocusedMonth_IgnoresOldSessions(t *testing.T) {
	now := time.Now()
	// 40h acumuladas, mas TODAS com mais de 30 dias: o total histórico não
	// conta para o mês (o bug antigo desbloqueava aqui).
	old := []analytics.Session{
		{Start: now.AddDate(0, 0, -31).Add(-20 * time.Hour), End: now.AddDate(0, 0, -31), Focus: 20 * time.Hour},
		{Start: now.AddDate(0, 0, -45).Add(-20 * time.Hour), End: now.AddDate(0, 0, -45), Focus: 20 * time.Hour},
	}
	st := analytics.Summarize(old, 7, now)
	if find(Calculate(st, old, now), "focused-month").Unlocked {
		t.Error("focused-month não deveria desbloquear com foco só fora dos últimos 30 dias")
	}
}

// TestCalculate_FocusedMonth_Boundary trava a fronteira da janela: a sessão
// que termina EXATAMENTE no marco de 30 dias conta (janela inclusiva, 1h das
// 40h = 2%); uma sessão que termina um segundo antes não conta (0%). O teste
// impede que uma mudança futura de comparação (After vs Before) mude o
// comportamento sem aviso.
func TestCalculate_FocusedMonth_Boundary(t *testing.T) {
	now := time.Now()
	cutoff := now.AddDate(0, 0, -30)

	// Exatamente no marco: conta (1h → 2% de 40h).
	exactly := []analytics.Session{
		{Start: cutoff.Add(-time.Hour), End: cutoff, Focus: time.Hour},
	}
	st := analytics.Summarize(exactly, 7, now)
	if p := find(Calculate(st, exactly, now), "focused-month").Progress; p != 2 {
		t.Errorf("sessão terminando exatamente no marco deveria contar (progress = %d, want 2)", p)
	}

	// Um segundo antes do marco: NÃO conta (progresso 0).
	justOutside := []analytics.Session{
		{Start: cutoff.Add(-time.Hour - time.Second), End: cutoff.Add(-time.Second), Focus: time.Hour},
	}
	st2 := analytics.Summarize(justOutside, 7, now)
	if p := find(Calculate(st2, justOutside, now), "focused-month").Progress; p != 0 {
		t.Errorf("sessão terminando um segundo antes do marco não deveria contar (progress = %d)", p)
	}
}

func TestCalculate_FocusedMonth_PartialProgress(t *testing.T) {
	now := time.Now()
	sessions := []analytics.Session{
		{Start: now.AddDate(0, 0, -2).Add(-10 * time.Hour), End: now.AddDate(0, 0, -2), Focus: 10 * time.Hour},
	}
	st := analytics.Summarize(sessions, 7, now)
	a := find(Calculate(st, sessions, now), "focused-month")
	if a.Unlocked {
		t.Fatal("10h não desbloqueia focused-month")
	}
	// 10h de 40h = 25%.
	if a.Progress != 25 {
		t.Errorf("focused-month progress = %d, want 25", a.Progress)
	}
}

// find localiza uma badge por ID (falha o teste se ausente).
func find(list []Achievement, id string) Achievement {
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	panic("badge " + id + " não encontrada")
}
