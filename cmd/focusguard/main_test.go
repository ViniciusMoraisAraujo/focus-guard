package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/ipc"
	"focusguard/internal/policy"
	"focusguard/internal/preset"
)

type exitPanic struct {
	code int
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		io.Copy(&buf, r)
		wg.Done()
	}()

	fn()

	w.Close()
	os.Stdout = orig
	wg.Wait()
	return buf.String()
}

func captureStdoutAndStderr(t *testing.T, fn func()) (string, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	origOut := os.Stdout
	origErr := os.Stderr
	os.Stdout = wOut
	os.Stderr = wErr

	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { io.Copy(&outBuf, rOut); wg.Done() }()
	go func() { io.Copy(&errBuf, rErr); wg.Done() }()

	fn()

	wOut.Close()
	wErr.Close()
	os.Stdout = origOut
	os.Stderr = origErr
	wg.Wait()
	return outBuf.String(), errBuf.String()
}

func runWithExitMock(fn func()) (caught bool, exitCode int) {
	origExit := osExit
	osExit = func(code int) {
		panic(exitPanic{code})
	}
	defer func() { osExit = origExit }()

	defer func() {
		if r := recover(); r != nil {
			if ep, ok := r.(exitPanic); ok {
				caught = true
				exitCode = ep.code
			} else {
				panic(r)
			}
		}
	}()

	fn()
	return false, 0
}

func startTestIPCServer(t *testing.T, handler func(ipc.Request) ipc.Response) {
	t.Helper()

	var ln net.Listener
	var err error

	if runtime.GOOS == "windows" {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("test server listen: %v", err)
		}
		ipc.TestDialAddr = ln.Addr().String()
		t.Cleanup(func() { ipc.TestDialAddr = "" })
	} else {
		dir := t.TempDir()
		socketPath := filepath.Join(dir, "test.sock")
		ln, err = net.Listen("unix", socketPath)
		if err != nil {
			t.Fatalf("test server listen: %v", err)
		}
		ipc.TestSocketPath = socketPath
		t.Cleanup(func() { ipc.TestSocketPath = "" })
	}

	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var req ipc.Request
				if err := json.NewDecoder(c).Decode(&req); err != nil {
					return
				}
				json.NewEncoder(c).Encode(handler(req))
			}(conn)
		}
	}()
}

func TestPrintUsage(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, c := range []string{"FocusGuard", "block <dominio>", "status", "interactive", "TUI"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q", c)
		}
	}
}

func TestHandleBlockCommand_Success(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: fmt.Sprintf("Domain %s blocked", req.Domain)}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handleBlockCommand(client, []string{"twitter.com", "4h"})
	})

	if gotReq.Domain != "twitter.com" {
		t.Errorf("expected domain twitter.com, got %q", gotReq.Domain)
	}
	if gotReq.Duration != "4h" {
		t.Errorf("expected duration 4h, got %q", gotReq.Duration)
	}
	if gotReq.Action != "block" {
		t.Errorf("expected action block, got %q", gotReq.Action)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestHandleBlockCommand_PositionalDuration(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handleBlockCommand(client, []string{"twitter.com", "4h"})
	})

	if gotReq.Duration != "4h" {
		t.Errorf("expected duration 4h, got %q", gotReq.Duration)
	}
}

func TestHandleBlockCommand_FailureResponse(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: false, Message: "Domain already blocked"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleBlockCommand(client, []string{"twitter.com", "4h"})
	})

	if !caught {
		t.Fatal("expected osExit to be called")
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestHandleStatusCommand_Empty(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Blocks: []policy.Block{}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleStatusCommand(client) })

	if !strings.Contains(output, "Nenhum bloqueio ativo") {
		t.Errorf("expected empty message, got: %s", output)
	}
}

func TestPrintProtectionStatus_Active(t *testing.T) {
	resp := &ipc.Response{
		Success:       true,
		DoHActive:     true,
		ExpectedDoH:   true,
		FirewallRules: 6,
	}

	output := captureStdout(t, func() { printProtectionStatus(resp) })

	for _, c := range []string{"Proteção DoH/DoT", "ATIVA", "6"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
	}
}

