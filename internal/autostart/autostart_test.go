package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeCmd struct {
	fn func() ([]byte, error)
}

func (c *fakeCmd) CombinedOutput() ([]byte, error) {
	return c.fn()
}

func TestInstall_CreatesService(t *testing.T) {
	var capturedCmds []struct {
		name string
		args []string
	}
	var capturedDir string

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		capturedCmds = append(capturedCmds, struct {
			name string
			args []string
		}{name: name, args: args})
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origMkdir := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		capturedDir = path
		return nil
	}
	defer func() { osMkdirAll = origMkdir }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	exePath := `C:\Program Files\FocusGuard\focusguard-daemon.exe`
	err := Install(exePath)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	// Should have called sc twice: create + failure
	if len(capturedCmds) < 2 {
		t.Fatalf("expected at least 2 sc commands, got %d", len(capturedCmds))
	}

	// Check first command: sc create
	createCmd := capturedCmds[0]
	if createCmd.name != "sc" {
		t.Errorf("expected sc, got %q", createCmd.name)
	}
	argsJoined := strings.Join(createCmd.args, " ")
	if !strings.Contains(argsJoined, "create") {
		t.Errorf("expected create in args, got %v", createCmd.args)
	}
	if !strings.Contains(argsJoined, "FocusGuard") {
		t.Errorf("expected FocusGuard in args, got %v", createCmd.args)
	}
	if !strings.Contains(argsJoined, exePath) {
		t.Errorf("expected exePath %q in args, got %v", exePath, createCmd.args)
	}
	if !strings.Contains(argsJoined, "auto") {
		t.Errorf("expected auto start in args, got %v", createCmd.args)
	}

	// Check second command: sc failure (recovery)
	failureCmd := capturedCmds[1]
	if failureCmd.name != "sc" {
		t.Errorf("expected sc for failure command, got %q", failureCmd.name)
	}
	failureArgsJoined := strings.Join(failureCmd.args, " ")
	if !strings.Contains(failureArgsJoined, "failure") {
		t.Errorf("expected 'failure' in second command args, got %v", failureCmd.args)
	}
	if !strings.Contains(failureArgsJoined, "restart") {
		t.Errorf("expected 'restart' in failure actions, got %v", failureCmd.args)
	}

	if !strings.Contains(capturedDir, "FocusGuard") {
		t.Errorf("expected FocusGuard dir, got %q", capturedDir)
	}
}

func TestInstall_InvalidPath(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := Install("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestInstall_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	err := Install("/usr/local/bin/focusguard-daemon")
	if err == nil || !strings.Contains(err.Error(), "não é suportada") {
		t.Fatalf("expected unsupported platform error on linux, got: %v", err)
	}
}

func TestUninstall_CorrectCommand(t *testing.T) {
	var capturedName string
	var capturedArgs []string

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		capturedName = name
		capturedArgs = args
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall returned error: %v", err)
	}

	if capturedName != "sc" {
		t.Errorf("expected sc, got %q", capturedName)
	}

	argsJoined := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsJoined, "delete") {
		t.Errorf("expected delete in args, got %v", capturedArgs)
	}
	if !strings.Contains(argsJoined, "FocusGuard") {
		t.Errorf("expected FocusGuard in args, got %v", capturedArgs)
	}
}

func TestUninstall_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "darwin"
	defer func() { goos = origGoos }()

	err := Uninstall()
	if err == nil || !strings.Contains(err.Error(), "não é suportada") {
		t.Fatalf("expected unsupported platform error on darwin, got: %v", err)
	}
}

func TestIsInstalled_DetectsExistingService(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("STATE: 4 RUNNING"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	installed, err := IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled returned error: %v", err)
	}
	if !installed {
		t.Error("expected installed=true when sc query succeeds")
	}
}

func TestIsInstalled_NotInstalled(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("FAILED 1060: The specified service does not exist"), fmt.Errorf("exit status 1060")
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	installed, err := IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled returned error: %v", err)
	}
	if installed {
		t.Error("expected installed=false when sc query fails with 'does not exist'")
	}
}

func TestInstallSvc_Systemd(t *testing.T) {
	dir := t.TempDir()

	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origSvcDir := systemdServiceDir
	systemdServiceDir = func() string {
		return dir
	}
	defer func() { systemdServiceDir = origSvcDir }()

	installed, err := IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled on linux: %v", err)
	}
	if installed {
		t.Error("expected not installed before creating service file")
	}

	svcPath := filepath.Join(dir, "focusguard.service")
	if err := os.WriteFile(svcPath, []byte("[Unit]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	installed, err = IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled on linux after creating file: %v", err)
	}
	if !installed {
		t.Error("expected installed after creating service file")
	}
}

func TestInstallSvc_Systemd_Unsupported(t *testing.T) {
	origGoos := goos
	goos = "darwin"
	defer func() { goos = origGoos }()

	err := InstallSvc("/usr/local/bin/focusguard-daemon")
	if err == nil || !strings.Contains(err.Error(), "não é suportada") {
		t.Fatalf("expected unsupported error on darwin, got: %v", err)
	}
}

