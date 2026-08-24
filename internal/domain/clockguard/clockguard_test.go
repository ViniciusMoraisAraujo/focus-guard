package clockguard

import (
	"sync"
	"testing"
	"time"
)

// fakeState guarda a referência do relógio (espelho do scheduler).
type fakeState struct {
	mu   sync.Mutex
	last time.Time
}

func (f *fakeState) LastKnownTime() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func (f *fakeState) SetLastKnownTime(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = t
	return nil
}

// fakeNTP responde um horário fixo ou um erro.
type fakeNTP struct {
	t   time.Time
	err error
}

func (f *fakeNTP) Time() (time.Time, error) {
	if f.err != nil {
		return time.Now(), f.err
	}
	return f.t, nil
}

// fakeLogger acumula eventos de tamper.
type fakeLogger struct {
	mu  sync.Mutex
	log []string
}

func (f *fakeLogger) Log(source, action, detail string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, source+"/"+action)
}

func (f *fakeLogger) events() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

// guardWith monta um guard com os fakes e um relógio controlado. ntp é a
// INTERFACE (não o ponteiro): passar nil como NTPClient(nil) mantém a
// interface nula de verdade — um *fakeNTP(nil) boxed seria uma interface
// não-nula com ponteiro nil (typed-nil), que o guard trataria como cliente
// presente.
func guardWith(now, last time.Time, ntp NTPClient) (*Guard, *fakeState, *fakeLogger) {
	st := &fakeState{last: last}
	lg := &fakeLogger{}
	g := New(Deps{
		State: st, NTP: ntp,
		Now: func() time.Time { return now }, Logger: lg,
	})
	return g, st, lg
}

func TestFirstRunStampsReference(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	g, st, _ := guardWith(now, time.Time{}, nil) // last zero → primeira execução

	out := g.Check()
	if out.Suspicion || out.Confirmed {
		t.Fatalf("primeira execução não pode gerar suspeita: %+v", out)
	}
	if st.LastKnownTime().Unix() != now.Unix() {
		t.Errorf("referência não gravada: got %v, want %v", st.LastKnownTime(), now)
	}
}

func TestConsistentClockPasses(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	last := now.Add(-2 * time.Minute) // dentro da tolerância
	g, st, _ := guardWith(now, last, &fakeNTP{t: now})

	out := g.Check()
	if out.Suspicion || out.Confirmed {
		t.Fatalf("gap pequeno não pode gerar suspeita: %+v", out)
	}
	// A referência deslizou para a leitura atual.
	if st.LastKnownTime().Unix() != now.Unix() {
		t.Errorf("referência não deslizou: got %v", st.LastKnownTime())
	}
}

func TestClockRewoundConfirmedByNTP(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	// Usuário voltou o relógio 2h (de 12:00 para 10:00); NTP diz 12:00.
	local := real.Add(-2 * time.Hour)
	last := real // última leitura confiada era 12:00 (antes da burla)
	g, st, lg := guardWith(local, last, &fakeNTP{t: real})

	out := g.Check()
	if !out.Suspicion || !out.Confirmed {
		t.Fatalf("burla confirmada esperada: %+v", out)
	}
	// Offset exposto para o daemon ajustar as expirações (local − real).
	if want := local.Sub(real); out.Offset != want {
		t.Errorf("Offset = %v, want %v", out.Offset, want)
	}
	if len(lg.events()) != 1 {
		t.Errorf("tamper não registrado: %v", lg.events())
	}
	// Referência re-anchorada no horário real.
	if st.LastKnownTime().Unix() != real.Unix() {
		t.Errorf("referência não re-anchorada no NTP: got %v, want %v", st.LastKnownTime(), real)
	}
}

func TestClockAdvancedConfirmedByNTP(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	// Usuário adiantou o relógio 1 dia (12:00 → amanhã 12:00) para os
	// bloqueios expirarem cedo; NTP diz 12:00.
	local := real.Add(24 * time.Hour)
	last := real
	g, _, lg := guardWith(local, last, &fakeNTP{t: real})

	out := g.Check()
	if !out.Suspicion || !out.Confirmed {
		t.Fatalf("burla por adiantamento deveria ser confirmada: %+v", out)
	}
	if want := local.Sub(real); out.Offset != want {
		t.Errorf("Offset = %v, want %v", out.Offset, want)
	}
	if len(lg.events()) != 1 {
		t.Errorf("tamper não registrado: %v", lg.events())
	}
}

