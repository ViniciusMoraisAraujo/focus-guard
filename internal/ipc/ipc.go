package ipc

import "focusguard/internal/policy"

var (
	TestSocketPath string
	TestDialAddr   string
)

type Request struct {
	Action   string `json:"action"` // "block" ou "status"
	Domain   string `json:"domain,omitempty"`
	Duration string `json:"duration,omitempty"` // ex: "4h", "30m"
}

type Response struct {
	Success         bool           `json:"success"`
	Message         string         `json:"message,omitempty"`
	Blocks          []policy.Block `json:"blocks,omitempty"`
	ExpectedDoH     bool           `json:"expected_doh,omitempty"`
	DoHActive       bool           `json:"doh_active,omitempty"`
	FirewallRules   int            `json:"firewall_rules,omitempty"`
	ProtectionError string         `json:"protection_error,omitempty"`
}
