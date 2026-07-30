package watchdog

import (
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

type mockHealthChecker struct {
	shouldFail bool
	callCount  int32
}

func (m *mockHealthChecker) Ping() error {
	atomic.AddInt32(&m.callCount, 1)
	if m.shouldFail {
		return os.ErrPermission
	}
	return nil
}

func getTestSocketPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/watchdog-test.sock"
}

func TestNew_NilWhenNoSocketEnv(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	w := New(&mockHealthChecker{}, 10)
	if w != nil {
		t.Error("expected nil when NOTIFY_SOCKET is not set")
	}
}

func TestNew_NilWhenZeroSec(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "/tmp/test.sock")
	w := New(&mockHealthChecker{}, 0)
	if w != nil {
		t.Error("expected nil when watchdogSec is 0")
	}
}

func TestNew_NilWhenNegativeSec(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "/tmp/test.sock")
	w := New(&mockHealthChecker{}, -5)
	if w != nil {
		t.Error("expected nil when watchdogSec is negative")
	}
}

func TestNew_ValidConfig(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "/tmp/focusguard-watchdog.sock")
	w := New(&mockHealthChecker{}, 10)
	if w == nil {
		t.Fatal("expected non-nil Watchdog for valid configuration")
	}

	if w.socketPath != "/tmp/focusguard-watchdog.sock" {
		t.Errorf("expected socketPath %q, got %q",
			"/tmp/focusguard-watchdog.sock", w.socketPath)
	}

	expectedInterval := 5 * time.Second // watchdogSec/2 = 10/2 = 5s
	if w.interval != expectedInterval {
		t.Errorf("expected interval %v, got %v", expectedInterval, w.interval)
	}

	if w.checker == nil {
		t.Error("expected checker to be passed through")
	}
}

func TestNew_IntervalCalculation(t *testing.T) {
	tests := []struct {
		name        string
		watchdogSec int
		want        time.Duration
	}{
		{
			name:        "10 seconds -> 5s interval",
			watchdogSec: 10,
			want:        5 * time.Second,
		},
		{
			name:        "1 second -> 0s interval (integer division 1/2 = 0)",
			watchdogSec: 1,
			want:        0,
		},
		{
			name:        "60 seconds -> 30s interval",
			watchdogSec: 60,
			want:        30 * time.Second,
		},
		{
			name:        "3 seconds -> 1s interval (integer division 3/2 = 1)",
			watchdogSec: 3,
			want:        1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NOTIFY_SOCKET", "/tmp/test.sock")
			w := New(&mockHealthChecker{}, tt.watchdogSec)
			if w == nil {
				t.Fatal("expected non-nil Watchdog")
			}
			if w.interval != tt.want {
				t.Errorf("New(…, %d).interval = %v, want %v",
					tt.watchdogSec, w.interval, tt.want)
			}
		})
	}
}

