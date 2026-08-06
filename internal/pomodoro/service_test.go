package pomodoro

import (
	"context"
	"errors"
	"testing"
	"time"

	"focusguard/internal/ipcerr"
	"focusguard/internal/preset"
)

// fakeRunner records Start/Stop calls and returns canned results.
type fakeRunner struct {
	started  Session
	startErr error
	stopErr  error
}

func (f *fakeRunner) Start(s Session) (State, error) {
	f.started = s
	return State{Active: true, Phase: PhaseWork, Cycle: 1}, f.startErr
}

func (f *fakeRunner) Stop() (State, error) {
	return State{Active: false}, f.stopErr
}

// fakePrefs returns canned defaults and records the Resolve/Remember inputs.
type fakePrefs struct {
	work, rest, cycles         int
	resolveWork, resolveCycles int
	resolveRest                int
	remembered                 bool
}

func (f *fakePrefs) Resolve(work, rest, cycles int) (int, int, int) {
	f.resolveWork, f.resolveRest, f.resolveCycles = work, rest, cycles
	return f.work, f.rest, f.cycles
}

func (f *fakePrefs) Remember(work, rest, cycles int) {
	f.remembered = true
}

// fakeCatalog resolves the preset to its name + domains.
type fakeCatalog struct {
	resolved string
	resErr   error
}

func (f *fakeCatalog) Resolve(name string) (preset.Preset, error) {
	f.resolved = name
	if f.resErr != nil {
		return preset.Preset{}, f.resErr
	}
	return preset.Preset{Name: name, Domains: []string{"x.com", "y.com"}}, nil
}

func newTestService(runner Runner, prefs PrefsStore, catalog Catalog) *Service {
	return NewService(runner, prefs, catalog)
}

func assertServiceError(t *testing.T, err error, wantCode, wantMsg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("esperava erro, veio nil")
	}
	se, ok := err.(*ipcerr.Error)
	if !ok {
		t.Fatalf("erro = %T (%v), esperava *ipcerr.Error", err, err)
	}
	if se.Code != wantCode {
		t.Errorf("Code = %q, esperava %q", se.Code, wantCode)
	}
	if se.Message != wantMsg {
		t.Errorf("Message = %q, esperava %q", se.Message, wantMsg)
	}
}

func TestStart_NoRunner_PlainError(t *testing.T) {
	svc := newTestService(nil, nil, &fakeCatalog{})
	_, err := svc.Start(context.Background(), "social", "", 25, 5, 4, false, false)
	if err == nil {
		t.Fatal("esperava erro, veio nil")
	}
	if _, ok := err.(*ipcerr.Error); ok {
		t.Fatalf("erro inesperado com código: %v", err)
	}
	if err.Error() != "pomodoro não configurado" {
		t.Errorf("Message = %q, esperava %q", err.Error(), "pomodoro não configurado")
	}
}

func TestStart_EmptyPreset_CodeInvalid(t *testing.T) {
	svc := newTestService(&fakeRunner{}, &fakePrefs{}, &fakeCatalog{})
	_, err := svc.Start(context.Background(), "  ", "", 25, 5, 4, false, false)
	assertServiceError(t, err, ipcerr.CodeInvalid, "Informe um preset (ex: --preset social).")
}

func TestStart_InvalidWork_CodeDurationInvalid(t *testing.T) {
	svc := newTestService(&fakeRunner{}, &fakePrefs{}, &fakeCatalog{})
	_, err := svc.Start(context.Background(), "social", "", 0, 5, 4, false, false)
	assertServiceError(t, err, ipcerr.CodeDurationInvalid, "Duração de trabalho inválida (--work entre 1 e 10080 minutos).")
}

