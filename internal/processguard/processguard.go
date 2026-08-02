// Package processguard monitors running processes while a focus session is
// active and terminates executables that match a denylist (e.g. games and chat
// apps like steam.exe/discord.exe), so they cannot be used during a block.
package processguard

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

// ActivityChecker reports whether a focus session is currently active. The
// daemon wires it to scheduler.HasActiveBlocks, so the guard only acts while
// there is an active block.
type ActivityChecker interface {
	HasActiveBlocks() bool
}

// listProcesses and killProcess are the two platform-dependent operations.
// They are package vars so tests can stub them without root/admin privileges;
// the real implementations live in the build-tagged platform files.
var (
	listProcesses = platformListProcesses
	killProcess   = platformKillProcess
)

// defaultInterval is how often the guard scans the process table while a
// session is active.
const defaultInterval = 5 * time.Second

// Guard periodically scans running processes and kills any that match the
// denylist, but only while the session is active.
type Guard struct {
	mu       sync.Mutex
	denylist []string
	interval time.Duration
	stop     chan struct{}
}

// New returns a Guard with the given denylist and the default scan interval.
func New(denylist []string) *Guard {
	return &Guard{
		denylist: append([]string(nil), denylist...),
		interval: defaultInterval,
	}
}

// Denylist returns a copy of the current denylist.
func (g *Guard) Denylist() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.denylist...)
}

// SetDenylist atomically replaces the denylist; a running guard picks it up on
// the next scan.
func (g *Guard) SetDenylist(names []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.denylist = append([]string(nil), names...)
}

// Start launches the background scan loop. isActive gates the scans: while it
// returns false (no active session) the guard does nothing. Calling Start more
// than once is a no-op.
func (g *Guard) Start(isActive func() bool) {
	g.mu.Lock()
	if g.stop != nil {
		g.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	g.stop = stop
	interval := g.interval
	g.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if isActive != nil && !isActive() {
					continue
				}
				g.RunOnce()
			}
		}
	}()
}

// Stop halts the background scan loop. It is safe to call more than once.
func (g *Guard) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stop == nil {
		return
	}
	close(g.stop)
	g.stop = nil
}

// RunOnce performs a single scan: it lists running processes, kills the ones
// matching the denylist and returns the names actually killed. Failures
// (list or kill) are best-effort — a failed kill is simply not reported, and
// the next scan retries.
func (g *Guard) RunOnce() []string {
	g.mu.Lock()
	denylist := append([]string(nil), g.denylist...)
	g.mu.Unlock()

	if len(denylist) == 0 {
		return nil
	}

	procs, err := listProcesses()
	if err != nil || len(procs) == 0 {
		return nil
	}

	var killed []string
	for _, raw := range denylist {
		// Normaliza antes do kill: o denylist padrão usa "steam.exe"/"discord.exe",
		// mas o pkill -x do Linux casa com o comm ("steam"), não com o nome com
		// extensão — e o taskkill do Windows adiciona o .exe por conta própria.
		name := normalizeName(raw)
		if name == "" || !processRunning(procs, name) {
			continue
		}
		if killProcess(name) == nil {
			killed = append(killed, name)
		}
	}
	return killed
}

// processRunning reports whether any running process matches the denylist name.
func processRunning(procs []string, name string) bool {
	for _, p := range procs {
		if matches(p, []string{name}) {
			return true
		}
	}
	return false
}

// matches reports whether a running process name matches a denylist entry,
// comparing normalized names (lowercase, ".exe" stripped). Windows reports
// "Discord.exe" while Linux comm is "discord"; normalization makes both hit
// the same entry.
func matches(processName string, denylist []string) bool {
	np := normalizeName(processName)
	for _, d := range denylist {
		if normalizeName(d) == np {
			return true
		}
	}
	return false
}

// normalizeName lowercases and strips a trailing ".exe" (any casing), so
// "Discord.exe", "DISCORD.EXE" and "discord" all normalize to "discord".
func normalizeName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(n, ".exe")
}

// parseProcComm extracts the process name from a /proc/<pid>/comm file: a
// single line terminated by '\n' (some kernels pad with NULs). Leading/trailing
// NULs and whitespace are stripped in either order.
func parseProcComm(comm []byte) string {
	return string(bytes.Trim(comm, "\x00\n\r\t "))
}

// parseTasklistNames extracts image names from `tasklist /FO CSV /NH` output:
// each line starts with a quoted image name.
func parseTasklistNames(output []byte) []string {
	var names []string
	for _, line := range bytes.Split(output, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '"' {
			continue
		}
		end := bytes.IndexByte(line[1:], '"')
		if end < 0 {
			continue
		}
		name := string(line[1 : 1+end])
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