func TestSendNotification_Success(t *testing.T) {
	socketPath := getTestSocketPath(t)

	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Skipf("unixgram sockets not supported on this platform: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	w := &Watchdog{socketPath: socketPath}
	if err := w.sendNotification("READY=1"); err != nil {
		t.Fatalf("sendNotification failed: %v", err)
	}

	buf := make([]byte, 128)
	if err := listener.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := listener.Read(buf)
	if err != nil {
		t.Fatalf("failed to read from socket: %v", err)
	}

	if got := string(buf[:n]); got != "READY=1" {
		t.Errorf("sendNotification sent %q, want %q", got, "READY=1")
	}
}

func TestSendNotification_MultipleMessages(t *testing.T) {
	socketPath := getTestSocketPath(t)

	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Skipf("unixgram sockets not supported on this platform: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	w := &Watchdog{socketPath: socketPath}

	// Send multiple messages
	messages := []string{"MSG_A", "MSG_B", "MSG_C"}
	for _, msg := range messages {
		if err := w.sendNotification(msg); err != nil {
			t.Fatalf("sendNotification(%q) failed: %v", msg, err)
		}
	}

	buf := make([]byte, 128)
	for i, want := range messages {
		if err := listener.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			t.Fatal(err)
		}
		n, err := listener.Read(buf)
		if err != nil {
			t.Fatalf("failed to read message %d from socket: %v", i, err)
		}
		if got := string(buf[:n]); got != want {
			t.Errorf("message %d: got %q, want %q", i, got, want)
		}
	}
}

func TestSendNotification_NoListener(t *testing.T) {
	w := &Watchdog{socketPath: "/nonexistent/watchdog-no-listener.sock"}

	if err := w.sendNotification("PING"); err == nil {
		t.Error("expected error when no listener exists on the socket")
	}
}

func TestStart_NilReceiver(t *testing.T) {
	var w *Watchdog
	// Must not panic
	w.Start()
}

func TestStart_WithHealthCheckFailing(t *testing.T) {
	socketPath := getTestSocketPath(t)

	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Skipf("unixgram sockets not supported on this platform: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	checker := &mockHealthChecker{shouldFail: true}
	w := &Watchdog{
		socketPath: socketPath,
		interval:   20 * time.Millisecond,
		checker:    checker,
	}

	go w.Start()

	buf := make([]byte, 128)
	if err := listener.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := listener.Read(buf)
	if err != nil {
		t.Fatal("timed out waiting for READY=1")
	}
	if string(buf[:n]) != "READY=1" {
		t.Errorf("expected 'READY=1', got %q", string(buf[:n]))
	}

	if err := listener.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = listener.Read(buf)
	if err == nil {
		t.Error("expected timeout: WATCHDOG=1 should not be sent when Ping() returns error")
	}
}

func TestStart_WithHealthCheckPassing(t *testing.T) {
	socketPath := getTestSocketPath(t)

	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Skipf("unixgram sockets not supported on this platform: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	checker := &mockHealthChecker{}
	w := &Watchdog{
		socketPath: socketPath,
		interval:   20 * time.Millisecond,
		checker:    checker,
	}

	go w.Start()

	readMsg := func(deadline time.Duration) (string, error) {
		buf := make([]byte, 128)
		if err := listener.SetReadDeadline(time.Now().Add(deadline)); err != nil {
			return "", err
		}
		n, err := listener.Read(buf)
		if err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	}

	msg, err := readMsg(1 * time.Second)
	if err != nil {
		t.Fatal("timed out waiting for READY=1")
	}
	if msg != "READY=1" {
		t.Fatalf("expected 'READY=1', got %q", msg)
	}

	for i := 1; i <= 3; i++ {
		msg, err := readMsg(500 * time.Millisecond)
		if err != nil {
			t.Fatalf("timed out waiting for WATCHDOG=1 (iteration %d/3)", i)
		}
		if msg != "WATCHDOG=1" {
			t.Fatalf("expected 'WATCHDOG=1', got %q (iteration %d/3)", msg, i)
		}
	}

	if atomic.LoadInt32(&checker.callCount) < 3 {
		t.Errorf("expected Ping() to be called at least 3 times, got %d",
			atomic.LoadInt32(&checker.callCount))
	}
}

func TestStart_ReadinessBeforeTicker(t *testing.T) {
	socketPath := getTestSocketPath(t)

	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Skipf("unixgram sockets not supported on this platform: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	w := &Watchdog{
		socketPath: socketPath,
		interval:   1 * time.Hour, // extremely long interval
		checker:    &mockHealthChecker{},
	}

	go w.Start()

	buf := make([]byte, 128)
	if err := listener.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := listener.Read(buf)
	if err != nil {
		t.Fatal("READY=1 was not sent immediately before the ticker")
	}
	if string(buf[:n]) != "READY=1" {
		t.Errorf("expected 'READY=1', got %q", string(buf[:n]))
	}
}

func TestSendNotification_ErrorMessage(t *testing.T) {
	w := &Watchdog{socketPath: "/this/path/definitely/does/not/exist/42"}
	err := w.sendNotification("FAIL")
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if len(errMsg) == 0 {
		t.Error("expected non-empty error message")
	}
}

func TestNew_CheckerPersisted(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "/tmp/test.sock")
	checker := &mockHealthChecker{}
	w := New(checker, 10)
	if w == nil {
		t.Fatal("expected non-nil Watchdog")
	}
	if w.checker != checker {
		t.Error("expected checker pointer to be preserved")
	}
}
