package interceptor

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeChecker decide bloqueio por domínio e devolve um tempo restante fixo.
type fakeChecker struct {
	blocked map[string]bool
	remain  time.Duration
}

func (f *fakeChecker) IsBlocked(domain string) bool { return f.blocked[domain] }
func (f *fakeChecker) BlockRemaining(domain string) time.Duration {
	if f.blocked[domain] {
		return f.remain
	}
	return 0
}

// startSUT binds an ephemeral port and returns the base URL.
func startSUT(t *testing.T, c Checker) string {
	t.Helper()
	s := New(c)
	if err := s.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return "http://" + s.Addr()
}

func TestBlockedDomainGetsPage(t *testing.T) {
	base := startSUT(t, &fakeChecker{blocked: map[string]bool{"youtube.com": true}, remain: 2 * time.Hour})

	// O navegador conecta em 127.0.0.1:porta mas envia o Host do domínio
	// bloqueado (é ele que resolveu para cá) — o Host é o que decide.
	req, _ := http.NewRequest("GET", base+"/", nil)
	req.Host = "youtube.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	text := string(body)
	if !strings.Contains(text, "youtube.com") {
		t.Errorf("página não menciona o domínio: %s", text)
	}
	if !strings.Contains(text, "2h0m0s") && !strings.Contains(text, "2h") {
		t.Errorf("página não mostra o tempo restante: %s", text)
	}
	// Frase motivacional presente: a frase determinística do domínio + as
	// aspas tipográficas do template.
	wantQuote := quoteFor("youtube.com")
	if !strings.Contains(text, wantQuote) || !strings.Contains(text, "“") {
		t.Errorf("página sem a frase motivacional %q: %s", wantQuote, text)
	}
}

func TestAllowedDomainGets404(t *testing.T) {
	base := startSUT(t, &fakeChecker{blocked: map[string]bool{"youtube.com": true}})

	// Host liberado (example.com) → 404, não a página de bloqueio.
	req, _ := http.NewRequest("GET", base+"/", nil)
	req.Host = "example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 para host liberado", resp.StatusCode)
	}
}

func TestHostWithPortNormalized(t *testing.T) {
	base := startSUT(t, &fakeChecker{blocked: map[string]bool{"youtube.com": true}, remain: time.Hour})

	req, _ := http.NewRequest("GET", base+"/", nil)
	req.Host = "youtube.com:80" // navegador envia o Host com a porta explícita
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (porta do Host normalizada)", resp.StatusCode)
	}
}

// TestIPv6LoopbackServesPage: no Desktop o daemon liga o interceptor em
// 127.0.0.1:80 E [::1]:80 (dual-stack, casando com as duas entradas que o
// enforcer escreve no hosts). Navegadores modernos tentam IPv6 primeiro — sem
// o ::1 a página não apareceria até o fallback. Este teste prova que o
// listener IPv6 serve a página de bloqueio normal.
func TestIPv6LoopbackServesPage(t *testing.T) {
	s := New(&fakeChecker{blocked: map[string]bool{"youtube.com": true}, remain: 30 * time.Minute})
	if err := s.Start("[::1]:0"); err != nil {
		t.Skipf("IPv6 loopback indisponível neste ambiente: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	base := "http://" + s.Addr()
	req, _ := http.NewRequest("GET", base+"/", nil)
	req.Host = "youtube.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status via IPv6 = %d, want 200", resp.StatusCode)
	}
	text := string(body)
	if !strings.Contains(text, "youtube.com") || !strings.Contains(text, quoteFor("youtube.com")) {
		t.Errorf("página IPv6 sem domínio/frase: %s", text)
	}
}

