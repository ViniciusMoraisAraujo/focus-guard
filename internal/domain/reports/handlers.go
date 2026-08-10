// Handlers das ações reports-config-get / reports-config-set /
// reports-generate — cada ação é um Handler auto-contido que depende só da
// interface mínima (DIP); o *reports.Store e o provider de sessões satisfazem
// estruturalmente. O transporte os adapta via ipc.DomainAction.
package reports

import (
	"context"
	"time"

	"focusguard/internal/domain/ipcerr"
)

// NoInput: ações sem payload de entrada.
type NoInput struct{}

// ConfigResult é a resposta de reports-config-get.
type ConfigResult struct {
	Config Config
}

// ConfigInput carrega a nova config (reports-config-set).
type ConfigInput struct {
	Config Config
}

// ConfigSetResult é a resposta de reports-config-set.
type ConfigSetResult struct {
	Message string
	Config  Config
}

// GenerateInput carrega um caminho de export opcional para reports-generate
// (vazio = a pasta configurada).
type GenerateInput struct {
	ExportPath string
}

// GenerateResult é a resposta de reports-generate.
type GenerateResult struct {
	HTMLPath string
	JSONPath string
	Message  string
}

// ConfigStore é a superfície da config persistida (satisfeita por *Store).
type ConfigStore interface {
	Get() Config
	Set(Config) error
}

// ConfigGetHandler executa "reports-config-get".
type ConfigGetHandler struct {
	store ConfigStore
}

// NewConfigGet builds the "reports-config-get" handler.
func NewConfigGet(store ConfigStore) *ConfigGetHandler { return &ConfigGetHandler{store: store} }

func (h *ConfigGetHandler) Action() string { return "reports-config-get" }

func (h *ConfigGetHandler) Validate(*NoInput) error { return nil }

func (h *ConfigGetHandler) Handle(_ context.Context, _ *NoInput) (*ConfigResult, error) {
	if h.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "relatório semanal não configurado")
	}
	return &ConfigResult{Config: h.store.Get()}, nil
}

// ConfigSetHandler executa "reports-config-set".
type ConfigSetHandler struct {
	store ConfigStore
}

// NewConfigSet builds the "reports-config-set" handler.
func NewConfigSet(store ConfigStore) *ConfigSetHandler { return &ConfigSetHandler{store: store} }

func (h *ConfigSetHandler) Action() string { return "reports-config-set" }

func (h *ConfigSetHandler) Validate(*ConfigInput) error { return nil }

func (h *ConfigSetHandler) Handle(_ context.Context, in *ConfigInput) (*ConfigSetResult, error) {
	if h.store == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "relatório semanal não configurado")
	}
	if err := h.store.Set(in.Config); err != nil {
		return nil, err
	}
	cfg := h.store.Get()
	msg := "Relatório semanal desativado"
	if cfg.Enabled {
		msg = "Relatório semanal ativado"
	}
	return &ConfigSetResult{Message: msg, Config: cfg}, nil
}

// GenerateHandler executa "reports-generate" (geração imediata — o botão
// "Gerar agora" da UI e a primeira execução do worker no boot).
type GenerateHandler struct {
	store ConfigStore
	p     Provider
}

// NewGenerate builds the "reports-generate" handler.
func NewGenerate(store ConfigStore, p Provider) *GenerateHandler {
	return &GenerateHandler{store: store, p: p}
}

func (h *GenerateHandler) Action() string { return "reports-generate" }

func (h *GenerateHandler) Validate(*GenerateInput) error { return nil }

func (h *GenerateHandler) Handle(_ context.Context, in *GenerateInput) (*GenerateResult, error) {
	if h.store == nil || h.p == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "relatório semanal não configurado")
	}
	cfg := h.store.Get()
	if in != nil && in.ExportPath != "" {
		cfg.ExportPath = in.ExportPath
	}
	htmlPath, jsonPath, err := Generate(h.p, cfg, time.Now())
	if err != nil {
		return nil, err
	}
	return &GenerateResult{
		HTMLPath: htmlPath,
		JSONPath: jsonPath,
		Message:  "Relatório gerado: " + htmlPath,
	}, nil
}