func TestPrintProtectionStatus_InactiveWithActiveBlocks(t *testing.T) {
	resp := &ipc.Response{
		Success:       true,
		ExpectedDoH:   true,
		DoHActive:     false,
		FirewallRules: 0,
	}

	output := captureStdout(t, func() { printProtectionStatus(resp) })

	if !strings.Contains(output, "inativa") {
		t.Errorf("expected 'inativa' status, got: %s", output)
	}
	if !strings.Contains(output, "Atenção") {
		t.Errorf("expected warning when blocks active but rules missing, got: %s", output)
	}
}

func TestPrintProtectionStatus_Error(t *testing.T) {
	resp := &ipc.Response{
		Success:         true,
		ProtectionError: "netsh: permission denied",
	}

	output := captureStdout(t, func() { printProtectionStatus(resp) })

	if !strings.Contains(output, "Não foi possível consultar") {
		t.Errorf("expected consultation error message, got: %s", output)
	}
	if !strings.Contains(output, "netsh: permission denied") {
		t.Errorf("expected error detail in output, got: %s", output)
	}
}

func TestHandleStatusCommand_ProtectionError(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Blocks: []policy.Block{}, ProtectionError: "checkAdmin: permission denied"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleStatusCommand(client) })

	if !strings.Contains(output, "Não foi possível consultar") {
		t.Errorf("expected consultation error message, got: %s", output)
	}
}

func TestHandleStatusCommand_WithBlocks(t *testing.T) {
	now := time.Now()
	block := policy.Block{
		Domain:      "twitter.com",
		StartedAt:   now,
		ExpiresAt:   now.Add(4 * time.Hour),
		ResolvedIPs: []string{"104.244.42.1"},
	}

	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Blocks: []policy.Block{block}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleStatusCommand(client) })

	for _, c := range []string{"twitter.com", "DOMÍNIO", "Bloqueios Ativos"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q", c)
		}
	}
}

func TestHandleStatusCommand_FailureResponse(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: false, Message: "Daemon error"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleStatusCommand(client)
	})

	if !caught {
		t.Fatal("expected osExit to be called")
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestMain_HelpFlag(t *testing.T) {
	for name, args := range map[string][]string{
		"help":    {"focusguard", "help"},
		"short_h": {"focusguard", "-h"},
		"long_h":  {"focusguard", "--help"},
	} {
		t.Run(name, func(t *testing.T) {
			origArgs := os.Args
			os.Args = args
			defer func() { os.Args = origArgs }()

			output := captureStdout(t, main)
			if !strings.Contains(output, "FocusGuard") {
				t.Errorf("expected FocusGuard in output, got: %s", output)
			}
		})
	}
}

func TestMain_UnknownCommand(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"focusguard", "invalid-command"}
	defer func() { os.Args = origArgs }()

	output := captureStdout(t, func() {
		caught, _ := runWithExitMock(main)
		if !caught {
			t.Fatal("expected osExit for unknown command")
		}
	})

	if !strings.Contains(output, "Comando desconhecido") {
		t.Errorf("expected unknown command message, got: %s", output)
	}
}

func TestMain_StatusWithServer(t *testing.T) {
	block := policy.Block{
		Domain:      "youtube.com",
		StartedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		ResolvedIPs: []string{"1.2.3.4"},
	}

	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Blocks: []policy.Block{block}}
	})

	origArgs := os.Args
	os.Args = []string{"focusguard", "status"}
	defer func() { os.Args = origArgs }()

	output := captureStdout(t, main)
	if !strings.Contains(output, "youtube.com") {
		t.Errorf("output should contain domain, got: %s", output)
	}
}

func TestMain_BlockWithServer(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	origArgs := os.Args
	os.Args = []string{"focusguard", "block", "test.com", "-d=1h"}
	defer func() { os.Args = origArgs }()

	output := captureStdout(t, main)
	if !strings.Contains(output, "OK") {
		t.Errorf("output should contain OK, got: %s", output)
	}
}

