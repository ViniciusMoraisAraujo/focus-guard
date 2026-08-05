package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeCmd struct {
	fn func() ([]byte, error)
}

func (c *fakeCmd) CombinedOutput() ([]byte, error) {
	return c.fn()
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() interface{}   { return nil }

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

	if len(capturedCmds) != 3 {
		t.Fatalf("expected 3 sc commands (create, failure, start), got %d", len(capturedCmds))
	}

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

	startCmd := capturedCmds[2]
	startArgsJoined := strings.Join(startCmd.args, " ")
	if startCmd.name != "sc" || !strings.Contains(startArgsJoined, "start") || !strings.Contains(startArgsJoined, serviceName) {
		t.Errorf("expected sc start %s as third command, got %v", serviceName, startCmd.args)
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
	var captured []struct {
		name string
		args []string
	}

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, struct {
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

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall returned error: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 sc commands (stop, delete), got %d", len(captured))
	}

	for _, c := range captured {
		if c.name != "sc" {
			t.Errorf("expected sc, got %q", c.name)
		}
	}

	stopArgs := strings.Join(captured[0].args, " ")
	if !strings.Contains(stopArgs, "stop") || !strings.Contains(stopArgs, serviceName) {
		t.Errorf("expected sc stop %s first, got %v", serviceName, captured[0].args)
	}

	deleteArgs := strings.Join(captured[1].args, " ")
	if !strings.Contains(deleteArgs, "delete") || !strings.Contains(deleteArgs, serviceName) {
		t.Errorf("expected sc delete %s second, got %v", serviceName, captured[1].args)
	}
}

func TestUninstall_NotInstalledIsIdempotent(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("FALHA 1060: O serviço especificado não existe"), errors.New("exit status 1060")
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	if err := Uninstall(); err != nil {
		t.Fatalf("uninstall should be idempotent when daemon service is not installed (exit 1060), got: %v", err)
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

func TestInstallWatchdog_CreatesService(t *testing.T) {
	var captured []struct {
		name string
		args []string
	}

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, struct {
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

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	exePath := `C:\Program Files\FocusGuard\focusguard-watchdog.exe`
	err := InstallWatchdog(exePath)
	if err != nil {
		t.Fatalf("InstallWatchdog returned error: %v", err)
	}

	if len(captured) != 4 {
		t.Fatalf("expected 4 sc commands (create, description, failure, start), got %d", len(captured))
	}

	for _, c := range captured {
		joined := strings.Join(c.args, " ")
		if strings.Contains(joined, "depend=") || strings.Contains(joined, "config") {
			t.Errorf("BUG REGRESSION: watchdog must NOT depend on the daemon (sc config depend= caused start error 1068), got %v", c.args)
		}
	}

	createCmd := captured[0]
	if createCmd.name != "sc" {
		t.Errorf("expected sc, got %q", createCmd.name)
	}
	createArgsJoined := strings.Join(createCmd.args, " ")
	if !strings.Contains(createArgsJoined, "create") {
		t.Errorf("expected create in args, got %v", createCmd.args)
	}
	if !strings.Contains(createArgsJoined, watchdogServiceName) {
		t.Errorf("expected %q in args, got %v", watchdogServiceName, createCmd.args)
	}
	if !strings.Contains(createArgsJoined, exePath) {
		t.Errorf("expected exePath %q in args, got %v", exePath, createCmd.args)
	}
	if !strings.Contains(createArgsJoined, "displayname=") {
		t.Errorf("expected displayname in args, got %v", createCmd.args)
	}
	if !strings.Contains(createArgsJoined, "start=") || !strings.Contains(createArgsJoined, "auto") {
		t.Errorf("expected start= auto in args, got %v", createCmd.args)
	}
	if strings.Contains(createArgsJoined, "description=") {
		t.Errorf("BUG REGRESSION: 'description=' must NOT be passed to sc create (exit status 1639), got %v", createCmd.args)
	}

	descCmd := captured[1]
	descArgsJoined := strings.Join(descCmd.args, " ")
	if !strings.Contains(descArgsJoined, "description") {
		t.Errorf("expected sc description as second command, got %v", descCmd.args)
	}
	if !strings.Contains(descArgsJoined, watchdogServiceName) {
		t.Errorf("expected watchdog service name in description command, got %v", descCmd.args)
	}

	failureCmd := captured[2]
	failureArgsJoined := strings.Join(failureCmd.args, " ")
	if !strings.Contains(failureArgsJoined, "failure") {
		t.Errorf("expected sc failure as third command, got %v", failureCmd.args)
	}
	if !strings.Contains(failureArgsJoined, "restart") {
		t.Errorf("expected restart actions in failure command, got %v", failureCmd.args)
	}

	startCmd := captured[3]
	startArgsJoined := strings.Join(startCmd.args, " ")
	if !strings.Contains(startArgsJoined, "start") {
		t.Errorf("expected sc start as fourth command, got %v", startCmd.args)
	}
	if !strings.Contains(startArgsJoined, watchdogServiceName) {
		t.Errorf("expected watchdog service name in start command, got %v", startCmd.args)
	}
}

func TestInstallWatchdog_DescriptionFailureIsWarning(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				if len(args) > 0 && args[0] == "description" {
					return []byte("FAILED"), errors.New("exit status 1")
				}
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	if err := InstallWatchdog(`C:\focusguard-watchdog.exe`); err != nil {
		t.Fatalf("InstallWatchdog should succeed even if sc description/config fail (cosmetic, ignored), got: %v", err)
	}
}

func TestInstallWatchdog_CreateFails(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				if len(args) > 0 && args[0] == "create" {
					return []byte("exit status 1639"), errors.New("exit status 1639")
				}
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := InstallWatchdog(`C:\focusguard-watchdog.exe`)
	if err == nil {
		t.Fatal("expected error when sc create fails")
	}
	if !strings.Contains(err.Error(), "falha ao criar serviço watchdog") {
		t.Errorf("expected create failure message, got: %v", err)
	}
}

func TestInstallWatchdog_StartFails(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				if len(args) > 0 && args[0] == "start" {
					return []byte("FAILED 1066"), errors.New("exit status 1066")
				}
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := InstallWatchdog(`C:\focusguard-watchdog.exe`)
	if err == nil {
		t.Fatal("expected error when sc start fails")
	}
	if !strings.Contains(err.Error(), "falha ao iniciar watchdog") {
		t.Errorf("expected start failure message, got: %v", err)
	}
}

func TestInstallWatchdog_RecoveryFailureIsWarning(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				if len(args) > 0 && args[0] == "failure" {
					return []byte("FAILED"), errors.New("exit status 1")
				}
				return []byte("success"), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	if err := InstallWatchdog(`C:\focusguard-watchdog.exe`); err != nil {
		t.Fatalf("InstallWatchdog should succeed even if sc failure fails (warning only), got: %v", err)
	}
}

func TestInstallWatchdog_InvalidPath(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := InstallWatchdog("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestInstallWatchdog_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	err := InstallWatchdog("/usr/local/bin/focusguard-watchdog")
	if err == nil || !strings.Contains(err.Error(), "exclusivo do Windows") {
		t.Fatalf("expected unsupported platform error on linux, got: %v", err)
	}
}

func TestUninstallWatchdog_CorrectCommands(t *testing.T) {
	var captured []struct {
		name string
		args []string
	}

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, struct {
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

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := UninstallWatchdog()
	if err != nil {
		t.Fatalf("UninstallWatchdog returned error: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 sc commands (stop, delete), got %d", len(captured))
	}

	stopArgs := strings.Join(captured[0].args, " ")
	if !strings.Contains(stopArgs, "stop") || !strings.Contains(stopArgs, watchdogServiceName) {
		t.Errorf("expected sc stop %s first, got %v", watchdogServiceName, captured[0].args)
	}

	deleteArgs := strings.Join(captured[1].args, " ")
	if !strings.Contains(deleteArgs, "delete") || !strings.Contains(deleteArgs, watchdogServiceName) {
		t.Errorf("expected sc delete %s second, got %v", watchdogServiceName, captured[1].args)
	}
}

func TestUninstallWatchdog_DeleteFails(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("FAILED 5: Acesso negado"), errors.New("exit status 5")
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := UninstallWatchdog()
	if err == nil {
		t.Fatal("expected error when sc delete fails")
	}
	if !strings.Contains(err.Error(), "falha ao remover watchdog") {
		t.Errorf("expected delete failure message, got: %v", err)
	}
}

func TestUninstallWatchdog_NotInstalledIsIdempotent(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("FAILED 1060: The specified service does not exist"), errors.New("exit status 1060")
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	if err := UninstallWatchdog(); err != nil {
		t.Fatalf("uninstall should be idempotent when service is not installed (exit 1060), got: %v", err)
	}
}

func TestUninstallWatchdog_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "darwin"
	defer func() { goos = origGoos }()

	err := UninstallWatchdog()
	if err == nil || !strings.Contains(err.Error(), "exclusivo do Windows") {
		t.Fatalf("expected unsupported platform error on darwin, got: %v", err)
	}
}

func TestInstallTray_CreatesRunKey(t *testing.T) {
	var captured []struct {
		name string
		args []string
	}

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, struct {
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

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	exePath := `C:\Program Files\FocusGuard\focusguard-tray.exe`
	err := InstallTray(exePath)
	if err != nil {
		t.Fatalf("InstallTray returned error: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected 1 reg command, got %d", len(captured))
	}
	cmd := captured[0]
	if cmd.name != "reg" {
		t.Errorf("expected reg, got %q", cmd.name)
	}
	joined := strings.Join(cmd.args, " ")
	for _, want := range []string{
		"add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "FocusGuardTray",
		"/t", "REG_SZ",
		"/d", `"` + exePath + `"`,
		"/f",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got %v", want, cmd.args)
		}
	}
	// O valor do Run key precisa de aspas por causa do espaço em "Program Files"
	// — sem elas o Windows tenta executar "C:\Program" e o tray não inicia.
	if !strings.Contains(joined, `"`+exePath+`"`) {
		t.Errorf("Run key value must be quoted (path has spaces), got args %v", cmd.args)
	}
}

func TestInstallTray_InvalidPath(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := InstallTray("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestInstallTray_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	err := InstallTray("/usr/local/bin/focusguard-tray")
	if err == nil || !strings.Contains(err.Error(), "exclusivo do Windows") {
		t.Fatalf("expected unsupported platform error on linux, got: %v", err)
	}
}

func TestUninstallTray_RemovesRunKey(t *testing.T) {
	var captured []struct {
		name string
		args []string
	}

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, struct {
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

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	if err := UninstallTray(); err != nil {
		t.Fatalf("UninstallTray returned error: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 reg command, got %d", len(captured))
	}
	joined := strings.Join(captured[0].args, " ")
	if captured[0].name != "reg" || !strings.Contains(joined, "delete") || !strings.Contains(joined, "FocusGuardTray") {
		t.Errorf("expected reg delete of FocusGuardTray, got %v", captured[0].args)
	}
}

func TestUninstallTray_NotInstalledIsIdempotent(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("ERROR: The system was unable to find the specified registry key or value."), errors.New("exit status 1")
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	if err := UninstallTray(); err != nil {
		t.Fatalf("uninstall should be idempotent when value is missing (exit 1), got: %v", err)
	}
}

func TestUninstallTray_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "darwin"
	defer func() { goos = origGoos }()

	err := UninstallTray()
	if err == nil || !strings.Contains(err.Error(), "exclusivo do Windows") {
		t.Fatalf("expected unsupported platform error on darwin, got: %v", err)
	}
}

func TestIsTrayInstalled_DetectsExisting(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte(`    FocusGuardTray    REG_SZ    C:\focusguard-tray.exe`), nil
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	installed, err := IsTrayInstalled()
	if err != nil {
		t.Fatalf("IsTrayInstalled returned error: %v", err)
	}
	if !installed {
		t.Error("expected installed=true when reg query succeeds")
	}
}

func TestIsTrayInstalled_NotInstalled(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("ERROR: The system was unable to find the specified registry key or value."), errors.New("exit status 1")
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	installed, err := IsTrayInstalled()
	if err != nil {
		t.Fatalf("IsTrayInstalled returned error: %v", err)
	}
	if installed {
		t.Error("expected installed=false when value is missing")
	}
}

func TestIsTrayInstalled_OtherError(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{
			fn: func() ([]byte, error) {
				return []byte("ERROR: Access denied"), errors.New("exit status 5")
			},
		}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	_, err := IsTrayInstalled()
	if err == nil {
		t.Fatal("expected error when reg query fails with access denied")
	}
}

func TestInstallDir_Windows(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	t.Setenv("ProgramFiles", `C:\Program Files`)
	if got := InstallDir(); got != `C:\Program Files\FocusGuard` {
		t.Errorf("expected C:\\Program Files\\FocusGuard, got %q", got)
	}
}

func TestInstallDir_FallbackWhenProgramFilesMissing(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	t.Setenv("ProgramFiles", "")
	if got := InstallDir(); got != `C:\Program Files\FocusGuard` {
		t.Errorf("expected fallback C:\\Program Files\\FocusGuard, got %q", got)
	}
}

func TestInstallDir_NonWindows(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	if got := InstallDir(); got != "" {
		t.Errorf("expected empty install dir on non-windows, got %q", got)
	}
}

func TestInstallBinaries_CopiesExistingBinaries(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	base := t.TempDir()
	t.Setenv("ProgramFiles", base)
	src := t.TempDir()

	present := []string{"focusguard.exe", "focusguard-daemon.exe", "focusguard-tray.exe"}
	for _, name := range present {
		if err := os.WriteFile(filepath.Join(src, name), []byte("bin-"+name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// focusguard-watchdog.exe ausente intencionalmente.

	dir, err := InstallBinaries(src)
	if err != nil {
		t.Fatalf("InstallBinaries returned error: %v", err)
	}
	wantDir := filepath.Join(base, "FocusGuard")
	if dir != wantDir {
		t.Errorf("expected install dir %q, got %q", wantDir, dir)
	}
	for _, name := range present {
		data, err := os.ReadFile(filepath.Join(wantDir, name))
		if err != nil {
			t.Errorf("expected %s to be copied: %v", name, err)
			continue
		}
		if string(data) != "bin-"+name {
			t.Errorf("expected %s content %q, got %q", name, "bin-"+name, string(data))
		}
	}
	if _, err := os.Stat(filepath.Join(wantDir, "focusguard-watchdog.exe")); !os.IsNotExist(err) {
		t.Error("absent binaries should not be copied")
	}
}

func TestInstallBinaries_SkipsSelfCopy(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	base := t.TempDir()
	src := filepath.Join(base, "FocusGuard")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "focusguard.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ProgramFiles", base)

	dir, err := InstallBinaries(src)
	if err != nil {
		t.Fatalf("InstallBinaries should tolerate src == dst, got: %v", err)
	}
	if dir != src {
		t.Errorf("expected install dir %q, got %q", src, dir)
	}
}

func TestCreateDesktopShortcut_CallsPowerShell(t *testing.T) {
	var captured []struct {
		name string
		args []string
	}

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, struct {
			name string
			args []string
		}{name: name, args: args})
		return &fakeCmd{fn: func() ([]byte, error) { return []byte("ok"), nil }}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	// Simula o daemon instalado ao lado do CLI, para o ícone ser extraído dele.
	origStat := osStat
	osStat = func(name string) (os.FileInfo, error) {
		if name == `C:\Program Files\FocusGuard\focusguard-daemon.exe` {
			return fakeFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	}
	defer func() { osStat = origStat }()

	public := t.TempDir()
	t.Setenv("PUBLIC", public)

	target := `C:\Program Files\FocusGuard\focusguard.exe`
	if err := CreateDesktopShortcut(target); err != nil {
		t.Fatalf("CreateDesktopShortcut returned error: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 powershell commands (extract icon + shortcut), got %d", len(captured))
	}
	if captured[0].name != "powershell" || captured[1].name != "powershell" {
		t.Errorf("expected powershell for both commands, got %q, %q", captured[0].name, captured[1].name)
	}

	// Primeira chamada: extração do ícone embutido do daemon.
	extractScript := strings.Join(captured[0].args, " ")
	for _, want := range []string{
		"ExtractAssociatedIcon",
		`C:\Program Files\FocusGuard\focusguard-daemon.exe`,
		`C:\Program Files\FocusGuard\focusguard.ico`,
	} {
		if !strings.Contains(extractScript, want) {
			t.Errorf("expected %q in extract script, got: %s", want, extractScript)
		}
	}

	// Segunda chamada: atalho com IconLocation no .ico extraído.
	script := strings.Join(captured[1].args, " ")
	for _, want := range []string{
		"CreateShortcut",
		"FocusGuard.lnk",
		target,
		filepath.Join(public, "Desktop"),
		`IconLocation = 'C:\Program Files\FocusGuard\focusguard.ico'`,
		".Save()",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("expected %q in powershell script, got: %s", want, script)
		}
	}
}

func TestExtractIcon_CallsPowerShell(t *testing.T) {
	var captured []struct {
		name string
		args []string
	}

	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		captured = append(captured, struct {
			name string
			args []string
		}{name: name, args: args})
		return &fakeCmd{fn: func() ([]byte, error) { return []byte("ok"), nil }}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	exePath := filepath.Join(t.TempDir(), "focusguard-daemon.exe")
	if err := os.WriteFile(exePath, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	icoPath := filepath.Join(t.TempDir(), "focusguard.ico")

	if err := ExtractIcon(exePath, icoPath); err != nil {
		t.Fatalf("ExtractIcon returned error: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 powershell command, got %d", len(captured))
	}
	if captured[0].name != "powershell" {
		t.Errorf("expected powershell, got %q", captured[0].name)
	}
	script := strings.Join(captured[0].args, " ")
	for _, want := range []string{"ExtractAssociatedIcon", exePath, icoPath, ".Save("} {
		if !strings.Contains(script, want) {
			t.Errorf("expected %q in powershell script, got: %s", want, script)
		}
	}
}

func TestExtractIcon_MissingExe(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	err := ExtractIcon(filepath.Join(t.TempDir(), "nope.exe"), filepath.Join(t.TempDir(), "focusguard.ico"))
	if err == nil || !strings.Contains(err.Error(), "não encontrado") {
		t.Fatalf("expected missing-exe error, got: %v", err)
	}
}

func TestExtractIcon_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	err := ExtractIcon("/usr/local/bin/focusguard-daemon", "/tmp/focusguard.ico")
	if err == nil || !strings.Contains(err.Error(), "exclusiva do Windows") {
		t.Fatalf("expected unsupported platform error on linux, got: %v", err)
	}
}

func TestExtractIcon_Error(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{fn: func() ([]byte, error) {
			return []byte("error extracting"), errors.New("exit status 1")
		}}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	exePath := filepath.Join(t.TempDir(), "focusguard-daemon.exe")
	if err := os.WriteFile(exePath, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ExtractIcon(exePath, filepath.Join(t.TempDir(), "focusguard.ico"))
	if err == nil || !strings.Contains(err.Error(), "falha ao extrair ícone") {
		t.Fatalf("expected extract failure message, got: %v", err)
	}
}

func TestCreateDesktopShortcut_UnsupportedPlatform(t *testing.T) {
	origGoos := goos
	goos = "linux"
	defer func() { goos = origGoos }()

	err := CreateDesktopShortcut("/usr/local/bin/focusguard")
	if err == nil || !strings.Contains(err.Error(), "exclusivo do Windows") {
		t.Fatalf("expected unsupported platform error on linux, got: %v", err)
	}
}

func TestCreateDesktopShortcut_Error(t *testing.T) {
	origCmd := execCommand
	execCommand = func(name string, args ...string) cmdRunner {
		return &fakeCmd{fn: func() ([]byte, error) {
			return []byte("0x80004005"), errors.New("exit status 1")
		}}
	}
	defer func() { execCommand = origCmd }()

	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()
	t.Setenv("PUBLIC", t.TempDir())

	err := CreateDesktopShortcut(`C:\Program Files\FocusGuard\focusguard.exe`)
	if err == nil {
		t.Fatal("expected error when powershell fails")
	}
	if !strings.Contains(err.Error(), "falha ao criar atalho") {
		t.Errorf("expected shortcut failure message, got: %v", err)
	}
}

func TestRemoveDesktopShortcut_RemovesFile(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	public := t.TempDir()
	t.Setenv("PUBLIC", public)

	lnk := filepath.Join(public, "Desktop", "FocusGuard.lnk")
	if err := os.MkdirAll(filepath.Dir(lnk), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lnk, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveDesktopShortcut(); err != nil {
		t.Fatalf("RemoveDesktopShortcut returned error: %v", err)
	}
	if _, err := os.Stat(lnk); !os.IsNotExist(err) {
		t.Error("shortcut should be removed")
	}
}

func TestRemoveDesktopShortcut_MissingIsIdempotent(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()
	t.Setenv("PUBLIC", t.TempDir())

	if err := RemoveDesktopShortcut(); err != nil {
		t.Fatalf("removing a missing shortcut should be idempotent, got: %v", err)
	}
}

func TestRemoveInstall_RemovesDirAndShortcut(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	base := t.TempDir()
	t.Setenv("ProgramFiles", base)
	t.Setenv("PUBLIC", base)

	installDir := filepath.Join(base, "FocusGuard")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "focusguard.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	lnk := filepath.Join(base, "Desktop", "FocusGuard.lnk")
	if err := os.MkdirAll(filepath.Dir(lnk), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lnk, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveInstall(); err != nil {
		t.Fatalf("RemoveInstall returned error: %v", err)
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("install dir should be removed")
	}
	if _, err := os.Stat(lnk); !os.IsNotExist(err) {
		t.Error("shortcut should be removed")
	}
}

func TestEnsureInInstallDir_CopiesWhenInstalled(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	base := t.TempDir()
	t.Setenv("ProgramFiles", base)
	installDir := filepath.Join(base, "FocusGuard")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "focusguard-tray.exe")
	if err := os.WriteFile(src, []byte("tray"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureInInstallDir(src)
	if err != nil {
		t.Fatalf("EnsureInInstallDir returned error: %v", err)
	}
	want := filepath.Join(installDir, "focusguard-tray.exe")
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected copied binary: %v", err)
	}
	if string(data) != "tray" {
		t.Errorf("expected content 'tray', got %q", string(data))
	}
}

// TestEnsureInInstallDir_AlreadyInstalledIsNoOp verifies the same-path guard:
// a binary that already lives in the install directory is NOT copied onto
// itself (a self-copy via os.Create would truncate the file) — the function
// just returns the canonical path.
func TestEnsureInInstallDir_AlreadyInstalledIsNoOp(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	base := t.TempDir()
	t.Setenv("ProgramFiles", base)
	installDir := filepath.Join(base, "FocusGuard")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	tray := filepath.Join(installDir, "focusguard-tray.exe")
	if err := os.WriteFile(tray, []byte("tray-installed"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureInInstallDir(tray)
	if err != nil {
		t.Fatalf("EnsureInInstallDir returned error: %v", err)
	}
	if got != tray {
		t.Errorf("expected the same canonical path %q, got %q", tray, got)
	}
	// O binário não pode ser corrompido por um self-copy (os.Create truncaria).
	data, err := os.ReadFile(tray)
	if err != nil {
		t.Fatalf("read tray: %v", err)
	}
	if string(data) != "tray-installed" {
		t.Errorf("tray content = %q, want original (no self-copy)", data)
	}
}

func TestEnsureInInstallDir_NotInstalledReturnsSrc(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	t.Setenv("ProgramFiles", t.TempDir()) // diretório de instalação não existe

	src := filepath.Join(t.TempDir(), "focusguard-watchdog.exe")
	if err := os.WriteFile(src, []byte("wd"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureInInstallDir(src)
	if err != nil {
		t.Fatalf("EnsureInInstallDir returned error: %v", err)
	}
	if got != src {
		t.Errorf("expected original path %q, got %q", src, got)
	}
}

func TestShortcutPath_UsesPublicDesktop(t *testing.T) {
	origGoos := goos
	goos = "windows"
	defer func() { goos = origGoos }()

	public := t.TempDir()
	t.Setenv("PUBLIC", public)
	want := filepath.Join(public, "Desktop", "FocusGuard.lnk")
	if got := ShortcutPath(); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
