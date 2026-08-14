package dnsserver

import (
	"bytes"
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// fakeChecker records every domain it is asked about and answers from a
// concurrency-safe set. The DNS server calls IsBlocked from many goroutines.
type fakeChecker struct {
	mu      sync.Mutex
	blocked map[string]bool
	seen    []string
	panicOn string
}

func newFakeChecker(blocked ...string) *fakeChecker {
	m := make(map[string]bool, len(blocked))
	for _, d := range blocked {
		m[d] = true
	}
	return &fakeChecker{blocked: m}
}

func (f *fakeChecker) IsBlocked(domain string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, domain)
	if f.panicOn != "" && domain == f.panicOn {
		panic("fake checker panic")
	}
	return f.blocked[domain]
}

func (f *fakeChecker) seenDomains() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.seen))
	copy(out, f.seen)
	return out
}

// startFakeUpstream spins a minimal authoritative server the SUT forwards to.
// It answers A/AAAA for any name with the given TTL and records queried names.
func startFakeUpstream(t *testing.T, ttl uint32) (addr string, queried *sync.Map) {
	t.Helper()
	queried = &sync.Map{}
	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		q := r.Question[0]
		queried.Store(normalizeDomain(q.Name), true)
		m := new(dns.Msg)
		m.SetReply(r)
		hdr := dns.RR_Header{Name: q.Name, Rrtype: q.Qtype, Class: dns.ClassINET, Ttl: ttl}
		switch q.Qtype {
		case dns.TypeA:
			m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.ParseIP("93.184.216.34")}}
		case dns.TypeAAAA:
			m.Answer = []dns.RR{&dns.AAAA{Hdr: hdr, AAAA: net.ParseIP("2606:2800:220:1::34")}}
		default:
			m.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(m)
	})

	pc, l, bound, err := bindBoth("127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake upstream bind: %v", err)
	}
	udp := &dns.Server{PacketConn: pc, Handler: mux, Net: "udp"}
	tcp := &dns.Server{Listener: l, Handler: mux, Net: "tcp"}
	go func() { _ = udp.ActivateAndServe() }()
	go func() { _ = tcp.ActivateAndServe() }()
	t.Cleanup(func() {
		_ = pc.Close()
		_ = l.Close()
	})
	return bound, queried
}

// startSUT binds the server under test on an ephemeral port and returns it.
func startSUT(t *testing.T, checker PolicyChecker, upstream string) *Server {
	t.Helper()
	s := New(checker, upstream)
	if err := s.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s
}

// doQuery exchanges a single query over the given transport.
func doQuery(t *testing.T, addr, network, name string, qtype uint16) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: network, Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s (%s): %v", name, network, err)
	}
	return resp
}

func TestBlockedDomainReturnsSinkhole(t *testing.T) {
	checker := newFakeChecker("youtube.com")
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %s, want success", dns.RcodeToString[resp.Rcode])
	}
	if !resp.Authoritative {
		t.Error("Authoritative = false, want true (resposta OK de autoridade, nunca erro)")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0] = %T, want *dns.A", resp.Answer[0])
	}
	if !a.A.Equal(net.IPv4zero) {
		t.Errorf("A = %v, want 0.0.0.0", a.A)
	}
	if a.Hdr.Ttl != sinkholeTTL {
		t.Errorf("A TTL = %d, want %d", a.Hdr.Ttl, sinkholeTTL)
	}

	resp6 := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeAAAA)
	if len(resp6.Answer) != 1 {
		t.Fatalf("AAAA Answer len = %d, want 1", len(resp6.Answer))
	}
	aaaa, ok := resp6.Answer[0].(*dns.AAAA)
	if !ok {
		t.Fatalf("Answer[0] = %T, want *dns.AAAA", resp6.Answer[0])
	}
	if !aaaa.AAAA.Equal(net.IPv6zero) {
		t.Errorf("AAAA = %v, want ::", aaaa.AAAA)
	}

	if s.Blocked() != 2 {
		t.Errorf("Blocked = %d, want 2", s.Blocked())
	}
	if s.Queries() != 2 {
		t.Errorf("Queries = %d, want 2", s.Queries())
	}
}

