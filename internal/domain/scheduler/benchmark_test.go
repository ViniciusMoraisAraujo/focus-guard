package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"focusguard/internal/domain/policy"
	"focusguard/internal/store"
)

func slowResolve(domain string) ([]string, error) {
	time.Sleep(50 * time.Millisecond)
	return []string{"10.0.0.1"}, nil
}

// fastResolve models a warm DNS cache: zero-latency lookups so benchmarks can
// isolate the scheduler/store/enforcer overhead from the DNS cost that
// dominates the real block path.
func fastResolve(domain string) ([]string, error) {
	return []string{"10.0.0.1"}, nil
}

func fastResolveCtx(_ context.Context, domain string) ([]string, error) {
	return []string{"10.0.0.1"}, nil
}

func benchmarkSetup(b *testing.B, numDomains int) (*Scheduler, *mockEnforcer, *store.Store) {
	return benchmarkSetupWithResolver(b, numDomains, slowResolve)
}

func benchmarkSetupWithResolver(b *testing.B, numDomains int, resolve func(string) ([]string, error)) (*Scheduler, *mockEnforcer, *store.Store) {
	b.Helper()

	origResolve := resolveFunc
	resolveFunc = resolve
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

// BenchmarkScheduler_BlockFastResolve measures the per-block overhead of the
// full block path (RAM + store.Save + enforcer) with zero-latency DNS, so the
// scheduler/store/enforcer cost is visible instead of being drowned by the
// 100ms slow resolver.
func BenchmarkScheduler_BlockFastResolve(b *testing.B) {
	sched, _, _ := benchmarkSetupWithResolver(b, 10, fastResolve)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		domain := fmt.Sprintf("fast-%d.test", i%100)
		sched.Block(domain, 30*time.Minute)
	}
}

// BenchmarkScheduler_BlockWithManyBlocks measures how the per-block cost grows
// with the number of already-active blocks: every Block copies the whole RAM
// map (ramState) and re-marshals + rewrites the state.json under the write
// lock, so this is O(n) per block.
func BenchmarkScheduler_BlockWithManyBlocks(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			sched, _, _ := benchmarkSetupWithResolver(b, numDomains, fastResolve)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				domain := fmt.Sprintf("new-%d.test", i%100)
				sched.Block(domain, 30*time.Minute)
			}
		})
	}
}

// BenchmarkScheduler_ListBlocksWithManyBlocks measures ListBlocks (full map
// copy under RLock) as the block count grows — the cost every status IPC call
// pays.
func BenchmarkScheduler_ListBlocksWithManyBlocks(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			sched, _, _ := benchmarkSetupWithResolver(b, numDomains, fastResolve)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sched.ListBlocks()
			}
		})
	}
}

// BenchmarkScheduler_BlockDomainsFastResolve measures the batched preset path
// (BlockDomains) with warm-DNS lookups: N domains resolved in parallel, one
// store.Save and one enforcer.Sync for the whole batch.
func BenchmarkScheduler_BlockDomainsFastResolve(b *testing.B) {
	origResolveCtx := resolveFuncCtx
	resolveFuncCtx = fastResolveCtx
	b.Cleanup(func() { resolveFuncCtx = origResolveCtx })

	sched, _, _ := benchmarkSetupWithResolver(b, 10, fastResolve)

	domains := make([]string, 10)
	for i := range domains {
		domains[i] = fmt.Sprintf("batch-%d.test", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sched.BlockDomains(domains, 30*time.Minute); err != nil {
			b.Fatalf("BlockDomains: %v", err)
		}
	}
}
