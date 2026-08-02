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
	"focusguard/internal/schedule"
	"focusguard/internal/tamper"
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
	output := captureStdout(t, func() { handleUpdateCommand(client, nil) })

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
	output := captureStdout(t, func() { handleUpdateCommand(client, nil) })

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
		handleUpdateCommand(client, nil)
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

// ---------------------------------------------------------------------------
// Apps configuráveis no process guard
// ---------------------------------------------------------------------------

func TestHandleAppsCommand_Add(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Processo spotify adicionado à denylist"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleAppsCommand(client, []string{"add", "spotify.exe"}) })

	if gotReq.Action != "apps-add" {
		t.Errorf("expected action apps-add, got %q", gotReq.Action)
	}
	if gotReq.AppName != "spotify.exe" {
		t.Errorf("expected AppName spotify.exe, got %q", gotReq.AppName)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestHandleAppsCommand_AddWithoutName(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleAppsCommand(client, []string{"add"})
	})
	if !caught || code != 1 {
		t.Fatalf("expected exit 1 when app name is missing, caught=%v code=%d", caught, code)
	}
}

func TestHandleAppsCommand_Remove(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Processo steam removido da denylist"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleAppsCommand(client, []string{"remove", "steam"}) })

	if gotReq.Action != "apps-remove" {
		t.Errorf("expected action apps-remove, got %q", gotReq.Action)
	}
	if gotReq.AppName != "steam" {
		t.Errorf("expected AppName steam, got %q", gotReq.AppName)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestHandleAppsCommand_List(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		if req.Action != "apps-list" {
			t.Errorf("expected action apps-list, got %q", req.Action)
		}
		return ipc.Response{Success: true, Apps: []string{"steam", "discord", "spotify"}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleAppsCommand(client, nil) })

	for _, c := range []string{"steam", "discord", "spotify"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
	}
}

func TestPrintUsage_IncludesApps(t *testing.T) {
	output := captureStdout(t, printUsage)
	if !strings.Contains(output, "apps") {
		t.Errorf("usage should mention apps, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Relatório HTML & resumo semanal
// ---------------------------------------------------------------------------

func TestHandleStatsCommand_ExportHTML(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Stats: &analytics.Stats{
			TotalSessions: 1,
			TotalFocus:    time.Hour,
			PerDay:        []analytics.DayStat{{Day: time.Now().Format("2006-01-02"), Duration: time.Hour, Sessions: 1}},
			PerDomain:     []analytics.DomainStat{{Domain: "twitter.com", Duration: time.Hour}},
		}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handleStatsCommand(client, []string{"--export", "html"})
	})

	if !strings.Contains(output, "<!DOCTYPE html>") {
		t.Errorf("html export should render a full page, got: %.120s", output)
	}
}

func TestHandleReportCommand_PrintsWeeklySummary(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		if req.Action != "stats" {
			t.Errorf("expected action stats, got %q", req.Action)
		}
		return ipc.Response{Success: true, Stats: &analytics.Stats{
			TotalSessions: 3,
			TotalFocus:    3 * time.Hour,
			PerDay:        []analytics.DayStat{{Day: time.Now().Format("2006-01-02"), Duration: 3 * time.Hour, Sessions: 3}},
			PerDomain:     []analytics.DomainStat{{Domain: "twitter.com", Duration: 3 * time.Hour}},
			Streak:        2,
		}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleReportCommand(client) })

	for _, c := range []string{"Resumo semanal", "3", "twitter.com", "Raia"} {
		if !strings.Contains(output, c) {
			t.Errorf("report should contain %q, got: %s", c, output)
		}
	}
}

func TestHandleReportCommand_Failure(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: false, Message: "analytics não configurado"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleReportCommand(client)
	})
	if !caught || code != 1 {
		t.Fatalf("expected exit 1 on failure, caught=%v code=%d", caught, code)
	}
}

