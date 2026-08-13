//go:build linux

package dnsserver

// platformBindHint returns the most common Linux cause of a port-53 bind
// failure: systemd-resolved (e o dnsmasq de alguns roteadores/containers)
// segura a porta 53 exclusivamente e precisa ser liberado para o sinkhole
// bindar. O runner do `update-ca-certificates` não é o caso aqui — é o DNS
// local do sistema.
func platformBindHint() string {
	return " (porta 53 ocupada? no Linux o systemd-resolved costuma segurar a porta — libere com: sudo systemctl stop systemd-resolved; desative em definitivo com: sudo systemctl disable --now systemd-resolved)"
}
