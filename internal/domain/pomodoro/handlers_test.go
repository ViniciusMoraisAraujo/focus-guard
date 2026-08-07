// Testes dos handlers das ações pomodoro/pomodoro-defaults/pomodoro-stop
// (pós-reorg item 1: handler + handler_test por pacote).
package pomodoro

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/domain/preset"
)

type handlerRunner struct {
	started bool
	state   State
	err     error
}

func (f *handlerRunner) Start(_ Session) (State, error) {
	if f.err != nil {
		return State{}, f.err
	}
	f.started = true
	return f.state, nil
}

func (f *handlerRunner) Stop() (State, error) {
	if f.err != nil {
		return State{}, f.err
	}
	return f.state, nil
}

type handlerPrefs struct{ work, rest, cycles int }

func (f *handlerPrefs) Resolve(work, rest, cycles int) (int, int, int) {
	if work == 0 {
		work = f.work
	}
	if rest == -1 {
		rest = f.rest
	}
	if cycles == 0 {
		cycles = f.cycles
	}
	return work, rest, cycles
}

func (f *handlerPrefs) Remember(work, rest, cycles int) {
	f.work, f.rest, f.cycles = work, rest, cycles
}

type handlerCatalog struct{}

func (handlerCatalog) Resolve(name string) (preset.Preset, error) {
	if name == "" {
		return preset.Preset{}, errors.New("preset desconhecido")
	}
	return preset.Preset{Name: name, Domains: []string{"x.com"}}, nil
}

func assertActionError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var ae *ipcerr.Error
	if !errors.As(err, &ae) || ae.Code != wantCode {
		t.Fatalf("esperava código %q, got %v", wantCode, err)
	}
}

func TestPomodoroStart_SemRunner(t *testing.T) {
	h := NewStartHandler(nil, &handlerPrefs{}, handlerCatalog{})
	_, err := h.Handle(context.Background(), &StartInput{Preset: "social"})
	// Runner nulo devolve erro SEM código (mensagem pura) — semântica do switch legado.
	if err == nil || err.Error() != "pomodoro não configurado" {
		t.Fatalf("runner nil: esperava 'pomodoro não configurado', got %v", err)
	}
}

func TestPomodoroStart_PresetVazio(t *testing.T) {
	h := NewStartHandler(&handlerRunner{}, &handlerPrefs{}, handlerCatalog{})
	_, err := h.Handle(context.Background(), &StartInput{})
	assertActionError(t, err, ipcerr.CodeInvalid)
}

func TestPomodoroStart_WorkInvalido(t *testing.T) {
	h := NewStartHandler(&handlerRunner{}, &handlerPrefs{}, handlerCatalog{})
	_, err := h.Handle(context.Background(), &StartInput{Preset: "social", WorkMin: -1})
	assertActionError(t, err, ipcerr.CodeDurationInvalid)
}

func TestPomodoroStart_OK(t *testing.T) {
	runner := &handlerRunner{state: State{Active: true, Preset: "social"}}
	h := NewStartHandler(runner, &handlerPrefs{work: 25, rest: 5, cycles: 4}, handlerCatalog{})
	resp, err := h.Handle(context.Background(), &StartInput{Preset: "social"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.State.Active || resp.Message == "" {
		t.Fatalf("esperava estado ativo + mensagem, got %+v", resp)
	}
	if !runner.started {
		t.Fatal("runner não iniciou a sessão")
	}
}

func TestPomodoroDefaults_SemPrefs(t *testing.T) {
	h := NewDefaultsHandler(nil)
	_, err := h.Handle(context.Background(), &NoInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestPomodoroDefaults_OK(t *testing.T) {
	h := NewDefaultsHandler(&handlerPrefs{work: 25, rest: 5, cycles: 4})
	resp, err := h.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Work != 25 || resp.Rest != 5 || resp.Cycles != 4 || resp.Message == "" {
		t.Fatalf("esperava defaults 25/5/4 + mensagem, got %+v", resp)
	}
}

func TestPomodoroStop_SemRunner(t *testing.T) {
	h := NewStopHandler(nil)
	_, err := h.Handle(context.Background(), &NoInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestPomodoroStop_OK(t *testing.T) {
	h := NewStopHandler(&handlerRunner{state: State{Active: false}})
	resp, err := h.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message == "" {
		t.Fatalf("esperava mensagem, got %+v", resp)
	}
}

func TestPomodoroHandlers_ActionNames(t *testing.T) {
	if NewStartHandler(nil, nil, nil).Action() != "pomodoro" {
		t.Fatal("pomodoro action name errado")
	}
	if NewDefaultsHandler(nil).Action() != "pomodoro-defaults" {
		t.Fatal("pomodoro-defaults action name errado")
	}
	if NewStopHandler(nil).Action() != "pomodoro-stop" {
		t.Fatal("pomodoro-stop action name errado")
	}
}
