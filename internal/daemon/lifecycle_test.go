package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// recorder acumula os eventos de lifecycle em ordem global.
type recorder struct {
	mu    sync.Mutex
	order []string
}

func (r *recorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, s)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// fakeComponent registra Start/Stop no recorder compartilhado.
type fakeComponent struct {
	name string
	rec  *recorder
	// startErr faz o Start falhar (aborta o boot).
	startErr error
}

func (c *fakeComponent) Start() error {
	if c.startErr != nil {
		return c.startErr // falhou — nunca "iniciou"
	}
	c.rec.record("start:" + c.name)
	return nil
}

func (c *fakeComponent) Stop() { c.rec.record("stop:" + c.name) }

// fakeServer implementa Server: Start bloqueia até Stop (ou falha na hora
// quando startErr está setado).
type fakeServer struct {
	mu       sync.Mutex
	started  bool
	stopped  bool
	startErr error
	stopCh   chan struct{}
	stopOnce sync.Once
}

func newFakeServer() *fakeServer {
	return &fakeServer{stopCh: make(chan struct{})}
}

func (f *fakeServer) Start() error {
	f.mu.Lock()
	if f.startErr != nil {
		err := f.startErr
		f.mu.Unlock()
		return err
	}
	f.started = true
	f.mu.Unlock()

	<-f.stopCh
	f.mu.Lock()
	stopped := f.stopped
	f.mu.Unlock()
	if !stopped {
		return errors.New("server returned without Stop")
	}
	return nil
}

func (f *fakeServer) Stop() error {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	f.stopOnce.Do(func() { close(f.stopCh) })
	return nil
}

// waitFor bloqueia até cond ou o timeout — evita testes pendurados.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout esperando %s", what)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// newTestDeps monta um Deps com 3 componentes (worker → guard → watcher) e um
// canal de stop próprio (devolvido para o teste fechar).
func newTestDeps(rec *recorder, srv Server) (Deps, chan struct{}) {
	stop := make(chan struct{})
	return Deps{
		Server: srv,
		Components: []Component{
			&fakeComponent{name: "worker", rec: rec},
			&fakeComponent{name: "guard", rec: rec},
			&fakeComponent{name: "watcher", rec: rec},
		},
		Stop:    stop,
		CanStop: func() bool { return true },
	}, stop
}

// TestRun_StartsInOrderStopsInReverse verifies the core lifecycle contract:
// components start in registration order at boot and stop in REVERSE order at
// shutdown — the orderly teardown (workers → guards → watchers).
func TestRun_StartsInOrderStopsInReverse(t *testing.T) {
	rec := &recorder{}
	srv := newFakeServer()
	deps, stop := newTestDeps(rec, srv)

	done := make(chan error, 1)
	go func() { done <- New(deps).Run(context.Background()) }()

	waitFor(t, 3*time.Second, "o servidor iniciar", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.started
	})

	close(stop)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run deve retornar nil na parada limpa, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run não retornou após o stop")
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
			t.Fatalf("ordem[%d] = %q, want %q (ordem completa %v)", i, got[i], want[i], got)
		}
	}
}

// TestRun_CtxCancellation_Stops verifies ctx cancellation is honored as an
// unconditional shutdown (used by tests and future supervisors).
func TestRun_CtxCancellation_Stops(t *testing.T) {
	rec := &recorder{}
	srv := newFakeServer()
	deps, _ := newTestDeps(rec, srv)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(deps).Run(ctx) }()

	waitFor(t, 3*time.Second, "o servidor iniciar", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.started
	})

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run deve retornar nil no cancelamento, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run não retornou após o cancelamento do ctx")
	}
	if got := rec.snapshot(); len(got) != 6 {
		t.Errorf("esperava 3 starts + 3 stops, got %v", got)
	}
}

// TestRun_ServerError_StopsComponents verifies a server Start failure is
// returned and the already-started components are still torn down in reverse
// order.
func TestRun_ServerError_StopsComponents(t *testing.T) {
	rec := &recorder{}
	srv := newFakeServer()
	srv.startErr = errors.New("listen: address already in use")
	deps, _ := newTestDeps(rec, srv)

	err := New(deps).Run(context.Background())
	if err == nil || err.Error() != "listen: address already in use" {
		t.Fatalf("esperava o erro do servidor, got %v", err)
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
			t.Fatalf("ordem[%d] = %q, want %q (ordem completa %v)", i, got[i], want[i], got)
		}
	}
}

// TestRun_ComponentStartError_AbortsBoot verifies a component that fails to
// start aborts the boot: components already started are stopped in reverse
// order and the failing one (plus the ones after it) is never started.
func TestRun_ComponentStartError_AbortsBoot(t *testing.T) {
	rec := &recorder{}
	deps, _ := newTestDeps(rec, newFakeServer())
	deps.Components = []Component{
		&fakeComponent{name: "worker", rec: rec},
		&fakeComponent{name: "guard", rec: rec, startErr: errors.New("boot falhou")},
		&fakeComponent{name: "watcher", rec: rec},
	}

	err := New(deps).Run(context.Background())
	if err == nil || err.Error() != "boot falhou" {
		t.Fatalf("esperava o erro do componente, got %v", err)
	}
	want := []string{"start:worker", "stop:worker"}
	got := rec.snapshot()
	if len(got) != len(want) {
		t.Fatalf("ordem = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordem[%d] = %q, want %q (ordem completa %v)", i, got[i], want[i], got)
		}
	}
}

// TestRun_IgnoresStopWhenCanStopFalse verifies the protection gate: a stop
// request with active blocks/session is IGNORED (the daemon keeps serving),
// and a later ctx cancellation (unconditional) ends it.
func TestRun_IgnoresStopWhenCanStopFalse(t *testing.T) {
	rec := &recorder{}
	srv := newFakeServer()
	deps, stop := newTestDeps(rec, srv)
	deps.CanStop = func() bool { return false }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(deps).Run(ctx) }()

	waitFor(t, 3*time.Second, "o servidor iniciar", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.started
	})

	close(stop) // pedido de parada com bloqueios ativos — deve ser ignorado

	// O daemon continua servindo: nada foi encerrado.
	select {
	case err := <-done:
		t.Fatalf("Run não deveria retornar com CanStop=false, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if got := rec.snapshot(); len(got) != 3 {
		t.Errorf("nenhum componente deveria ter sido parado, got %v", got)
	}

	// Só um cancelamento incondicional (ctx) encerra o ciclo.
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
		t.Errorf("esperava 3 starts + 3 stops após o cancelamento, got %v", got)
	}
}
