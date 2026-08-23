package ipc

import (
	"net"
	"strings"
)

// lanInfo devolve (IPv4, MAC) da máquina na LAN: o IPv4 da rota default (a
// interface de saída) e o MAC da interface que o detém — exatamente os valores
// que entram na reserva DHCP do roteador (Guia: "IP e MAC desta máquina").
// Best-effort: ambos vazios sem rota ou sem interface correspondente. Var (não
// const) para o teste injetar uma resposta determinística.
var lanInfo = machineLAN

// machineLAN implementa a descoberta de IP+MAC por trás de lanInfo.
func machineLAN() (ip4, mac string) {
	// UDP dial não envia pacotes: só escolhe a rota default e devolve o IP
	// local de saída — funciona mesmo sem conectividade real.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", ""
	}
	defer conn.Close()
	ip4 = strings.Split(conn.LocalAddr().String(), ":")[0]
	if ip4 == "" {
		return "", ""
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ip4, ""
	}
	for _, iface := range ifaces {
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ip := addrIP(a)
			if ip != nil && ip.To4() != nil && ip.String() == ip4 {
				return ip4, iface.HardwareAddr.String()
			}
		}
	}
	return ip4, ""
}

// addrIP extrai o net.IP de um endereço de interface (IPNet ou IPAddr).
func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	}
	return nil
}