func TestHandleUpdateCommand_Applied(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		if req.Action != "update" {
			t.Errorf("expected action update, got %q", req.Action)
		}
		return ipc.Response{
			Success:         true,
			UpdateAvailable: true,
			UpdateVersion:   "1.1.0",
			CurrentVersion:  "1.0.0",
		}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleUpdateCommand(client) })

	for _, c := range []string{"✔", "1.1.0", "próxima reinicialização"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
	}
}

func TestHandleUpdateCommand_UpToDate(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, CurrentVersion: "1.0.0"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleUpdateCommand(client) })

	if !strings.Contains(output, "versão mais recente") {
		t.Errorf("expected up-to-date message, got: %s", output)
	}
	if !strings.Contains(output, "1.0.0") {
		t.Errorf("expected current version in output, got: %s", output)
	}
}

func TestHandleUpdateCommand_FailureResponse(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: false, Message: "auto-update não configurado"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleUpdateCommand(client)
	})

	if !caught {
		t.Fatal("expected osExit to be called")
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestHandleStatusCommand_ShowsUpdateHint(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{
			Success:         true,
			Blocks:          []policy.Block{},
			UpdateAvailable: true,
			UpdateVersion:   "1.1.0",
			CurrentVersion:  "1.0.0",
		}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleStatusCommand(client) })

	if !strings.Contains(output, "Nova versão disponível") {
		t.Errorf("expected update hint in status, got: %s", output)
	}
	if !strings.Contains(output, "1.1.0") {
		t.Errorf("expected new version in status, got: %s", output)
	}
}

func TestMain_UpdateCommand(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, CurrentVersion: "1.0.0"}
	})

	origArgs := os.Args
	os.Args = []string{"focusguard", "update"}
	defer func() { os.Args = origArgs }()

	output := captureStdout(t, main)
	if !strings.Contains(output, "versão mais recente") {
		t.Errorf("output should contain up-to-date message, got: %s", output)
	}
}

func TestPrintUsage_IncludesUpdate(t *testing.T) {
	output := captureStdout(t, printUsage)
	if !strings.Contains(output, "focusguard update") {
		t.Errorf("usage should mention focusguard update, got: %s", output)
	}
}

func TestRunInteractive_MockedTUI(t *testing.T) {
	origRunTUI := runTUI
	runTUI = func(client *ipc.Client) error {
		return fmt.Errorf("simulated terminal error")
	}
	defer func() { runTUI = origRunTUI }()

	_, stderr := captureStdoutAndStderr(t, func() {
		caught, code := runWithExitMock(func() {
			runInteractive()
		})
		if !caught {
			t.Fatal("expected osExit when TUI errors")
		}
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
	})

	if !strings.Contains(stderr, "Erro no modo interativo") {
		t.Errorf("stderr should mention interactive error, got: %s", stderr)
	}
	if !strings.Contains(stderr, "simulated terminal error") {
		t.Errorf("stderr should contain simulated error, got: %s", stderr)
	}
}

// ---------------------------------------------------------------------------
// Presets & Pomodoro
// ---------------------------------------------------------------------------

func TestHandlePresetsCommand_ListsPresets(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		if req.Action != "presets" {
			t.Errorf("expected action presets, got %q", req.Action)
		}
		return ipc.Response{
			Success: true,
			Presets: []preset.Preset{{Name: "social", Label: "Redes sociais", Domains: []string{"twitter.com"}}},
		}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handlePresetsCommand(client) })

	for _, c := range []string{"social", "Redes sociais", "twitter.com"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
	}
}

func TestHandlePomodoroCommand_DefaultFlags(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Pomodoro iniciado"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handlePomodoroCommand(client, []string{"--preset", "social"})
	})

	if gotReq.Action != "pomodoro" {
		t.Errorf("expected action pomodoro, got %q", gotReq.Action)
	}
	if gotReq.Preset != "social" {
		t.Errorf("expected preset social, got %q", gotReq.Preset)
	}
	if gotReq.WorkMin != 25 {
		t.Errorf("default WorkMin = %d, want 25", gotReq.WorkMin)
	}
	if gotReq.RestMin != 5 {
		t.Errorf("default RestMin = %d, want 5", gotReq.RestMin)
	}
	if gotReq.Cycles != 4 {
		t.Errorf("default Cycles = %d, want 4", gotReq.Cycles)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestHandlePomodoroCommand_CustomFlags(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handlePomodoroCommand(client, []string{"--preset", "video", "--work", "50", "--rest", "10", "--cycles", "2"})
	})

	if gotReq.Preset != "video" || gotReq.WorkMin != 50 || gotReq.RestMin != 10 || gotReq.Cycles != 2 {
		t.Errorf("unexpected request: %+v", gotReq)
	}
}

