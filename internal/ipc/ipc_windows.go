//go:build windows

package ipc

import (
	"net"
	"os"
	"path/filepath"
)

var SocketPath = filepath.Join(os.Getenv("PROGRAMDATA"), "FocusGuard", "focusguard.sock")

func Listen() (net.Listener, error) {
	dir := filepath.Dir(SocketPath)
	_ = os.MkdirAll(dir, 0755)
	_ = os.Remove(SocketPath)

	return net.Listen("unix", SocketPath)
}

func Dial() (net.Conn, error) {
	return net.Dial("unix", SocketPath)
}
