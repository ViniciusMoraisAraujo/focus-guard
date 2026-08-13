//go:build windows

package dnsserver

// platformBindHint returns the most common Windows cause of a port-53 bind
// failure: Internet Connection Sharing (SharedAccess) and the DNS Client
// (dnscache) hold port 53 exclusively and must be stopped for the daemon to
// bind.
func platformBindHint() string {
	return " (porta 53 ocupada? desative o ICS: sc config SharedAccess start= disabled & net stop SharedAccess)"
}
