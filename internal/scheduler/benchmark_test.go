package scheduler

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"focusguard/internal/policy"
	"focusguard/internal/store"
)

func slowResolve(domain string) ([]string, error) {
	time.Sleep(50 * time.Millisecond)
	return []string{"10.0.0.1"}, nil
}

func benchmarkSetup(b *testing.B, numDomains int) (*Scheduler, *mockEnforcer, *store.Store) {
	b.Helper()

	origResolve := resolveFunc
	resolveFunc = slowResolve
	b.Cleanup(func() { resolveFunc = origResolve })

	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "state.json")
	st, err := store.NewStore(dbPath)
	if err != nil {
		b.Fatalf("store: %v", err)
	}

	now := time.Now()
	blocks := make(map[string]policy.Block)
	for i := 0; i < numDomains; i++ {
		domain := fmt.Sprintf("domain-%d.test", i)
		blocks[domain] = policy.Block{
			Domain:      domain,
			StartedAt:   now,
			ExpiresAt:   now.Add(24 * time.Hour),
			ResolvedIPs: []string{fmt.Sprintf("10.0.0.%d", i)},
		}
	}
	if err := st.Save(&store.State{Blocks: blocks}); err != nil {
		b.Fatalf("save: %v", err)
	}

	enf := newMockEnforcer()
	sched := NewScheduler(st, enf)
	if err := sched.Start(); err != nil {
		b.Fatalf("Start: %v", err)
	}

	return sched, enf, st
}

func BenchmarkScheduler_BlockSequential(b *testing.B) {
	sched, _, _ := benchmarkSetup(b, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		domain := fmt.Sprintf("new-domain-%d.test", i%100)
		sched.Block(domain, 30*time.Minute)
	}
}

func BenchmarkScheduler_BlockConcurrent(b *testing.B) {
	sched, _, _ := benchmarkSetup(b, 10)

	b.ResetTimer()
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < b.N/10; i++ {
				domain := fmt.Sprintf("concurrent-%d-%d.test", gid, i)
				sched.Block(domain, 30*time.Minute)
			}
		}(g)
	}
	wg.Wait()
}

func BenchmarkScheduler_ListBlocksDuringSlowBlock(b *testing.B) {
	sched, _, _ := benchmarkSetup(b, 10)

	b.ResetTimer()
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			domain := fmt.Sprintf("blocking-%d.test", i)
			sched.Block(domain, 30*time.Minute)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			sched.ListBlocks()
		}
	}()

	wg.Wait()
}

func BenchmarkScheduler_HasActiveBlocksDuringSlowBlock(b *testing.B) {
	sched, _, _ := benchmarkSetup(b, 10)

	b.ResetTimer()
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			domain := fmt.Sprintf("blocking-%d.test", i)
			sched.Block(domain, 30*time.Minute)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			sched.HasActiveBlocks()
		}
	}()

	wg.Wait()
}

func BenchmarkScheduler_MixedLoad(b *testing.B) {
	sched, _, _ := benchmarkSetup(b, 10)

	b.ResetTimer()
	var wg sync.WaitGroup

	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < b.N/5; i++ {
				domain := fmt.Sprintf("mixed-%d-%d.test", gid, i)
				sched.Block(domain, 30*time.Minute)
			}
		}(g)
	}

	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < b.N/3; i++ {
				sched.ListBlocks()
			}
		}()
	}

	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < b.N/2; i++ {
				sched.HasActiveBlocks()
			}
		}()
	}

	wg.Wait()
}
