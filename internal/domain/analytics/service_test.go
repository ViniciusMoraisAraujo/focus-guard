package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"focusguard/internal/domain/ipcerr"
)

type fakeProvider struct {
	sessions []Session
}

func (f *fakeProvider) Sessions() ([]Session, error) { return f.sessions, nil }

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

func TestStats_OK(t *testing.T) {
	now := time.Now()
	svc := NewService(&fakeProvider{sessions: []Session{
		{Start: now.Add(-time.Hour), End: now.Add(-30 * time.Minute), Focus: 30 * time.Minute},
	}})
	st, err := svc.Stats(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.TotalSessions != 1 {
		t.Fatalf("esperava 1 sessão, got %+v", st)
	}
}

func TestStats_PorMissao(t *testing.T) {
	now := time.Now()
	svc := NewService(&fakeProvider{sessions: []Session{
		{Start: now.Add(-time.Hour), End: now.Add(-30 * time.Minute), Label: "ENEM", Focus: 30 * time.Minute},
		{Start: now.Add(-2 * time.Hour), End: now.Add(-90 * time.Minute), Label: "Trabalho", Focus: 30 * time.Minute},
	}})
	st, err := svc.Stats(context.Background(), "ENEM")
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.TotalSessions != 1 {
		t.Fatalf("esperava filtro por missão (1 sessão), got %+v", st)
	}
}

func TestStats_SemProvider(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.Stats(context.Background(), "")
	assertServiceError(t, err, ipcerr.CodeNotConfigured)
}

func TestMissions_OK(t *testing.T) {
	now := time.Now()
	svc := NewService(&fakeProvider{sessions: []Session{
		{Start: now.Add(-time.Hour), End: now.Add(-30 * time.Minute), Label: "ENEM", Focus: 30 * time.Minute},
		{Start: now.Add(-2 * time.Hour), End: now.Add(-90 * time.Minute), Label: "ENEM", Focus: 30 * time.Minute},
		{Start: now.Add(-3 * time.Hour), End: now.Add(-150 * time.Minute), Focus: 30 * time.Minute},
	}})
	ls, err := svc.Missions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 1 || ls[0].Sessions != 2 {
		t.Fatalf("esperava 1 missão com 2 sessões, got %+v", ls)
	}
}

func TestMissions_SemProvider(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.Missions(context.Background())
	assertServiceError(t, err, ipcerr.CodeNotConfigured)
}

func TestSessions_OK_Cap(tt *testing.T) {
	now := time.Now()
	var sessions []Session
	for i := 0; i < maxSessionsReturned+10; i++ {
		sessions = append(sessions, Session{Start: now.Add(-time.Duration(i) * time.Minute), End: now, Focus: time.Minute})
	}
	svc := NewService(&fakeProvider{sessions: sessions})
	got, err := svc.Sessions(context.Background())
	if err != nil {
		tt.Fatal(err)
	}
	if len(got) != maxSessionsReturned {
		tt.Fatalf("esperava cap de %d sessões, got %d", maxSessionsReturned, len(got))
	}
	// Ordem mais recente primeiro (o service preserva o RecentSessions).
	if !got[0].Start.After(got[len(got)-1].Start) {
		tt.Fatal("esperava sessões mais novas primeiro")
	}
}

func TestSessions_SemProvider(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.Sessions(context.Background())
	assertServiceError(t, err, ipcerr.CodeNotConfigured)
}
