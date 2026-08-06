package processguard

import (
	"fmt"
	"testing"
)

// BenchmarkRunOnce measures one full scan of a realistic process table (~300
// processes) against a typical denylist, using stubbed list/kill so no real
// processes are touched. In production this runs every 5s while a session is
// active and lists processes via a subprocess (tasklist on Windows, /proc on
// Linux) — the stub keeps the pure in-process cost measurable.
func BenchmarkRunOnce(b *testing.B) {
	procs := make([]string, 300)
	for i := range procs {
		procs[i] = fmt.Sprintf("proc%d.exe", i)
	}
	procs[0] = "steam.exe"
	procs[150] = "discord.exe"

	origList, origKill := listProcesses, killProcess
	listProcesses = func() ([]string, error) { return procs, nil }
	killProcess = func(string) error { return nil }
	b.Cleanup(func() { listProcesses, killProcess = origList, origKill })

	g := New([]string{"steam.exe", "discord.exe", "telegram.exe", "whatsapp.exe", "netflix.exe"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.RunOnce()
	}
}
