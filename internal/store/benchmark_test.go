package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"focusguard/internal/fsutil"
	"focusguard/internal/policy"
)

// benchState builds a state with numDomains active blocks (two IPs each),
// mirroring a long-running daemon's state mirror.
func benchState(numDomains int) *State {
	now := time.Now()
	blocks := make(map[string]policy.Block, numDomains)
	for i := 0; i < numDomains; i++ {
		domain := fmt.Sprintf("domain-%d.test", i)
		blocks[domain] = policy.Block{
			Domain:      domain,
			StartedAt:   now,
			ExpiresAt:   now.Add(24 * time.Hour),
			ResolvedIPs: []string{"10.0.0.1", "2001:db8::1"},
		}
	}
	return &State{Version: 1, Blocks: blocks}
}

func BenchmarkStore_Save(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			st, err := NewStore(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatalf("store: %v", err)
			}
			state := benchState(numDomains)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := st.Save(state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStore_SaveWithWatcherHash models the real daemon save chain: after
// every save the statewatch hashes the freshly-written file (fsutil.HashFile)
// to match self-writes. The callback cost is part of the Save critical path.
func BenchmarkStore_SaveWithWatcherHash(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "state.json")
			st, err := NewStore(path)
			if err != nil {
				b.Fatalf("store: %v", err)
			}
			st.SetOnSave(func() { _, _ = fsutil.HashFile(path) })
			state := benchState(numDomains)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := st.Save(state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStore_Load(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "state.json")
			st, err := NewStore(path)
			if err != nil {
				b.Fatalf("store: %v", err)
			}
			if err := st.Save(benchState(numDomains)); err != nil {
				b.Fatalf("seed save: %v", err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := st.Load(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
