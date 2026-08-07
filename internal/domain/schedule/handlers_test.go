// Testes dos handlers das ações schedule-list/add/import/remove (pós-reorg
// item 1: handler + handler_test por pacote).
package schedule

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/domain/preset"
)

type handlerStore struct {
	rules []Rule
	ics   []Rule
	err   error
}

func (f *handlerStore) List() []Rule { return f.rules }

func (f *handlerStore) Add(r Rule) (Rule, error) {
	if f.err != nil {
		return Rule{}, f.err
	}
	f.rules = append(f.rules, r)
	return r, nil
}

func (f *handlerStore) Remove(id string) error {
	if f.err != nil {
		return f.err
	}
	for i, r := range f.rules {
		if r.ID == id {
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *handlerStore) ImportICS(_ []byte, _ string) ([]Rule, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ics, nil
}

type handlerResolver struct{ err error }

func (f *handlerResolver) Resolve(name string) (preset.Preset, error) {
	if f.err != nil {
		return preset.Preset{}, f.err
	}
	return preset.Preset{Name: name}, nil
}

func assertActionError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var ae *ipcerr.Error
	if !errors.As(err, &ae) || ae.Code != wantCode {
		t.Fatalf("esperava código %q, got %v", wantCode, err)
	}
}

func TestScheduleList_SemStore(t *testing.T) {
	h := NewListHandler(nil, nil)
	_, err := h.Handle(context.Background(), &NoInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestScheduleList_OK(t *testing.T) {
	h := NewListHandler(&handlerStore{rules: []Rule{{ID: "r1"}}}, nil)
	resp, err := h.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Schedules) != 1 {
		t.Fatalf("esperava 1 regra, got %+v", resp)
	}
}

func TestScheduleAdd_OK(t *testing.T) {
	st := &handlerStore{}
	h := NewAddHandler(st, nil)
	resp, err := h.Handle(context.Background(), &AddInput{Rule: Rule{ID: "r1", Preset: "social", Start: "08:00", End: "12:00"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message == "" || len(st.rules) != 1 {
		t.Fatalf("esperava mensagem + regra persistida, got %+v", resp)
	}
}

func TestScheduleImport_PresetObrigatorio(t *testing.T) {
	h := NewImportHandler(&handlerStore{}, &handlerResolver{})
	_, err := h.Handle(context.Background(), &ImportInput{ICSContent: "BEGIN:VCALENDAR"})
	assertActionError(t, err, ipcerr.CodeInvalid)
}

func TestScheduleImport_ConteudoVazio(t *testing.T) {
	h := NewImportHandler(&handlerStore{}, &handlerResolver{})
	_, err := h.Handle(context.Background(), &ImportInput{ICSPreset: "social"})
	assertActionError(t, err, ipcerr.CodeInvalid)
}

func TestScheduleImport_PresetNaoResolve(t *testing.T) {
	h := NewImportHandler(&handlerStore{ics: []Rule{{ID: "r1"}}}, &handlerResolver{err: errors.New("preset desconhecido")})
	_, err := h.Handle(context.Background(), &ImportInput{ICSContent: "BEGIN:VCALENDAR", ICSPreset: "social"})
	if err == nil {
		t.Fatal("esperava o erro do resolver")
	}
}

func TestScheduleImport_SemEventosSemanais(t *testing.T) {
	// Import que não acha evento semanal é falha SEM código (mensagem pura).
	h := NewImportHandler(&handlerStore{ics: []Rule{}}, &handlerResolver{})
	_, err := h.Handle(context.Background(), &ImportInput{ICSContent: "BEGIN:VCALENDAR", ICSPreset: "social"})
	if err == nil || err.Error() != "Nenhum evento semanal encontrado no calendário." {
		t.Fatalf("esperava erro de calendário sem eventos, got %v", err)
	}
}

func TestScheduleImport_OK(t *testing.T) {
	st := &handlerStore{ics: []Rule{{ID: "r1"}}}
	h := NewImportHandler(st, &handlerResolver{})
	resp, err := h.Handle(context.Background(), &ImportInput{ICSContent: "BEGIN:VCALENDAR", ICSPreset: "social"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Schedules) != 1 || resp.Message == "" {
		t.Fatalf("esperava 1 regra + mensagem, got %+v", resp)
	}
}

func TestScheduleRemove_OK(t *testing.T) {
	st := &handlerStore{rules: []Rule{{ID: "r1"}}}
	h := NewRemoveHandler(st, nil)
	resp, err := h.Handle(context.Background(), &RemoveInput{ScheduleID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message == "" || len(st.rules) != 0 {
		t.Fatalf("esperava sucesso + remoção, got resp=%+v rules=%v", resp, st.rules)
	}
}

func TestScheduleHandlers_ActionNames(t *testing.T) {
	if NewListHandler(nil, nil).Action() != "schedule-list" {
		t.Fatal("schedule-list action name errado")
	}
	if NewAddHandler(nil, nil).Action() != "schedule-add" {
		t.Fatal("schedule-add action name errado")
	}
	if NewImportHandler(nil, nil).Action() != "schedule-import" {
		t.Fatal("schedule-import action name errado")
	}
	if NewRemoveHandler(nil, nil).Action() != "schedule-remove" {
		t.Fatal("schedule-remove action name errado")
	}
}
