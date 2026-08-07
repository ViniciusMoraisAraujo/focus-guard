// Package schedule implements the domain service for the schedule-list/add/
// import/remove IPC actions (recurring block rules — Fase 4 do refactor-plan).
// The Service depends only on narrow interfaces (RuleStore, PresetResolver —
// DIP); the *schedule.Manager and the preset catalog satisfy them structurally.
//
// The Service intentionally does NOT import internal/transport/ipc: ipc imports schedule
// for the wire types (Request.ScheduleRule, Response.Schedules), so importing
// it back would create an import cycle. The ipc adapter (internal/transport/ipc) reads
// the domain results and builds the wire Response; stable error codes travel in
// *ipcerr.Error.
package schedule

import (
	"context"
	"errors"
	"strings"

	"focusguard/internal/domain/preset"
	"focusguard/internal/transport/ipcerr"
)

// RuleStore is the recurring-rule catalog the service reads and mutates.
type RuleStore interface {
	List() []Rule
	Add(r Rule) (Rule, error)
	Remove(id string) error
	ImportICS(data []byte, preset string) ([]Rule, error)
}

// PresetResolver validates that a calendar preset exists before import — a
// preset that never resolves would be imported silently and never applied
// (the worker skips presets it cannot resolve).
type PresetResolver interface {
	Resolve(name string) (preset.Preset, error)
}

// Service executes the schedule actions against a RuleStore.
type Service struct {
	store    RuleStore
	resolver PresetResolver
}

// NewService builds the schedule service. A nil store makes every action fail
// with the "não configurado" message (tests/dev builds), as the legacy switch
// did.
func NewService(store RuleStore, resolver PresetResolver) *Service {
	return &Service{store: store, resolver: resolver}
}

// List returns the rule catalog.
func (s *Service) List(_ context.Context) ([]Rule, error) {
	if s.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "agendamento não configurado")
	}
	return s.store.List(), nil
}

// Add persists a new rule.
func (s *Service) Add(_ context.Context, r Rule) (Rule, error) {
	if s.store == nil {
		return Rule{}, ipcerr.New(ipcerr.CodeNotConfigured, "agendamento não configurado")
	}
	return s.store.Add(r)
}

// Remove deletes a rule by ID.
func (s *Service) Remove(_ context.Context, id string) error {
	if s.store == nil {
		return ipcerr.New(ipcerr.CodeNotConfigured, "agendamento não configurado")
	}
	return s.store.Remove(id)
}

// Import parses an .ics calendar into rules scoped to one preset. Validation
// order and messages mirror the legacy switch: preset required, non-empty
// content, preset must resolve, and an import that finds no weekly event is a
// failure (no code — plain message).
func (s *Service) Import(_ context.Context, content, presetName string) ([]Rule, error) {
	if s.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "agendamento não configurado")
	}
	if strings.TrimSpace(presetName) == "" {
		return nil, ipcerr.New(ipcerr.CodeInvalid, "Informe o preset do calendário (ex: --preset social).")
	}
	if strings.TrimSpace(content) == "" {
		return nil, ipcerr.New(ipcerr.CodeInvalid, "Arquivo .ics vazio.")
	}
	if _, err := s.resolver.Resolve(presetName); err != nil {
		return nil, err
	}
	added, err := s.store.ImportICS([]byte(content), presetName)
	if err != nil {
		return nil, err
	}
	if len(added) == 0 {
		return nil, errors.New("Nenhum evento semanal encontrado no calendário.")
	}
	return added, nil
}
