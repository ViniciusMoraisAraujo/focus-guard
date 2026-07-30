package policy

import (
	"testing"
	"time"
)

func TestBlockLifeCycle(t *testing.T) {
	now := time.Now()

	activeBlock := Block{
		Domain:    "youtube.com",
		StartedAt: now,
		ExpiresAt: now.Add(1 * time.Hour),
	}

	if !activeBlock.IsActive() {
		t.Errorf("block should be active")
	}

	if activeBlock.CanUnblock() {
		t.Errorf("block should not be unblocked")
	}

	expiredBlock := Block{
		Domain:    "youtube.com",
		StartedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-1 * time.Hour),
	}

	if expiredBlock.IsActive() {
		t.Errorf("block should not be active")
	}

	if !expiredBlock.CanUnblock() {
		t.Errorf("expired block should be ready for unblock (CanUnblock = true)")
	}
}