func TestPrintUsage_IncludesReport(t *testing.T) {
	output := captureStdout(t, printUsage)
	if !strings.Contains(output, "report") {
		t.Errorf("usage should mention report, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Tamper log (histórico de tentativas de burla)
// ---------------------------------------------------------------------------

func TestHandleTamperLogCommand_PrintsEvents(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		if req.Action != "tamper-log" {
			t.Errorf("expected action tamper-log, got %q", req.Action)
		}
		return ipc.Response{Success: true, TamperLog: []tamper.Event{
			{At: time.Now(), Source: "hosts", Action: "restore", Detail: "twitter.com"},
		}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleTamperLogCommand(client) })

	for _, c := range []string{"hosts", "restore", "twitter.com"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
	}
}

func TestHandleTamperLogCommand_Empty(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, TamperLog: nil}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleTamperLogCommand(client) })

	if !strings.Contains(output, "Nenhuma") {
		t.Errorf("empty tamper log should say none, got: %s", output)
	}
}

func TestHandleTamperLogCommand_Failure(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: false, Message: "tamper-log não configurado"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleTamperLogCommand(client)
	})
	if !caught || code != 1 {
		t.Fatalf("expected exit 1 on failure, caught=%v code=%d", caught, code)
	}
}

func TestPrintUsage_IncludesTamperLog(t *testing.T) {
	output := captureStdout(t, printUsage)
	if !strings.Contains(output, "tamper-log") {
		t.Errorf("usage should mention tamper-log, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Múltiplas janelas por agendamento (--windows)
// ---------------------------------------------------------------------------

func TestHandleScheduleAddCommand_WithWindows(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handleScheduleAddCommand(client, []string{
			"--preset", "social", "--days", "seg,ter,qua,qui,sex",
			"--windows", "08:00-12:00,14:00-18:00", "--label", "Manhã e tarde",
		})
	})

	r := gotReq.ScheduleRule
	if len(r.Windows) != 2 || r.Windows[0] != "08:00-12:00" || r.Windows[1] != "14:00-18:00" {
		t.Errorf("Windows inesperado: %+v", r.Windows)
	}
}

func TestHandleScheduleAddCommand_WindowsRequiresDays(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleScheduleAddCommand(client, []string{"--windows", "08:00-12:00"})
	})
	if !caught || code != 1 {
		t.Fatalf("expected exit 1 when days are missing, caught=%v code=%d", caught, code)
	}
}

func TestHandleScheduleListCommand_ShowsWindows(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Schedules: []schedule.Rule{
			{ID: "abc1", Preset: "social", Days: []int{1, 2, 3, 4, 5}, Windows: []string{"08:00-12:00", "14:00-18:00"}, Enabled: true},
		}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleScheduleCommand(client, nil) })

	for _, c := range []string{"08:00-12:00", "14:00-18:00"} {
		if !strings.Contains(output, c) {
			t.Errorf("list should show window %q, got: %s", c, output)
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
	// Flags não informadas → sentinelas: work=0 (usar salvo/default), rest=-1
	// (não informado — 0 é "sem descanso" legítimo), cycles=0.
	if gotReq.WorkMin != 0 {
		t.Errorf("WorkMin = %d, want 0 (sentinel, não informado)", gotReq.WorkMin)
	}
	if gotReq.RestMin != -1 {
		t.Errorf("RestMin = %d, want -1 (sentinel, não informado)", gotReq.RestMin)
	}
	if gotReq.Cycles != 0 {
		t.Errorf("Cycles = %d, want 0 (sentinel, não informado)", gotReq.Cycles)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestHandlePomodoroCommand_SaveFlag(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Pomodoro iniciado"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handlePomodoroCommand(client, []string{"--preset", "social", "--work", "50", "--rest", "10", "--cycles", "2", "--save"})
	})

	if !gotReq.Save {
		t.Error("expected Save=true when --save is passed")
	}
	if gotReq.WorkMin != 50 || gotReq.RestMin != 10 || gotReq.Cycles != 2 {
		t.Errorf("unexpected values: work=%d rest=%d cycles=%d", gotReq.WorkMin, gotReq.RestMin, gotReq.Cycles)
	}
}

func TestHandlePomodoroCommand_RestZeroIsKept(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handlePomodoroCommand(client, []string{"--preset", "social", "--rest", "0"})
	})

	if gotReq.RestMin != 0 {
		t.Errorf("RestMin = %d, want 0 (sem descanso explícito)", gotReq.RestMin)
	}
}

