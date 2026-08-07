package schedule

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/domain/preset"
)

type fakeStore struct {
	rules      []Rule
	addErr     error
	removeErr  error
	importICS  []Rule
	importErr  error
	lastAdd    Rule
	lastID     string
	lastICS    string
	lastPreset string
}

func (f *fakeStore) List() []Rule { return f.rules }
func (f *fakeStore) Add(r Rule) (Rule, error) {
	f.lastAdd = r
	if f.addErr != nil {
		return Rule{}, f.addErr
	}
	r.ID = "abc12345"
	return r, nil
}
func (f *fakeStore) Remove(id string) error {
	f.lastID = id
	return f.removeErr
}
func (f *fakeStore) ImportICS(data []byte, preset string) ([]Rule, error) {
	f.lastICS = string(data)
	f.lastPreset = preset
	if f.importErr != nil {
		return nil, f.importErr
	}
	return f.importICS, nil
}

type fakePresetResolver struct {
	err error
}

func (f fakePresetResolver) Resolve(name string) (preset.Preset, error) {
	if f.err != nil {
		return preset.Preset{}, f.err
	}
	return preset.Preset{Name: name}, nil
}

func assertServiceError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var se *ipcerr.Error
	if !errors.As(err, &se) {
		t.Fatalf("esperava ipcerr.Error, got %v", err)
	}
	if se.Code != wantCode {
		t.Fatalf("esperava código %q, got %q (%v)", wantCode, se.Code, err)
	}
}

func TestList_OK(t *testing.T) {
	svc := NewService(&fakeStore{rules: []Rule{{ID: "abc1", Preset: "social"}}}, fakePresetResolver{})
	rules, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Preset != "social" {
		t.Fatalf("esperava 1 regra social, got %+v", rules)
	}
}

func TestList_SemStore(t *testing.T) {
	svc := NewService(nil, fakePresetResolver{})
	_, err := svc.List(context.Background())
	assertServiceError(t, err, ipcerr.CodeNotConfigured)
}

func TestAdd_OK(t *testing.T) {
	st := &fakeStore{}
	svc := NewService(st, fakePresetResolver{})
	r, err := svc.Add(context.Background(), Rule{Preset: "social", Days: []int{1, 2}, Start: "08:00", End: "12:00"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" {
		t.Fatal("esperava ID gerado pelo store")
	}
	if st.lastAdd.Preset != "social" {
		t.Fatalf("Add chamado com %+v", st.lastAdd)
	}
}

func TestAdd_ErroDoStore(t *testing.T) {
	svc := NewService(&fakeStore{addErr: errors.New("schedule: informe um preset")}, fakePresetResolver{})
	_, err := svc.Add(context.Background(), Rule{})
	if err == nil || err.Error() != "schedule: informe um preset" {
		t.Fatalf("esperava erro do store propagado, got %v", err)
	}
}

func TestAdd_SemStore(t *testing.T) {
	svc := NewService(nil, fakePresetResolver{})
	_, err := svc.Add(context.Background(), Rule{})
	assertServiceError(t, err, ipcerr.CodeNotConfigured)
}

func TestRemove_OK(t *testing.T) {
	st := &fakeStore{}
	svc := NewService(st, fakePresetResolver{})
	if err := svc.Remove(context.Background(), "abc1"); err != nil {
		t.Fatal(err)
	}
	if st.lastID != "abc1" {
		t.Fatalf("Remove chamado com %q", st.lastID)
	}
}

func TestRemove_ErroDoStore(t *testing.T) {
	svc := NewService(&fakeStore{removeErr: errors.New("schedule: regra não encontrada")}, fakePresetResolver{})
	err := svc.Remove(context.Background(), "zzz")
	if err == nil || err.Error() != "schedule: regra não encontrada" {
		t.Fatalf("esperava erro do store propagado, got %v", err)
	}
}

func TestRemove_SemStore(t *testing.T) {
	svc := NewService(nil, fakePresetResolver{})
	err := svc.Remove(context.Background(), "abc1")
	assertServiceError(t, err, ipcerr.CodeNotConfigured)
}

func TestImport_OK(t *testing.T) {
	st := &fakeStore{importICS: []Rule{{ID: "abc1", Preset: "social"}}}
	svc := NewService(st, fakePresetResolver{})
	added, err := svc.Import(context.Background(), testICSContent, "social")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Fatalf("esperava 1 regra, got %d", len(added))
	}
	if st.lastPreset != "social" || st.lastICS != testICSContent {
		t.Fatalf("ImportICS recebeu preset=%q content=%q", st.lastPreset, st.lastICS)
	}
}

func TestImport_PresetVazio(t *testing.T) {
	svc := NewService(&fakeStore{}, fakePresetResolver{})
	_, err := svc.Import(context.Background(), testICSContent, "")
	assertServiceError(t, err, ipcerr.CodeInvalid)
}

func TestImport_ConteudoVazio(t *testing.T) {
	svc := NewService(&fakeStore{}, fakePresetResolver{})
	_, err := svc.Import(context.Background(), "  ", "social")
	assertServiceError(t, err, ipcerr.CodeInvalid)
}

func TestImport_ResolverError(t *testing.T) {
	svc := NewService(&fakeStore{}, fakePresetResolver{err: errors.New("preset inexistente")})
	_, err := svc.Import(context.Background(), testICSContent, "nao-existe")
	if err == nil || err.Error() != "preset inexistente" {
		t.Fatalf("esperava erro do resolver propagado, got %v", err)
	}
}

func TestImport_NenhumEvento(t *testing.T) {
	svc := NewService(&fakeStore{}, fakePresetResolver{})
	_, err := svc.Import(context.Background(), testICSContent, "social")
	if err == nil || err.Error() != "Nenhum evento semanal encontrado no calendário." {
		t.Fatalf("esperava mensagem de calendário vazio, got %v", err)
	}
}

func TestImport_ImportErr(t *testing.T) {
	svc := NewService(&fakeStore{importErr: errors.New("ics: parse falhou")}, fakePresetResolver{})
	_, err := svc.Import(context.Background(), testICSContent, "social")
	if err == nil || err.Error() != "ics: parse falhou" {
		t.Fatalf("esperava erro do ImportICS propagado, got %v", err)
	}
}

func TestImport_SemStore(t *testing.T) {
	svc := NewService(nil, fakePresetResolver{})
	_, err := svc.Import(context.Background(), testICSContent, "social")
	assertServiceError(t, err, ipcerr.CodeNotConfigured)
}

const testICSContent = `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:1
SUMMARY:Aula
DTSTART:20260202T080000
DTEND:20260202T120000
RRULE:FREQ=WEEKLY;BYDAY=MO,WE
END:VEVENT
END:VCALENDAR
`
