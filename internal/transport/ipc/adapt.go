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
// covers it at boot. Fails fast (panic) when a required closure is missing, so
// a misconfigured action breaks at boot, not at dispatch time.
func (a DomainAction[In, Out]) Handler() Handler {
	if a.Name == "" || a.Decode == nil || a.Handle == nil || a.Encode == nil {
		panic("ipc.DomainAction: Name, Decode, Handle e Encode são obrigatórios")
	}
	return a.handler()
}

// NoInputDecode returns a Decode for actions without payload: it ignores the
// wire Request and returns a zero In. Actions with fields still write their
// own Decode.
func NoInputDecode[In any]() func(*Request) (*In, error) {
	return func(*Request) (*In, error) { return new(In), nil }
}
