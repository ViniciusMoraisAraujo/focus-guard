//go:build linux

package dnsserver

import (
	"strings"
	"testing"
)

func TestBindHint_LinuxMentionsSystemdResolved(t *testing.T) {
	got := bindHint("0.0.0.0:53")
	if !strings.Contains(got, "systemd-resolved") {
		t.Errorf("bindHint(53) = %q, esperava mencionar systemd-resolved (causa comum no Linux)", got)
	}
	if got := bindHint("127.0.0.1:5300"); got != "" {
		t.Errorf("bindHint(5300) = %q, esperava vazio", got)
	}
}
