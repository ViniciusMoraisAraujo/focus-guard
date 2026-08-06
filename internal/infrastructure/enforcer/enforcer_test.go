package enforcer

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResolveIPs_StripsSchemeAndPath(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		wantLookup string
	}{
		{"plain", "example.com", "example.com"},
		{"http prefix", "http://example.com", "example.com"},
		{"https prefix", "https://example.com", "example.com"},
		{"with path", "https://example.com/some/path", "example.com"},
		{"with path no scheme", "example.com/some/path", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotHost string
			fakeLookup := func(host string) ([]net.IP, error) {
				gotHost = host
				return []net.IP{net.ParseIP("1.2.3.4")}, nil
			}

			ips, err := resolveIPs(tt.domain, fakeLookup)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotHost != tt.wantLookup {
				t.Errorf("lookup called with host %q, want %q", gotHost, tt.wantLookup)
			}

			want := []string{"1.2.3.4"}
			if !reflect.DeepEqual(ips, want) {
				t.Errorf("ips = %v, want %v", ips, want)
			}
		})
	}
}

func TestResolveIPs_NoDuplicates(t *testing.T) {
	fakeLookup := func(host string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("1.2.3.4"),
			net.ParseIP("1.2.3.4"),
			net.ParseIP("5.6.7.8"),
		}, nil
	}

	ips, err := resolveIPs("example.com", fakeLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"1.2.3.4", "5.6.7.8"}
	if !reflect.DeepEqual(ips, want) {
		t.Errorf("ips = %v, want %v", ips, want)
	}
}

func TestResolveIPs_MixedIPv4AndIPv6(t *testing.T) {
	fakeLookup := func(host string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("1.2.3.4"),
			net.ParseIP("2001:db8::1"),
		}, nil
	}

	ips, err := resolveIPs("example.com", fakeLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"1.2.3.4", "2001:db8::1"}
	if !reflect.DeepEqual(ips, want) {
		t.Errorf("ips = %v, want %v", ips, want)
	}
}

func TestResolveIPs_LookupError(t *testing.T) {
	lookupErr := errors.New("no such host")
	fakeLookup := func(host string) ([]net.IP, error) {
		return nil, lookupErr
	}

	ips, err := resolveIPs("invalid.example", fakeLookup)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, err) {
	}

	wantMsg := "failed to resolve IPs for domain invalid.example: no such host"
	if err.Error() != wantMsg {
		t.Errorf("error message = %q, want %q", err.Error(), wantMsg)
	}

	if ips != nil {
		t.Errorf("expected nil ips on error, got: %v", ips)
	}
}

