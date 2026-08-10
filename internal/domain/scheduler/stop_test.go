package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestScheduler_Stop_StopsPeriodicRefresh: o refresh periódico de IPs roda em
// goroutine (startPeriodicIPRefresh) e precisa sair no Stop — antes da Etapa 4
// do bug-hunt a goroutine vazava no shutdown do daemon. O teste observa a
// parada pelas chamadas ao resolver: depois do Stop, nenhuma resolução nova
// acontece.
func TestScheduler_Stop_StopsPeriodicRefresh(t *testing.T) {
	sched, _, _ := setupTestScheduler(t)

	// Sem DNS real no teste: o Block resolve via stub.
	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) { return []string{"203.0.113.1"}, nil }
	defer func() { resolveFunc = origResolve }()

	if _, err := sched.Block("example.com", time.Hour); err != nil {
		t.Fatalf("block: %v", err)
	}

	var resolveCalls int32
	origResolveCtx := resolveFuncCtx
	resolveFuncCtx = func(_ context.Context, _ string) ([]string, error) {
		atomic.AddInt32(&resolveCalls, 1)
		return nil, nil
	}
	defer func() { resolveFuncCtx = origResolveCtx }()

	go sched.startPeriodicIPRefresh(20 * time.Millisecond)
	// Garante o fechamento do canal mesmo se um t.Fatal acontecer antes do
	// Stop explícito — senão a goroutine vazaria e, com o resolveFuncCtx já
	// restaurado pelo defer, passaria a resolver DNS real no resto da suíte.
	defer sched.Stop()

	// A goroutine está viva: pelo menos uma rodada de refresh aconteceu.
	if !waitForCondition(2*time.Second, func() bool {
		return atomic.LoadInt32(&resolveCalls) >= 1
	}) {
		t.Fatal("refresh periódico nunca rodou")
	}

	sched.Stop()
	sched.Stop() // idempotente — não pode panicar (canal já fechado)

	// Drain: espera ~2-3 intervalos do ticker para qualquer ciclo iniciado
	// ANTES do Stop terminar (a goroutine sai no próximo select) — só então
	// mede a linha de base. Sem o drain, um ciclo in-flight poderia incrementar
	// o contador depois da leitura e flakar o teste.
	time.Sleep(50 * time.Millisecond)
	after := atomic.LoadInt32(&resolveCalls)
	time.Sleep(120 * time.Millisecond) // ~6 intervalos do ticker

	if got := atomic.LoadInt32(&resolveCalls); got != after {
		t.Errorf("refresh continuou após Stop: %d → %d", after, got)
	}
}
