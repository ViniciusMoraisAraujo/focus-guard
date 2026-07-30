//go:build linux

package ipc

import (
	"net"
	"os"
)

const SocketPath = "/run/focusguard.sock"

func Listen() (net.Listener, error) {
	_ = os.Remove(SocketPath)
	l, err := net.Listen("unix", SocketPath)
	if err != nil {
		return nil, err
	}

	_ = os.Chmod(SocketPath, 0660)
	return l, nil
}

func Dial() (net.Conn, error) {
	return net.Dial("unix", SocketPath)
}
