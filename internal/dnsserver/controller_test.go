package dnsserver

import (
	"strings"
	"testing"
)

func TestControllerStartStopLifecycle(t *testing.T) {
	c := NewController(newFakeChecker("youtube.com"), "127.0.0.1:0", "127.0.0.1:9")

	st := c.Status()
	if st.Listening {
		t.Error("Listening = true antes de Start")
	}

	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Start(); err != nil { // idempotente
		t.Fatalf("Start duplicado: %v", err)
	}

	st = c.Status()
	if !st.Listening {
		t.Fatal("Listening = false após Start")
	}
	if st.Addr == "" {
		t.Error("Addr vazio após Start")
	}
	if st.Upstream != "127.0.0.1:9" {
		t.Errorf("Upstream = %q, want 127.0.0.1:9", st.Upstream)
	}

	// Uma consulta bloqueada deve refletir nos contadores.
	addr := st.Addr
	if err := c.Start(); err != nil { // reentrância não deve resetar nada
		t.Fatalf("Start: %v", err)
	}
	_ = doQuery(t, addr, "udp", "youtube.com", 1)
	st = c.Status()
	if st.Queries != 1 {
		t.Errorf("Queries = %d, want 1", st.Queries)
	}
	if st.Blocked != 1 {
		t.Errorf("Blocked = %d, want 1", st.Blocked)
	}

	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st = c.Status()
	if st.Listening {
		t.Error("Listening = true após Stop")
	}

	if err := c.Stop(); err != nil { // idempotente
		t.Fatalf("Stop duplicado: %v", err)
	}

	// Stop liberou a porta: um novo Start no mesmo endereço funciona.
	if err := c.Start(); err != nil {
		t.Fatalf("Start após Stop: %v", err)
	}
	if st := c.Status(); !st.Listening {
		t.Error("Listening = false após segundo Start")
	}
}

func TestControllerReportsBindError(t *testing.T) {
	first := NewController(newFakeChecker(), "127.0.0.1:0", "127.0.0.1:9")
	if err := first.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { _ = first.Stop() })

	// Segundo controller no MESMO endereço: o bind deve falhar e o erro deve
	// aparecer no status.
	occupied := first.Status().Addr
	second := NewController(newFakeChecker(), occupied, "127.0.0.1:9")
	if err := second.Start(); err == nil {
		t.Fatal("Start deveria falhar com a porta já em uso")
	}
	st := second.Status()
	if st.Listening {
		t.Error("Listening = true apesar do bind falhar")
	}
	if !strings.Contains(st.BindError, "bind") {
		t.Errorf("BindError = %q, esperava conter 'bind'", st.BindError)
	}
}

func TestControllerSetUpstream_WhileStoppedAppliesOnNextStart(t *testing.T) {
	c := NewController(newFakeChecker("youtube.com"), "127.0.0.1:0", "127.0.0.1:9")

	if err := c.SetUpstream("9.9.9.9:53"); err != nil {
		t.Fatalf("SetUpstream parado: %v", err)
	}
	if st := c.Status(); st.Upstream != "9.9.9.9:53" {
		t.Errorf("Upstream = %q após SetUpstream, esperava 9.9.9.9:53", st.Upstream)
	}
	if st := c.Status(); st.Listening {
		t.Error("SetUpstream parado não deveria subir o listener")
	}

	// O novo upstream é usado no Start seguinte.
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := c.Status(); st.Upstream != "9.9.9.9:53" {
		t.Errorf("Upstream após Start = %q, esperava 9.9.9.9:53", st.Upstream)
	}
	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestControllerSetUpstream_RestartsWhenListening(t *testing.T) {
	c := NewController(newFakeChecker("youtube.com"), "127.0.0.1:0", "127.0.0.1:9")
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := c.Status().Addr
	// Consulta registrada ANTES da troca: os contadores são do servidor
	// anterior e resetam no restart.
	_ = doQuery(t, addr, "udp", "youtube.com", 1)
	if st := c.Status(); st.Queries != 1 {
		t.Fatalf("Queries antes = %d, want 1", st.Queries)
	}

	if err := c.SetUpstream("9.9.9.9:53"); err != nil {
		t.Fatalf("SetUpstream ligado: %v", err)
	}
	st := c.Status()
	if !st.Listening {
		t.Fatal("SetUpstream derrubou o listener; esperava restart mantendo ouvindo")
	}
	if st.Upstream != "9.9.9.9:53" {
		t.Errorf("Upstream = %q, esperava 9.9.9.9:53", st.Upstream)
	}
	// Restart = servidor novo: contadores zerados.
	if st.Queries != 0 {
		t.Errorf("Queries após restart = %d, esperava 0 (contadores resetam)", st.Queries)
	}

	// E o novo listener continua servindo.
	_ = doQuery(t, st.Addr, "udp", "youtube.com", 1)
	if st := c.Status(); st.Queries != 1 || st.Blocked != 1 {
		t.Errorf("Queries/Blocked após nova consulta = %d/%d, esperava 1/1", st.Queries, st.Blocked)
	}

	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestBindHintOnlyForPort53(t *testing.T) {
	if got := bindHint("0.0.0.0:53"); got == "" {
		t.Error("bindHint(53) vazio, esperava guia ICS/dnscache")
	}
	if got := bindHint("127.0.0.1:5300"); got != "" {
		t.Errorf("bindHint(5300) = %q, esperava vazio", got)
	}
}
