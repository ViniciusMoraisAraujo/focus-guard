//go:build linux

package ipc

import (
	"net"
	"os"
	"time"
)

const SocketPath = "/run/focusguard.sock"

func Listen() (net.Listener, error) {
	path := SocketPath
	if TestSocketPath != "" {
		path = TestSocketPath
	}
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0660)
	return l, nil
}

func Dial() (net.Conn, error) {
	path := SocketPath
	if TestSocketPath != "" {
		path = TestSocketPath
	}
	return net.Dial("unix", path)
}

func DialTimeout(timeout time.Duration) (net.Conn, error) {
	path := SocketPath
	if TestSocketPath != "" {
		path = TestSocketPath
	}
	return net.DialTimeout("unix", path, timeout)
}
