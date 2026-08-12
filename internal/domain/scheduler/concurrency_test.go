package scheduler

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_ConcurrentBlock_DifferentDomains(t *testing.T) {
	sched, enf, _ := setupTestScheduler(t)

	var wg sync.WaitGroup
	domains := []string{"localhost", "127.0.0.1", "0.0.0.0", "::1", "localhost.localdomain"}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			domain := domains[i%len(domains)]
			_, err := sched.Block(domain, 1*time.Hour)
			if err != nil {
				t.Logf("block %s: %v", domain, err)
			}
		}(i)
	}
	wg.Wait()

	enf.mu.Lock()
	blockedCount := len(enf.blockedDomains)
	enf.mu.Unlock()

	if blockedCount < 3 {
		t.Errorf("expected at least 3 unique blocked domains, got %d", blockedCount)
	}

	blocks, err := sched.ListBlocks()
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(blocks) < 3 {
		t.Errorf("expected at least 3 blocks, got %d", len(blocks))
	}
}

func TestScheduler_ConcurrentBlock_SameDomain(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sched.Block("127.0.0.1", 1*time.Hour)
		}()
	}
	wg.Wait()

	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Blocks) != 1 {
		t.Errorf("expected exactly 1 block for same domain, got %d", len(state.Blocks))
	}

	enf.mu.Lock()
	_, exists := enf.blockedDomains["127.0.0.1"]
	enf.mu.Unlock()
	if !exists {
		t.Error("expected domain to be in enforcer blocked domains")
	}
}

func TestScheduler_ConcurrentBlockAndList(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	var stop atomic.Bool
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		idx := 0
		for !stop.Load() {
			domain := "127.0.0.1"
			if idx%2 == 0 {
				domain = "localhost"
			}
			sched.Block(domain, 30*time.Minute)
			idx++
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			blocks, err := sched.ListBlocks()
			if err == nil {
				_ = len(blocks)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			_ = sched.HasActiveBlocks()
		}
	}()

	time.Sleep(500 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}

func TestScheduler_ConcurrentBlockExpiresWithReads(t *testing.T) {
	sched, enf, _ := setupTestScheduler(t)

	var stop atomic.Bool
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			sched.Block("localhost", 50*time.Millisecond)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			sched.ListBlocks()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			sched.HasActiveBlocks()
		}
	}()

	time.Sleep(2 * time.Second)
	stop.Store(true)
	wg.Wait()

	// Drena os timers de expiração ainda pendentes (o último Block é de 50ms):
	// sem isso, no Windows o callback grava no state.json durante a remoção do
	// TempDir e o cleanup falha com "pasta não está vazia" (flake sob carga).
	time.Sleep(150 * time.Millisecond)

	enf.mu.Lock()
	unblockedCount := len(enf.unblockedDomains)
	enf.mu.Unlock()

	if unblockedCount == 0 {
		t.Log("0 unblocks occurred — timer may not have fired")
	}
}

func TestScheduler_ConcurrentAllOperations(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	var stop atomic.Bool
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			domains := []string{"localhost", "127.0.0.1", "0.0.0.0"}
			idx := 0
			for !stop.Load() {
				domain := domains[idx%len(domains)]
				sched.Block(domain, 30*time.Minute)
				idx++
			}
		}(i)
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				sched.ListBlocks()
			}
		}()
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				sched.HasActiveBlocks()
			}
		}()
	}

	time.Sleep(1 * time.Second)
	stop.Store(true)
	wg.Wait()
}

func TestScheduler_ConcurrentBlockAndPing(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	var stop atomic.Bool
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			sched.Block("localhost", 1*time.Hour)
		}
	}()

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				sched.Ping()
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}

func TestScheduler_ConcurrentListBlocks(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	sched.Block("localhost", 1*time.Hour)
	sched.Block("127.0.0.1", 1*time.Hour)
	sched.Block("0.0.0.0", 1*time.Hour)

	var stop atomic.Bool
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				blocks, err := sched.ListBlocks()
				if err != nil {
					t.Logf("ListBlocks error: %v", err)
				}
				if len(blocks) != 3 {
					t.Logf("expected 3 blocks, got %d", len(blocks))
				}
			}
		}()
	}

	time.Sleep(500 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}

func TestScheduler_ConcurrentBlockThenReBlock(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	sched.Block("localhost", 100*time.Millisecond)
	sched.Block("localhost", 1*time.Hour)

	time.Sleep(200 * time.Millisecond)

	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	block, exists := state.Blocks["localhost"]
	if !exists {
		t.Fatal("expected localhost to still be blocked after re-block with longer duration")
	}
	if !block.IsActive() {
		t.Error("expected block to be active after re-block")
	}

	enf.mu.Lock()
	_, blocked := enf.blockedDomains["localhost"]
	enf.mu.Unlock()
	if !blocked {
		t.Error("expected localhost to be in enforcer blocked domains")
	}
}

func TestScheduler_ConcurrentBlockAndHasActive(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	var stop atomic.Bool
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			sched.Block("localhost", 30*time.Minute)
			sched.Block("127.0.0.1", 30*time.Minute)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			active := sched.HasActiveBlocks()
			if active {
				sched.ListBlocks()
			}
		}
	}()

	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}