func TestInstallSvc_InvalidPath(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	err := InstallSvc("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestInstallSvc_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origSvcDir := systemdServiceDir
	systemdServiceDir = func() string {
		return dir
	}
	defer func() { systemdServiceDir = origSvcDir }()

	var cmds []string
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	exePath := "/usr/local/bin/focusguard-daemon"
	err := InstallSvc(exePath)
	if err != nil {
		t.Fatalf("InstallSvc returned error: %v", err)
	}

	svcPath := filepath.Join(dir, "focusguard.service")
	data, err := os.ReadFile(svcPath)
	if err != nil {
		t.Fatalf("failed to read service file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, exePath) {
		t.Errorf("expected service file to contain exe path %q, got:\n%s", exePath, content)
	}
	if !strings.Contains(content, "ExecStart") {
		t.Errorf("expected ExecStart in service file, got:\n%s", content)
	}
	if !strings.Contains(content, "WantedBy=multi-user.target") {
		t.Errorf("expected systemd service file with WantedBy, got:\n%s", content)
	}

	if len(cmds) == 0 {
		t.Error("expected systemctl commands to be executed")
	}
}

func TestInstallSvc_RunsSystemctl(t *testing.T) {
	dir := t.TempDir()
	var captured [][]string

	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origSvcDir := systemdServiceDir
	systemdServiceDir = func() string {
		return dir
	}
	defer func() { systemdServiceDir = origSvcDir }()

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, append([]string{name}, args...))
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	if err := InstallSvc("/usr/local/bin/focusguard-daemon"); err != nil {
		t.Fatalf("InstallSvc returned error: %v", err)
	}

	foundReload := false
	foundEnable := false
	foundStart := false
	for _, cmd := range captured {
		if len(cmd) < 2 {
			continue
		}
		if cmd[0] == "systemctl" && cmd[1] == "daemon-reload" {
			foundReload = true
		}
		if cmd[0] == "systemctl" && cmd[1] == "enable" && len(cmd) >= 3 && cmd[2] == "focusguard" {
			foundEnable = true
		}
		if cmd[0] == "systemctl" && cmd[1] == "start" && len(cmd) >= 3 && cmd[2] == "focusguard" {
			foundStart = true
		}
	}

	if !foundReload {
		t.Error("expected systemctl daemon-reload to be called")
	}
	if !foundEnable {
		t.Error("expected systemctl enable focusguard to be called")
	}
	if !foundStart {
		t.Error("expected systemctl start focusguard to be called")
	}
}

func TestUninstallSvc_RemovesFile(t *testing.T) {
	dir := t.TempDir()

	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origSvcDir := systemdServiceDir
	systemdServiceDir = func() string {
		return dir
	}
	defer func() { systemdServiceDir = origSvcDir }()

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	svcPath := filepath.Join(dir, "focusguard.service")
	if err := os.WriteFile(svcPath, []byte("[Unit]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := UninstallSvc(); err != nil {
		t.Fatalf("UninstallSvc returned error: %v", err)
	}

	if _, err := os.Stat(svcPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected service file to be removed, still exists at %q", svcPath)
	}
}

func TestIsInstalled_Linux_StatError(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origSvcDir := systemdServiceDir
	systemdServiceDir = func() string {
		return "/nonexistent"
	}
	defer func() { systemdServiceDir = origSvcDir }()

	origStat := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrPermission
	}
	defer func() { osStat = origStat }()

	_, err := IsInstalled()
	if err == nil {
		t.Fatal("expected error when os.Stat returns permission denied")
	}
}

func TestIsInstalled_Linux_FileExists(t *testing.T) {
	dir := t.TempDir()

	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origSvcDir := systemdServiceDir
	systemdServiceDir = func() string {
		return dir
	}
	defer func() { systemdServiceDir = origSvcDir }()

	svcPath := filepath.Join(dir, "focusguard.service")
	if err := os.WriteFile(svcPath, []byte("[Unit]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	installed, err := IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled returned error: %v", err)
	}
	if !installed {
		t.Error("expected installed=true when service file exists")
	}
}

func TestUninstallSvc_RunsSystemctl(t *testing.T) {
	dir := t.TempDir()
	var captured [][]string

	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	origSvcDir := systemdServiceDir
	systemdServiceDir = func() string {
		return dir
	}
	defer func() { systemdServiceDir = origSvcDir }()

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, append([]string{name}, args...))
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	svcPath := filepath.Join(dir, "focusguard.service")
	if err := os.WriteFile(svcPath, []byte("[Unit]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := UninstallSvc(); err != nil {
		t.Fatalf("UninstallSvc returned error: %v", err)
	}

	foundDisable := false
	foundReload := false
	for _, cmd := range captured {
		if len(cmd) < 2 {
			continue
		}
		if cmd[0] == "systemctl" && cmd[1] == "disable" && len(cmd) >= 3 && cmd[2] == "focusguard" {
			foundDisable = true
		}
		if cmd[0] == "systemctl" && cmd[1] == "daemon-reload" {
			foundReload = true
		}
	}

	if !foundDisable {
		t.Error("expected systemctl disable focusguard to be called")
	}
	if !foundReload {
		t.Error("expected systemctl daemon-reload to be called")
	}
}
