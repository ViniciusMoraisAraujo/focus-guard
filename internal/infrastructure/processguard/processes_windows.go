//go:build windows

package processguard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// platformListProcesses enumerates running process image names via
// `tasklist /FO CSV /NH` (native, no PowerShell runtime).
func platformListProcesses() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tasklist", "/FO", "CSV", "/NH")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tasklist: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return parseTasklistNames(out), nil
}

// platformKillProcess terminates the process by image name via
// `taskkill /F /IM <name>.exe`. taskkill matches image names case-insensitively
// and is a no-match no-op when the process is not running.
func platformKillProcess(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "taskkill", "/F", "/IM", name+".exe")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill /IM %s.exe: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