// TestBlockedDomainNeverFailsForward: mesmo com o upstream morto, um domínio
// bloqueado recebe 0.0.0.0 com Status OK — nunca SERVFAIL/REFUSED, que
// vazaria o bloqueio para o DNS secundário do roteador (spec §4.1).
func TestBlockedDomainNeverFailsForward(t *testing.T) {
	checker := newFakeChecker("blocked.example")
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "udp", "blocked.example", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %s, want success para domínio bloqueado", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1", len(resp.Answer))
	}
}

func TestAllowedDomainForwardsToUpstream(t *testing.T) {
	checker := newFakeChecker("youtube.com")
	upstream, queried := startFakeUpstream(t, 60)
	s := startSUT(t, checker, upstream)

	resp := doQuery(t, s.Addr(), "udp", "example.com", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %s, want success", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0] = %T, want *dns.A", resp.Answer[0])
	}
	if !a.A.Equal(net.ParseIP("93.184.216.34")) {
		t.Errorf("A = %v, want upstream record 93.184.216.34", a.A)
	}
	if _, ok := queried.Load("example.com"); !ok {
		t.Error("upstream não foi consultado")
	}
	if s.Blocked() != 0 {
		t.Errorf("Blocked = %d, want 0", s.Blocked())
	}
}

// TestUpstreamDownReturnsServFail: upstream inacessível → SERVFAIL (o fallback
// para o DNS secundário do roteador é o comportamento desejado, spec §3.2).
// Usa TCP para obter recusa imediata do connect em vez de esperar o timeout.
func TestUpstreamDownReturnsServFail(t *testing.T) {
	checker := newFakeChecker()
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "tcp", "example.com", dns.TypeA)
	if resp.Rcode != dns.RcodeServerFailure {
		t.Errorf("Rcode = %s, want SERVFAIL", dns.RcodeToString[resp.Rcode])
	}
}

func TestSinkholeOverTCP(t *testing.T) {
	checker := newFakeChecker("blocked.example")
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "tcp", "blocked.example", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("TCP sinkhole: Rcode=%s answer=%d, want success/1", dns.RcodeToString[resp.Rcode], len(resp.Answer))
	}
}

func TestForwardedTTLClamped(t *testing.T) {
	checker := newFakeChecker()
	upstream, _ := startFakeUpstream(t, 3600)
	s := startSUT(t, checker, upstream)

	resp := doQuery(t, s.Addr(), "udp", "example.com", dns.TypeA)
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1", len(resp.Answer))
	}
	if got := resp.Answer[0].Header().Ttl; got != sinkholeTTL {
		t.Errorf("TTL = %d, want clampado em %d (mitigação de cache §4.3)", got, sinkholeTTL)
	}
}

func TestNonQueryOpcodeNotForwarded(t *testing.T) {
	checker := newFakeChecker()
	upstream, queried := startFakeUpstream(t, 60)
	s := startSUT(t, checker, upstream)

	c := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)
	m.Opcode = dns.OpcodeStatus
	resp, _, err := c.Exchange(m, s.Addr())
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if len(resp.Answer) != 0 {
		t.Errorf("Answer len = %d, want 0 para opcode não-query", len(resp.Answer))
	}
	queried.Range(func(k, _ any) bool {
		t.Errorf("upstream não deveria ter sido consultado, mas foi (%v)", k)
		return true
	})
}

// TestDomainNormalizedBeforeChecker: o checker recebe o domínio já normalizado
// (minúsculas, sem ponto final) — o matching por sufixo é responsabilidade do
// scheduler, não deste pacote.
func TestDomainNormalizedBeforeChecker(t *testing.T) {
	checker := newFakeChecker("www.youtube.com")
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "udp", "WWW.YouTube.COM", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("sinkhole não disparou para caso misto: rcode=%s answer=%d", dns.RcodeToString[resp.Rcode], len(resp.Answer))
	}
	seen := checker.seenDomains()
	if len(seen) != 1 || seen[0] != "www.youtube.com" {
		t.Errorf("checker recebeu %v, want [www.youtube.com]", seen)
	}
}

