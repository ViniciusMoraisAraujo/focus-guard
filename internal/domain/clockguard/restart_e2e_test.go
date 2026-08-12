package clockguard

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"focusguard/internal/domain/policy"
	"focusguard/internal/domain/scheduler"
	"focusguard/internal/infrastructure/enforcer"
	"focusguard/internal/infrastructure/store"
	"focusguard/internal/infrastructure/tamper"
)

// e2eEnforcer é um stub do enforcer que conta as chamadas de BlockAll/
// UnblockAll — o cenário E2E roda sem elevação (o enforcer real executaria
// netsh/iptables e exigiria admin). Tudo o mais do fluxo usa os componentes
// reais de produção.
type e2eEnforcer struct {
	mu              sync.Mutex
	blockAllCalls   int
	unblockAllCalls int
}

func (e *e2eEnforcer) BlockDomain(string, []string) error   { return nil }
func (e *e2eEnforcer) UnblockDomain(string, []string) error { return nil }
func (e *e2eEnforcer) Sync(map[string][]string) error       { return nil }
func (e *e2eEnforcer) BlockDoH() error                      { return nil }
func (e *e2eEnforcer) UnblockDoH() error                    { return nil }
func (e *e2eEnforcer) BlockAll([]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.blockAllCalls++
	return nil
}
func (e *e2eEnforcer) UnblockAll() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unblockAllCalls++
	return nil
}
func (e *e2eEnforcer) Status() (enforcer.EnforcerStatus, error) {
	return enforcer.EnforcerStatus{}, nil
}

func (e *e2eEnforcer) blockAll() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.blockAllCalls
}

func (e *e2eEnforcer) unblockAll() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.unblockAllCalls
}

// e2eLockdown adapta o *scheduler.Scheduler ao clockguard.Lockdown — espelho
// do clockLockdownAdapter do composition root do daemon (a origem
// clock-guard é o que torna a liberação segura).
type e2eLockdown struct{ s *scheduler.Scheduler }

func (a e2eLockdown) BlockAllInternet(_ []string, duration time.Duration) error {
	_, err := a.s.ApplyClockLockdown(duration)
	return err
}

func (a e2eLockdown) UnblockAllInternet() error {
	return a.s.ReleaseClockLockdown()
}

// e2eLogger adapta o *tamper.Recorder ao clockguard.Logger — espelho do
// clockLoggerAdapter do daemon.
type e2eLogger struct{ rec *tamper.Recorder }

func (l e2eLogger) Log(source, action, detail string) {
	l.rec.Log(tamper.Event{At: time.Now(), Source: source, Action: action, Detail: detail})
}

