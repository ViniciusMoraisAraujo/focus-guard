//go:build windows

package ipc

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func checkAdmin(t *testing.T) {
	t.Helper()
	cmd := exec.Command("net", "session")
	if err := cmd.Run(); err != nil {
		t.Skip("Skipping test: Administrator privileges are required to manage IPC sockets in ProgramData")
	}
}

func TestListenAndDial(t *testing.T) {
	checkAdmin(t)

	defer func() {
		_ = os.Remove(SocketPath)
		_ = os.Remove(filepath.Dir(SocketPath))
	}()

	listener, err := Listen()
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	defer listener.Close()

	if _, err := os.Stat(SocketPath); err != nil {
		t.Fatalf("Failed to stat socket file: %v", err)
	}

	serverDone := make(chan error, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		buf := make([]byte, 128)
		n, err := conn.Read(buf)
		if err != nil && err != io.EOF {
			serverDone <- err
			return
		}

		response := append([]byte("ACK: "), buf[:n]...)
		if _, err := conn.Write(response); err != nil {
			serverDone <- err
			return
		}

		serverDone <- nil
	}()

	time.Sleep(10 * time.Millisecond)

	clientConn, err := Dial()
	if err != nil {
		t.Fatalf("Dial() failed: %v", err)
	}
	defer clientConn.Close()

	testMsg := []byte("PING")
	if _, err := clientConn.Write(testMsg); err != nil {
		t.Fatalf("Failed to write to socket from client: %v", err)
	}

	responseBuf := make([]byte, 128)
	n, err := clientConn.Read(responseBuf)
	if err != nil {
		t.Fatalf("Failed to read server response: %v", err)
	}

	expectedResp := []byte("ACK: PING")
	if !bytes.Equal(responseBuf[:n], expectedResp) {
		t.Errorf("Unexpected response: expected %q, got %q", expectedResp, responseBuf[:n])
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("Server goroutine error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for server goroutine response")
	}
}

func TestDial_NoServer(t *testing.T) {
	checkAdmin(t)

	_ = os.Remove(SocketPath)

	conn, err := Dial()
	if err == nil {
		conn.Close()
		t.Fatal("Expected Dial() to fail without an active server, but connection succeeded")
	}
}

func TestListen_CleanupExistingSocket(t *testing.T) {
	checkAdmin(t)

	defer func() {
		_ = os.Remove(SocketPath)
		_ = os.Remove(filepath.Dir(SocketPath))
	}()

	dir := filepath.Dir(SocketPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create parent directory for socket: %v", err)
	}

	if err := os.WriteFile(SocketPath, []byte("stale socket"), 0600); err != nil {
		t.Fatalf("Failed to create residual socket file: %v", err)
	}

	listener, err := Listen()
	if err != nil {
		t.Fatalf("Listen() failed to overwrite old residual socket: %v", err)
	}
	listener.Close()
}