// startTLSSUT binds an ephemeral TLS port and returns a client that trusts
// the self-signed certificate (the browser warns but lets the user continue —
// this test does the "continue" part) plus the https base URL.
func startTLSSUT(t *testing.T, c Checker) (string, *http.Client) {
	t.Helper()
	s := New(c)
	if err := s.StartTLS("127.0.0.1:0"); err != nil {
		t.Fatalf("StartTLS: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	// Cliente que confia no cert auto-assinado do servidor (equivalente ao
	// usuário clicando "Avançado → Continuar" no Firefox). O dial de extração
	// usa o MESMO ServerName do request (o cert é gerado por SNI).
	pool := x509.NewCertPool()
	conn, err := tls.Dial("tcp", s.Addr(), &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "youtube.com",
	})
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	pool.AddCert(conn.ConnectionState().PeerCertificates[0])
	_ = conn.Close()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "youtube.com"},
	}}
	return "https://" + s.Addr(), client
}

// TestBlockedDomainGetsPageOverHTTPS: sites HTTPS-only (YouTube/Instagram —
// HSTS força TLS) conectam na porta 443 do interceptor. O cert auto-assinado
// por SNI permite continuar pelo aviso e a página de bloqueio é servida — o
// fluxo que o usuário final vê.
func TestBlockedDomainGetsPageOverHTTPS(t *testing.T) {
	base, client := startTLSSUT(t, &fakeChecker{blocked: map[string]bool{"youtube.com": true}, remain: 2 * time.Hour})

	req, _ := http.NewRequest("GET", base+"/", nil)
	req.Host = "youtube.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	text := string(body)
	if !strings.Contains(text, "youtube.com") || !strings.Contains(text, quoteFor("youtube.com")) {
		t.Errorf("página HTTPS sem domínio/frase: %s", text)
	}
}

// TestSelfSignedCertScopedToSNI: o certificado gerado sob demanda carrega a
// SAN exata do hostname do SNI — sem hostname-mismatch ao continuar pelo
// aviso de certificado.
func TestSelfSignedCertScopedToSNI(t *testing.T) {
	s := New(&fakeChecker{})
	if err := s.StartTLS("127.0.0.1:0"); err != nil {
		t.Fatalf("StartTLS: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	conn, err := tls.Dial("tcp", s.Addr(), &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "instagram.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		t.Fatal("sem certificado no handshake")
	}
	if len(certs[0].DNSNames) != 1 || certs[0].DNSNames[0] != "instagram.com" {
		t.Errorf("SAN do cert = %v, want [instagram.com]", certs[0].DNSNames)
	}
}

// TestBusyPortFailsStartWithoutPanic: HTTP e TLS compartilham o mesmo
// best-effort — uma porta ocupada falha o Start sem derrubar nada.
func TestBusyPortFailsStartWithoutPanic(t *testing.T) {
	// Ocupa uma porta e tenta bindar o interceptor nela — deve falhar com
	// erro (best-effort do daemon), nunca derrubar nada.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	s := New(&fakeChecker{})
	if err := s.Start(ln.Addr().String()); err == nil {
		t.Fatal("Start numa porta ocupada deveria falhar")
	}
	_ = s.Stop() // idempotente

	// Mesmo contrato para o listener TLS.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	s2 := New(&fakeChecker{})
	if err := s2.StartTLS(ln2.Addr().String()); err == nil {
		t.Fatal("StartTLS numa porta ocupada deveria falhar")
	}
	_ = s2.Stop() // idempotente
}

func TestQuoteForIsDeterministic(t *testing.T) {
	a := quoteFor("youtube.com")
	b := quoteFor("youtube.com")
	if a == "" || a != b {
		t.Errorf("frase deveria ser determinística por domínio: %q vs %q", a, b)
	}
	// Domínios diferentes podem (e em geral vão) ter frases diferentes.
	if quoteFor("instagram.com") == a {
		t.Log("mesma frase para domínios diferentes (ok — colisão de hash)")
	}
}

func TestQuoteForEmptyList(t *testing.T) {
	orig := motivationalQuotes
	motivationalQuotes = nil
	defer func() { motivationalQuotes = orig }()
	if q := quoteFor("x.com"); q != "" {
		t.Errorf("frase com lista vazia = %q, want \"\"", q)
	}
}