func TestHandlePomodoroCommand_MissingPreset(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handlePomodoroCommand(client, []string{"--work", "25"})
	})

	if !caught {
		t.Fatal("expected osExit when preset is missing")
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestHandlePomodoroStopCommand(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Pomodoro encerrado"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handlePomodoroStopCommand(client) })

	if gotReq.Action != "pomodoro-stop" {
		t.Errorf("expected action pomodoro-stop, got %q", gotReq.Action)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestHandleBlockCommand_WithPreset(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Preset social bloqueado"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handleBlockCommand(client, []string{"--preset", "social", "--duration", "2h"})
	})

	if gotReq.Action != "block" {
		t.Errorf("expected action block, got %q", gotReq.Action)
	}
	if gotReq.Preset != "social" {
		t.Errorf("expected preset social, got %q", gotReq.Preset)
	}
	if gotReq.Duration != "2h" {
		t.Errorf("expected duration 2h, got %q", gotReq.Duration)
	}
}

func TestPrintUsage_IncludesPomodoro(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, c := range []string{"pomodoro", "presets", "pomodoro-stop"} {
		if !strings.Contains(output, c) {
			t.Errorf("usage should mention %q", c)
		}
	}
}

// ---------------------------------------------------------------------------
// Strict Mode & Analytics
// ---------------------------------------------------------------------------

func TestHandleStatsCommand_PrintsChart(t *testing.T) {
	now := time.Now()
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		if req.Action != "stats" {
			t.Errorf("expected action stats, got %q", req.Action)
		}
		return ipc.Response{
			Success: true,
			Stats: &analytics.Stats{
				TotalSessions: 1,
				TotalFocus:    time.Hour,
				PerDay: []analytics.DayStat{
					{Day: now.Format("2006-01-02"), Duration: time.Hour, Sessions: 1},
				},
				PerDomain: []analytics.DomainStat{{Domain: "twitter.com", Duration: time.Hour}},
			},
		}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleStatsCommand(client) })

	for _, c := range []string{"FocusGuard", "1", "twitter.com", "█"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
	}
}

func TestHandleStatsCommand_FailureResponse(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: false, Message: "analytics não configurado"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleStatsCommand(client)
	})

	if !caught {
		t.Fatal("expected osExit to be called")
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestMain_StatsCommand(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Stats: &analytics.Stats{TotalSessions: 0}}
	})

	origArgs := os.Args
	os.Args = []string{"focusguard", "stats"}
	defer func() { os.Args = origArgs }()

	output := captureStdout(t, main)
	if !strings.Contains(output, "FocusGuard") {
		t.Errorf("output should contain FocusGuard chart header, got: %s", output)
	}
}

func TestHandlePomodoroCommand_StrictFlag(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Pomodoro estrito iniciado"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handlePomodoroCommand(client, []string{"--preset", "social", "--strict"})
	})

	if !gotReq.Strict {
		t.Error("expected Strict=true when --strict is passed")
	}
}

func TestHandlePomodoroCommand_NoStrictByDefault(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handlePomodoroCommand(client, []string{"--preset", "social"})
	})

	if gotReq.Strict {
		t.Error("expected Strict=false by default")
	}
}

func TestPrintUsage_IncludesStats(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, c := range []string{"stats", "--strict"} {
		if !strings.Contains(output, c) {
			t.Errorf("usage should mention %q", c)
		}
	}
}

func TestRunInteractive_MockedTUISuccess(t *testing.T) {
	origRunTUI := runTUI
	runTUI = func(client *ipc.Client) error {
		return nil
	}
	defer func() { runTUI = origRunTUI }()

	output := captureStdout(t, func() {
		runInteractive()
	})

	if output != "" {
		t.Errorf("expected no output on success, got: %s", output)
	}
}
