// Package analytics implements the domain service for the stats/missions/
// sessions IPC actions (focus reports — Fase 4 do refactor-plan). The Service
// depends only on the minimal Provider interface (DIP); the *analytics.Recorder
// satisfies it structurally.
//
// The Service intentionally does NOT import internal/transport/ipc: ipc imports analytics
// for the wire types (Response embeds Stats/LabelStat/Session), so importing it
// back would create an import cycle. The ipc adapter (internal/transport/ipc) reads the
// domain results and builds the wire Response; stable error codes travel in
// *ipcerr.Error.
package analytics

import (
	"context"
	"time"

	"focusguard/internal/transport/ipcerr"
)

// maxSessionsReturned caps the "sessions" IPC response so the UI never
// receives the whole analytics file (a long-time user can have thousands of
// lines).
const maxSessionsReturned = 50

// Provider supplies the recorded sessions for the report actions. The daemon
// wires the analytics recorder; tests stub it.
type Provider interface {
	Sessions() ([]Session, error)
}

// Service executes the focus report actions against a Provider.
type Service struct {
	provider Provider
}

// NewService builds the report service. A nil provider makes every action fail
// with the "não configurado" message (tests/dev builds), as the legacy switch
// did.
func NewService(provider Provider) *Service { return &Service{provider: provider} }

// Stats aggregates the focus report (last 7 days), optionally scoped to a
// mission via the Mission filter.
func (s *Service) Stats(_ context.Context, mission string) (*Stats, error) {
	if s.provider == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "analytics não configurado")
	}
	sessions, err := s.provider.Sessions()
	if err != nil {
		return nil, err
	}
	if mission != "" {
		return SummarizeByLabel(sessions, mission, 7, time.Now()), nil
	}
	return Summarize(sessions, 7, time.Now()), nil
}

// Missions aggregates the focus per named mission (sessions with a Label).
func (s *Service) Missions(_ context.Context) ([]LabelStat, error) {
	if s.provider == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "analytics não configurado")
	}
	sessions, err := s.provider.Sessions()
	if err != nil {
		return nil, err
	}
	return SummarizeLabels(sessions), nil
}

// Sessions returns the most recent completed sessions, newest first, capped by
// maxSessionsReturned.
func (s *Service) Sessions(_ context.Context) ([]Session, error) {
	if s.provider == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "analytics não configurado")
	}
	sessions, err := s.provider.Sessions()
	if err != nil {
		return nil, err
	}
	return RecentSessions(sessions, maxSessionsReturned), nil
}