func TestStart_InvalidRestOrCycles_CodeDurationInvalid(t *testing.T) {
	svc := newTestService(&fakeRunner{}, &fakePrefs{work: 25, rest: -2, cycles: 4}, &fakeCatalog{})
	_, err := svc.Start(context.Background(), "social", "", 25, 5, 4, false, false)
	assertServiceError(t, err, ipcerr.CodeDurationInvalid, "Parâmetros de pomodoro inválidos (--rest entre 0 e 10080 minutos, --cycles entre 1 e 1000).")

	svc = newTestService(&fakeRunner{}, &fakePrefs{work: 25, rest: 5, cycles: 0}, &fakeCatalog{})
	_, err = svc.Start(context.Background(), "social", "", 25, 5, 4, false, false)
	assertServiceError(t, err, ipcerr.CodeDurationInvalid, "Parâmetros de pomodoro inválidos (--rest entre 0 e 10080 minutos, --cycles entre 1 e 1000).")
}

func TestStart_ResolvedDefaultsCanFailValidation(t *testing.T) {
	svc := newTestService(&fakeRunner{}, &fakePrefs{work: 0}, &fakeCatalog{})
	_, err := svc.Start(context.Background(), "social", "", 0, -1, 0, false, false)
	assertServiceError(t, err, ipcerr.CodeDurationInvalid, "Duração de trabalho inválida (--work entre 1 e 10080 minutos).")
}

func TestStart_UnknownPreset_PlainError(t *testing.T) {
	cat := &fakeCatalog{resErr: errors.New("preset desconhecido: social")}
	svc := newTestService(&fakeRunner{}, &fakePrefs{work: 25, rest: 5, cycles: 4}, cat)
	_, err := svc.Start(context.Background(), "social", "", 25, 5, 4, false, false)
	if err == nil || err.Error() != "preset desconhecido: social" {
		t.Fatalf("erro = %v, esperava propagação do catálogo", err)
	}
}

func TestStart_RunnerError_Plain(t *testing.T) {
	run := &fakeRunner{startErr: errors.New("falha ao iniciar")}
	svc := newTestService(run, &fakePrefs{work: 25, rest: 5, cycles: 4}, &fakeCatalog{})
	_, err := svc.Start(context.Background(), "social", "", 25, 5, 4, false, false)
	if err == nil || err.Error() != "falha ao iniciar" {
		t.Fatalf("erro = %v, esperava propagação do runner", err)
	}
}

