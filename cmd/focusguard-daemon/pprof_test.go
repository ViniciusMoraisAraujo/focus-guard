package main

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestPprofAddr(t *testing.T) {
	cases := []struct{ env, want string }{
		{"", ""},
		{"6060", "127.0.0.1:6060"},
		{":6061", ":6061"},
	}
	for _, c := range cases {
		t.Setenv("FG_PPROF", c.env)
		if got := pprofAddr(); got != c.want {
			t.Errorf("pprofAddr(%q) = %q, want %q", c.env, got, c.want)
		}
	}
}

func TestStartPprof_ServesProfiles(t *testing.T) {
	addr := freeAddr(t)
	stop, err := startPprof(addr)
	if err != nil {
		t.Fatalf("startPprof: %v", err)
	}
	defer stop()

	urls := []string{
		"/debug/pprof/",
		"/debug/pprof/goroutine?debug=1",
		"/debug/pprof/heap",
		"/debug/pprof/cmdline",
	}
	for _, u := range urls {
		getOK(t, "http://"+addr+u)
	}
}

func TestStartPprof_AddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := startPprof(ln.Addr().String()); err == nil {
		t.Fatal("expected error when address is already in use")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func getOK(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
}
