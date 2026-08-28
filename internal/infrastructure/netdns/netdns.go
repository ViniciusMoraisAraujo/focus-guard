package netdns

// AdapterStatus describes the current local DNS configuration status across
// network adapters (e.g. Wi-Fi and Ethernet).
type AdapterStatus struct {
	Connected          bool     `json:"connected"`
	ConfiguredAdapters []string `json:"configured_adapters,omitempty"`
	Detail             string   `json:"detail,omitempty"`
}

// Configurator defines the interface for configuring and restoring local DNS
// settings on system network adapters (Wi-Fi, Ethernet).
type Configurator interface {
	// SetLocalDNS configures active network adapters (Wi-Fi, Ethernet) to point
	// their DNS resolver to the local sinkhole (127.0.0.1 / ::1). Returns the
	// names of the configured adapters.
	SetLocalDNS() ([]string, error)

	// RestoreDNS restores the network adapters DNS configuration back to DHCP
	// (or automatic configuration). Returns the names of the restored adapters.
	RestoreDNS() ([]string, error)

	// Status reports the current state of adapter DNS configuration.
	Status() AdapterStatus
}

// NewConfigurator returns the platform-specific network DNS configurator.
func NewConfigurator() Configurator {
	return newPlatformConfigurator()
}
