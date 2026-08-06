package ipc

import (
	"context"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/policy"
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
	"focusguard/internal/schedule"
	"focusguard/internal/tamper"
)

var (
	TestSocketPath string
	TestDialAddr   string
)

type Request struct {
	Action   string `json:"action"`
	Domain   string `json:"domain,omitempty"`
	Duration string `json:"duration,omitempty"`
	Preset   string `json:"preset,omitempty"`
	WorkMin  int    `json:"work_min,omitempty"`
	RestMin  int    `json:"rest_min,omitempty"`
	Cycles   int    `json:"cycles,omitempty"`
	Strict   bool   `json:"strict,omitempty"`
	// Save persists the resolved work/rest/cycles as the defaults for the
	// next pomodoro session (--save).
	Save bool `json:"save,omitempty"`
	// Label names the pomodoro session (a "mission", ex: --label "Estudar ENEM")
	// for the mission reports; Mission filters the stats report to one label.
	Label   string `json:"label,omitempty"`
	Mission string `json:"mission,omitempty"`
	// Channel selects the release channel for the update action: "" or
	// "stable" (default, skips prereleases) or "beta" (opt-in to prereleases).
	Channel string `json:"channel,omitempty"`
	// PresetName/Label/Domains describe a custom preset for preset-add/remove.
	PresetName    string   `json:"preset_name,omitempty"`
	PresetLabel   string   `json:"preset_label,omitempty"`
	PresetDomains []string `json:"preset_domains,omitempty"`
	// ScheduleRule/ScheduleID drive the schedule-add/remove actions.
	ScheduleRule schedule.Rule `json:"schedule_rule,omitempty"`
	ScheduleID   string        `json:"schedule_id,omitempty"`
	// ICSContent/ICSPreset drive schedule-import (raw .ics file content).
	ICSContent string `json:"ics_content,omitempty"`
	ICSPreset  string `json:"ics_preset,omitempty"`
	// Allowlist lists the domains still reachable under the block-all action
	// (deep-focus mode); empty means block all internet (panic mode).
	Allowlist []string `json:"allowlist,omitempty"`
	// GoalMinutes drives the goal-set action (daily focus goal in minutes).
	GoalMinutes int `json:"goal_minutes,omitempty"`
	// AppName drives the apps-add/apps-remove actions (process denylist).
	AppName string `json:"app_name,omitempty"`
	// Name is the focus-session/mission label for the pomodoro action.
	Name string `json:"name,omitempty"`
	// UserName/UserPassword drive the user-* actions (web login and user
	// management). The password travels over the local IPC socket only and is
	// hashed (bcrypt) by the daemon before anything reaches disk.
	UserName     string `json:"user_name,omitempty"`
	UserPassword string `json:"user_password,omitempty"`
	// Upstream drives the dns-set-upstream action: the resolver (host[:port])
	// the DNS sinkhole forwards allowed queries to.
	Upstream string `json:"upstream,omitempty"`
	// Extend/Replace resolve the conflict of the user-driven block action:
	// by default a block on an already-active domain returns Conflict=true so
	// the CLI/Web can ask the user; --extend sums the duration to the current
	// expiry and --replace restarts the window from now. Schedule and pomodoro
	// keep their silent upsert and never send these.
	Extend  bool `json:"extend,omitempty"`
	Replace bool `json:"replace,omitempty"`
	// Since drives the event-subscribe long-poll (Fase 7): the last event
	// sequence (rev) the client saw. The daemon blocks until an event with a
	// higher sequence is published (or its internal budget expires) and
	// returns the newer events plus the new rev.
	Since int64 `json:"since,omitempty"`
}

