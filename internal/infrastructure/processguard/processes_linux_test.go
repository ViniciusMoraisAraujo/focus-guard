//go:build linux

package processguard

import (
	"strings"
	"testing"
)

// TestPlatformListProcesses_FindsCurrentProcess smoke-tests the real /proc
// scan: it must return at least the current test process without error.
func TestPlatformListProcesses_FindsCurrentProcess(t *testing.T) {
	procs, err := platformListProcesses()
	if err != nil {
		t.Fatalf("platformListProcesses erro: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("expected at least the current process in /proc")
	}
	foundSelf := false
	for _, p := range procs {
		if strings.Contains(p, "processguard") {
			foundSelf = true
			break
		}
	}
	if !foundSelf {
		t.Errorf("expected the test process (comm contém 'processguard') na lista, obteve %v", procs)
	}
}

// TestPlatformKillProcess_NoSuchProcess verifies the pkill path returns an
// error for a process that cannot exist — proving the exec path works without
// ever killing a real process.
func TestPlatformKillProcess_NoSuchProcess(t *testing.T) {
	err := platformKillProcess("focusguard-no-such-process-xyz")
	if err == nil {
		t.Error("expected pkill -x to fail for a nonexistent process")
	}
}