func TestHandlePomodoroDefaultsCommand(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Padrões atuais: 50m trabalho / 10m descanso / 2 ciclos",
			PomodoroWork: 50, PomodoroRest: 10, PomodoroCycle: 2}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handlePomodoroDefaultsCommand(client) })

	if gotReq.Action != "pomodoro-defaults" {
		t.Errorf("expected action pomodoro-defaults, got %q", gotReq.Action)
	}
	for _, c := range []string{"50", "10", "2"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
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
// Presets personalizados
// ---------------------------------------------------------------------------

func TestHandlePresetAddCommand(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Preset estudo criado"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handlePresetAddCommand(client, []string{"estudo", "khanacademy.org", "coursera.org"})
	})

	if gotReq.Action != "preset-add" {
		t.Errorf("expected action preset-add, got %q", gotReq.Action)
	}
	if gotReq.PresetName != "estudo" {
		t.Errorf("expected PresetName estudo, got %q", gotReq.PresetName)
	}
	if len(gotReq.PresetDomains) != 2 || gotReq.PresetDomains[0] != "khanacademy.org" {
		t.Errorf("unexpected PresetDomains: %v", gotReq.PresetDomains)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestHandlePresetAddCommand_MissingArgs(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handlePresetAddCommand(client, []string{"estudo"})
	})

	if !caught {
		t.Fatal("expected osExit when no domains are given")
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestHandlePresetRemoveCommand(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Preset estudo removido"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handlePresetRemoveCommand(client, []string{"estudo"})
	})

	if gotReq.Action != "preset-remove" {
		t.Errorf("expected action preset-remove, got %q", gotReq.Action)
	}
	if gotReq.PresetName != "estudo" {
		t.Errorf("expected PresetName estudo, got %q", gotReq.PresetName)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output should contain checkmark, got: %s", output)
	}
}

func TestPrintUsage_IncludesPresetCommands(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, c := range []string{"preset add", "preset remove"} {
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
	output := captureStdout(t, func() { handleStatsCommand(client, nil) })

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
		handleStatsCommand(client, nil)
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

func TestHandlePomodoroCommand_LabelFlag(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Pomodoro iniciado"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handlePomodoroCommand(client, []string{"--preset", "social", "--label", "Estudar ENEM"})
	})

	if gotReq.Label != "Estudar ENEM" {
		t.Errorf("Label = %q, want Estudar ENEM", gotReq.Label)
	}
}

func TestHandleStatsCommand_MissionFilter(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Stats: &analytics.Stats{
			TotalSessions: 1, TotalFocus: time.Hour,
			PerDay: []analytics.DayStat{{Day: time.Now().Format("2006-01-02"), Duration: time.Hour, Sessions: 1}},
		}}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handleStatsCommand(client, []string{"--mission", "ENEM"})
	})

	if gotReq.Action != "stats" || gotReq.Mission != "ENEM" {
		t.Errorf("unexpected request: %+v", gotReq)
	}
}

func TestHandleMissionCommand_PrintsLabels(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, LabelStats: []analytics.LabelStat{
			{Label: "ENEM", Duration: 3 * time.Hour, Sessions: 2},
		}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleMissionCommand(client) })

	if gotReq.Action != "missions" {
		t.Errorf("expected action missions, got %q", gotReq.Action)
	}
	for _, c := range []string{"ENEM", "3h"} {
		if !strings.Contains(output, c) {
			t.Errorf("output should contain %q, got: %s", c, output)
		}
	}
}

func TestHandleMissionCommand_Empty(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, LabelStats: nil}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleMissionCommand(client) })
	if !strings.Contains(output, "Nenhuma missão") {
		t.Errorf("empty missions should say none, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Update channels
// ---------------------------------------------------------------------------

func TestHandleUpdateCommand_ChannelBetaFlag(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, CurrentVersion: "1.0.0", UpdateVersion: "1.1.0-rc1", UpdateAvailable: true}
	})

	client := ipc.NewClient()
	captureStdout(t, func() { handleUpdateCommand(client, []string{"--channel", "beta"}) })

	if gotReq.Action != "update" {
		t.Errorf("expected action update, got %q", gotReq.Action)
	}
	if gotReq.Channel != "beta" {
		t.Errorf("expected Channel=beta, got %q", gotReq.Channel)
	}
}

func TestHandleUpdateCommand_DefaultChannelIsStable(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, CurrentVersion: "1.0.0"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() { handleUpdateCommand(client, nil) })

	if gotReq.Channel != "" {
		t.Errorf("expected empty channel by default, got %q", gotReq.Channel)
	}
}

