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

// fakeLockdown conta as chamadas de BlockAllInternet/UnblockAllInternet.
type fakeLockdown struct {
	mu       sync.Mutex
	calls    int
	unblocks int
}

func (f *fakeLockdown) BlockAllInternet(_ []string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return nil
}

func (f *fakeLockdown) UnblockAllInternet() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unblocks++
	return nil
}

func (f *fakeLockdown) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeLockdown) unblockCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unblocks
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
	// NTP confirmou → a hora real é CONHECIDA: o guard NÃO aplica lockdown
	// (bloquear tudo puniria um relógio do SO configurado errado — dual
	// boot), registra no tamper-log e libera um bloqueio pendente.
	if lock.count() != 0 {
		t.Errorf("NTP confirmou — lockdown não deveria ser aplicado, chamado %d vezes", lock.count())
	}
	if lock.unblockCount() != 1 {
		t.Errorf("confirmação deveria liberar um lockdown pendente, unblocks=%d", lock.unblockCount())
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
	g, _, lock, lg := guardWith(local, last, &fakeNTP{t: real})

	out := g.Check()
	if !out.Suspicion || !out.Confirmed {
		t.Fatalf("burla por adiantamento deveria ser confirmada: %+v", out)
	}
	if lock.count() != 0 {
		t.Errorf("NTP confirmou — lockdown não deveria ser aplicado, chamado %d vezes", lock.count())
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
	lock := &fakeLockdown{}
	lg := &fakeLogger{}
	g := New(Deps{State: st, NTP: &fakeNTP{t: real}, Lockdown: lock, Now: func() time.Time { return local }, Logger: lg})

	for i := 0; i < 3; i++ {
		out := g.Check()
		if !out.Suspicion || !out.Confirmed {
			t.Fatalf("check %d: divergência persistente deveria ser confirmada: %+v", i, out)
		}
		if lock.count() != 0 {
			t.Fatalf("check %d: nenhum lockdown deveria ser aplicado, chamado %d vezes", i, lock.count())
		}
	}
	if len(lg.events()) != 1 {
		t.Errorf("offset estável deveria logar UMA vez, got %v", lg.events())
	}

	// Usuário mexe o relógio de novo (offset novo) → novo evento.
	g2 := New(Deps{State: st, NTP: &fakeNTP{t: real}, Lockdown: lock, Now: func() time.Time { return real.Add(-5 * time.Hour) }, Logger: lg})
	if out := g2.Check(); !out.Confirmed {
		t.Fatalf("offset novo deveria ser confirmado: %+v", out)
	}
	if len(lg.events()) != 2 {
		t.Errorf("offset novo deveria registrar novo evento, got %v", lg.events())
	}
}

// TestConfirmed_ReleasesPendingLockdown: uma suspeita anterior com NTP
// offline aplicou o lockdown; o NTP volta e CONFIRMA a divergência → o guard
// libera o bloqueio preventivo (a hora real agora é conhecida e as expirações
// serão ajustadas) em vez de mantê-lo indefinidamente.
func TestConfirmed_ReleasesPendingLockdown(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(-2 * time.Hour)

	// 1º Check sem NTP: suspeita → lockdown preventivo aplicado e mantido.
	g1, st, lock, lg := guardWith(local, real, NTPClient(nil))
	if out := g1.Check(); !out.Suspicion || out.Confirmed {
		t.Fatalf("1º Check: suspeita sem NTP esperada: %+v", out)
	}
	if lock.count() != 1 {
		t.Fatalf("lockdown deveria ter sido aplicado na suspeita, chamado %d vezes", lock.count())
	}

	// 2º Check com NTP confirmando: libera o bloqueio (sem reaplicar).
	g2 := New(Deps{State: st, NTP: &fakeNTP{t: real}, Lockdown: lock, Now: func() time.Time { return local }, Logger: lg})
	out := g2.Check()
	if !out.Suspicion || !out.Confirmed {
		t.Fatalf("2º Check: confirmação esperada: %+v", out)
	}
	if lock.count() != 1 {
		t.Errorf("confirmação não pode reaplicar o lockdown, chamado %d vezes", lock.count())
	}
	if lock.unblockCount() != 1 {
		t.Errorf("confirmação deveria liberar o lockdown pendente, unblocks=%d", lock.unblockCount())
	}
	if len(lg.events()) != 1 {
		t.Errorf("confirmação deveria registrar 1 evento, got %v", lg.events())
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
	// Com NTP disponível e validando o relógio local, a suspeita é limpa SEM
	// bloqueio: o lockdown só é aplicado quando o NTP não decide (offline/
	// falha) ou confirma a burla — aplicar antes reescreveria o firewall a
	// cada ciclo (CheckInterval 10 min > Tolerance 5 min).
	if lock.count() != 0 {
		t.Errorf("NTP válido não deveria aplicar lockdown, chamado %d vezes", lock.count())
	}
	// Mesmo sem ter aplicado, o guard tenta liberar um bloqueio pendente
	// (janela em que o NTP estava fora) — no-op no scheduler.
	if lock.unblockCount() != 1 {
		t.Errorf("NTP validou o relógio — deveria tentar liberar um lockdown pendente, unblocks=%d", lock.unblockCount())
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
	// O bloqueio preventivo já foi aplicado na suspeita (antes do NTP); a
	// falha do NTP o MANTÉM (sem confirmar nem liberar).
	if lock.count() != 1 {
		t.Errorf("lockdown preventivo deveria ser aplicado na suspeita, chamado %d vezes", lock.count())
	}
	if lock.unblockCount() != 0 {
		t.Error("NTP falhou — bloqueio preventivo mantido, sem liberação")
	}
	// A referência NÃO é re-anchorada no relógio adulterado.
	if st.LastKnownTime().Unix() != real.Unix() {
		t.Errorf("referência foi re-anchorada com NTP falho: got %v", st.LastKnownTime())
	}
}

func TestNilNTP_SuspicionAppliesLockdown(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(-2 * time.Hour)
	g, st, lock, _ := guardWith(local, real, NTPClient(nil)) // daemon offline

	out := g.Check()
	if !out.Suspicion || out.Confirmed {
		t.Fatalf("sem NTP a suspeita fica mantida sem confirmação: %+v", out)
	}
	// O bloqueio preventivo é aplicado JÁ NA SUSPEITA mesmo com NTP offline
	// (features-plan: proteger o cenário "relógio adiantado + sem rede").
	if lock.count() != 1 {
		t.Errorf("lockdown preventivo deveria ser aplicado na suspeita, chamado %d vezes", lock.count())
	}
	if lock.unblockCount() != 0 {
		t.Error("sem NTP não há liberação")
	}
	// A referência NÃO é re-anchorada (o NTP nunca validou o relógio).
	if st.LastKnownTime().Unix() != real.Unix() {
		t.Errorf("referência foi re-anchorada sem NTP: got %v", st.LastKnownTime())
	}
}

// TestClockAdvancedWithNTPOffline_LockdownsAtSuspicion cobre o cenário que
// motivou o lockdown na suspeita (Fase 2 do features-plan): relógio
// ADIANTADO (para expirar os bloqueios cedo num restart) + NTP offline. Mesmo
// sem conseguir confirmar, a proteção precisa estar no ar desde a suspeita —
// senão o restart com o relógio adiantado expira os bloqueios sem defesa.
func TestClockAdvancedWithNTPOffline_LockdownsAtSuspicion(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(24 * time.Hour) // adiantou 1 dia
	last := real
	g, st, lock, _ := guardWith(local, last, NTPClient(nil)) // sem rede

	out := g.Check()
	if !out.Suspicion {
		t.Fatalf("gap de 24h deveria gerar suspeita: %+v", out)
	}
	if out.Confirmed {
		t.Fatal("sem NTP não há confirmação")
	}
	if lock.count() != 1 {
		t.Errorf("lockdown preventivo deveria ser aplicado na suspeita (NTP offline), chamado %d vezes", lock.count())
	}
	if lock.unblockCount() != 0 {
		t.Error("NTP offline não pode liberar o bloqueio")
	}
	// Referência preservada: o próximo Check ainda detecta o relógio errado.
	if st.LastKnownTime().Unix() != real.Unix() {
		t.Errorf("referência não deveria ser re-anchorada: got %v, want %v", st.LastKnownTime(), real)
	}
}

// TestConsistentClockAfterSuspicion_ReleasesLockdown: uma suspeita anterior
// (com NTP offline) aplicou o lockdown; o usuário corrige o relógio de volta
// para o horário confiado → o Check seguinte (gap normal) libera o bloqueio
// preventivo, sem esperar a expiração da duração.
func TestConsistentClockAfterSuspicion_ReleasesLockdown(t *testing.T) {
	real := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	local := real.Add(24 * time.Hour)
	g, _, lock, _ := guardWith(local, real, NTPClient(nil))

	if out := g.Check(); !out.Suspicion {
		t.Fatalf("1º Check deveria gerar suspeita: %+v", out)
	}
	if lock.count() != 1 {
		t.Fatalf("lockdown deveria ter sido aplicado na suspeita, chamado %d vezes", lock.count())
	}

	// Usuário corrige o relógio para perto da referência confiada.
	g2, _, lock2, _ := guardWith(real.Add(2*time.Minute), real, NTPClient(nil))
	out := g2.Check()
	if out.Suspicion || out.Confirmed {
		t.Fatalf("relógio corrigido não pode gerar suspeita: %+v", out)
	}
	if lock2.unblockCount() != 1 {
		t.Errorf("relógio consistente deveria liberar o lockdown pendente, unblocks=%d", lock2.unblockCount())
	}
}
