// Package daemon owns the daemon lifecycle (B10 do refactor-plan): starting
// the IPC server and the satellite components in order and tearing them down
// in reverse order on shutdown, with an explicit Run(ctx) error replacing the
// implicit defer-based teardown and the hand-rolled signal goroutine of the
// historical cmd/focusguard-daemon main.
//
// The composition root (cmd/focusguard-daemon) wires the Deps: the satellite
// components are started during boot construction (matching the historical
// order) and registered here only for their teardown; the lifecycle owns the
// IPC server start (the blocking piece) and ALL shutdowns.
package daemon

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Server is the surface the lifecycle needs from the IPC server: Start blocks
// until Stop is called or the server fails; Stop unwinds it (closing the
// listener makes Start return nil).
type Server interface {
	Start() error
	Stop() error
}

// Component is a piece of the daemon that participates in the lifecycle:
// Start runs at boot (in registration order) and Stop runs at shutdown (in
// reverse order). A Start error aborts the boot.
type Component interface {
	Start() error
	Stop()
}

// Deps wires everything the lifecycle coordinates.
type Deps struct {
	// Server is the IPC server: started after the components (it blocks
	// serving) and stopped first on shutdown.
	Server Server
	// Components are the satellites (watchers, workers, guards): started in
	// order at boot, stopped in reverse order at shutdown — the explicit
	// teardown order the defer-based main could only express implicitly.
	Components []Component
	// Stop is closed when a shutdown was requested by the service (SCM). A
	// stop request is only honored when CanStop reports true — otherwise it
	// is ignored and the daemon keeps protecting.
	Stop <-chan struct{}
	// CanStop reports whether a shutdown may proceed (no active blocks or
	// pomodoro session). Consulted for signals and service stops.
	CanStop func() bool
}

// stopOnly adapta um stop func a um Component cujo Start é no-op — usado
// para os satélites que o composition root inicia durante a construção do
// boot (a ordem histórica), cabendo ao lifecycle apenas o teardown ordenado.
type stopOnly struct{ stop func() }

func (c stopOnly) Start() error { return nil }
func (c stopOnly) Stop() {
	if c.stop != nil {
		c.stop()
	}
}

// StopOnly wraps a plain stop func into a Component whose Start is a no-op.
func StopOnly(stop func()) Component { return stopOnly{stop: stop} }

// Daemon runs the FocusGuard daemon lifecycle.
type Daemon struct{ deps Deps }

// New builds the lifecycle around the given dependencies.
func New(deps Deps) *Daemon { return &Daemon{deps: deps} }

// Run starts every component in order, then serves the IPC server until a
// shutdown is requested (signal, service stop or ctx cancellation) or the
// server fails, then tears everything down in reverse order. It returns the
// server error on failure and nil on a clean shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	// Sem servidor IPC não há o que servir — o contrato do Deps exige um; o
	// guard é para o erro ser tratável (e não um panic dentro da goroutine).
	if d.deps.Server == nil {
		return errors.New("daemon: IPC server not wired (Deps.Server is nil)")
	}

	started := 0
	for _, c := range d.deps.Components {
		if err := c.Start(); err != nil {
			// Aborta o boot: apenas os componentes que JÁ iniciaram são
			// parados (o que falhou é responsável pelo próprio cleanup).
			d.stopN(started)
			return err
		}
		started++
	}

	serverDone := make(chan error, 1)
	go func() { serverDone <- d.deps.Server.Start() }()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigChan)
		close(sigChan)
	}()

	// svcStop é a cópia local do canal de parada do serviço: após a PRIMEIRA
	// notificação (mesmo quando ignorada por bloqueios ativos) o canal é
	// zerado — um canal fechado fica "pronto" para sempre no select, o que
	// faria este loop hot-looper como um canal de sinal fechado.
	svcStop := d.deps.Stop
	for {
		select {
		case err := <-serverDone:
			d.shutdown()
			return err
		case <-ctx.Done():
			_ = d.deps.Server.Stop()
			<-serverDone
			d.shutdown()
			return nil
		case _, ok := <-sigChan:
			if !ok {
				d.shutdown()
				return nil
			}
			if !d.canStop() {
				log.Println("[FocusGuard Daemon] Sinal ignorado: existem bloqueios/sessão ativos.")
				continue
			}
			log.Println("[FocusGuard Daemon] Nenhum bloqueio/sessão ativo. Encerrando servidor IPC...")
			_ = d.deps.Server.Stop()
			<-serverDone
			d.shutdown()
			return nil
		case <-svcStop:
			svcStop = nil
			if !d.canStop() {
				log.Println("[FocusGuard Daemon] Parada do serviço ignorada: existem bloqueios/sessão ativos.")
				continue
			}
			log.Println("[FocusGuard Daemon] Serviço parando. Encerrando servidor IPC...")
			_ = d.deps.Server.Stop()
			<-serverDone
			d.shutdown()
			return nil
		}
	}
}

func (d *Daemon) canStop() bool {
	if d.deps.CanStop == nil {
		return true
	}
	return d.deps.CanStop()
}

// shutdown stops every component in reverse registration order — the orderly
// teardown (workers → guards → watchers) that B10 asked to make explicit.
func (d *Daemon) shutdown() {
	d.stopN(len(d.deps.Components))
}

// stopN para os primeiros n componentes em ordem reversa (teardown ordenado).
func (d *Daemon) stopN(n int) {
	for i := n - 1; i >= 0; i-- {
		d.deps.Components[i].Stop()
	}
}
