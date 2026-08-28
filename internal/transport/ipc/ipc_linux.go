//go:build linux

package ipc

import (
	"net"
	"os"
	"os/user"
	"strconv"
	"time"
)

const (
	SocketPath       = "/run/focusguard/focusguard.sock"
	LegacySocketPath = "/run/focusguard.sock"
)

// socketGroupName é o grupo cujos membros usam o CLI/tray/web sem sudo (F5 do
// ui-plan): o daemon roda sob o usuário/grupo focusguard e cria o socket com
// permissão 0660 no RuntimeDirectory (/run/focusguard).
const socketGroupName = "focusguard"

// lookupSocketGroup resolve o GID do grupo do socket. Best-effort e stubbable
// nos testes (não depende de o grupo existir na máquina que roda os testes).
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
	} else {
		// Garante que o diretório pai existe (caso não esteja sob systemd RuntimeDirectory)
		dir := "/run/focusguard"
		if err := os.MkdirAll(dir, 0775); err != nil {
			// Fallback para o caminho raiz se o subdiretório não puder ser criado
			path = LegacySocketPath
		}
	}

	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil && path != LegacySocketPath && TestSocketPath == "" {
		// Fallback para caminho legado se falhar no subdiretório
		path = LegacySocketPath
		_ = os.Remove(path)
		l, err = net.Listen("unix", path)
	}
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0660)

	// Se criamos em /run/focusguard/focusguard.sock, tentamos criar um symlink em /run/focusguard.sock (best-effort)
	if path == SocketPath && TestSocketPath == "" {
		_ = os.Remove(LegacySocketPath)
		_ = os.Symlink(SocketPath, LegacySocketPath)
	}

	// Acesso por grupo: membros do grupo focusguard falam com o daemon sem sudo.
	if gid, ok := lookupSocketGroup(); ok {
		_ = os.Chown(path, -1, gid)
	}
	return l, nil
}

func Dial() (net.Conn, error) {
	if TestSocketPath != "" {
		return net.Dial("unix", TestSocketPath)
	}
	conn, err := net.Dial("unix", SocketPath)
	if err == nil {
		return conn, nil
	}
	// Fallback para o socket legado se o novo não responder
	return net.Dial("unix", LegacySocketPath)
}

func DialTimeout(timeout time.Duration) (net.Conn, error) {
	if TestSocketPath != "" {
		return net.DialTimeout("unix", TestSocketPath, timeout)
	}
	conn, err := net.DialTimeout("unix", SocketPath, timeout)
	if err == nil {
		return conn, nil
	}
	// Fallback para o socket legado se o novo não responder
	return net.DialTimeout("unix", LegacySocketPath, timeout)
}
