package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"focusguard/internal/domain/scheduler"
	"focusguard/internal/infrastructure/store"
	"focusguard/internal/infrastructure/tlsca"
	"focusguard/internal/transport/ipc"
)

// setDaemonTestEndpoint aponta o endpoint IPC (Listen/Dial do pacote ipc) para
// um socket de teste — unix socket num temp dir no Linux, porta TCP livre no
// Windows — espelhando o setTestEndpoint dos testes de integração do ipc.
func setDaemonTestEndpoint(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("achar porta livre: %v", err)
		}
		addr := ln.Addr().String()
		ln.Close()
		orig := ipc.TestDialAddr
		ipc.TestDialAddr = addr
		t.Cleanup(func() { ipc.TestDialAddr = orig })
		return
	}
	orig := ipc.TestSocketPath
	ipc.TestSocketPath = filepath.Join(t.TempDir(), "focusguard-daemon-test.sock")
	t.Cleanup(func() { ipc.TestSocketPath = orig })
}

// parseCAPEM extrai o *x509.Certificate da CA a partir do CertPEM (para montar
// o pool de raízes do cliente TLS).
func parseCAPEM(t *testing.T, ca *tlsca.CA) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(ca.CertPEM())
	if block == nil {
		t.Fatal("CertPEM inválido")
	}
	crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return crt
}

// TestIntegration_InterceptorSetOn_GeneratesCAAndSignsLeafs é o teste de
// integração do fluxo completo: o cliente IPC envia "interceptor-set on" (como
// o CLI/web fazem), o handler de domínio persiste o flag e o il.set garante a
// CA local + sobe o listener TLS. O teste então valida que:
//
//  1. A CA foi gerada e persistida no caDir (tlsca.Exists).
//  2. O listener TLS serve um leaf ASSINADO PELA CA — um handshake com o pool
//     de raízes contendo apenas a CA valida o certificado (sem
//     InsecureSkipVerify). Com o fallback auto-assinado, esse handshake
//     falharia.
//  3. A página de bloqueio HTTPS é servida para um domínio bloqueado (200 com
//     o domínio no corpo).
//
// A instalação no trust store é stubada (ensureCAInstalled) — a suíte nunca
// toca o trust store real da máquina; o comportamento do trust store é
// coberto pelos testes do pacote tlsca.
func TestIntegration_InterceptorSetOn_GeneratesCAAndSignsLeafs(t *testing.T) {
	setDaemonTestEndpoint(t)

	// Store + scheduler reais (mesmo chain do boot, com enforcer fake — o
	// enforcer real precisa de root/firewall).
	statePath := filepath.Join(t.TempDir(), "state.json")
	st, err := store.NewStore(statePath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	sched := scheduler.NewScheduler(st, &fakeDaemonEnforcer{})
	if err := sched.Start(); err != nil {
		t.Fatalf("scheduler.Start: %v", err)
	}
	t.Cleanup(sched.Stop)

	// Lifecycle do interceptor com caDir real (temp) e porta TLS efêmera (o
	// boot real usa 127.0.0.1:443; aqui não podemos ocupar portas privilegiadas).
	il := &interceptorLifecycle{
		checker:  sched,
		tlsAddrs: []string{"127.0.0.1:0"},
		caDir:    filepath.Join(t.TempDir(), "ca"),
	}
	t.Cleanup(il.stop)

	// Stub da instalação no trust store (não tocar o store real da máquina).
	orig := ensureCAInstalled
	ensureCAInstalled = func(ca *tlsca.CA) error { return nil }
	t.Cleanup(func() { ensureCAInstalled = orig })

	// Server IPC real + a MESMA wiring do composition root (helper
	// compartilhado registerInterceptorSet — o teste não replica o registro).
	srv := ipc.NewServer(sched)
	registerInterceptorSet(srv, sched, il.set)
	go func() {
		if err := srv.Start(); err != nil {
			t.Logf("server IPC encerrado: %v", err)
		}
	}()
	t.Cleanup(func() { _ = srv.Stop() })
	time.Sleep(50 * time.Millisecond)

	client := ipc.NewClient()

	// Bloqueia um domínio que resolve localmente (localhost → 127.0.0.1) para
	// o checker reconhecer na página de bloqueio.
	if _, err := client.Send(ipc.Request{Action: "block", Domain: "localhost", Duration: "1h"}); err != nil {
		t.Fatalf("block localhost: %v", err)
	}

	// 1) Ativa a página via IPC — o caminho real que o CLI/web usam.
	resp, err := client.Send(ipc.Request{Action: "interceptor-set", InterceptorEnabled: true})
	if err != nil {
		t.Fatalf("interceptor-set: %v", err)
	}
	if !resp.Success {
		t.Fatalf("interceptor-set falhou: %s", resp.Message)
	}
	if !resp.InterceptorEnabled {
		t.Error("resposta do interceptor-set deveria reportar flag ativo")
	}

	// 2) CA gerada e persistida no caDir.
	if il.ca == nil {
		t.Fatal("il.set deveria ter garantido a CA local")
	}
	if !tlsca.Exists(il.caDir) {
		t.Error("CA deveria estar persistida no caDir")
	}

	// 3) Handshake TLS com o pool de raízes = só a CA: se o leaf servido for
	// assinado pela CA, valida sem InsecureSkipVerify; se caísse no fallback
	// auto-assinado, o handshake falharia.
	if len(il.servers) != 1 {
		t.Fatalf("esperava 1 listener TLS, got %d", len(il.servers))
	}
	addr := il.servers[0].Addr()
	pool := x509.NewCertPool()
	pool.AddCert(parseCAPEM(t, il.ca))

	conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool, ServerName: "localhost"})
	if err != nil {
		t.Fatalf("handshake TLS deveria validar contra a CA (leaf assinado por ela): %v", err)
	}
	certs := conn.ConnectionState().PeerCertificates
	conn.Close()
	if len(certs) == 0 {
		t.Fatal("sem certificado no handshake")
	}
	if len(certs[0].DNSNames) != 1 || certs[0].DNSNames[0] != "localhost" {
		t.Errorf("SAN do leaf = %v, want [localhost]", certs[0].DNSNames)
	}
	if len(certs) < 2 {
		t.Error("a chain deveria incluir a CA (leaf + CA)")
	}

	// 4) A página de bloqueio HTTPS é servida para o domínio bloqueado — o
	// fluxo que o usuário vê após o fix (sem aviso de certificado).
	clientHTTP := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "localhost"},
	}}
	req, _ := http.NewRequest("GET", "https://"+addr+"/", nil)
	req.Host = "localhost"
	hresp, err := clientHTTP.Do(req)
	if err != nil {
		t.Fatalf("GET https via CA falhou: %v", err)
	}
	defer hresp.Body.Close()
	body, _ := io.ReadAll(hresp.Body)
	if hresp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", hresp.StatusCode)
	}
	if !strings.Contains(string(body), "localhost") {
		t.Errorf("página HTTPS sem o domínio: %s", string(body))
	}
}

