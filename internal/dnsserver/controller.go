package dnsserver

import (
	"fmt"
	"sync"
	"time"
)

// Status is a read-only snapshot of the DNS server lifecycle used by the IPC
// status/dns-status actions and the CLI.
type Status struct {
	Listening bool      `json:"listening"`
	Addr      string    `json:"addr,omitempty"`
	BindAddr  string    `json:"bind_addr,omitempty"`
	Upstream  string    `json:"upstream,omitempty"`
	Queries   uint64    `json:"queries,omitempty"`
	Blocked   uint64    `json:"blocked,omitempty"`
	BindError string    `json:"bind_error,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

// Controller owns the server lifecycle on behalf of the daemon: it tracks the
// listening state, the live counters, and any bind error. The daemon persists
// the enabled flag separately (state.json) and calls Start at boot when it is
// set; the IPC/CLI layer combines the two for status.
type Controller struct {
	checker  PolicyChecker
	bindAddr string
	upstream string

	mu        sync.RWMutex
	server    *Server
	listening bool
	addr      string
	startErr  error
	startedAt time.Time
}

// NewController wires a checker and the listen/forward targets into a
// lifecycle controller. An empty bindAddr falls back to DefaultBindAddr and an
// empty upstream to DefaultUpstream.
func NewController(checker PolicyChecker, bindAddr, upstream string) *Controller {
	if bindAddr == "" {
		bindAddr = DefaultBindAddr
	}
	return &Controller{checker: checker, bindAddr: bindAddr, upstream: upstream}
}

// Start binds and serves. It is idempotent: calling Start while already
// listening is a no-op.
func (c *Controller) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listening {
		return nil
	}

	srv := New(c.checker, c.upstream)
	if err := srv.Start(c.bindAddr); err != nil {
		c.startErr = fmt.Errorf("%w%s", err, bindHint(c.bindAddr))
		return c.startErr
	}

	c.server = srv
	c.listening = true
	c.addr = srv.Addr()
	c.startedAt = time.Now()
	c.startErr = nil
	return nil
}

// Stop shuts the server down and releases the port. Idempotent.
func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.server == nil {
		c.listening = false
		c.addr = ""
		return nil
	}
	err := c.server.Stop()
	c.server = nil
	c.listening = false
	c.addr = ""
	return err
}

// SetUpstream changes the upstream resolver the server forwards allowed
// queries to. When the server is listening, the change is applied by
// restarting it — DNS is stateless, so the gap is instant; as with any
// restart the live counters reset (Start resets them). When the server is
// stopped, the new upstream is used on the next Start. An empty upstream
// falls back to DefaultUpstream.
func (c *Controller) SetUpstream(upstream string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if upstream == "" {
		upstream = DefaultUpstream
	}
	c.upstream = upstream
	if !c.listening || c.server == nil {
		return nil
	}

	// Restart para aplicar: para o listener atual e sobe um novo com o
	// upstream novo. Um bind que falha no restart deixa o estado "parado"
	// (listening=false) com o erro visível no status — igual ao Start comum.
	//
	// Stop é best-effort de propósito: mesmo que ele reporte erro, os sockets
	// normalmente fecham — seguir o restart impede que o upstream gravado e o
	// servidor vivo divirjam (um return aqui deixaria listening=true com o
	// server antigo e o c.upstream novo).
	_ = c.server.Stop()
	c.server = nil
	c.listening = false
	c.addr = ""

	srv := New(c.checker, c.upstream)
	if err := srv.Start(c.bindAddr); err != nil {
		c.startErr = fmt.Errorf("%w%s", err, bindHint(c.bindAddr))
		return c.startErr
	}
	c.server = srv
	c.listening = true
	c.addr = srv.Addr()
	c.startedAt = time.Now()
	c.startErr = nil
	return nil
}

// Status snapshots the current controller state.
func (c *Controller) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := Status{
		Listening: c.listening,
		Addr:      c.addr,
		BindAddr:  c.bindAddr,
		Upstream:  c.upstream,
		StartedAt: c.startedAt,
	}
	if c.startErr != nil {
		st.BindError = c.startErr.Error()
	}
	if c.server != nil {
		st.Queries = c.server.Queries()
		st.Blocked = c.server.Blocked()
	}
	return st
}

// Addr is a small accessor used by the daemon to log where the server bound.
func (c *Controller) Addr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.addr
}
