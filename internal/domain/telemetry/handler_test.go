package telemetry

import (
	"context"
	"testing"
	"time"
)

// fakeQuerier is a Querier stub returning a fixed query list.
type fakeQuerier struct{ qs []BlockedQuery }

func (f *fakeQuerier) Queries() ([]BlockedQuery, error) { return f.qs, nil }

func TestGetHandlerReturnsRecentAndSummary(t *testing.T) {
	now := time.Now().UTC()
	rec := &fakeQuerier{qs: []BlockedQuery{
		{Domain: "youtube.com", ClientIP: "192.168.1.50", Timestamp: now},
		{Domain: "youtube.com", ClientIP: "192.168.1.99", Timestamp: now.Add(time.Minute)},
		{Domain: "twitter.com", ClientIP: "192.168.1.60", Timestamp: now.Add(2 * time.Minute)},
	}}
	h := NewGetHandler(rec)

	res, err := h.Handle(context.Background(), &TelemetryInput{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalBlocked != 3 {
		t.Errorf("TotalBlocked = %d, want 3", res.TotalBlocked)
	}
	if len(res.Entries) != 3 {
		t.Errorf("Entries len = %d, want 3", len(res.Entries))
	}
	if res.Entries[0].Domain != "twitter.com" {
		t.Errorf("Entries[0] = %s, want twitter.com (mais recente primeiro)", res.Entries[0].Domain)
	}
	if len(res.Summary) != 2 {
		t.Errorf("Summary len = %d, want 2", len(res.Summary))
	}
}

func TestGetHandlerClampsLimit(t *testing.T) {
	now := time.Now().UTC()
	qs := make([]BlockedQuery, 0, 300)
	for i := 0; i < 300; i++ {
		qs = append(qs, BlockedQuery{Domain: "d.com", ClientIP: "10.0.0.1", Timestamp: now.Add(time.Duration(i) * time.Second)})
	}
	h := NewGetHandler(&fakeQuerier{qs: qs})

	res, err := h.Handle(context.Background(), &TelemetryInput{Limit: 0}) // default
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) > 200 {
		t.Errorf("Entries len = %d, want clampado em 200", len(res.Entries))
	}
}