// TestPanicInCheckerDoesNotKillServer: um panic no checker vira SERVFAIL
// logado; o servidor continua atendendo as próximas consultas (§5).
// TestInterceptIPAnswersLocalAddress: com a Interceptor Page ativa (Fase 3),
// o sinkhole responde o IP local no A (e mantém :: no AAAA sem IPv6 local) em
// vez de 0.0.0.0 — o navegador conecta no listener :80 que explica o bloqueio.
func TestInterceptIPAnswersLocalAddress(t *testing.T) {
	checker := newFakeChecker("youtube.com")
	s := startSUT(t, checker, "127.0.0.1:9")
	s.SetInterceptIP(net.ParseIP("192.168.1.100"))

	resp := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)
	if len(resp.Answer) != 1 {
		t.Fatalf("Answer len = %d, want 1", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Answer[0] = %T, want *dns.A", resp.Answer[0])
	}
	if !a.A.Equal(net.ParseIP("192.168.1.100")) {
		t.Errorf("A = %v, want 192.168.1.100 (IP local do interceptor)", a.A)
	}

	// AAAA com intercept IPv4: sem endereço IPv6 local, continua :: (morto).
	resp6 := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeAAAA)
	aaaa, ok := resp6.Answer[0].(*dns.AAAA)
	if !ok || !aaaa.AAAA.Equal(net.IPv6zero) {
		t.Errorf("AAAA = %v, want :: sem IPv6 local", aaaa)
	}
}

// TestInterceptIPNilKeepsDeadAddress: interceptor desligado (nil) = sinkhole
// clássico 0.0.0.0.
func TestInterceptIPNilKeepsDeadAddress(t *testing.T) {
	checker := newFakeChecker("youtube.com")
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)
	a, ok := resp.Answer[0].(*dns.A)
	if !ok || !a.A.Equal(net.IPv4zero) {
		t.Errorf("A = %v, want 0.0.0.0 sem interceptor", a.A)
	}
}

// TestOnBlockedHookFiresForSinkholedQuery: o hook de telemetria (Fase 1.2)
// é chamado exatamente uma vez por query bloqueada, com o domínio normalizado
// e o IP de origem; queries permitidas NUNCA disparam o hook.
func TestOnBlockedHookFiresForSinkholedQuery(t *testing.T) {
	checker := newFakeChecker("youtube.com")
	upstream, _ := startFakeUpstream(t, 60)
	s := startSUT(t, checker, upstream)

	var (
		mu  sync.Mutex
		got []string
	)
	s.SetOnBlocked(func(domain, clientIP string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, domain+"|"+clientIP)
	})

	doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)
	doQuery(t, s.Addr(), "udp", "allowed.com", dns.TypeA) // não bloqueado — sem hook

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("hook chamado %d vezes, want 1 (só bloqueadas)", len(got))
	}
	parts := strings.Split(got[0], "|")
	if parts[0] != "youtube.com" {
		t.Errorf("hook domain = %q, want youtube.com", parts[0])
	}
	if parts[1] == "" {
		t.Error("hook clientIP vazio")
	}
}

// TestOnBlockedHookUnset: sem hook registrado, o sinkhole segue funcionando
// normal (telemetria é best-effort, nunca afeta o caminho do DNS).
func TestOnBlockedHookUnset(t *testing.T) {
	checker := newFakeChecker("youtube.com")
	s := startSUT(t, checker, "127.0.0.1:9")
	resp := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("sinkhole sem hook quebrou: rcode=%s answer=%d", dns.RcodeToString[resp.Rcode], len(resp.Answer))
	}
}

// fakeClientChecker é um PolicyChecker + ClientAwareChecker de teste (Fase 4):
// bloqueia por (domínio, IP de origem), como o scheduler com o devices store.
type fakeClientChecker struct {
	mu      sync.Mutex
	blocked map[string]bool // domínio → bloqueado globalmente
	rules   map[string]bool // "ip|domain" → decisão por dispositivo
}

func (f *fakeClientChecker) IsBlocked(domain string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blocked[domain]
}

func (f *fakeClientChecker) IsBlockedFor(domain, clientIP string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.rules[clientIP+"|"+domain]; ok {
		return v
	}
	return f.blocked[domain]
}

