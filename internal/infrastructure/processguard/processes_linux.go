//go:build linux

package processguard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// platformListProcesses enumerates running process names by reading the comm
// file of every numeric entry under /proc. Unreadable entries (other users'
// processes) are skipped.
func platformListProcesses() ([]string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // não é um PID
		}
		comm, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if name := parseProcComm(comm); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// platformKillProcess terminates every process whose exact name matches via
// `pkill -x <name>` (procps). Best-effort: the caller only invokes it for
// processes observed running, so a failure means a race or lack of privilege —
// the next scan retries.
func platformKillProcess(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pkill", "-x", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pkill -x %s: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
