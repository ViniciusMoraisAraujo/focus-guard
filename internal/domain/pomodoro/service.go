// Package pomodoro implements the domain service for the pomodoro/
// pomodoro-defaults/pomodoro-stop IPC actions (Fase 4 do refactor-plan). The
// Service depends only on narrow interfaces (Runner, Prefs, Catalog — DIP); the
// *pomodoro.Controller, *pomodoro.Prefs and the preset catalog satisfy them
// structurally.
//
// The Service intentionally does NOT import internal/ipc: ipc imports pomodoro
// for the wire types (Response.Pomodoro embeds pomodoro.State), so importing it
// back would create an import cycle. The ipc adapter (internal/ipc) reads the
// domain results and builds the wire Response; stable error codes travel in
// *ipcerr.Error.
package pomodoro

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"focusguard/internal/domain/preset"
	"focusguard/internal/transport/ipcerr"
)

// Tetos defensivos: time.Duration(work)*time.Minute faria overflow (wrap) no
// int64 para valores gigantes — um --work 1e9 virava uma sessão de ~147 anos em
// vez de erro. O mesmo vale para o acumulador focus do controller (focus +=
// s.Work por ciclo): um --cycles 1e9 transbordaria o tempo registrado no
// analytics. Uma semana por fase / 1k ciclos já é absurdo para um pomodoro,
// então estes tetos nunca atrapalham o uso real.
const (
	maxPomodoroMinutes = 7 * 24 * 60 // 7 dias por fase
	maxPomodoroCycles  = 1000
)

// Runner is the pomodoro controller surface the service starts and stops.
type Runner interface {
	Start(s Session) (State, error)
	Stop() (State, error)
}

// PrefsStore persists/reads the pomodoro defaults (work/rest/cycles) so a
// plain session reuses the last values. The ipc layer's PomodoroPrefs is the
// *Prefs view.
type PrefsStore interface {
	Resolve(work, rest, cycles int) (int, int, int)
	Remember(work, rest, cycles int)
}

// Catalog resolves the session preset to its domains.
type Catalog interface {
	Resolve(name string) (preset.Preset, error)
}

// Service executes the pomodoro actions.
type Service struct {
	runner  Runner
	prefs   PrefsStore
	catalog Catalog
}

// NewService builds the pomodoro service. A nil runner/prefs makes the
// respective actions fail as the legacy switch did (tests/dev builds).
func NewService(runner Runner, prefs PrefsStore, catalog Catalog) *Service {
	return &Service{runner: runner, prefs: prefs, catalog: catalog}
}

// StartResult is the outcome of a successful session start: the live state and
// the PT-BR confirmation message (which depends on the resolved preset and
// defaults).
type StartResult struct {
	State   State
	Message string
}

// Start begins a pomodoro session: resolves the preset to domains, merges the
// explicit parameters with the saved defaults and hands the session to the
// runner. Validation order and messages mirror the legacy switch.
func (s *Service) Start(_ context.Context, presetName, label string, work, rest, cycles int, strict, save bool) (StartResult, error) {
	// O runner nulo é o primeiro caso e devolve um erro SEM código (mensagem
	// pura) — comportamento do switch legado, diferente do pomodoro-stop.
	if s.runner == nil {
		return StartResult{}, errors.New("pomodoro não configurado")
	}
	if strings.TrimSpace(presetName) == "" {
		return StartResult{}, ipcerr.New(ipcerr.CodeInvalid, "Informe um preset (ex: --preset social).")
	}

	// Resolve defaults salvos: a CLI envia 0 (não informado) para work/cycles
	// e -1 (não informado) para rest — 0 é um valor legítimo para rest (sem
	// descanso). Sem prefs configuradas, caem no clássico 25/5/4.
	resWork, resRest, resCycles := work, rest, cycles
	if s.prefs != nil {
		resWork, resRest, resCycles = s.prefs.Resolve(work, rest, cycles)
	}
	if resWork <= 0 || resWork > maxPomodoroMinutes {
		return StartResult{}, ipcerr.New(ipcerr.CodeDurationInvalid, fmt.Sprintf("Duração de trabalho inválida (--work entre 1 e %d minutos).", maxPomodoroMinutes))
	}
	if resRest < 0 || resRest > maxPomodoroMinutes || resCycles < 1 || resCycles > maxPomodoroCycles {
		return StartResult{}, ipcerr.New(ipcerr.CodeDurationInvalid, fmt.Sprintf("Parâmetros de pomodoro inválidos (--rest entre 0 e %d minutos, --cycles entre 1 e %d).", maxPomodoroMinutes, maxPomodoroCycles))
	}

	p, err := s.catalog.Resolve(presetName)
	if err != nil {
		return StartResult{}, err
	}

	sess := Session{
		Preset:  p.Name,
		Label:   label,
		Domains: p.Domains,
		Work:    time.Duration(resWork) * time.Minute,
		Rest:    time.Duration(resRest) * time.Minute,
		Cycles:  resCycles,
		Strict:  strict,
	}
	st, err := s.runner.Start(sess)
	if err != nil {
		return StartResult{}, err
	}

	msg := fmt.Sprintf("Pomodoro %s iniciado: %d ciclos de %dm trabalho / %dm descanso", p.Name, sess.Cycles, resWork, resRest)
	if save && s.prefs != nil {
		s.prefs.Remember(resWork, resRest, resCycles)
		msg += " (padrões salvos para a próxima sessão)"
	}
	return StartResult{State: st, Message: msg}, nil
}

// DefaultsResult is the resolved default work/rest/cycles plus the PT-BR
// report message.
type DefaultsResult struct {
	Work    int
	Rest    int
	Cycles  int
	Message string
}

// Defaults returns the current default parameters — work/cycles não informados
// (0) e rest não informado (-1): 0 é um valor legítimo para rest (sem
// descanso).
func (s *Service) Defaults(_ context.Context) (DefaultsResult, error) {
	if s.prefs == nil {
		return DefaultsResult{}, ipcerr.New(ipcerr.CodeNotConfigured, "preferências de pomodoro não configuradas")
	}
	work, rest, cycles := s.prefs.Resolve(0, -1, 0)
	return DefaultsResult{
		Work:    work,
		Rest:    rest,
		Cycles:  cycles,
		Message: fmt.Sprintf("Padrões atuais: %dm trabalho / %dm descanso / %d ciclos", work, rest, cycles),
	}, nil
}

// StopResult is the live state after stopping plus the PT-BR message.
type StopResult struct {
	State   State
	Message string
}

// Stop ends the current session.
func (s *Service) Stop(_ context.Context) (StopResult, error) {
	if s.runner == nil {
		return StopResult{}, ipcerr.New(ipcerr.CodeNotConfigured, "pomodoro não configurado")
	}
	st, err := s.runner.Stop()
	if err != nil {
		return StopResult{}, err
	}
	return StopResult{State: st, Message: "Pomodoro encerrado"}, nil
}