// TestClockGuard_RestartE2E_AdvanceOfflineConfirmRelease valida o cenário E2E
// completo do Clock Guard (Fase 2) ATRAVÉS da fronteira de restart, com os
// componentes reais de produção (store persistido + scheduler + guard +
// tamper-recorder) e um relógio injetável (a única peça que o teste não pode
// tocar sem admin — o daemon real usa time.Now). O enforcer é um stub que
// conta chamadas: o enforcer real (netsh/iptables), o NTP real e o relógio
// do SO ficam para o checklist manual da Etapa 9 (exigem shell elevado).
//
//  1. Boot saudável em T: referência gravada e deslizando; nada bloqueado.
//  2. "Restart" com o relógio ADIANTADO (+24h) e a rede BLOQUEADA (NTP nil):
//     lockdown preventivo aplicado JÁ NA SUSPEITA, persistido com origem
//     clock-guard; o tamper-log NÃO registra (suspeita não confirmada não é
//     evento histórico — só o estado vivo aparece).
//  3. A rede volta e o NTP CONFIRMA a burla: tamper-log registra
//     clock/lockdown ("confirmado por NTP") e o bloqueio é mantido.
//  4. O relógio é corrigido (gap normal): liberação automática — o sentinela
//     sai do RAM e do state.json e o enforcer.UnblockAll é chamado.
func TestClockGuard_RestartE2E_AdvanceOfflineConfirmRelease(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	enf := &e2eEnforcer{}
	rec := tamper.NewRecorder(filepath.Join(dir, "tamper.jsonl"))
	logger := e2eLogger{rec: rec}

	// --- 1. Boot saudável em T (relógio correto) ---------------------------
	T := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sched1 := scheduler.NewScheduler(st, enf)
	if err := sched1.Reconcile(); err != nil {
		t.Fatalf("Reconcile (boot 1): %v", err)
	}
	guard1 := New(Deps{
		State: sched1, NTP: &fakeNTP{t: T.Add(2 * time.Minute)},
		Lockdown: e2eLockdown{s: sched1}, Now: func() time.Time { return T }, Logger: logger,
	})
	if out := guard1.Check(); out.Suspicion {
		t.Fatalf("boot 1: primeira execução não pode gerar suspeita: %+v", out)
	}
	// Relógio consistente: a referência desliza e é persistida para o restart.
	guard1b := New(Deps{
		State: sched1, NTP: &fakeNTP{t: T.Add(2 * time.Minute)},
		Lockdown: e2eLockdown{s: sched1}, Now: func() time.Time { return T.Add(2 * time.Minute) }, Logger: logger,
	})
	if out := guard1b.Check(); out.Suspicion {
		t.Fatalf("boot 1: relógio consistente não pode gerar suspeita: %+v", out)
	}
	if sched1.HasActiveBlocks() {
		t.Fatal("boot 1: nenhum bloqueio deveria existir")
	}
	if loaded, _ := st.Load(); loaded.LastKnownTime.IsZero() {
		t.Fatal("boot 1: last_known_time deveria estar persistido")
	}

	// --- 2. Restart com relógio adiantado + rede bloqueada (NTP nil) -------
	sched2 := scheduler.NewScheduler(st, enf) // mesmo store = restart
	if err := sched2.Reconcile(); err != nil {
		t.Fatalf("Reconcile (boot 2): %v", err)
	}
	guard2 := New(Deps{
		State: sched2, NTP: NTPClient(nil), // rede bloqueada
		Lockdown: e2eLockdown{s: sched2}, Now: func() time.Time { return T.Add(24 * time.Hour) }, Logger: logger,
	})
	out := guard2.Check()
	if !out.Suspicion || out.Confirmed {
		t.Fatalf("boot 2: suspeita sem confirmação esperada (rede bloqueada): %+v", out)
	}
	if !sched2.HasActiveBlocks() {
		t.Fatal("boot 2: o lockdown preventivo deveria ter sido aplicado na suspeita (NTP offline)")
	}
	list, _ := sched2.ListBlocks()
	sentinel := findSentinel(list)
	if sentinel == nil || sentinel.Source != policy.SourceClockGuard {
		t.Fatalf("boot 2: sentinela deveria ser do clock guard (source=clock-guard), got %+v", list)
	}
	if enf.blockAll() != 1 {
		t.Errorf("boot 2: enforcer.BlockAll chamado %d vezes, want 1", enf.blockAll())
	}
	// Expiração fresca (~1h) a cada re-aplicação — o lockdown não fica
	// preso a uma expiração antiga. (O scheduler usa o relógio real para
	// StartedAt/ExpiresAt — o clock injetado vale só para o guard — então o
	// contrato checado é a DURAÇÃO, não o instante absoluto.)
	if dur := sentinel.ExpiresAt.Sub(sentinel.StartedAt); dur <= 0 || dur > 2*time.Hour {
		t.Errorf("boot 2: duração do lockdown deveria ser ~1h, got %v", dur)
	}
	// Nuance documentada: suspeita NÃO confirmada não grava evento no
	// tamper-log — o bloqueio é estado vivo (status/UI), não evento histórico.
	events, err := rec.Events()
	if err != nil {
		t.Fatalf("tamper-log: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("boot 2: suspeita não confirmada não pode registrar tamper, got %v", events)
	}
	// O sentinela persiste com a origem (um terceiro restart o preservaria).
	loaded, _ := st.Load()
	saved, ok := loaded.Blocks[enforcer.AllInternetDomain]
	if !ok || saved.Source != policy.SourceClockGuard {
		t.Errorf("boot 2: sentinela deveria estar persistido com source=clock-guard, got %+v", saved)
	}

	// --- 3. Rede volta; NTP confirma a burla --------------------------------
	guard3 := New(Deps{
		State: sched2, NTP: &fakeNTP{t: T.Add(2 * time.Minute)}, // hora REAL
		Lockdown: e2eLockdown{s: sched2}, Now: func() time.Time { return T.Add(24 * time.Hour) }, Logger: logger,
	})
	out = guard3.Check()
	if !out.Suspicion || !out.Confirmed {
		t.Fatalf("check 3: burla deveria ser confirmada pelo NTP: %+v", out)
	}
	if !sched2.HasActiveBlocks() {
		t.Fatal("check 3: lockdown deveria ser mantido após a confirmação")
	}
	events, err = rec.Events()
	if err != nil {
		t.Fatalf("tamper-log: %v", err)
	}
	if len(events) != 1 || events[0].Source != "clock" || events[0].Action != "lockdown" {
		t.Fatalf("tamper-log deveria ter exatamente 1 clock/lockdown, got %v", events)
	}
	if !strings.Contains(events[0].Detail, "confirmado por NTP") {
		t.Errorf("detalhe do tamper: %q, want mencionando confirmação por NTP", events[0].Detail)
	}

	// --- 4. Relógio corrigido (dentro da tolerância da referência real) -----
	guard4 := New(Deps{
		State: sched2, NTP: NTPClient(nil), // segue sem rede — a liberação não depende do NTP
		Lockdown: e2eLockdown{s: sched2}, Now: func() time.Time { return T.Add(4 * time.Minute) }, Logger: logger,
	})
	out = guard4.Check()
	if out.Suspicion || out.Confirmed {
		t.Fatalf("relógio corrigido não pode gerar suspeita: %+v", out)
	}
	if sched2.HasActiveBlocks() {
		t.Fatal("relógio corrigido deveria liberar o lockdown preventivo")
	}
	if enf.unblockAll() != 1 {
		t.Errorf("enforcer.UnblockAll chamado %d vezes, want 1 (liberação)", enf.unblockAll())
	}
	loaded, _ = st.Load()
	if _, ok := loaded.Blocks[enforcer.AllInternetDomain]; ok {
		t.Error("state.json deveria estar sem o sentinela após a liberação")
	}
}

// findSentinel localiza o bloco all-internet na lista (nil quando ausente).
func findSentinel(blocks []policy.Block) *policy.Block {
	for i := range blocks {
		if blocks[i].Domain == enforcer.AllInternetDomain {
			return &blocks[i]
		}
	}
	return nil
}
