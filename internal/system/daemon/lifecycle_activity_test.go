package daemon

import (
	"context"
	"testing"
	"time"
)

// slowStopComponent é um componente cujo teardown demora — simula o Stop de um
// update em andamento (swap) ou de um sync de guard concluindo no shutdown.
type slowStopComponent struct {
	name  string
	rec   *recorder
	sleep time.Duration
}

func (c *slowStopComponent) Start() error {
	c.rec.record("start:" + c.name)
	return nil
}

func (c *slowStopComponent) Stop() {
	time.Sleep(c.sleep)
	c.rec.record("stop:" + c.name)
}

// TestRun_CtxCancel_ActiveLongPoll_DoesNotWaitForConnections: shutdown com
// atividade simultânea — uma conexão de long-poll (event-subscribe) ativa no
// momento do cancelamento. O lifecycle para o servidor (fecha o listener) mas
// NÃO espera conexões ativas drenarem: no servidor real a long-poll morre pelo
// timeout do handler (20s do hub), não pelo Run. O Run precisa retornar sem
// travar.
func TestRun_CtxCancel_ActiveLongPoll_DoesNotWaitForConnections(t *testing.T) {
	rec := &recorder{}
	srv := newFakeServer()

	// Simula o handler de long-poll em andamento durante o shutdown. É uma
	// goroutine livre (o lifecycle não a conhece): o teste documenta a intenção
	// — o Run não espera conexões ativas drenarem; no servidor real a long-poll
	// morre pelo timeout do handler (20s do hub), não pelo shutdown.
	handlerBlocked := make(chan struct{})
	releaseHandler := make(chan struct{})
	defer close(releaseHandler)
	go func() {
		close(handlerBlocked)
		<-releaseHandler
	}()

	deps, _ := newTestDeps(rec, srv)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(deps).Run(ctx) }()

	waitFor(t, 3*time.Second, "o servidor iniciar", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.started
	})
	<-handlerBlocked // long-poll ativa no momento do shutdown

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run deve retornar nil no cancelamento, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run travou esperando conexões ativas drenarem")
	}

	if got := rec.snapshot(); len(got) != 6 {
		t.Errorf("esperava 3 starts + 3 stops, got %v", got)
	}
}

// TestRun_CtxCancel_SlowTeardown_CompletesWithoutDeadlock: shutdown com um
// componente cujo Stop demora (teardown de update/guard em andamento). O Run
// espera o teardown terminar e completa a ordem reversa — sem deadlock e sem
// pular o componente lento.
func TestRun_CtxCancel_SlowTeardown_CompletesWithoutDeadlock(t *testing.T) {
	rec := &recorder{}
	srv := newFakeServer()
	deps, _ := newTestDeps(rec, srv)
	deps.Components = []Component{
		&fakeComponent{name: "worker", rec: rec},
		&slowStopComponent{name: "guard", rec: rec, sleep: 150 * time.Millisecond},
		&fakeComponent{name: "watcher", rec: rec},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(deps).Run(ctx) }()

	waitFor(t, 3*time.Second, "o servidor iniciar", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.started
	})

	start := time.Now()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run deve retornar nil, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run não retornou — deadlock no teardown?")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("teardown demorou demais: %v", elapsed)
	}

	want := []string{
		"start:worker", "start:guard", "start:watcher",
		"stop:watcher", "stop:guard", "stop:worker",
	}
	got := rec.snapshot()
	if len(got) != len(want) {
		t.Fatalf("ordem = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordem[%d] = %q, want %q (completa %v)", i, got[i], want[i], got)
		}
	}
}

// TestRun_ServiceStop_SecondRequestIgnoredAfterFirstRefused: após uma parada
// de serviço recusada (bloqueios ativos), o canal local svcStop é zerado —
// pedidos subsequentes no mesmo canal não são re-observados e o daemon segue
// protegendo até um cancelamento incondicional (ctx). Comportamento do gate
// CanStop congelado por teste (shutdown com atividade: os bloqueios ativos
// seguram a parada).
func TestRun_ServiceStop_SecondRequestIgnoredAfterFirstRefused(t *testing.T) {
	rec := &recorder{}
	srv := newFakeServer()
	deps, stop := newTestDeps(rec, srv)
	deps.CanStop = func() bool { return false } // bloqueios permanecem ativos

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(deps).Run(ctx) }()

	waitFor(t, 3*time.Second, "o servidor iniciar", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.started
	})

	close(stop) // 1ª parada: recusada (CanStop=false)

	// "Segunda parada" = o canal fechado NÃO é re-observado: após o primeiro
	// evento o svcStop local é zerado (senão o select hot-looparia num canal
	// fechado). O daemon continua servindo e nenhum componente para.
	select {
	case err := <-done:
		t.Fatalf("Run não deveria retornar com CanStop=false, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if got := rec.snapshot(); len(got) != 3 {
		t.Errorf("nenhum componente deveria ter parado, got %v", got)
	}

	// Só o cancelamento incondicional encerra.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run deve retornar nil, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run não retornou após o cancelamento do ctx")
	}
	if got := rec.snapshot(); len(got) != 6 {
		t.Errorf("esperava 3 starts + 3 stops, got %v", got)
	}
}
