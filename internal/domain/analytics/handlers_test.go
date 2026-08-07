// Testes dos handlers das ações stats/missions/sessions (pós-reorg item 1:
// handler + handler_test por pacote, mesmo padrão do item 2).
package analytics

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/domain/ipcerr"
)

type handlerProvider struct {
	sessions []Session
	err      error
}

func (f *handlerProvider) Sessions() ([]Session, error) { return f.sessions, f.err }

func assertActionError(t *testing.T, err error, wantCode string) {
	t.Helper()
	var ae *ipcerr.Error
	if !errors.As(err, &ae) || ae.Code != wantCode {
		t.Fatalf("esperava código %q, got %v", wantCode, err)
	}
}

func TestStatsHandler_SemProvider(t *testing.T) {
	h := NewStatsHandler(nil)
	_, err := h.Handle(context.Background(), &StatsInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestStatsHandler_OK(t *testing.T) {
	h := NewStatsHandler(&handlerProvider{sessions: []Session{}})
	resp, err := h.Handle(context.Background(), &StatsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stats == nil {
		t.Fatal("esperava Stats preenchido")
	}
}

func TestMissionsHandler_SemProvider(t *testing.T) {
	h := NewMissionsHandler(nil)
	_, err := h.Handle(context.Background(), &NoInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestMissionsHandler_OK(t *testing.T) {
	h := NewMissionsHandler(&handlerProvider{sessions: []Session{}})
	if _, err := h.Handle(context.Background(), &NoInput{}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsHandler_SemProvider(t *testing.T) {
	h := NewSessionsHandler(nil)
	_, err := h.Handle(context.Background(), &NoInput{})
	assertActionError(t, err, ipcerr.CodeNotConfigured)
}

func TestSessionsHandler_OK(t *testing.T) {
	h := NewSessionsHandler(&handlerProvider{sessions: []Session{}})
	if _, err := h.Handle(context.Background(), &NoInput{}); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyticsHandlers_ActionNames(t *testing.T) {
	cases := map[string]string{
		NewStatsHandler(nil).Action():    "stats",
		NewMissionsHandler(nil).Action(): "missions",
		NewSessionsHandler(nil).Action(): "sessions",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("action name = %q, esperava %q", got, want)
		}
	}
}
