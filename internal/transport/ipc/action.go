package ipc

import (
	"context"
	"errors"

	"focusguard/internal/transport/ipcerr"
)

// Handler executes ONE action, registered in the Registry. Validate is pure
// (payload × action checks, no dependencies or side effects) and runs before
// Handle; Handle performs the work with the dependencies it received by
// constructor (DIP). The router converts errors via writeError.
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

// errorResponse converts a handler error into the wire Response. ActionError
// keeps its stable code; *ipcerr.Error (the shared code carried by the domain
// services that cannot import ipc — analytics, pomodoro, schedule, update) is
// mapped to the same wire shape; any other error keeps its message
// (success:false) — behavior identical to the pre-registry switch.
func errorResponse(err error) *Response {
	var ae *ActionError
	if errors.As(err, &ae) {
		return &Response{Success: false, Code: ae.Code, Message: ae.Message}
	}
	var se *ipcerr.Error
	if errors.As(err, &se) {
		return &Response{Success: false, Code: se.Code, Message: se.Message}
	}
	return &Response{Success: false, Message: err.Error()}
}

// funcHandler adapts plain functions to the Handler interface — the adapter
// used by the server's in-package handlers (Fase 5 moves each one to a domain
// package with explicit structs).
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