func TestStart_ResolvesPresetAndStarts(t *testing.T) {
	run := &fakeRunner{}
	cat := &fakeCatalog{}
	svc := newTestService(run, &fakePrefs{work: 25, rest: 5, cycles: 4}, cat)

	res, err := svc.Start(context.Background(), "social", "Estudar ENEM", 25, 5, 4, true, false)
	if err != nil {
		t.Fatalf("Start falhou: %v", err)
	}
	if cat.resolved != "social" {
		t.Errorf("preset resolvido = %q, esperava social", cat.resolved)
	}
	want := Session{
		Preset:  "social",
		Label:   "Estudar ENEM",
		Domains: []string{"x.com", "y.com"},
		Work:    25 * time.Minute,
		Rest:    5 * time.Minute,
		Cycles:  4,
		Strict:  true,
	}
	if run.started.Preset != want.Preset ||
		run.started.Label != want.Label ||
		run.started.Work != want.Work ||
		run.started.Rest != want.Rest ||
		run.started.Cycles != want.Cycles ||
		run.started.Strict != want.Strict ||
		len(run.started.Domains) != len(want.Domains) ||
		(len(run.started.Domains) > 0 && run.started.Domains[0] != want.Domains[0]) {
		t.Errorf("sessão = %+v, esperava %+v", run.started, want)
	}
	if !res.State.Active || res.State.Phase != PhaseWork || res.State.Cycle != 1 {
		t.Errorf("state = %+v, esperava ativo work cycle 1", res.State)
	}
	if res.Message != "Pomodoro social iniciado: 4 ciclos de 25m trabalho / 5m descanso" {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestStart_NoPrefs_ExplicitValuesRequired(t *testing.T) {
	run := &fakeRunner{}
	svc := newTestService(run, nil, &fakeCatalog{})

	res, err := svc.Start(context.Background(), "social", "", 50, 10, 2, false, false)
	if err != nil {
		t.Fatalf("Start falhou: %v", err)
	}
	if run.started.Work != 50*time.Minute || run.started.Rest != 10*time.Minute || run.started.Cycles != 2 {
		t.Errorf("sessão = %+v, esperava 50m/10m/2 ciclos", run.started)
	}
	if res.Message != "Pomodoro social iniciado: 2 ciclos de 50m trabalho / 10m descanso" {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestStart_SavePersistsResolvedDefaults(t *testing.T) {
	run := &fakeRunner{}
	prefs := &fakePrefs{work: 50, rest: 10, cycles: 2}
	svc := newTestService(run, prefs, &fakeCatalog{})

	res, err := svc.Start(context.Background(), "social", "", 0, -1, 0, false, true)
	if err != nil {
		t.Fatalf("Start falhou: %v", err)
	}
	if prefs.resolveWork != 0 || prefs.resolveRest != -1 || prefs.resolveCycles != 0 {
		t.Errorf("Resolve(%d,%d,%d), esperava sentinelas 0,-1,0", prefs.resolveWork, prefs.resolveRest, prefs.resolveCycles)
	}
	if !prefs.remembered {
		t.Error("--save não persistiu os padrões")
	}
	if run.started.Work != 50*time.Minute || run.started.Rest != 10*time.Minute || run.started.Cycles != 2 {
		t.Errorf("sessão = %+v, esperava defaults salvos 50m/10m/2", run.started)
	}
	if res.Message != "Pomodoro social iniciado: 2 ciclos de 50m trabalho / 10m descanso (padrões salvos para a próxima sessão)" {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestStart_SaveWithoutPrefs_NoPersistence(t *testing.T) {
	svc := newTestService(&fakeRunner{}, nil, &fakeCatalog{})
	_, err := svc.Start(context.Background(), "social", "", 25, 5, 4, false, true)
	if err != nil {
		t.Fatalf("Start falhou: %v", err)
	}
}

func TestDefaults_NoPrefs_CodeNotConfigured(t *testing.T) {
	svc := newTestService(&fakeRunner{}, nil, &fakeCatalog{})
	_, err := svc.Defaults(context.Background())
	assertServiceError(t, err, ipcerr.CodeNotConfigured, "preferências de pomodoro não configuradas")
}

func TestDefaults_ResolvesSaved(t *testing.T) {
	prefs := &fakePrefs{work: 50, rest: 10, cycles: 2}
	svc := newTestService(&fakeRunner{}, prefs, &fakeCatalog{})

	res, err := svc.Defaults(context.Background())
	if err != nil {
		t.Fatalf("Defaults falhou: %v", err)
	}
	if prefs.resolveWork != 0 || prefs.resolveRest != -1 || prefs.resolveCycles != 0 {
		t.Errorf("Resolve(%d,%d,%d), esperava sentinelas 0,-1,0", prefs.resolveWork, prefs.resolveRest, prefs.resolveCycles)
	}
	if res.Work != 50 || res.Rest != 10 || res.Cycles != 2 {
		t.Errorf("defaults = %d/%d/%d, esperava 50/10/2", res.Work, res.Rest, res.Cycles)
	}
	if res.Message != "Padrões atuais: 50m trabalho / 10m descanso / 2 ciclos" {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestStop_NoRunner_CodeNotConfigured(t *testing.T) {
	svc := newTestService(nil, &fakePrefs{}, &fakeCatalog{})
	_, err := svc.Stop(context.Background())
	assertServiceError(t, err, ipcerr.CodeNotConfigured, "pomodoro não configurado")
}

func TestStop_ReturnsStateAndMessage(t *testing.T) {
	run := &fakeRunner{}
	svc := newTestService(run, &fakePrefs{}, &fakeCatalog{})

	res, err := svc.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop falhou: %v", err)
	}
	if res.State.Active {
		t.Error("state deve estar inativo após o stop")
	}
	if res.Message != "Pomodoro encerrado" {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestStop_RunnerError_Plain(t *testing.T) {
	run := &fakeRunner{stopErr: errors.New("pomodoro idle")}
	svc := newTestService(run, &fakePrefs{}, &fakeCatalog{})
	_, err := svc.Stop(context.Background())
	if err == nil || err.Error() != "pomodoro idle" {
		t.Fatalf("erro = %v, esperava propagação do runner", err)
	}
}