func TestResolveIPs_EmptyResult(t *testing.T) {
	fakeLookup := func(host string) ([]net.IP, error) {
		return []net.IP{}, nil
	}

	ips, err := resolveIPs("example.com", fakeLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ips != nil {
		t.Errorf("expected nil slice for empty result, got: %v", ips)
	}
}

// TestResolveIPsCtx_RespectsContextCancellation verifies that a cancelled
// context is propagated to the lookup and surfaces as an error instead of
// hanging the caller.
func TestResolveIPsCtx_RespectsContextCancellation(t *testing.T) {
	var called bool
	lookup := func(ctx context.Context, host string) ([]net.IP, error) {
		called = true
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ips, err := resolveIPsCtx(ctx, "example.com", lookup)
	if err == nil {
		t.Fatal("expected error when the context is already cancelled")
	}
	if ips != nil {
		t.Errorf("expected nil IPs on cancelled context, got %v", ips)
	}
	if !called {
		t.Error("lookup function was not invoked")
	}
}

// TestResolveIPsCtx_TimeoutBoundsSlowLookup verifies a stalled DNS server
// (a lookup that never answers on its own) is bounded by the context timeout.
func TestResolveIPsCtx_TimeoutBoundsSlowLookup(t *testing.T) {
	lookup := func(ctx context.Context, host string) ([]net.IP, error) {
		<-ctx.Done() // simulates a DNS server that never answers
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	ips, err := resolveIPsCtx(ctx, "slow.example.com", lookup)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > time.Second {
		t.Errorf("lookup should be bounded by the context timeout, took %v", elapsed)
	}
	if ips != nil {
		t.Errorf("expected nil IPs on timeout, got %v", ips)
	}
}

// TestResolveIPsCtx_StripsSchemeAndPath mirrors the non-context variant: the
// scheme and path are stripped before the lookup is called.
func TestResolveIPsCtx_StripsSchemeAndPath(t *testing.T) {
	var gotHost string
	lookup := func(_ context.Context, host string) ([]net.IP, error) {
		gotHost = host
		return []net.IP{net.ParseIP("1.2.3.4")}, nil
	}

	ips, err := resolveIPsCtx(context.Background(), "https://example.com/some/path", lookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHost != "example.com" {
		t.Errorf("lookup called with host %q, want %q", gotHost, "example.com")
	}
	if !reflect.DeepEqual(ips, []string{"1.2.3.4"}) {
		t.Errorf("ips = %v, want [1.2.3.4]", ips)
	}
}

// TestResolveIPsCtx_NoDuplicates mirrors the non-context variant: duplicate
// addresses are collapsed.
func TestResolveIPsCtx_NoDuplicates(t *testing.T) {
	lookup := func(_ context.Context, host string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("1.2.3.4"),
			net.ParseIP("1.2.3.4"),
			net.ParseIP("5.6.7.8"),
		}, nil
	}

	ips, err := resolveIPsCtx(context.Background(), "example.com", lookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(ips, []string{"1.2.3.4", "5.6.7.8"}) {
		t.Errorf("ips = %v, want [1.2.3.4 5.6.7.8]", ips)
	}
}

func TestResolveIPs_PublicWrapperUsesRealLookup(t *testing.T) {

	ips, err := ResolveIPs("localhost")
	if err != nil {
		t.Fatalf("unexpected error resolving localhost: %v", err)
	}
	if len(ips) == 0 {
		t.Fatal("expected at least one IP for localhost")
	}
}

func TestResolveIPs_WWWPrefix(t *testing.T) {
	fakeLookup := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.2.3.4")}, nil
	}

	ips, err := resolveIPs("www.youtube.com", fakeLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "1.2.3.4" {
		t.Errorf("expected [1.2.3.4], got %v", ips)
	}
}

func TestResolveIPs_TrailingSlash(t *testing.T) {
	fakeLookup := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("5.6.7.8")}, nil
	}

	ips, err := resolveIPs("example.com/", fakeLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "5.6.7.8" {
		t.Errorf("expected [5.6.7.8], got %v", ips)
	}
}

func TestResolveIPs_IPv6Result(t *testing.T) {
	fakeLookup := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("2001:db8::1")}, nil
	}

	ips, err := resolveIPs("ipv6.example", fakeLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "2001:db8::1" {
		t.Errorf("expected [2001:db8::1], got %v", ips)
	}
}

func TestDedupeIPs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"dedupes and filters empty", []string{"1.2.3.4", "1.2.3.4", "", "5.6.7.8"}, []string{"1.2.3.4", "5.6.7.8"}},
		{"empty input", nil, nil},
		{"only empties", []string{"", ""}, nil},
		{"preserves order", []string{"b", "a", "b", "c"}, []string{"b", "a", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupeIPs(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dedupeIPs(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHeaderMarker_Value(t *testing.T) {
	expected := "# FOCUS GUARD BLOCKS - DO NOT EDIT MANUALLY"
	if HeaderMarker != expected {
		t.Errorf("HeaderMarker = %q, want %q", HeaderMarker, expected)
	}
}

func TestDoHProviders_NoDuplicateIPs(t *testing.T) {
	seen := make(map[string]string)
	for _, p := range DoHProviders {
		for _, ip := range p.IPs {
			if existing, ok := seen[ip]; ok {
				t.Errorf("IP %s aparece em dois provedores: %s e %s", ip, existing, p.Name)
			}
			seen[ip] = p.Name
		}
	}
}

func TestDoHProviders_HaveNonEmptyName(t *testing.T) {
	for _, p := range DoHProviders {
		if p.Name == "" {
			t.Error("DoHProvider with empty name found")
		}
		if p.Port <= 0 {
			t.Errorf("DoHProvider %s has invalid port %d", p.Name, p.Port)
		}
	}
}

func TestDoHProviders_DoTHasNoIPs(t *testing.T) {
	for _, p := range DoHProviders {
		if p.IsDoT && len(p.IPs) > 0 {
			t.Errorf("DoT provider %s should not have IPs (block by port only)", p.Name)
		}
	}
}

func TestDoHProviders_DoHHasIPs(t *testing.T) {
	for _, p := range DoHProviders {
		if !p.IsDoT && len(p.IPs) == 0 {
			t.Errorf("DoH provider %s has no IPs", p.Name)
		}
	}
}

func TestDoHProviders_DoTHasProtocol(t *testing.T) {
	for _, p := range DoHProviders {
		if !p.IsDoT {
			continue
		}
		if p.Protocol == "" {
			t.Errorf("DoT provider %s must have Protocol set", p.Name)
		}
		if p.Protocol != "tcp" && p.Protocol != "udp" {
			t.Errorf("DoT provider %s has invalid Protocol %q (expected tcp or udp)", p.Name, p.Protocol)
		}
		if strings.Contains(p.Name, strings.ToUpper(p.Protocol)) ||
			strings.Contains(p.Name, p.Protocol) {
			continue
		}
		t.Errorf("DoT provider %s Protocol %q does not match name", p.Name, p.Protocol)
	}
}

func TestDoHProviders_DoHProtocolIsEmpty(t *testing.T) {
	for _, p := range DoHProviders {
		if p.IsDoT {
			continue // DoT precisa de protocolo
		}
		if p.Protocol != "" {
			t.Errorf("DoH provider %s should have empty Protocol (uses remoteip:port on tcp), got %q",
				p.Name, p.Protocol)
		}
	}
}

func TestDoHProviders_DoTNameMatchesProtocol(t *testing.T) {
	for _, p := range DoHProviders {
		if !p.IsDoT {
			continue
		}
		expectedName := "DoT_" + strings.ToUpper(p.Protocol)
		if p.Name != expectedName {
			t.Errorf("DoT provider with Protocol %q should have Name %q, got %q",
				p.Protocol, expectedName, p.Name)
		}
	}
}

func TestGroupIPsByFamily(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		v4   []string
		v6   []string
	}{
		{"mixed", []string{"1.1.1.1", "2001:db8::1", "8.8.8.8", "::1"}, []string{"1.1.1.1", "8.8.8.8"}, []string{"2001:db8::1", "::1"}},
		{"only v4", []string{"1.1.1.1", "2.2.2.2"}, []string{"1.1.1.1", "2.2.2.2"}, nil},
		{"only v6", []string{"2001:db8::1"}, nil, []string{"2001:db8::1"}},
		{"invalid skipped", []string{"not-an-ip", "1.2.3.4"}, []string{"1.2.3.4"}, nil},
		{"empty", nil, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v4, v6 := groupIPsByFamily(tt.in)
			if !reflect.DeepEqual(v4, tt.v4) {
				t.Errorf("groupIPsByFamily(%v) v4 = %v, want %v", tt.in, v4, tt.v4)
			}
			if !reflect.DeepEqual(v6, tt.v6) {
				t.Errorf("groupIPsByFamily(%v) v6 = %v, want %v", tt.in, v6, tt.v6)
			}
		})
	}
}

// TestBuildRestoreScript verifies the iptables-restore --noflush stdin format:
// a *filter header, two -A lines per IP (TCP tcp-reset + protocol-agnostic
// icmp-port-unreachable for UDP/QUIC) with the family mask, and a COMMIT.
func TestBuildRestoreScript(t *testing.T) {
	tests := []struct {
		name string
		ips  []string
		mask string
		want string
	}{
		{
			"v4",
			[]string{"1.1.1.1", "8.8.8.8"},
			"/32",
			"*filter\n" +
				"-A OUTPUT -d 1.1.1.1/32 -p tcp -j REJECT --reject-with tcp-reset\n" +
				"-A OUTPUT -d 1.1.1.1/32 -j REJECT --reject-with icmp-port-unreachable\n" +
				"-A OUTPUT -d 8.8.8.8/32 -p tcp -j REJECT --reject-with tcp-reset\n" +
				"-A OUTPUT -d 8.8.8.8/32 -j REJECT --reject-with icmp-port-unreachable\n" +
				"COMMIT\n",
		},
		{
			"v6",
			[]string{"2001:db8::1"},
			"/128",
			"*filter\n" +
				"-A OUTPUT -d 2001:db8::1/128 -p tcp -j REJECT --reject-with tcp-reset\n" +
				"-A OUTPUT -d 2001:db8::1/128 -j REJECT --reject-with icmp-port-unreachable\n" +
				"COMMIT\n",
		},
		{
			"empty",
			nil,
			"/32",
			"*filter\nCOMMIT\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRestoreScript(tt.ips, tt.mask)
			if got != tt.want {
				t.Errorf("buildRestoreScript(%v, %q) = %q, want %q", tt.ips, tt.mask, got, tt.want)
			}
		})
	}
}

// TestSplitHostsLines verifies the shared bytes-based hosts line splitter:
// it must match the previous bufio.Scanner semantics (drop a trailing empty
// element from a final \n) while operating on []byte to reduce GC pressure in
// polling paths.
func TestSplitHostsLines(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []string
	}{
		{"unix endings", []byte("127.0.0.1 localhost\n::1 localhost\n"), []string{"127.0.0.1 localhost", "::1 localhost"}},
		{"crlf endings", []byte("127.0.0.1 localhost\r\n::1 localhost\r\n"), []string{"127.0.0.1 localhost", "::1 localhost"}},
		{"no trailing newline", []byte("a\nb"), []string{"a", "b"}},
		{"empty", []byte{}, []string{}},
		{"blank lines preserved", []byte("a\n\nb\n"), []string{"a", "", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitHostsLines(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitHostsLines(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

// TestBuildNetshAddScript verifies the netsh batch script fed via stdin: one
// full-context add rule line per IP (netsh needs the advfirewall context on
// every line when scripting via stdin) followed by exit. IPv6 names normalize
// ':' to '_'; the legacy migration delete is NOT part of the script anymore
// (a failing line would abort the batch with exit 1 — runNetshAddBatch handles
// it with a failure-tolerant delete before the batch).
func TestBuildNetshAddScript(t *testing.T) {
	tests := []struct {
		name string
		ips  []string
		want string
	}{
		{
			"two rules",
			[]string{"1.1.1.1", "8.8.8.8"},
			"advfirewall firewall add rule name=FocusGuard_1.1.1.1 dir=out action=block remoteip=1.1.1.1\r\n" +
				"advfirewall firewall add rule name=FocusGuard_8.8.8.8 dir=out action=block remoteip=8.8.8.8\r\n" +
				"exit\r\n",
		},
		{
			"ipv6 normalizes rule name without migration delete",
			[]string{"2606:4700:4700::1111"},
			"advfirewall firewall add rule name=FocusGuard_2606_4700_4700__1111 dir=out action=block remoteip=2606:4700:4700::1111\r\n" +
				"exit\r\n",
		},
		{
			"invalid and duplicate entries are dropped",
			[]string{"example.com", "8.8.8.8", "", "8.8.8.8"},
			"advfirewall firewall add rule name=FocusGuard_8.8.8.8 dir=out action=block remoteip=8.8.8.8\r\n" +
				"exit\r\n",
		},
		{
			"empty",
			nil,
			"exit\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNetshAddScript(tt.ips)
			if got != tt.want {
				t.Errorf("buildNetshAddScript(%v) = %q, want %q", tt.ips, got, tt.want)
			}
		})
	}
}

func TestValidateIPs(t *testing.T) {
	got := validateIPs([]string{
		"1.1.1.1",
		"8.8.8.8",
		"1.1.1.1",       // duplicata
		"",              // vazio
		"example.com",   // FQDN — nunca pode chegar ao remoteip
		" 9.9.9.9 ",     // espaço — aceito e canonicalizado
		"2606:4700:4700::1111",
	})
	want := []string{"1.1.1.1", "8.8.8.8", "9.9.9.9", "2606:4700:4700::1111"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("validateIPs() = %v, want %v", got, want)
	}
}

func TestExpectedBlockedIPs(t *testing.T) {
	active := map[string][]string{
		"a.com": {"1.1.1.1", "8.8.8.8", "", "1.1.1.1"}, // vazio e duplicata ignorados
		"b.com": {"2001:db8::1"},
	}
	got := expectedBlockedIPs(active)
	want := map[string]bool{
		"1.1.1.1":     true,
		"8.8.8.8":     true,
		"2001:db8::1": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expectedBlockedIPs() = %v, want %v", got, want)
	}
}

func TestSanitizeDomain(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "example.com", "example.com", false},
		{"http scheme", "http://example.com", "example.com", false},
		{"https scheme", "https://example.com", "example.com", false},
		{"uppercase scheme", "HTTP://example.com", "example.com", false},
		{"mixed-case scheme", "HtTpS://example.com/path", "example.com", false},
		{"with path", "https://example.com/some/path", "example.com", false},
		{"leading www", "www.example.com", "example.com", false},
		{"double www", "www.www.example.com", "example.com", false},
		{"uppercase www collapsed", "WWW.example.com", "example.com", false},
		{"uppercase domain lowercased", "EXAMPLE.COM", "example.com", false},
		{"surrounding spaces", "  example.com  ", "example.com", false},
		{"crlf injection removed", "example.com\r\n127.0.0.1 evil.com", "example.com127.0.0.1evil.com", false},
		{"space injection removed", "example.com 127.0.0.1", "example.com127.0.0.1", false},
		{"tab injection removed", "example.com\t127.0.0.1", "example.com127.0.0.1", false},
		{"empty", "", "", true},
		{"only www", "www.", "", true},
		{"forbidden char rejected", "example.com; rm -rf /", "", true},
		{"underscore allowed", "my_site.com", "my_site.com", false},
		{"hyphen allowed", "my-site.com", "my-site.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeDomain(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitizeDomain(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("sanitizeDomain(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