// TestConfirmedDedup_SameOffsetLogsOnce: um relógio PERSISTENTEMENTE fora
// (dual boot com RTC em padrão errado) é confirmado a cada Check — o
// tamper-log não pode espalhar um evento a cada 10 min para uma máquina cujo
// usuário não fez nada. Só um offset NOVO (relógio mexido de novo) gera outro
// evento.
func TestConfirmedDedup_SameOffsetLogsOnce(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(-3 * time.Hour) // dual boot: relógio fixo 3h atrás do real
	last := real
	st := &fakeState{last: last}
	lg := &fakeLogger{}
	g := New(Deps{State: st, NTP: &fakeNTP{t: real}, Now: func() time.Time { return local }, Logger: lg})

	for i := 0; i < 3; i++ {
		out := g.Check()
		if !out.Suspicion || !out.Confirmed {
			t.Fatalf("check %d: divergência persistente deveria ser confirmada: %+v", i, out)
		}
	}
	if len(lg.events()) != 1 {
		t.Errorf("offset estável deveria logar UMA vez, got %v", lg.events())
	}

	// Usuário mexe o relógio de novo (offset novo) → novo evento.
	g2 := New(Deps{State: st, NTP: &fakeNTP{t: real}, Now: func() time.Time { return real.Add(-5 * time.Hour) }, Logger: lg})
	if out := g2.Check(); !out.Confirmed {
		t.Fatalf("offset novo deveria ser confirmado: %+v", out)
	}
	if len(lg.events()) != 2 {
		t.Errorf("offset novo deveria registrar novo evento, got %v", lg.events())
	}
}

func TestClockJumpValidatedByNTPIsLegit(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	// Relógio local adiantou 3h (DST manual), mas NTP diz que a hora REAL é
	// a local (o SO foi ajustado junto, ex.: viagem de fuso) → legítimo.
	local := real.Add(3 * time.Hour)
	last := real.Add(-time.Hour)
	g, st, lg := guardWith(local, last, &fakeNTP{t: local})

	out := g.Check()
	if out.Confirmed {
		t.Fatalf("NTP validou — não deveria ser confirmação de burla: %+v", out)
	}
	if len(lg.events()) != 0 {
		t.Errorf("ajuste legítimo não deveria registrar evento, got %v", lg.events())
	}
	// Referência re-anchorada no horário do NTP (= local neste caso).
	if st.LastKnownTime().Unix() != local.Unix() {
		t.Errorf("referência não re-anchorada: got %v, want %v", st.LastKnownTime(), local)
	}
}

func TestNTPOfflineSuspicionNoAction(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(-2 * time.Hour) // relógio voltou 2h
	last := real
	g, st, _ := guardWith(local, last, NTPClient(nil)) // NTP nil = indisponível

	out := g.Check()
	if !out.Suspicion {
		t.Fatalf("suspeita deveria ser detectada sem NTP: %+v", out)
	}
	if out.Confirmed {
		t.Fatalf("sem NTP não pode confirmar: %+v", out)
	}
	// Referência NÃO muda (NTP não validou).
	if st.LastKnownTime().Unix() != real.Unix() {
		t.Errorf("referência não deveria ser alterada sem NTP: got %v", st.LastKnownTime())
	}
}

func TestNTPFailureSuspicionNoAction(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(-2 * time.Hour)
	last := real
	g, st, _ := guardWith(local, last, &fakeNTP{err: errFakeNTP})

	out := g.Check()
	if !out.Suspicion {
		t.Fatalf("suspeita deveria ser detectada quando NTP falha: %+v", out)
	}
	if out.Confirmed {
		t.Fatalf("falha NTP não pode confirmar: %+v", out)
	}
	// Referência NÃO muda.
	if st.LastKnownTime().Unix() != real.Unix() {
		t.Errorf("referência não deveria ser alterada com NTP falho: got %v", st.LastKnownTime())
	}
}

var errFakeNTP = &fakeNTPErr{}

type fakeNTPErr struct{}

func (e *fakeNTPErr) Error() string { return "ntp: timeout" }
