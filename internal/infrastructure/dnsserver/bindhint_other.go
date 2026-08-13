//go:build !windows && !linux

package dnsserver

// platformBindHint returns an empty hint on platforms without a known common
// cause for a port-53 bind failure.
func platformBindHint() string {
	return ""
}
