package policy

import "time"

type Block struct {
	Domain      string    `json:"domain"`
	StartedAt   time.Time `json:"started_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	ResolvedIPs []string  `json:"resolved_ips"`
	// Allowlist names the domains still reachable under the all-internet
	// sentinel (deep-focus mode). Only populated for the
	// enforcer.AllInternetDomain block: the DNS sinkhole must NOT sinkhole
	// these, so the names (not just their IPs) are carried here. Additive
	// field — old blocks simply have an empty list.
	Allowlist []string `json:"allowlist,omitempty"`
}

func (b *Block) IsActive() bool {
	return time.Now().Before(b.ExpiresAt)
}

func (b *Block) CanUnblock() bool {

	return time.Now().After(b.ExpiresAt) || time.Now().Equal(b.ExpiresAt)
}

func (b *Block) RemainingTime() time.Duration {
	if !b.IsActive() {
		return 0
	}
	return time.Until(b.ExpiresAt)
}

// Extend pushes ExpiresAt forward by extra time (used by the "extend" IPC
// action — adds to the current expiry, it never restarts or shortens a
// block). A non-positive extra is a no-op.
func (b *Block) Extend(extra time.Duration) {
	if extra <= 0 {
		return
	}
	b.ExpiresAt = b.ExpiresAt.Add(extra)
}
