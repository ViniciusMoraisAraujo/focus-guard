package ipc

import (
	"context"

	"focusguard/internal/policy"
)

var (
	TestSocketPath string
	TestDialAddr   string
)

type Request struct {
	Action   string `json:"action"`
	Domain   string `json:"domain,omitempty"`
	Duration string `json:"duration,omitempty"`
}

type Response struct {
	Success         bool           `json:"success"`
	Message         string         `json:"message,omitempty"`
	Blocks          []policy.Block `json:"blocks,omitempty"`
	ExpectedDoH     bool           `json:"expected_doh,omitempty"`
	DoHActive       bool           `json:"doh_active,omitempty"`
	FirewallRules   int            `json:"firewall_rules,omitempty"`
	ProtectionError string         `json:"protection_error,omitempty"`
	UpdateAvailable bool           `json:"update_available,omitempty"`
	UpdateVersion   string         `json:"update_version,omitempty"`
	CurrentVersion  string         `json:"current_version,omitempty"`
}

// UpdateStatus holds the outcome of an auto-update check.
type UpdateStatus struct {
	CurrentVersion string
	NewVersion     string
	Available      bool
	Applied        bool
}

// UpdateChecker performs an auto-update check. When apply is true the checker
// also applies the update to the daemon binary.
type UpdateChecker interface {
	Check(ctx context.Context, apply bool) (UpdateStatus, error)
}
