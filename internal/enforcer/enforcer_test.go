package enforcer

import (
	"errors"
	"net"
	"reflect"
	"testing"
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

func TestHeaderMarker_Value(t *testing.T) {
	expected := "# FOCUS GUARD BLOCKS - DO NOT EDIT MANUALLY"
	if HeaderMarker != expected {
		t.Errorf("HeaderMarker = %q, want %q", HeaderMarker, expected)
	}
}
