package processguard

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// normalizeName
// ---------------------------------------------------------------------------

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"discord", "discord"},
		{"Discord.exe", "discord"},
		{"STEAM", "steam"},
		{"Steam.exe", "steam"},
		{"firefox", "firefox"},
		{" firefox.exe ", "firefox"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeName(tt.in); got != tt.want {
			t.Errorf("normalizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// matches / processRunning
// ---------------------------------------------------------------------------

func TestMatches(t *testing.T) {
	tests := []struct {
		process  string
		denylist []string
		want     bool
	}{
		{"steam", []string{"steam"}, true},
		{"Steam.exe", []string{"steam"}, true},
		{"discord", []string{"Discord.exe"}, true},
		{"steamcmd", []string{"steam"}, false}, // nome exato, não substring
		{"firefox", []string{"steam", "discord"}, false},
		{"firefox", nil, false},
	}
	for _, tt := range tests {
		if got := matches(tt.process, tt.denylist); got != tt.want {
			t.Errorf("matches(%q, %v) = %v, want %v", tt.process, tt.denylist, got, tt.want)
		}
	}
}

func TestProcessRunning(t *testing.T) {
	if !processRunning([]string{"bash", "Discord.exe"}, "discord") {
		t.Error("expected Discord.exe to match denylist entry discord")
	}
	if processRunning([]string{"bash", "firefox"}, "steam") {
		t.Error("expected no match for steam")
	}
	if processRunning(nil, "steam") {
		t.Error("empty process list must not match")
	}
}

// ---------------------------------------------------------------------------
// parseProcComm / parseTasklistNames (pure, cross-platform)
// ---------------------------------------------------------------------------

func TestParseProcComm(t *testing.T) {
	tests := []struct {
		in   []byte
		want string
	}{
		{[]byte("discord\n"), "discord"},
		{[]byte("steam\x00\n"), "steam"},
		{[]byte("  bash  \n"), "bash"},
		{[]byte{}, ""},
	}
	for _, tt := range tests {
		if got := parseProcComm(tt.in); got != tt.want {
			t.Errorf("parseProcComm(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseTasklistNames(t *testing.T) {
	output := []byte("\"discord.exe\",\"1234\",\"Console\",\"1\",\"48,5 K\"\r\n" +
		"\"steam.exe\",\"5678\",\"Console\",\"1\",\"120,0 K\"\r\n" +
		"\"System Idle Process\",\"0\",\"Services\",\"0\",\"8 K\"\r\n")
	names := parseTasklistNames(output)

	want := map[string]bool{"discord.exe": true, "steam.exe": true, "System Idle Process": true}
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d: %v", len(names), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected name %q", n)
		}
	}
}

func TestParseTasklistNames_EmptyOutput(t *testing.T) {
	if names := parseTasklistNames([]byte("")); len(names) != 0 {
		t.Errorf("expected no names from empty output, got %v", names)
	}
}

// ---------------------------------------------------------------------------
// stubs
// ---------------------------------------------------------------------------

// stubProcesses replaces the package list/kill vars with stubs (restored on
// cleanup) and records every name passed to killProcess (mutex-protected, safe
// for the ticker goroutine).
func stubProcesses(t *testing.T, procs []string, listErr error) *[]string {
	t.Helper()

	origList := listProcesses
	origKill := killProcess

	var mu sync.Mutex
	killed := make([]string, 0, 2)

	listProcesses = func() ([]string, error) { return procs, listErr }
	killProcess = func(name string) error {
		mu.Lock()
		defer mu.Unlock()
		killed = append(killed, name)
		return nil
	}

	t.Cleanup(func() {
		listProcesses = origList
		killProcess = origKill
	})
	return &killed
}

// ---------------------------------------------------------------------------
// RunOnce
// ---------------------------------------------------------------------------

func TestRunOnce_KillsOnlyMatching(t *testing.T) {
	killedNames := stubProcesses(t, []string{"steam", "firefox", "Discord.exe"}, nil)
	g := New([]string{"steam", "discord.exe"})

	killed := g.RunOnce()

	if len(killed) != 2 {
		t.Fatalf("expected 2 killed, got %v", killed)
	}
	// Ordem preservada do denylist.
	if killed[0] != "steam" || killed[1] != "discord" {
		t.Errorf("killed = %v, want [steam discord]", killed)
	}

	// O killProcess deve receber o nome NORMALIZADO (sem .exe): o pkill -x do
	// Linux casa com o comm, e o taskkill do Windows adiciona o .exe sozinho.
	if len(*killedNames) != 2 || (*killedNames)[0] != "steam" || (*killedNames)[1] != "discord" {
		t.Errorf("killProcess chamado com %v, want [steam discord] (normalizado)", *killedNames)
	}
}

func TestRunOnce_EmptyDenylist(t *testing.T) {
	stubProcesses(t, []string{"steam"}, nil)
	g := New(nil)

	if killed := g.RunOnce(); len(killed) != 0 {
		t.Errorf("empty denylist must not kill anything, got %v", killed)
	}
}

func TestRunOnce_NoMatches(t *testing.T) {
	stubProcesses(t, []string{"firefox", "bash"}, nil)
	g := New([]string{"steam"})

	if killed := g.RunOnce(); len(killed) != 0 {
		t.Errorf("no matching processes should be killed, got %v", killed)
	}
}

func TestRunOnce_KillFailureNotCounted(t *testing.T) {
	origList := listProcesses
	origKill := killProcess
	defer func() { listProcesses, killProcess = origList, origKill }()

	listProcesses = func() ([]string, error) { return []string{"steam", "discord"}, nil }
	killProcess = func(name string) error {
		if name == "steam" {
			return errors.New("permission denied")
		}
		return nil
	}

	g := New([]string{"steam", "discord"})
	killed := g.RunOnce()

	// Falha de kill é best-effort: o processo não entra na lista de mortos.
	if len(killed) != 1 || killed[0] != "discord" {
		t.Errorf("killed = %v, want only [discord] (steam falhou)", killed)
	}
}

func TestRunOnce_ListFailureNoPanic(t *testing.T) {
	origList := listProcesses
	defer func() { listProcesses = origList }()
	listProcesses = func() ([]string, error) { return nil, errors.New("no permission") }

	g := New([]string{"steam"})
	if killed := g.RunOnce(); len(killed) != 0 {
		t.Errorf("list failure must be best-effort (nothing killed), got %v", killed)
	}
}

func TestSetDenylist_ReplacesAtomically(t *testing.T) {
	origList := listProcesses
	origKill := killProcess
	defer func() { listProcesses, killProcess = origList, origKill }()

	var killed []string
	listProcesses = func() ([]string, error) { return []string{"steam", "discord"}, nil }
	killProcess = func(name string) error { killed = append(killed, name); return nil }

	g := New([]string{"steam"})
	if got := g.RunOnce(); len(got) != 1 {
		t.Fatalf("expected steam killed with initial denylist, got %v", got)
	}

	g.SetDenylist([]string{"discord"})
	killed = nil
	if got := g.RunOnce(); len(got) != 1 || got[0] != "discord" {
		t.Errorf("after SetDenylist expected only discord killed, got %v", got)
	}
	if dl := g.Denylist(); len(dl) != 1 || dl[0] != "discord" {
		t.Errorf("Denylist() = %v, want [discord]", dl)
	}
}

// ---------------------------------------------------------------------------
// Start / Stop ticker
// ---------------------------------------------------------------------------

func TestGuard_StartTicksAndStops(t *testing.T) {
	origList := listProcesses
	origKill := killProcess
	defer func() { listProcesses, killProcess = origList, origKill }()

	listProcesses = func() ([]string, error) { return []string{"steam"}, nil }
	var kills int32
	killProcess = func(name string) error { atomic.AddInt32(&kills, 1); return nil }

	g := New([]string{"steam"})
	g.interval = 20 * time.Millisecond
	g.Start(func() bool { return true })

	// Aguarda pelo menos 1 varredura com kill.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&kills) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&kills) == 0 {
		t.Fatal("expected the ticker to kill the matching process while active")
	}

	g.Stop()
	after := atomic.LoadInt32(&kills)
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&kills); got != after {
		t.Errorf("ticker must stop after Stop(): kills %d -> %d", after, got)
	}
}

func TestGuard_SkipsWhenInactive(t *testing.T) {
	origList := listProcesses
	origKill := killProcess
	defer func() { listProcesses, killProcess = origList, origKill }()

	listProcesses = func() ([]string, error) { return []string{"steam"}, nil }
	var kills int32
	killProcess = func(name string) error { atomic.AddInt32(&kills, 1); return nil }

	g := New([]string{"steam"})
	g.interval = 10 * time.Millisecond
	g.Start(func() bool { return false }) // nenhuma sessão ativa

	time.Sleep(150 * time.Millisecond)
	g.Stop()

	if got := atomic.LoadInt32(&kills); got != 0 {
		t.Errorf("no kills expected while inactive, got %d", got)
	}
}

func TestGuard_StartIsIdempotent(t *testing.T) {
	origList := listProcesses
	origKill := killProcess
	defer func() { listProcesses, killProcess = origList, origKill }()

	listProcesses = func() ([]string, error) { return []string{"steam"}, nil }
	var kills int32
	killProcess = func(name string) error { atomic.AddInt32(&kills, 1); return nil }

	g := New([]string{"steam"})
	g.interval = 20 * time.Millisecond
	g.Start(func() bool { return true })
	g.Start(func() bool { return true }) // segunda chamada é no-op

	time.Sleep(80 * time.Millisecond)
	g.Stop()
	g.Stop() // Stop duplo é seguro

	if atomic.LoadInt32(&kills) == 0 {
		t.Error("expected kills while active (idempotent Start must not break the ticker)")
	}
}

func TestNew_SetsPositiveDefaultInterval(t *testing.T) {
	g := New([]string{"steam"})
	if g.interval <= 0 {
		t.Errorf("default interval must be positive, got %v", g.interval)
	}
	if dl := g.Denylist(); len(dl) != 1 || dl[0] != "steam" {
		t.Errorf("Denylist() = %v, want [steam]", dl)
	}
}
