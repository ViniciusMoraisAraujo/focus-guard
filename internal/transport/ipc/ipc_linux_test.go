//go:build linux

package ipc

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func setTestEndpoint(t *testing.T) {
	t.Helper()

	orig := TestSocketPath
	TestSocketPath = filepath.Join(t.TempDir(), "focusguard-test.sock")
	t.Cleanup(func() { TestSocketPath = orig })
}

// newTestListener binds a unix socket at a temp path and points the package
// dial endpoint at it, so client tests exercise the real Listen/Dial path
// without needing root access to /run.
func newTestListener(t *testing.T) net.Listener {
	t.Helper()
	setTestEndpoint(t)

	ln, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func TestListenAndDial(t *testing.T) {
	setTestEndpoint(t)

	defer os.Remove(TestSocketPath)

	listener, err := Listen()
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	defer listener.Close()

	info, err := os.Stat(TestSocketPath)
	if err != nil {
		t.Fatalf("Failed to stat socket file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0660 {
		t.Errorf("Expected socket permissions 0660, got %o", mode)
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
	setTestEndpoint(t)

	_ = os.Remove(TestSocketPath)

	conn, err := Dial()
	if err == nil {
		conn.Close()
		t.Fatal("Expected Dial() to fail without an active server, but connection succeeded")
	}
}

func TestListen_CleanupExistingSocket(t *testing.T) {
	setTestEndpoint(t)

	defer os.Remove(TestSocketPath)

	if err := os.WriteFile(TestSocketPath, []byte("stale socket"), 0600); err != nil {
		t.Fatalf("Failed to create residual socket file: %v", err)
	}

	listener, err := Listen()
	if err != nil {
		t.Fatalf("Listen() failed to overwrite old residual socket: %v", err)
	}
	listener.Close()
}

// TestListen_ChownsSocketToFocusGuardGroup verifica o F5 (acesso ao socket por
// grupo): com o grupo resolvido, o Listen chowna o socket para o GID dele.
// Só roda como root (o chown exige root); sem root o chown é ignorado por
// design (best-effort) e não há o que verificar.
func TestListen_ChownsSocketToFocusGuardGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requer root para verificar o chown do socket")
	}
	setTestEndpoint(t)
	defer os.Remove(TestSocketPath)

	orig := lookupSocketGroup
	t.Cleanup(func() { lookupSocketGroup = orig })
	// GID diferente do GID padrão do processo: devolver o próprio egid seria
	// tautológico (o socket já nasce com ele) e o chown viraria no-op. Como o
	// teste só roda como root, um GID arbitrário não-zero funciona (o kernel
	// não exige que o GID exista no /etc/group para o chown).
	targetGid := 0
	if os.Getegid() == 0 {
		targetGid = 1
	}
	lookupSocketGroup = func() (int, bool) { return targetGid, true }

	listener, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	info, err := os.Stat(TestSocketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Gid) != targetGid {
		t.Errorf("socket gid = %d, want %d (grupo focusguard)", st.Gid, targetGid)
	}
}