// Event is one daemon state-change notification (event-subscribe). Events are
// coarse and payload-free on purpose: the daemon state is the source of truth,
// so the subscriber re-fetches the affected data when an event arrives. Type
// is a stable token ("blocks-changed", "pomodoro-changed",
// "pomodoro-complete", "schedule-changed").
type Event struct {
	Type string    `json:"type"`
	At   time.Time `json:"at"`
}

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	// Code is a stable, machine-readable error identifier (aditivo — Fase 2 do
	// refactor-plan.md). Empty on success and on domain errors that carry no
	// stable code; when present the UI/CLI may branch on it instead of parsing
	// the human message (B12). Additive: old clients simply ignore it.
	Code            string           `json:"code,omitempty"`
	Blocks          []policy.Block   `json:"blocks,omitempty"`
	ExpectedDoH     bool             `json:"expected_doh,omitempty"`
	DoHActive       bool             `json:"doh_active,omitempty"`
	FirewallRules   int              `json:"firewall_rules,omitempty"`
	ProtectionError string           `json:"protection_error,omitempty"`
	UpdateAvailable bool             `json:"update_available,omitempty"`
	UpdateVersion   string           `json:"update_version,omitempty"`
	CurrentVersion  string           `json:"current_version,omitempty"`
	Presets         []preset.Preset  `json:"presets,omitempty"`
	Pomodoro        *pomodoro.State  `json:"pomodoro,omitempty"`
	Stats           *analytics.Stats `json:"stats,omitempty"`
	Schedules       []schedule.Rule  `json:"schedules,omitempty"`
	// Goal is the current daily focus goal (goal-get / status).
	Goal time.Duration `json:"goal,omitempty"`
	// PomodoroWork/Rest/Cycles are the resolved defaults (pomodoro-defaults).
	PomodoroWork  int `json:"pomodoro_work,omitempty"`
	PomodoroRest  int `json:"pomodoro_rest,omitempty"`
	PomodoroCycle int `json:"pomodoro_cycles,omitempty"`
	// LabelStats aggregates focus per named mission (mission command).
	LabelStats []analytics.LabelStat `json:"label_stats,omitempty"`
	// Sessions lists the most recent completed focus sessions (sessions
	// action), newest first, capped by the server.
	Sessions []analytics.Session `json:"sessions,omitempty"`
	// Apps is the process denylist (apps-list).
	Apps []string `json:"apps,omitempty"`
	// TamperLog lists detected tamper attempts (tamper-log).
	TamperLog []tamper.Event `json:"tamper_log,omitempty"`
	// Conflict marks a failed "block" request that hit an already-active block
	// (the default ask-first behavior of user-driven blocking). The client
	// should surface it as "somar/substituir" instead of a hard error.
	Conflict bool `json:"conflict,omitempty"`
	// ConflictBlock carries the existing block that caused the conflict, so the
	// UI/CLI can show its expiry without an extra round-trip.
	ConflictBlock *policy.Block `json:"conflict_block,omitempty"`
	// Rev/Events come from the event-subscribe long-poll (Fase 7): Rev is the
	// latest event sequence (the client echoes it back as Request.Since) and
	// Events are the notifications newer than the requested sequence, oldest
	// first (empty when the internal budget expired with no changes).
	Rev    int64   `json:"rev,omitempty"`
	Events []Event `json:"events,omitempty"`
	// Users lists the web UI usernames (user-list) — names only, never hashes.
	Users []string `json:"users,omitempty"`
	// UserIsAdmin reports whether the user-verify credentials belong to the
	// admin account, so the web UI can gate the user-management actions.
	UserIsAdmin bool `json:"user_is_admin,omitempty"`
	// UpdatePendingReboot marks an update that could not replace the running
	// binaries right away (Windows file lock) and was scheduled to complete at
	// the next system boot (MoveFileEx + MOVEFILE_DELAY_UNTIL_REBOOT). The
	// daemon keeps running the old version until then.
	UpdatePendingReboot bool `json:"update_pending_reboot,omitempty"`
	// DNSEnabled reports whether the DNS sinkhole server should be running
	// (persisted setting). DNSListening/DNSAddr/DNSUpstream/DNSQueries/
	// DNSBlocked describe its live state and counters (dns-status/status);
	// DNSBindError surfaces a port-53 bind failure so the UI/CLI can guide the
	// user to disable ICS/dnscache.
	DNSEnabled   bool   `json:"dns_enabled,omitempty"`
	DNSListening bool   `json:"dns_listening,omitempty"`
	DNSAddr      string `json:"dns_addr,omitempty"`
	DNSUpstream  string `json:"dns_upstream,omitempty"`
	DNSQueries   uint64 `json:"dns_queries,omitempty"`
	DNSBlocked   uint64 `json:"dns_blocked,omitempty"`
	DNSBindError string `json:"dns_bind_error,omitempty"`
}

// UpdateStatus holds the outcome of an auto-update check.
type UpdateStatus struct {
	CurrentVersion string
	NewVersion     string
	Available      bool
	Applied        bool
	// PendingReboot marca o fallback move-on-reboot: a troca dos binários foi
	// agendada para o próximo boot (Windows) e o daemon segue na versão antiga.
	PendingReboot bool
}

// UpdateChecker performs an auto-update check. When apply is true the checker
// also applies the update to the daemon binary. channel selects the release
// channel ("" or "stable" skips prereleases; "beta" opts in).
type UpdateChecker interface {
	Check(ctx context.Context, apply bool, channel string) (UpdateStatus, error)
}
