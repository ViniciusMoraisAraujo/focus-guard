package clockguard

import (
	"errors"
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

// fakeLockdown conta as chamadas de BlockAllInternet.
type fakeLockdown struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeLockdown) BlockAllInternet(_ []string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return nil
}

func (f *fakeLockdown) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
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
func guardWith(now, last time.Time, ntp NTPClient) (*Guard, *fakeState, *fakeLockdown, *fakeLogger) {
	st := &fakeState{last: last}
	lock := &fakeLockdown{}
	lg := &fakeLogger{}
	g := New(Deps{
		State: st, NTP: ntp, Lockdown: lock,
		Now: func() time.Time { return now }, Logger: lg,
	})
	return g, st, lock, lg
}

func TestFirstRunStampsReference(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	g, st, _, _ := guardWith(now, time.Time{}, nil) // last zero → primeira execução

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
	g, st, lock, _ := guardWith(now, last, &fakeNTP{t: now})

	out := g.Check()
	if out.Suspicion || out.Confirmed {
		t.Fatalf("gap pequeno não pode gerar suspeita: %+v", out)
	}
	if lock.count() != 0 {
		t.Error("lockdown não deveria ser aplicado com relógio consistente")
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
	g, st, lock, lg := guardWith(local, last, &fakeNTP{t: real})

	out := g.Check()
	if !out.Suspicion || !out.Confirmed {
		t.Fatalf("burla confirmada esperada: %+v", out)
	}
	if lock.count() != 1 {
		t.Errorf("lockdown chamado %d vezes, want 1", lock.count())
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
	g, _, lock, lg := guardWith(local, last, &fakeNTP{t: real})

	out := g.Check()
	if !out.Suspicion || !out.Confirmed {
		t.Fatalf("burla por adiantamento deveria ser confirmada: %+v", out)
	}
	if lock.count() != 1 {
		t.Errorf("lockdown chamado %d vezes, want 1", lock.count())
	}
	if len(lg.events()) != 1 {
		t.Errorf("tamper não registrado: %v", lg.events())
	}
}

func TestClockJumpValidatedByNTPIsLegit(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	// Relógio local adiantou 3h (DST manual), mas NTP diz que a hora REAL é
	// a local (o SO foi ajustado junto, ex.: viagem de fuso) → legítimo.
	local := real.Add(3 * time.Hour)
	last := real.Add(-time.Hour)
	g, st, lock, lg := guardWith(local, last, &fakeNTP{t: local})

	out := g.Check()
	if !out.Suspicion || out.Confirmed {
		t.Fatalf("ajuste legítimo deveria gerar suspeita SEM confirmação: %+v", out)
	}
	if lock.count() != 0 {
		t.Error("lockdown não deveria ser aplicado para ajuste legítimo")
	}
	if len(lg.events()) != 0 {
		t.Errorf("ajuste legítimo não deveria registrar tamper: %v", lg.events())
	}
	// Referência re-anchorada no horário real (o local validado).
	if st.LastKnownTime().Unix() != local.Unix() {
		t.Errorf("referência não re-anchorada: got %v, want %v", st.LastKnownTime(), local)
	}
}

func TestNTPFailureKeepsSuspicionUnresolved(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(-2 * time.Hour) // salto
	last := real
	g, st, lock, _ := guardWith(local, last, &fakeNTP{err: errors.New("timeout")})

	out := g.Check()
	if !out.Suspicion {
		t.Fatalf("suspeita esperada com gap: %+v", out)
	}
	if out.Confirmed {
		t.Fatal("NTP falhou — não pode confirmar")
	}
	if lock.count() != 0 {
		t.Error("sem confirmação, sem lockdown")
	}
	// A referência NÃO é re-anchorada no relógio adulterado.
	if st.LastKnownTime().Unix() != real.Unix() {
		t.Errorf("referência foi re-anchorada com NTP falho: got %v", st.LastKnownTime())
	}
}

func TestNilNTPKeepsSuspicionUnresolved(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(-2 * time.Hour)
	g, _, lock, _ := guardWith(local, real, NTPClient(nil)) // daemon offline

	out := g.Check()
	if !out.Suspicion || out.Confirmed {
		t.Fatalf("sem NTP a suspeita fica mantida sem confirmação: %+v", out)
	}
	if lock.count() != 0 {
		t.Error("sem confirmação, sem lockdown")
	}
}
