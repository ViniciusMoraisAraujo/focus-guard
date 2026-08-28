//go:build !windows && !linux

package netdns

type noopConfigurator struct{}

func newPlatformConfigurator() Configurator {
	return &noopConfigurator{}
}

func (n *noopConfigurator) SetLocalDNS() ([]string, error) {
	return nil, nil
}

func (n *noopConfigurator) RestoreDNS() ([]string, error) {
	return nil, nil
}

func (n *noopConfigurator) Status() AdapterStatus {
	return AdapterStatus{
		Connected: false,
		Detail:    "configurador não suportado nesta plataforma",
	}
}
