package policy

import "time"

type Block struct {
	Domain      string    `json:"domain"`
	StartedAt   time.Time `json:"started_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	ResolvedIPs []string  `json:"resolved_ips"`
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
