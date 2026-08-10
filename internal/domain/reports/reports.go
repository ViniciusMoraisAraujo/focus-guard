// Package reports implements the automatic weekly focus report (Fase 5.1 do
// features-plan): a persisted schedule config (enabled, day of week, hour,
// minute, export path) and an on-demand generator that writes a
// self-contained HTML report (plus a JSON copy) from the analytics sessions.
//
// The generator reuses the analytics exports (ExportHTML/ExportJSON) — this
// package owns only the schedule + the file writing. Writing is best-effort
// by contract: a missing export path degrades to the default folder and a
// write failure surfaces as an error for the caller (the daemon logs it and
// keeps running — a report failure never takes down the daemon).
package reports

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"focusguard/internal/domain/analytics"
)

// DefaultExportPath é a pasta padrão dos relatórios quando o usuário não
// configura uma (expande ~ no momento da escrita).
const DefaultExportPath = "~/FocusGuardReports"

// DefaultConfig é o horário padrão: domingo (0) às 23:59, desativado.
// A geração fica desligada até o usuário ligar na UI/CLI.
func DefaultConfig() Config {
	return Config{DayOfWeek: 0, Hour: 23, Minute: 59, ExportPath: DefaultExportPath}
}

// Config é o agendamento do relatório semanal, persistido em reports.json.
// DayOfWeek segue time.Weekday (0 = domingo … 6 = sábado).
type Config struct {
	Enabled    bool   `json:"enabled"`
	DayOfWeek  int    `json:"day_of_week,omitempty"`
	Hour       int    `json:"hour,omitempty"`
	Minute     int    `json:"minute,omitempty"`
	ExportPath string `json:"export_path,omitempty"`
}

// Valid valida o agendamento (dia 0-6, hora 0-23, minuto 0-59). Um caminho
// vazio é válido (usa o padrão na hora de escrever).
func (c Config) Valid() error {
	if c.DayOfWeek < 0 || c.DayOfWeek > 6 {
		return errors.New("reports: dia da semana inválido (0=domingo … 6=sábado)")
	}
	if c.Hour < 0 || c.Hour > 23 {
		return errors.New("reports: hora inválida (0-23)")
	}
	if c.Minute < 0 || c.Minute > 59 {
		return errors.New("reports: minuto inválido (0-59)")
	}
	return nil
}

// Store persiste o Config em reports.json (ao lado do state.json). Arquivo
// ausente/corrompido degrada para o DefaultConfig — a feature é opcional.
type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

// NewStore abre (ou cria) a config de relatórios em path. Path vazio mantém
// tudo em memória (testes).
func NewStore(path string) *Store {
	s := &Store{path: path, cfg: DefaultConfig()}
	if path == "" {
		return s
	}
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return
	}
	if c.ExportPath == "" {
		c.ExportPath = DefaultExportPath
	}
	s.cfg = c
}

// Get devolve a config atual (cópia).
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Set persiste a config (validando antes). Setar uma config inválida não
// altera nada.
func (s *Store) Set(c Config) error {
	if err := c.Valid(); err != nil {
		return err
	}
	if c.ExportPath == "" {
		c.ExportPath = DefaultExportPath
	}
	s.mu.Lock()
	s.cfg = c
	err := s.save()
	s.mu.Unlock()
	return err
}

func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// NextRun calcula o próximo instante de geração a partir de now, segundo a
// config: o próximo dia da semana (time.Weekday) às HH:MM. Quando now já
// passou do horário hoje, vai para a semana seguinte (a geração do dia em
// curso, se atrasada, é coberta pelo worker no boot).
func (c Config) NextRun(now time.Time) time.Time {
	daysAhead := (int(c.DayOfWeek) - int(now.Weekday()) + 7) % 7
	next := time.Date(now.Year(), now.Month(), now.Day(), c.Hour, c.Minute, 0, 0, now.Location()).AddDate(0, 0, daysAhead)
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

// Provider supplies the sessions for the report (satisfied by
// *analytics.Recorder — DIP, como o Service de analytics).
type Provider interface {
	Sessions() ([]analytics.Session, error)
}

// Generate escreve o relatório semanal (HTML + JSON) na pasta de export da
// config e devolve os caminhos escritos. A pasta é criada se necessário
// (expande ~ no caminho). Erro de escrita é reportado ao chamador — nunca
// derruba o daemon.
func Generate(p Provider, cfg Config, now time.Time) (htmlPath, jsonPath string, err error) {
	if p == nil {
		return "", "", errors.New("reports: analytics não configurado")
	}
	sessions, err := p.Sessions()
	if err != nil {
		return "", "", err
	}
	st := analytics.Summarize(sessions, 7, now)

	dir := expandHome(cfg.ExportPath)
	if dir == "" {
		dir = expandHome(DefaultExportPath)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("reports: criar pasta %s: %w", dir, err)
	}

	// Nome com a semana de referência: focusguard-2026-W33.html (ISO 8601).
	year, week := now.ISOWeek()
	base := fmt.Sprintf("focusguard-%04d-W%02d", year, week)
	htmlPath = filepath.Join(dir, base+".html")
	jsonPath = filepath.Join(dir, base+".json")

	if err := os.WriteFile(htmlPath, []byte(analytics.ExportHTML(st)), 0o644); err != nil {
		return "", "", fmt.Errorf("reports: escrever %s: %w", htmlPath, err)
	}
	if js, jerr := analytics.ExportJSON(st); jerr != nil {
		return "", "", jerr
	} else if err := os.WriteFile(jsonPath, []byte(js), 0o644); err != nil {
		return "", "", fmt.Errorf("reports: escrever %s: %w", jsonPath, err)
	}
	return htmlPath, jsonPath, nil
}

// expandHome resolve um "~/" inicial para o diretório home do usuário
// (os.UserHomeDir; sem home, devolve o caminho sem expandir).
func expandHome(path string) string {
	if path == "" || path == "~" {
		return path
	}
	if path[:2] == "~/" || path[:2] == "~\\" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
