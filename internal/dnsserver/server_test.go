package dnsserver

import (
	"net"
	"sync"
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
