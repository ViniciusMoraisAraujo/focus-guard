package ipc

import (
	"sort"
	"time"
)

// Permission models the authorization level the web proxy (focusguard-web)
// enforces for an action. The daemon itself trusts the local IPC — CLI, tray
// and web all share the same access level — so authorization lives in the web
// layer; the spec table is the single place that declares it (B6).
type Permission int

const (
	// PermPublic: no session required (health, ping, login, auth-status).
	// Reservado para o vocabulário do enum — nenhuma ação do /api/action usa
	// hoje (todas exigem sessão; as públicas vivem em rotas próprias do httpapi).
	PermPublic Permission = iota
	// PermAuthenticated: any valid session.
	PermAuthenticated
	// PermSelf: valid session; a non-admin may only act on the resource named
	// by SelfField (ex.: user-set-password); admin always passes.
	PermSelf
	// PermAdmin: admin only (user-list/user-add/user-remove).
	PermAdmin
)

// ActionSpec carries the declarative metadata for one IPC action, shared by
// the daemon (execution) and focusguard-web (authz + proxy timeout) — both
// binaries import internal/ipc, so this table is the single source of truth.
type ActionSpec struct {
	Action     string
	Permission Permission
	// SelfField names the Request field that is the "own resource" for
	// PermSelf actions (ex.: "user_name" for user-set-password).
	SelfField string
	// Timeout is the web proxy's send budget for the action. It must be ≥ the
	// daemon's own internal budget for the same action (the proxy only waits
	// for the reply), so a slow-but-successful action is never reported as
	// "daemon indisponível".
	Timeout time.Duration
}

// specs is the single source of truth for action metadata. user-verify is
// deliberately ABSENT: it is web-only (used by /api/login, which talks
// directly to the daemon) and must never be reachable through /api/action —
// SpecFor returns false for it, so the proxy 403s it (allowlist by spec)
// instead of forwarding (which would turn it into a password oracle without
// the login rate limit).
var specs = map[string]ActionSpec{
	"block":             {Action: "block", Permission: PermAuthenticated, Timeout: 30 * time.Second},
	"block-all":         {Action: "block-all", Permission: PermAuthenticated, Timeout: 30 * time.Second},
	"status":            {Action: "status", Permission: PermAuthenticated, Timeout: 15 * time.Second},
	"ping":              {Action: "ping", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"update":            {Action: "update", Permission: PermAuthenticated, Timeout: 150 * time.Second},
	"update-check":      {Action: "update-check", Permission: PermAuthenticated, Timeout: 150 * time.Second},
	"presets":           {Action: "presets", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"preset-add":        {Action: "preset-add", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"preset-remove":     {Action: "preset-remove", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"tamper-log":        {Action: "tamper-log", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"apps-list":         {Action: "apps-list", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"apps-add":          {Action: "apps-add", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"apps-remove":       {Action: "apps-remove", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"user-list":         {Action: "user-list", Permission: PermAdmin, Timeout: 5 * time.Second},
	"user-add":          {Action: "user-add", Permission: PermAdmin, Timeout: 5 * time.Second},
	"user-remove":       {Action: "user-remove", Permission: PermAdmin, Timeout: 5 * time.Second},
	"user-set-password": {Action: "user-set-password", Permission: PermSelf, SelfField: "user_name", Timeout: 5 * time.Second},
	"schedule-list":     {Action: "schedule-list", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"schedule-add":      {Action: "schedule-add", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"schedule-import":   {Action: "schedule-import", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"schedule-remove":   {Action: "schedule-remove", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"pomodoro":          {Action: "pomodoro", Permission: PermAuthenticated, Timeout: 30 * time.Second},
	"pomodoro-stop":     {Action: "pomodoro-stop", Permission: PermAuthenticated, Timeout: 30 * time.Second},
	"pomodoro-defaults": {Action: "pomodoro-defaults", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"goal-get":          {Action: "goal-get", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"goal-set":          {Action: "goal-set", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"stats":             {Action: "stats", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"missions":          {Action: "missions", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"sessions":          {Action: "sessions", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"event-subscribe":   {Action: "event-subscribe", Permission: PermAuthenticated, Timeout: 30 * time.Second},
	"dns-start":         {Action: "dns-start", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"dns-stop":          {Action: "dns-stop", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"dns-status":        {Action: "dns-status", Permission: PermAuthenticated, Timeout: 5 * time.Second},
	"dns-set-upstream":  {Action: "dns-set-upstream", Permission: PermAuthenticated, Timeout: 5 * time.Second},
}

// SpecFor returns the declarative metadata for an action. Absence means the
// action is web-only (user-verify) or unknown — the web proxy must not
// forward it (allowlist by spec → 403).
func SpecFor(action string) (ActionSpec, bool) {
	s, ok := specs[action]
	return s, ok
}

// SpecActions returns the spec'd action names, sorted — used by boot
// validation and tests to detect drift between the spec table and the
// registry (every spec must have a handler).
func SpecActions() []string {
	out := make([]string, 0, len(specs))
	for a := range specs {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}