func TestPrintUsage_IncludesChannelFlag(t *testing.T) {
	output := captureStdout(t, printUsage)
	if !strings.Contains(output, "--channel") {
		t.Error("usage should mention --channel")
	}
}

// ---------------------------------------------------------------------------
// Agendamento recorrente (schedule add/list/remove)
// ---------------------------------------------------------------------------

func TestHandleScheduleAdd_DefaultFlags(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Regra abc123 criada: social das 08:00 às 12:00"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handleScheduleAddCommand(client, []string{"--preset", "social", "--days", "mon,tue,wed", "--start", "08:00", "--end", "12:00"})
	})

	if gotReq.Action != "schedule-add" {
		t.Errorf("expected action schedule-add, got %q", gotReq.Action)
	}
	r := gotReq.ScheduleRule
	if r.Preset != "social" {
		t.Errorf("Preset = %q, want social", r.Preset)
	}
	if len(r.Days) != 3 || r.Days[0] != 1 || r.Days[1] != 2 || r.Days[2] != 3 {
		t.Errorf("Days = %v, want [1 2 3] (mon,tue,wed)", r.Days)
	}
	if r.Start != "08:00" || r.End != "12:00" {
		t.Errorf("Start/End = %s/%s, want 08:00/12:00", r.Start, r.End)
	}
	if !r.Enabled {
		t.Error("Enabled deveria ser true por padrão")
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output deveria conter checkmark, got: %s", output)
	}
}

func TestHandleScheduleAdd_EnglishAndPortugueseDays(t *testing.T) {
	cases := []struct {
		name string
		days string
		want []int
	}{
		{"ingles completo", "sun,mon,tue,wed,thu,fri,sat", []int{0, 1, 2, 3, 4, 5, 6}},
		{"portugues", "seg,ter,qua,qui,sex,sab,dom", []int{1, 2, 3, 4, 5, 6, 0}},
		{"misturado case-insensitive", "MON,SEG,Sat", []int{1, 1, 6}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq ipc.Request
			startTestIPCServer(t, func(req ipc.Request) ipc.Response {
				gotReq = req
				return ipc.Response{Success: true, Message: "OK"}
			})
			client := ipc.NewClient()
			captureStdout(t, func() {
				handleScheduleAddCommand(client, []string{"--preset", "social", "--days", tc.days, "--start", "08:00", "--end", "12:00"})
			})
			got := gotReq.ScheduleRule.Days
			if len(got) != len(tc.want) {
				t.Errorf("Days = %v, want %v", got, tc.want)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("Days = %v, want %v", got, tc.want)
					return
				}
			}
		})
	}
}

func TestHandleScheduleAdd_InvalidDay(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleScheduleAddCommand(client, []string{"--preset", "social", "--days", "xpto", "--start", "08:00", "--end", "12:00"})
	})
	if !caught {
		t.Fatal("expected osExit on invalid day")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestHandleScheduleAdd_MissingFlags(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleScheduleAddCommand(client, []string{"--preset", "social", "--start", "08:00", "--end", "12:00"})
	})
	if !caught {
		t.Fatal("expected osExit when days are missing")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestHandleScheduleList_PrintsRules(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		if req.Action != "schedule-list" {
			t.Errorf("expected action schedule-list, got %q", req.Action)
		}
		return ipc.Response{
			Success: true,
			Schedules: []schedule.Rule{
				{ID: "abc1", Preset: "social", Days: []int{1, 2, 3}, Start: "08:00", End: "12:00", Enabled: true},
			},
		}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleScheduleListCommand(client) })

	for _, c := range []string{"abc1", "social", "08:00", "12:00"} {
		if !strings.Contains(output, c) {
			t.Errorf("output deveria conter %q, got: %s", c, output)
		}
	}
}

func TestHandleScheduleRemove(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Regra abc1 removida"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleScheduleRemoveCommand(client, []string{"abc1"}) })

	if gotReq.Action != "schedule-remove" {
		t.Errorf("expected action schedule-remove, got %q", gotReq.Action)
	}
	if gotReq.ScheduleID != "abc1" {
		t.Errorf("ScheduleID = %q, want abc1", gotReq.ScheduleID)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output deveria conter checkmark, got: %s", output)
	}
}

