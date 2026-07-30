package ipc

import "focusguard/internal/policy"

type Request struct {
	Action   string `json:"action"` // "block" ou "status"
	Domain   string `json:"domain,omitempty"`
	Duration string `json:"duration,omitempty"` // ex: "4h", "30m"
}

type Response struct {
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Blocks  []policy.Block `json:"blocks,omitempty"`
}
