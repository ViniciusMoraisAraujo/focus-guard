package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"
)

// pprofAddr returns the loopback address for the temporary pprof endpoint
// driven by the FG_PPROF env var (a bare port or ":port"). Empty when the
// endpoint is disabled. Loopback only — profiling must never leave the host.
func pprofAddr() string {
	v := os.Getenv("FG_PPROF")
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, ":") {
		return v
	}
	return "127.0.0.1:" + v
}

// startPprof serves the standard runtime pprof endpoints on the given loopback
// address. Returns a stop func that gracefully shuts the server down.
func startPprof(addr string) (func(), error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("pprof listen %s: %w", addr, err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}, nil
}
