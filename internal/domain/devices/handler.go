package devices

import (
	"context"

	"focusguard/internal/domain/ipcerr"
)

// Service is the catalog surface the handlers need (satisfied by *Store).
type Service interface {
	List() []Device
	Get(ip string) (Device, bool)
	Upsert(d Device) error
	Remove(ip string) error
}

// NoInput é a ausência de payload (a listagem não precisa de argumento).
type NoInput struct{}

// ListResult é a resposta de devices-list.
type ListResult struct {
	Devices []Device
}

// UpsertInput carrega o device a criar/atualizar.
type UpsertInput struct {
	Device Device
}

// UpsertResult é a resposta de devices-upsert.
type UpsertResult struct {
	Message string
}

// RemoveInput carrega o IP a remover.
type RemoveInput struct {
	IP string
}

// RemoveResult é a resposta de devices-remove.
type RemoveResult struct {
	Message string
}

// ListHandler executa "devices-list".
type ListHandler struct {
	svc Service
}

// NewList builds the "devices-list" handler.
func NewList(svc Service) *ListHandler { return &ListHandler{svc: svc} }

func (h *ListHandler) Action() string { return "devices-list" }

func (h *ListHandler) Validate(*NoInput) error { return nil }

func (h *ListHandler) Handle(_ context.Context, _ *NoInput) (*ListResult, error) {
	if h.svc == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "catálogo de dispositivos não configurado")
	}
	return &ListResult{Devices: h.svc.List()}, nil
}

// UpsertHandler executa "devices-upsert".
type UpsertHandler struct {
	svc Service
}

// NewUpsert builds the "devices-upsert" handler.
func NewUpsert(svc Service) *UpsertHandler { return &UpsertHandler{svc: svc} }

func (h *UpsertHandler) Action() string { return "devices-upsert" }

func (h *UpsertHandler) Validate(*UpsertInput) error { return nil }

func (h *UpsertHandler) Handle(_ context.Context, in *UpsertInput) (*UpsertResult, error) {
	if h.svc == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "catálogo de dispositivos não configurado")
	}
	if err := h.svc.Upsert(in.Device); err != nil {
		return nil, err
	}
	label := in.Device.Name
	if label == "" {
		label = in.Device.IP
	}
	return &UpsertResult{Message: "Política de " + label + " atualizada"}, nil
}

// RemoveHandler executa "devices-remove".
type RemoveHandler struct {
	svc Service
}

// NewRemove builds the "devices-remove" handler.
func NewRemove(svc Service) *RemoveHandler { return &RemoveHandler{svc: svc} }

func (h *RemoveHandler) Action() string { return "devices-remove" }

func (h *RemoveHandler) Validate(*RemoveInput) error { return nil }

func (h *RemoveHandler) Handle(_ context.Context, in *RemoveInput) (*RemoveResult, error) {
	if h.svc == nil {
		return nil, ipcerr.New(ipcerr.CodeNotConfigured, "catálogo de dispositivos não configurado")
	}
	if err := h.svc.Remove(in.IP); err != nil {
		return nil, err
	}
	return &RemoveResult{Message: "Dispositivo " + in.IP + " removido"}, nil
}
