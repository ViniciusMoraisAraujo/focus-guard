//go:build windows

package dnsserver

import (
	"strings"
	"testing"
)

func TestBindHint_WindowsMentionsICS(t *testing.T) {
	got := bindHint("0.0.0.0:53")
	if !strings.Contains(got, "SharedAccess") {
		t.Errorf("bindHint(53) = %q, esperava mencionar ICS/SharedAccess (causa comum no Windows)", got)
	}
}
