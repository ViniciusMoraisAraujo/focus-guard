package ipc

import "context"

// DomainAction adapts one domain action to the ipc router (DIP — pós-reorg
// item 2). Domain packages define their own input/output types and never
// import ipc; the composition root mounts them here, translating the wire
// Request/Response via Decode/Encode. Validate is optional (nil = no pure
// validation); the router keeps converting errors exactly as before
// (*ipcerr.Error and *ActionError → Response{Success:false, Code, Message},
// plain errors → Response{Success:false, Message}).
type DomainAction[In, Out any] struct {
	// Name is the action id ("apps-list", "block", ...) — matches the Action
	// string of the domain handler and the spec in spec.go.
	Name string
	// Decode converts the wire Request into the domain input. It must be
	// pure (no side effects): the router calls it once for Validate and once
	// for Handle.
	Decode func(*Request) (*In, error)
	// Validate is the domain's pure payload validation (optional).
	Validate func(*In) error
	// Handle runs the domain action with the dependencies it received by
	// constructor.
	Handle func(ctx context.Context, in *In) (*Out, error)
	// Encode converts the domain output into the wire Response.
	Encode func(*Out) (*Response, error)
}

func (a DomainAction[In, Out]) handler() Handler {
	return funcHandler{
		action: a.Name,
		validate: func(req *Request) error {
			in, err := a.Decode(req)
			if err != nil {
				return err
			}
			if a.Validate == nil {
				return nil
			}
			return a.Validate(in)
		},
		handle: func(ctx context.Context, req *Request) (*Response, error) {
			in, err := a.Decode(req)
			if err != nil {
				return nil, err
			}
			out, err := a.Handle(ctx, in)
			if err != nil {
				return nil, err
			}
			return a.Encode(out)
		},
	}
}

// Handler returns the ipc.Handler for this domain action — the composition
// root mounts it with s.Register(ipc.DomainAction[...]{...}.Handler()). The
// action needs a spec in spec.go like any other handler — ValidateRegistry
// covers it at boot.
func (a DomainAction[In, Out]) Handler() Handler {
	return a.handler()
}
