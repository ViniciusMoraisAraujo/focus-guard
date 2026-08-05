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

// TestExtend_PushesExpiryForward verifies Extend adds to the current expiry
// (it does not restart the block from now).
func TestExtend_PushesExpiryForward(t *testing.T) {
	now := time.Now()
	b := Block{
		Domain:    "youtube.com",
		StartedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(time.Hour),
	}

	b.Extend(30 * time.Minute)

	want := now.Add(90 * time.Minute)
	if !b.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", b.ExpiresAt, want)
	}
	// StartedAt não muda: extensão não é reinício.
	if !b.StartedAt.Equal(now.Add(-time.Hour)) {
		t.Errorf("StartedAt = %v, want original", b.StartedAt)
	}
}

// TestExtend_NonPositiveIsNoOp verifies Extend never shortens or moves a block
// backwards.
func TestExtend_NonPositiveIsNoOp(t *testing.T) {
	now := time.Now()
	b := Block{Domain: "x.com", ExpiresAt: now.Add(time.Hour)}

	b.Extend(0)
	if !b.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Error("Extend(0) should be a no-op")
	}

	b.Extend(-time.Hour)
	if !b.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Error("negative Extend should be a no-op")
	}
}
