//go:build linux

package ipc

import (
	"net"
	"os"
	"os/user"
	"strconv"
	"time"
)

const SocketPath = "/run/focusguard.sock"

// socketGroupName é o grupo cujos membros usam o CLI/tray/web sem sudo (F5 do
// ui-plan): o daemon roda como root e criaria o socket root:root 0660, o que
// bloquearia o usuário comum. O install-linux.sh cria o grupo e adiciona o
// usuário; sem o grupo, o daemon segue com root:root 0660 (apenas root).
const socketGroupName = "focusguard"

// lookupSocketGroup resolve o GID do grupo do socket. Best-effort e stubbable
// nos testes (não depende de o grupo existir na máquina que roda os testes).
// Com CGO_ENABLED=0 (build do GoReleaser) o user.LookupGroup puro-Go lê
// apenas /etc/group — sem NSS/LDAP; o install-linux.sh cria o grupo localmente
// via groupadd, então funciona na prática.
var lookupSocketGroup = func() (int, bool) {
	g, err := user.LookupGroup(socketGroupName)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}

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
	// Acesso por grupo (F5): membros do grupo focusguard falam com o daemon
	// sem sudo. Best-effort — chown falhou (sem root/grupo), fica root:root.
	if gid, ok := lookupSocketGroup(); ok {
		_ = os.Chown(path, -1, gid)
	}
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