// TestIntegration_InterceptorSetOff_StopsListeners: desligar a página via IPC
// derruba o listener TLS — e a CA persiste (não é removida ao desativar, só no
// ca-uninstall/uninstall).
func TestIntegration_InterceptorSetOff_StopsListeners(t *testing.T) {
	setDaemonTestEndpoint(t)

	statePath := filepath.Join(t.TempDir(), "state.json")
	st, err := store.NewStore(statePath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	sched := scheduler.NewScheduler(st, &fakeDaemonEnforcer{})
	if err := sched.Start(); err != nil {
		t.Fatalf("scheduler.Start: %v", err)
	}
	t.Cleanup(sched.Stop)

	il := &interceptorLifecycle{
		checker:  sched,
		tlsAddrs: []string{"127.0.0.1:0"},
		caDir:    filepath.Join(t.TempDir(), "ca"),
	}
	t.Cleanup(il.stop)

	orig := ensureCAInstalled
	ensureCAInstalled = func(ca *tlsca.CA) error { return nil }
	t.Cleanup(func() { ensureCAInstalled = orig })

	srv := ipc.NewServer(sched)
	registerInterceptorSet(srv, sched, il.set)
	go func() {
		if err := srv.Start(); err != nil {
			t.Logf("server IPC encerrado: %v", err)
		}
	}()
	t.Cleanup(func() { _ = srv.Stop() })
	time.Sleep(50 * time.Millisecond)

	client := ipc.NewClient()

	if _, err := client.Send(ipc.Request{Action: "interceptor-set", InterceptorEnabled: true}); err != nil {
		t.Fatalf("interceptor-set on: %v", err)
	}
	if len(il.servers) != 1 {
		t.Fatalf("esperava 1 listener com a página ativa, got %d", len(il.servers))
	}
	addr := il.servers[0].Addr()

	// Desliga: o listener TLS é derrubado (a porta deixa de aceitar conexões).
	resp, err := client.Send(ipc.Request{Action: "interceptor-set", InterceptorEnabled: false})
	if err != nil {
		t.Fatalf("interceptor-set off: %v", err)
	}
	if !resp.Success {
		t.Fatalf("interceptor-set off falhou: %s", resp.Message)
	}
	if resp.InterceptorEnabled {
		t.Error("resposta do interceptor-set off deveria reportar flag inativo")
	}
	if len(il.servers) != 0 {
		t.Fatalf("listeners deveriam ter sido derrubados, got %d", len(il.servers))
	}

	pool := x509.NewCertPool()
	pool.AddCert(parseCAPEM(t, il.ca))
	if conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool, ServerName: "localhost"}); err == nil {
		conn.Close()
		t.Error("porta do listener deveria estar fechada após interceptor-set off")
	}

	// A CA persiste (desativar a página não remove a âncora; a remoção é do
	// ca-uninstall/uninstall).
	if !tlsca.Exists(il.caDir) {
		t.Error("desativar a página não deveria remover a CA persistida")
	}
}
