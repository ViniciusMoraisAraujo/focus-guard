//go:build windows

package ipc

import (
	"fmt"
	"net"
)

const WindowsPort = 48901

func Listen() (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", WindowsPort)
	if TestDialAddr != "" {
		addr = TestDialAddr
	}
	return net.Listen("tcp", addr)
}

func Dial() (net.Conn, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", WindowsPort)
	if TestDialAddr != "" {
		addr = TestDialAddr
	}
	return net.Dial("tcp", addr)
}
