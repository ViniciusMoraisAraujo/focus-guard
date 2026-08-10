package achievements

import (
	"context"
	"testing"
	"time"

	"focusguard/internal/domain/analytics"
)

func TestCalculate_EmptyStats_NoBadges(t *testing.T) {
	got := Calculate(&analytics.Stats{}, nil)
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
	got := Calculate(st, nil)
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
	if got := Calculate(st, nil); find(got, "steel-focus").Unlocked {
		t.Error("steel-focus não deveria desbloquear com 9h")
	}
	st = &analytics.Stats{TotalFocus: 10 * time.Hour}
	if got := Calculate(st, nil); !find(got, "steel-focus").Unlocked {
		t.Error("steel-focus deveria desbloquear com 10h")
	}
	if p := find(Calculate(st, nil), "steel-focus").Progress; p != 100 {
		t.Errorf("steel-focus progress = %d, want 100", p)
	}
}

func TestCalculate_ProgressPartial(t *testing.T) {
	st := &analytics.Stats{TotalFocus: 5 * time.Hour}
	a := find(Calculate(st, nil), "steel-focus")
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
	got := Calculate(st, sessions)
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
	if !find(Calculate(st, sessions), "night-guardian").Unlocked {
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
	if !find(Calculate(st, sessions), "mission-master").Unlocked {
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
	if !find(Calculate(st, sessions), "marathoner").Unlocked {
		t.Error("marathoner deveria desbloquear com 2h num único dia")
	}
}

func TestCalculate_DeepDiver(t *testing.T) {
	now := time.Now()
	sessions := []analytics.Session{{Start: now.Add(-2 * time.Hour), End: now, Focus: 90 * time.Minute}}
	st := analytics.Summarize(sessions, 7, now)
	if !find(Calculate(st, sessions), "deep-diver").Unlocked {
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
	if got := Calculate(nil, nil); len(got) != 0 {
		t.Fatalf("nil stats deveria devolver lista vazia, got %d", len(got))
	}
}

// fakeProvider devolve sessões fixas.
type fakeProvider struct{ sessions []analytics.Session }

func (f *fakeProvider) Sessions() ([]analytics.Session, error) { return f.sessions, nil }

// find localiza uma badge por ID (falha o teste se ausente).
func find(list []Achievement, id string) Achievement {
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	panic("badge " + id + " não encontrada")
}
