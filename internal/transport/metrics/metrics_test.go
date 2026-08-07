package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestRegistry_RecordAndSnapshot(t *testing.T) {
	r := New(8)
	r.Record("ping", 10*time.Millisecond)
	r.Record("ping", 30*time.Millisecond)
	r.Record("status", 200*time.Millisecond)

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot = %d ações, want 2", len(snap))
	}
	if snap[0].Action != "ping" || snap[1].Action != "status" {
		t.Errorf("Snapshot não ordenada: %v", snap)
	}
	ping := snap[0]
	if ping.Count != 2 || ping.Total != 40*time.Millisecond || ping.Avg != 20*time.Millisecond {
		t.Errorf("ping agg = %+v, want count=2 total=40ms avg=20ms", ping)
	}
	if ping.Max != 30*time.Millisecond || ping.Last != 30*time.Millisecond {
		t.Errorf("ping max/last = %+v", ping)
	}
}

func TestRegistry_PercentilesOverRecentWindow(t *testing.T) {
	r := New(4)
	// 1..10ms — janela recente (últimos 4) = 7,8,9,10 → p50=8/9, p95/p99=10.
	for i := 1; i <= 10; i++ {
		r.Record("block", time.Duration(i)*time.Millisecond)
	}
	st := r.Snapshot()[0]
	if st.Count != 10 {
		t.Errorf("count = %d, want 10 (acumula além do ring)", st.Count)
	}
	if st.P50 < 7*time.Millisecond || st.P50 > 9*time.Millisecond {
		t.Errorf("p50 = %v, want ~8ms (janela 7..10)", st.P50)
	}
	if st.P95 != 10*time.Millisecond || st.P99 != 10*time.Millisecond {
		t.Errorf("p95/p99 = %v/%v, want 10ms", st.P95, st.P99)
	}
}

func TestRegistry_Reset(t *testing.T) {
	r := New(4)
	r.Record("ping", time.Millisecond)
	r.Reset()
	if got := r.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot após Reset = %v, want vazio", got)
	}
}

func TestRegistry_ConcurrentRecord(t *testing.T) {
	r := New(64)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				r.Record("ping", time.Millisecond)
			}
		}()
	}
	wg.Wait()
	st := r.Snapshot()[0]
	if st.Count != 4000 {
		t.Errorf("count = %d, want 4000", st.Count)
	}
}
