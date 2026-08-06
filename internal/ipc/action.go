package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
)

// Handler executes ONE action, registered in the Registry. Validate is pure
// (payload × action checks, no dependencies or side effects) and runs before
// Handle; Handle performs the work with the dependencies it received by
// constructor (DIP). The router converts errors via writeError.
//
// Fase 3: low-risk actions are migrated as funcHandler adapters over the
// Server's existing interfaces; Fase 4 moves each one to a domain package
// with explicit structs and narrow dependency interfaces.
type Handler interface {
	Action() string
	Validate(*Request) error
	Handle(ctx context.Context, req *Request) (*Response, error)
}

// ActionError carries the stable error code (B12) plus the human message.
// The router converts it to Response{Success:false, Code, Message}; plain
// errors become Response{Success:false, Message} — behavior identical to the
// pre-registry switch.
type ActionError struct {
	Code    string
	Message string
}

func (e *ActionError) Error() string { return e.Message }

// Err builds an ActionError with a stable code and a human message.
func Err(code, message string) *ActionError {
	return &ActionError{Code: code, Message: message}
}

// writeError encodes an error into a Response on conn. ActionError keeps its
// stable code; any other error keeps its message (success:false).
func writeError(conn net.Conn, err error) {
	var ae *ActionError
	if errors.As(err, &ae) {
		_ = json.NewEncoder(conn).Encode(&Response{Success: false, Code: ae.Code, Message: ae.Message})
		return
	}
	_ = json.NewEncoder(conn).Encode(&Response{Success: false, Message: err.Error()})
}

// funcHandler adapts plain functions to the Handler interface — the Fase 3
// adapter used by the server's in-package handlers before they migrate to
// domain packages (Fase 4).
type funcHandler struct {
	action   string
	validate func(*Request) error
	handle   func(context.Context, *Request) (*Response, error)
}

func (h funcHandler) Action() string { return h.action }

func (h funcHandler) Validate(req *Request) error {
	if h.validate == nil {
		return nil
	}
	return h.validate(req)
}

func (h funcHandler) Handle(ctx context.Context, req *Request) (*Response, error) {
	return h.handle(ctx, req)
}
