package ipc

import "context"

// Handler is the executor of ONE action, registered in the daemon's
// ipc.Registry (Fase 3 do refactor-plan). Validate is pure (no dependencies
// nor effects); Handle receives only the dependencies it needs, injected by
// constructor (DIP). O wire protocol é preservado: o roteador decodifica o
// Request universal, valida e despacha — o Handler é a única peça que conhece
// a semântica de uma ação.
type Handler interface {
	Action() string
	Validate(*Request) error
	Handle(ctx context.Context, req *Request) (*Response, error)
}

// ActionError carries the stable error code (B12 — aditivo desde a Fase 2) +
// the human message. O roteador converte em Response{Success:false, Code,
// Message}; a UI/CLI passam a checar o código em vez do texto. Erros de
// domínio sem código estável continuam como erros comuns (errors.New) — o
// roteador devolve apenas a message original.
type ActionError struct {
	Code    string
	Message string
}

func (e *ActionError) Error() string { return e.Message }

// Err builds an ActionError com código estável e mensagem humana.
func Err(code, message string) *ActionError {
	return &ActionError{Code: code, Message: message}
}