// TestClientAwareCheckerOverridesPerDevice: quando o checker implementa
// ClientAwareChecker (Fase 4 — edição Server), o sinkhole decide pelo IP de
// origem: um device com allow_list libera o domínio para ELE enquanto a regra
// global ainda bloqueia os demais.
func TestClientAwareCheckerOverridesPerDevice(t *testing.T) {
	// O cliente real do teste conecta de 127.0.0.1 (o socket local) — a regra
	// do device é chaveada pelo IP de origem que o sinkhole vê na query.
	checker := &fakeClientChecker{
		blocked: map[string]bool{"youtube.com": true},
		rules: map[string]bool{
			"127.0.0.1|youtube.com": false, // allow_list: liberado para o device
		},
	}
	s := startSUT(t, checker, "127.0.0.1:9")

	// Device com allow_list: youtube.com NÃO é bloqueado → tenta encaminhar
	// (upstream morto → SERVFAIL, provando que saiu do caminho do sinkhole).
	resp := doQuery(t, s.Addr(), "tcp", "youtube.com", dns.TypeA)
	if resp.Rcode != dns.RcodeServerFailure {
		t.Errorf("device permitido: Rcode = %s, want SERVFAIL (upstream morto, domínio liberado)", dns.RcodeToString[resp.Rcode])
	}
}

