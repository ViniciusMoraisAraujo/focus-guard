// Package ipcerr defines the stable IPC error codes shared by the router
// (internal/transport/ipc) and the domain services (analytics, pomodoro, schedule,
// tamper). Those domain packages are imported by internal/transport/ipc for the wire
// types (Response embeds analytics.Stats, schedule.Rule, pomodoro.State,
// tamper.Event), so they cannot import ipc back — importing it would create an
// import cycle. The codes and the error type live here so every package reads
// the SAME constants instead of duplicating the strings (drift risk).
//
// internal/transport/ipc/codes.go re-exports the constants; the Response.Code wire field
// keeps its values. Additive only — new codes never break old clients.
package ipcerr

// Stable error codes (same values as internal/transport/ipc/codes.go).
const (
	// CodeDurationInvalid: malformed, zero or negative duration
	// (block/block-all/pomodoro).
	CodeDurationInvalid = "ERR_DURATION_INVALID"

	// CodeDomainRequired: a domain or preset to block is missing.
	CodeDomainRequired = "ERR_DOMAIN_REQUIRED"

	// CodeDomainConflict: domain already blocked and the client must decide
	// between extending (--extend) or replacing (--replace) — carries
	// Conflict=true and ConflictBlock.
	CodeDomainConflict = "ERR_DOMAIN_CONFLICT"

	// CodeInvalid: generic payload validation (invalid fields, wrong
	// credentials, malformed upstream, etc.).
	CodeInvalid = "ERR_INVALID"

	// CodeNotConfigured: the action requires a component that was not injected
	// into the server (tests, dev builds, incomplete install).
	CodeNotConfigured = "ERR_NOT_CONFIGURED"

	// CodeUnknownAction: action not recognized by the router.
	CodeUnknownAction = "ERR_UNKNOWN_ACTION"
)

// Error carries a stable code plus a human message for an IPC action failure.
// The ipc adapter maps it to ipc.Err (ActionError) when the service is
// reached through the router; services may also return plain errors, which
// surface as Success:false with the original message (no code), preserving
// the legacy behavior.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// New builds an Error with a stable code and a human message.
func New(code, message string) *Error { return &Error{Code: code, Message: message} }
