package ipc

import (
	"context"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/policy"
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
	"focusguard/internal/schedule"
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
	// Allowlist lists the domains still reachable under the block-all action
	// (deep-focus mode); empty means block all internet (panic mode).
	Allowlist []string `json:"allowlist,omitempty"`
	// GoalMinutes drives the goal-set action (daily focus goal in minutes).
	GoalMinutes int `json:"goal_minutes,omitempty"`
}

type Response struct {
	Success         bool             `json:"success"`
	Message         string           `json:"message,omitempty"`
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
}

// UpdateStatus holds the outcome of an auto-update check.
type UpdateStatus struct {
	CurrentVersion string
	NewVersion     string
	Available      bool
	Applied        bool
}

// UpdateChecker performs an auto-update check. When apply is true the checker
// also applies the update to the daemon binary. channel selects the release
// channel ("" or "stable" skips prereleases; "beta" opts in).
type UpdateChecker interface {
	Check(ctx context.Context, apply bool, channel string) (UpdateStatus, error)
}
