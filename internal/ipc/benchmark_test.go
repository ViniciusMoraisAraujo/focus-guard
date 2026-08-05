package ipc

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"focusguard/internal/policy"
	"focusguard/internal/scheduler"
	"focusguard/internal/store"
)

// setBenchEndpoint points the package Listen/Dial endpoints at a test-only
// address (ephemeral TCP port on Windows, temp unix socket on Linux), the
// benchmark equivalent of setTestEndpoint.
func setBenchEndpoint(b *testing.B) {
	b.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	origAddr := TestDialAddr
	TestDialAddr = addr
	b.Cleanup(func() { TestDialAddr = origAddr })

	origSock := TestSocketPath
	TestSocketPath = filepath.Join(b.TempDir(), "focusguard-bench.sock")
	b.Cleanup(func() { TestSocketPath = origSock })
}

// startBenchServer brings up a real Server + Scheduler + Store on the test
// endpoint, mirroring the integration tests but accepting a *testing.B.
func startBenchServer(b *testing.B) {
	b.Helper()

	setBenchEndpoint(b)

	st, err := store.NewStore(filepath.Join(b.TempDir(), "state.json"))
	if err != nil {
		b.Fatalf("store: %v", err)
	}
	sched := scheduler.NewScheduler(st, &integrationMockEnforcer{})
	if err := sched.Start(); err != nil {
		b.Fatalf("scheduler.Start: %v", err)
	}
	srv := NewServer(sched)
	go func() {
		_ = srv.Start()
	}()
	b.Cleanup(func() { srv.Stop() })

	time.Sleep(50 * time.Millisecond)
}

// seedBlocks writes numDomains active blocks into a fresh store and returns a
// scheduler bootstrapped on them, so the status response carries a realistic
// payload.
func seedBlocks(b *testing.B, numDomains int) (*scheduler.Scheduler, *store.Store) {
	b.Helper()

	now := time.Now()
	blocks := make(map[string]policy.Block, numDomains)
	for i := 0; i < numDomains; i++ {
		domain := fmt.Sprintf("domain-%d.test", i)
		blocks[domain] = policy.Block{
			Domain:      domain,
			StartedAt:   now,
			ExpiresAt:   now.Add(24 * time.Hour),
			ResolvedIPs: []string{"10.0.0.1"},
		}
	}

	st, err := store.NewStore(filepath.Join(b.TempDir(), "state.json"))
	if err != nil {
		b.Fatalf("store: %v", err)
	}
	if err := st.Save(&store.State{Version: 1, Blocks: blocks}); err != nil {
		b.Fatalf("seed save: %v", err)
	}

	sched := scheduler.NewScheduler(st, &integrationMockEnforcer{})
	if err := sched.Start(); err != nil {
		b.Fatalf("scheduler.Start: %v", err)
	}
	return sched, st
}

// BenchmarkIPC_Ping measures the round-trip latency of a trivial request over
// the real socket/JSON path: connect, encode, dispatch, decode, close.
func BenchmarkIPC_Ping(b *testing.B) {
	startBenchServer(b)
	client := NewClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Send(Request{Action: "ping"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIPC_Status(b *testing.B) {
	startBenchServer(b)
	client := NewClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Send(Request{Action: "status"}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIPC_StatusWithManyBlocks measures the status IPC with a growing
// block list, since the response ships every active block.
func BenchmarkIPC_StatusWithManyBlocks(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			setBenchEndpoint(b)
			sched, _ := seedBlocks(b, numDomains)
			srv := NewServer(sched)
			go func() { _ = srv.Start() }()
			b.Cleanup(func() { srv.Stop() })
			time.Sleep(50 * time.Millisecond)

			client := NewClient()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := client.Send(Request{Action: "status"}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
