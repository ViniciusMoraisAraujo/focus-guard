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

	"focusguard/internal/ipc"
	"focusguard/internal/policy"
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

type testIPCServer struct {
	t       *testing.T
	ln      net.Listener
	handler func(ipc.Request) ipc.Response
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