// TestClientAwareCheckerGlobalStillApplies: sem regra por dispositivo, o
// checker com consciência de cliente cai na decisão global (block_all local).
func TestClientAwareCheckerGlobalStillApplies(t *testing.T) {
	checker := &fakeClientChecker{blocked: map[string]bool{"youtube.com": true}}
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("regra global não aplicada para device sem regra: rcode=%s answer=%d", dns.RcodeToString[resp.Rcode], len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok || !a.A.Equal(net.IPv4zero) {
		t.Errorf("A = %v, want 0.0.0.0 (sinkhole global)", a.A)
	}
}

func TestPanicInCheckerDoesNotKillServer(t *testing.T) {
	checker := newFakeChecker()
	checker.panicOn = "crash.example"
	s := startSUT(t, checker, "127.0.0.1:9")

	resp := doQuery(t, s.Addr(), "udp", "crash.example", dns.TypeA)
	if resp.Rcode != dns.RcodeServerFailure {
		t.Errorf("Rcode = %s, want SERVFAIL após panic recuperado", dns.RcodeToString[resp.Rcode])
	}

	checker2 := newFakeChecker("youtube.com")
	s2 := startSUT(t, checker2, "127.0.0.1:9")
	resp2 := doQuery(t, s2.Addr(), "udp", "youtube.com", dns.TypeA)
	if resp2.Rcode != dns.RcodeSuccess || len(resp2.Answer) != 1 {
		t.Errorf("servidor não sobreviveu ao panic: rcode=%s answer=%d", dns.RcodeToString[resp2.Rcode], len(resp2.Answer))
	}
}

// supportsIPv6 reports whether the host has a usable IPv6 loopback, so the
// dual-stack tests skip on machines with IPv6 disabled instead of failing.
func supportsIPv6(t *testing.T) bool {
	t.Helper()
	pc, err := net.ListenPacket("udp", "[::1]:0")
	if err != nil {
		return false
	}
	_ = pc.Close()
	return true
}

// v6LoopbackAddr extracts the bound IPv6 wildcard address from an Addr()
// payload ("0.0.0.0:P, [::]:Q") rewritten to the loopback host, so queries
// can target the v6 listener concretely.
func v6LoopbackAddr(bound string) string {
	for _, a := range strings.Split(bound, ", ") {
		host, port, err := net.SplitHostPort(a)
		if err == nil && host == "::" {
			return net.JoinHostPort("::1", port)
		}
	}
	return ""
}

// TestStartDualStackBind: o bind wildcard padrão (0.0.0.0) abre também o par
// IPv6 ([::]) na mesma porta e o handler compartilhado responde queries
// vindas por IPv6 (clientes da rede que preferem v6 alcançam o sinkhole).
func TestStartDualStackBind(t *testing.T) {
	if !supportsIPv6(t) {
		t.Skip("IPv6 indisponível neste host")
	}

	s := New(newFakeChecker("youtube.com"), "127.0.0.1:9")
	if err := s.Start("0.0.0.0:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	addr := s.Addr()
	entries := strings.Split(addr, ", ")
	if len(entries) != 2 {
		t.Fatalf("Addr() = %q, esperava dois listeners (IPv4 + IPv6)", addr)
	}
	// Query via loopback IPv6: o sinkhole responde com endereço morto. Em
	// plataformas onde o listener v4 reporta [::] (Windows dual-stack), o
	// primeiro [::] funciona; no Linux o v6 é o segundo entry (V6ONLY).
	v6 := v6LoopbackAddr(addr)
	if v6 == "" {
		t.Fatalf("Addr() = %q, esperava listener IPv6 ([::]:porta)", addr)
	}
	resp := doQuery(t, v6, "udp", "youtube.com", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("query v6: rcode=%s answer=%d, want success/1", dns.RcodeToString[resp.Rcode], len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok || !a.A.Equal(net.IPv4zero) {
		t.Errorf("A = %v, want 0.0.0.0 (sinkhole via IPv6)", a.A)
	}
}

// TestStart_BestEffortFamilyFailure: uma família que não bindar (aqui o IPv6,
// simulando máquina com IPv6 desabilitado) não derruba o servidor — ele serve
// na família que sobreviveu e o Addr reflete apenas ela.
func TestStart_BestEffortFamilyFailure(t *testing.T) {
	orig := listenConfigForAddr
	listenConfigForAddr = func(addr string) *net.ListenConfig {
		if strings.HasPrefix(addr, "[") {
			return &net.ListenConfig{Control: func(_, _ string, _ syscall.RawConn) error {
				return errors.New("IPv6 indisponível (simulado)")
			}}
		}
		return nil
	}
	t.Cleanup(func() { listenConfigForAddr = orig })

	s := New(newFakeChecker("youtube.com"), "127.0.0.1:9")
	if err := s.Start("0.0.0.0:0"); err != nil {
		t.Fatalf("Start deveria servir com uma família: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	if entries := strings.Split(s.Addr(), ", "); len(entries) != 1 {
		t.Errorf("Addr() = %q, esperava apenas a família que bindou", s.Addr())
	}
	// O servidor continua respondendo na família que bindou.
	resp := doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("servidor não respondeu após falha de uma família: rcode=%s", dns.RcodeToString[resp.Rcode])
	}
}

// lockedBuffer é um bytes.Buffer seguro para escrita concorrente (o statsLoop
// loga via log.SetOutput de outra goroutine) e leitura no teste — sem mutex,
// o -race acusa a race entre o Write do log e o String() do polling.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestStatsLoopLogsActivity: o loop periódico de contadores loga a atividade
// do sinkhole (queries/bloqueadas do intervalo) quando há tráfego.
func TestStatsLoopLogsActivity(t *testing.T) {
	orig := statsInterval
	statsInterval = 50 * time.Millisecond
	defer func() { statsInterval = orig }()

	buf := &lockedBuffer{}
	origWriter := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
	}()

	s := startSUT(t, newFakeChecker("youtube.com"), "127.0.0.1:9")
	_ = doQuery(t, s.Addr(), "udp", "youtube.com", dns.TypeA)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "atividade no intervalo") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got := buf.String(); !strings.Contains(got, "atividade no intervalo") {
		t.Fatalf("log periódico de contadores não apareceu; output: %q", got)
	}
}

// TestStart_BothFamiliesFail: quando nenhuma família binda, o Start reporta
// erro (e o bindHint segue aplicável) em vez de reportar sucesso.
func TestStart_BothFamiliesFail(t *testing.T) {
	orig := listenConfigForAddr
	listenConfigForAddr = func(addr string) *net.ListenConfig {
		return &net.ListenConfig{Control: func(_, _ string, _ syscall.RawConn) error {
			return errors.New("bind simulado: porta em uso")
		}}
	}
	t.Cleanup(func() { listenConfigForAddr = orig })

	s := New(newFakeChecker("youtube.com"), "127.0.0.1:9")
	if err := s.Start("0.0.0.0:0"); err == nil {
		t.Fatal("Start deveria falhar quando as duas famílias falham")
	}
	t.Cleanup(func() { _ = s.Stop() })
}