func TestHandleScheduleImportCommand(t *testing.T) {
	icsPath := filepath.Join(t.TempDir(), "horario.ics")
	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:1
SUMMARY:Aula de matemática
DTSTART:20260202T080000
DTEND:20260202T100000
RRULE:FREQ=WEEKLY;BYDAY=MO,WE
END:VEVENT
END:VCALENDAR
`
	if err := os.WriteFile(icsPath, []byte(ics), 0600); err != nil {
		t.Fatalf("write ics: %v", err)
	}

	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "1 regras importadas do calendário (preset social)", Schedules: []schedule.Rule{
			{ID: "abc1", Preset: "social", Label: "Aula de matemática", Days: []int{1, 3}, Windows: []string{"08:00-10:00"}, Enabled: true},
		}}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handleScheduleImportCommand(client, []string{"--file", icsPath, "--preset", "social"})
	})

	if gotReq.Action != "schedule-import" {
		t.Errorf("expected action schedule-import, got %q", gotReq.Action)
	}
	if gotReq.ICSPreset != "social" {
		t.Errorf("ICSPreset = %q, want social", gotReq.ICSPreset)
	}
	if !strings.Contains(gotReq.ICSContent, "FREQ=WEEKLY") {
		t.Error("ICS content should be read from the file and sent raw")
	}
	if !strings.Contains(output, "importadas") {
		t.Errorf("output should mention the import, got: %s", output)
	}
}

func TestHandleScheduleImportCommand_MissingFile(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleScheduleImportCommand(client, []string{"--file", "nao-existe.ics", "--preset", "social"})
	})
	if !caught {
		t.Fatal("expected osExit when the .ics file is missing")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestHandleScheduleImportCommand_MissingPreset(t *testing.T) {
	icsPath := filepath.Join(t.TempDir(), "horario.ics")
	_ = os.WriteFile(icsPath, []byte("BEGIN:VCALENDAR"), 0600)

	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleScheduleImportCommand(client, []string{"--file", icsPath})
	})
	if !caught {
		t.Fatal("expected osExit when the preset is missing")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestHandleScheduleCommand_Dispatch(t *testing.T) {
	icsPath := filepath.Join(t.TempDir(), "horario.ics")
	_ = os.WriteFile(icsPath, []byte("BEGIN:VCALENDAR\n"), 0600)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"list", []string{"list"}, "schedule-list"},
		{"sem subcomando lista", nil, "schedule-list"},
		{"import", []string{"import", "--file", icsPath, "--preset", "social"}, "schedule-import"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq ipc.Request
			startTestIPCServer(t, func(req ipc.Request) ipc.Response {
				gotReq = req
				return ipc.Response{Success: true, Schedules: nil}
			})
			client := ipc.NewClient()
			captureStdout(t, func() { handleScheduleCommand(client, tc.args) })
			if gotReq.Action != tc.want {
				t.Errorf("expected action %q, got %q", tc.want, gotReq.Action)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Modo pânico / allowlist (block --internet / --allow)
// ---------------------------------------------------------------------------

func TestHandleBlockCommand_InternetPanicMode(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Internet bloqueada"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handleBlockCommand(client, []string{"--internet", "--duration", "30m"})
	})

	if gotReq.Action != "block-all" {
		t.Errorf("expected action block-all, got %q", gotReq.Action)
	}
	if gotReq.Duration != "30m" {
		t.Errorf("Duration = %q, want 30m", gotReq.Duration)
	}
	if len(gotReq.Allowlist) != 0 {
		t.Errorf("Allowlist deve ser vazia no modo pânico, got %v", gotReq.Allowlist)
	}
}

func TestHandleBlockCommand_InternetAllowlist(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "Internet bloqueada"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handleBlockCommand(client, []string{"--internet", "--allow", "docs.google.com,drive.google.com", "--duration", "1h"})
	})

	if gotReq.Action != "block-all" {
		t.Errorf("expected action block-all, got %q", gotReq.Action)
	}
	if len(gotReq.Allowlist) != 2 || gotReq.Allowlist[0] != "docs.google.com" || gotReq.Allowlist[1] != "drive.google.com" {
		t.Errorf("Allowlist = %v, want [docs.google.com drive.google.com]", gotReq.Allowlist)
	}
}

func TestHandleBlockCommand_InternetWithoutDuration(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleBlockCommand(client, []string{"--internet"})
	})
	if !caught {
		t.Fatal("expected osExit when --internet has no duration")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestHandleBlockCommand_InternetShortDurationFlag(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	captureStdout(t, func() {
		handleBlockCommand(client, []string{"--internet", "-d", "2h"})
	})
	if gotReq.Action != "block-all" || gotReq.Duration != "2h" {
		t.Errorf("unexpected request: %+v", gotReq)
	}
}

// ---------------------------------------------------------------------------
// Meta diária (goal) e export de stats (--export)
// ---------------------------------------------------------------------------

func TestHandleGoalCommand_Show(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Goal: 4 * time.Hour}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleGoalCommand(client, nil) })

	if gotReq.Action != "goal-get" {
		t.Errorf("expected action goal-get, got %q", gotReq.Action)
	}
	if !strings.Contains(output, "4h") {
		t.Errorf("output deveria mostrar a meta 4h, got: %s", output)
	}
}

func TestHandleGoalCommand_NoGoal(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Goal: 0}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleGoalCommand(client, nil) })
	if !strings.Contains(output, "nenhuma meta") && !strings.Contains(output, "meta") {
		t.Errorf("output deveria indicar que não há meta, got: %s", output)
	}
}

func TestHandleGoalCommand_Set(t *testing.T) {
	var gotReq ipc.Request
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		gotReq = req
		return ipc.Response{Success: true, Goal: 2 * time.Hour, Message: "Meta diária definida: 2h0m0s"}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() { handleGoalCommand(client, []string{"set", "2h"}) })

	if gotReq.Action != "goal-set" {
		t.Errorf("expected action goal-set, got %q", gotReq.Action)
	}
	if gotReq.GoalMinutes != 120 {
		t.Errorf("GoalMinutes = %d, want 120", gotReq.GoalMinutes)
	}
	if !strings.Contains(output, "✔") {
		t.Errorf("output deveria conter checkmark, got: %s", output)
	}
}

func TestHandleGoalCommand_InvalidDuration(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{Success: true, Message: "OK"}
	})

	client := ipc.NewClient()
	caught, code := runWithExitMock(func() {
		handleGoalCommand(client, []string{"set", "xpto"})
	})
	if !caught {
		t.Fatal("expected osExit on invalid duration")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestHandleStatsCommand_ExportCSV(t *testing.T) {
	now := time.Now()
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{
			Success: true,
			Stats: &analytics.Stats{
				TotalSessions: 1,
				TotalFocus:    time.Hour,
				PerDay: []analytics.DayStat{
					{Day: now.Format("2006-01-02"), Duration: time.Hour, Sessions: 1},
				},
				Streak: 1,
			},
		}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handleStatsCommand(client, []string{"--export", "csv"})
	})

	if !strings.Contains(output, "day,focus_minutes,sessions") {
		t.Errorf("export CSV deveria conter o cabeçalho, got: %s", output)
	}
	if strings.Contains(output, "█") {
		t.Errorf("export CSV não deveria conter gráfico ASCII, got: %s", output)
	}
}

func TestHandleStatsCommand_ExportJSON(t *testing.T) {
	startTestIPCServer(t, func(req ipc.Request) ipc.Response {
		return ipc.Response{
			Success: true,
			Stats:   &analytics.Stats{TotalSessions: 1, Streak: 1},
		}
	})

	client := ipc.NewClient()
	output := captureStdout(t, func() {
		handleStatsCommand(client, []string{"--export", "json"})
	})

	if !strings.Contains(output, "\"total_sessions\"") || !strings.Contains(output, "\"streak\"") {
		t.Errorf("export JSON deveria conter campos do Stats, got: %s", output)
	}
}

func TestPrintUsage_IncludesGoal(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, c := range []string{"goal", "--export"} {
		if !strings.Contains(output, c) {
			t.Errorf("usage deveria mencionar %q", c)
		}
	}
}

func TestPrintUsage_IncludesInternet(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, c := range []string{"--internet", "--allow"} {
		if !strings.Contains(output, c) {
			t.Errorf("usage deveria mencionar %q", c)
		}
	}
}

func TestPrintUsage_IncludesSchedule(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, c := range []string{"schedule add", "schedule list", "schedule remove"} {
		if !strings.Contains(output, c) {
			t.Errorf("usage deveria mencionar %q", c)
		}
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
