// Package dnsserver implements the FocusGuard Server: a LAN-wide DNS sinkhole
// built on github.com/miekg/dns. It answers blocked domains with a dead
// address (0.0.0.0 / ::) and Status OK so clients never fall back to the
// router's secondary DNS, and forwards everything else to a secure upstream
// (Cloudflare 1.1.1.2 by default).
package dnsserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/miekg/dns"
)

// DefaultBindAddr is where the daemon listens by default: all interfaces on
// port 53 (UDP/TCP), so any device on the LAN can use the sinkhole as its
// primary DNS server.
const DefaultBindAddr = "0.0.0.0:53"

// DefaultUpstream is the secure DNS the server forwards allowed queries to.
// Cloudflare 1.1.1.2 (Security) filters malware/phishing at the source — the
// spec's "upstreams filtrados de segurança" (section 6.1). It answers on plain
// port 53, so it does not collide with the DoH/DoT firewall guard.
const DefaultUpstream = "1.1.1.2:53"

// sinkholeTTL is the TTL stamped on blocked answers AND the cap applied to
// forwarded responses: a short TTL makes unblocks propagate within a minute
// even to clients that cache aggressively (spec section 4.3).
const sinkholeTTL = 60

// upstreamTimeout bounds each forwarding exchange. A dead upstream must never
// hang a worker goroutine — while the DNS server is the network's resolver, a
// stuck exchange would take down name resolution for the whole house.
const upstreamTimeout = 5 * time.Second

// statsInterval is how often the periodic activity log runs. Package var so
// tests can shrink it.
var statsInterval = time.Minute

// PolicyChecker decides whether a queried domain is blocked. Implementations
// must be safe for concurrent use: the DNS server runs one goroutine per
// request and calls IsBlocked from all of them. The domain is normalized
// (lowercase, no trailing dot).
type PolicyChecker interface {
	IsBlocked(domain string) bool
}

// ClientAwareChecker is the optional extension for per-device policies (Fase
// 4 — edição Server): the sinkhole consults it with the query's source IP so
// a device rule (block_all/allow_list) can override the global decision. A
// checker implementing both interfaces uses this variant; the plain desktop
// checker keeps IsBlocked.
type ClientAwareChecker interface {
	IsBlockedFor(domain, clientIP string) bool
}

// Server is the DNS sinkhole + forwarder. Zero-value is not usable; construct
// with New.
type Server struct {
	checker   PolicyChecker
	upstream  string
	udpClient *dns.Client
	tcpClient *dns.Client

	mu        sync.Mutex
	udps      []*dns.Server
	tcps      []*dns.Server
	addr      string
	statsStop chan struct{} // fechado pelo Stop para encerrar o loop de contadores

	queries atomic.Uint64
	blocked atomic.Uint64

	// onBlocked is the telemetry hook (Fase 1.2 do features-plan): called once
	// per sinkholed query with the domain and the client IP, outside any lock.
	// Must be cheap and non-blocking (the DNS server runs one goroutine per
	// request); the daemon wires it to the telemetry recorder's channel. May
	// be nil.
	onBlocked func(domain, clientIP string)

	// interceptIP is the local address answered for blocked domains when the
	// Focus Interceptor Page is active (Fase 3): instead of 0.0.0.0 (dead
	// address), the sinkhole answers with the server's own IP so the browser
	// connects to the interceptor HTTP listener on port 80. Nil = classic
	// dead-address sinkhole.
	interceptIP net.IP
}

// New returns a server that consults checker for sinkhole decisions and
// forwards allowed queries to upstream (host:port). An empty upstream falls
// back to DefaultUpstream.
func New(checker PolicyChecker, upstream string) *Server {
	if upstream == "" {
		upstream = DefaultUpstream
	}
	return &Server{
		checker:   checker,
		upstream:  upstream,
		udpClient: &dns.Client{Net: "udp", Timeout: upstreamTimeout},
		tcpClient: &dns.Client{Net: "tcp", Timeout: upstreamTimeout},
	}
}

// Addr returns the address(es) the server is actually bound to, comma-joined
// ("" when stopped). With the default wildcard bind it reports both families,
// e.g. "0.0.0.0:53, [::]:53"; with a concrete address (loopback in tests) it
// is the single bound host:port, useful to read back the ephemeral port the
// OS chose.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Queries returns the total number of DNS queries handled since Start.
func (s *Server) Queries() uint64 { return s.queries.Load() }

// Blocked returns how many of those queries were sinkholed.
func (s *Server) Blocked() uint64 { return s.blocked.Load() }

// statsLoop logs the sinkhole activity every statsInterval: queries and
// blocked deltas since the last tick plus the cumulative totals. Only fires
// when there was traffic in the interval — an idle server stays silent, so
// the log's presence/absence itself signals whether the LAN is using the
// sinkhole. Exits when stop is closed (Stop/restart).
func (s *Server) statsLoop(stop chan struct{}) {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()

	lastQueries := s.queries.Load()
	lastBlocked := s.blocked.Load()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			q := s.queries.Load()
			b := s.blocked.Load()
			dq, db := q-lastQueries, b-lastBlocked
			lastQueries, lastBlocked = q, b
			if dq > 0 {
				log.Printf("[FocusGuard DNS] atividade no intervalo: %d queries (%d bloqueadas) · total %d queries (%d bloqueadas)", dq, db, q, b)
			}
		}
	}
}

// SetOnBlocked registers the telemetry hook: called once per sinkholed query
// with the normalized domain and the client's IP. Replacing the hook is safe
// at any time (guarded); the previous hook (if any) stops receiving calls.
func (s *Server) SetOnBlocked(fn func(domain, clientIP string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onBlocked = fn
}

// SetInterceptIP toggles the Focus Interceptor Page answer (Fase 3): a
// non-nil IP makes the sinkhole answer blocked A/AAAA queries with that
// address (so the browser lands on the interceptor listener); nil restores
// the classic dead-address sinkhole. Safe at any time.
func (s *Server) SetInterceptIP(ip net.IP) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interceptIP = ip
}

// Start binds addr (default DefaultBindAddr) on both UDP and TCP and begins
// serving. It is synchronous: it returns as soon as both listeners are bound,
// so bind errors (e.g. another process holding port 53) surface immediately.
// It is not idempotent — a running server returns an error.
func (s *Server) Start(addr string) error {
	if addr == "" {
		addr = DefaultBindAddr
	}

	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handleDNSRequest)

	// Dual-stack: um wildcard IPv4 (0.0.0.0) também abre o par IPv6 ([::]) na
	// mesma porta, para clientes da rede que preferem IPv6 alcançarem o
	// sinkhole. Cada família bindada é independente e best-effort: uma família
	// que não bindar (ex.: IPv6 desabilitado na máquina) não derruba a outra —
	// só loga o aviso. O Start reporta sucesso se ao menos uma família bindou.
	var udps, tcps []*dns.Server
	var bound []string
	var failed []string
	for _, target := range bindTargets(addr) {
		pc, l, b, err := bindBothWith(listenConfigForAddr(target), target)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", target, err))
			continue
		}
		udps = append(udps, &dns.Server{PacketConn: pc, Handler: mux, Net: "udp"})
		tcps = append(tcps, &dns.Server{Listener: l, Handler: mux, Net: "tcp"})
		bound = append(bound, b)
	}
	if len(udps) == 0 {
		return fmt.Errorf("dns: %s", strings.Join(failed, "; "))
	}

	stop := make(chan struct{})
	s.mu.Lock()
	if len(s.udps) > 0 || len(s.tcps) > 0 {
		s.mu.Unlock()
		for i := range udps {
			_ = udps[i].PacketConn.Close()
			_ = tcps[i].Listener.Close()
		}
		return errors.New("dns: servidor já iniciado")
	}
	s.udps = udps
	s.tcps = tcps
	s.addr = strings.Join(bound, ", ")
	s.statsStop = stop
	s.mu.Unlock()

	if len(failed) > 0 {
		log.Printf("[FocusGuard DNS] aviso: família sem bind (%s) — servindo em %s", strings.Join(failed, "; "), s.addr)
	}

	// Loop periódico de contadores: loga a atividade do sinkhole a cada
	// statsInterval (queries/bloqueadas no intervalo + totais) para verificar
	// ao vivo se os dispositivos da rede estão usando o servidor.
	go s.statsLoop(stop)

	for i := range udps {
		udp, tcp := udps[i], tcps[i]
		go func() {
			if err := udp.ActivateAndServe(); err != nil && s.isRunning(udp) {
				log.Printf("[FocusGuard DNS] erro no servidor UDP: %v", err)
			}
		}()
		go func() {
			if err := tcp.ActivateAndServe(); err != nil && s.isRunning(tcp) {
				log.Printf("[FocusGuard DNS] erro no servidor TCP: %v", err)
			}
		}()
	}

	return nil
}

// isRunning reports whether srv is still an active server (false once Stop
// replaced the slices) — suppresses the expected "closed connection" error
// logged when the listeners are closed on purpose.
func (s *Server) isRunning(srv *dns.Server) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, udp := range s.udps {
		if udp == srv {
			return true
		}
	}
	for _, tcp := range s.tcps {
		if tcp == srv {
			return true
		}
	}
	return false
}

// bindTargets expands a listen address into the concrete sockets to open. A
// wildcard IPv4 address (0.0.0.0, the default) also opens the IPv6 twin ([::]
// on the same port) so the sinkhole serves both address families; any concrete
// address (loopback, LAN IP, [::]) binds only itself.
func bindTargets(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{addr} // endereço inválido: deixa o bind reportar o erro
	}
	if host != "0.0.0.0" && host != "" {
		return []string{addr}
	}
	if host == "" {
		host = "0.0.0.0"
	}
	return []string{net.JoinHostPort(host, port), net.JoinHostPort("::", port)}
}

// listenConfigForAddr supplies the ListenConfig for a bind target. The IPv6
// wildcard ([::]) socket is marked V6ONLY: Go's default wildcard-IPv6 socket
// accepts IPv4-mapped addresses, so without this flag the [::] and 0.0.0.0
// listeners would collide (EADDRINUSE on Linux). V6ONLY keeps them as two
// independent listeners on the same port. Nil = default behavior. A package
// var so tests can override it to simulate a family whose bind fails
// (best-effort path).
var listenConfigForAddr = func(addr string) *net.ListenConfig {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "::" {
		return nil
	}
	return &net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = setV6Only(fd)
			}); err != nil {
				return err
			}
			return serr
		},
	}
}

// bindBoth opens the UDP and TCP listeners on the same address and returns
// them with the concrete bound address. With an ephemeral port (":0") the two
// listens would otherwise pick different ports; the TCP listen retries a few
// times until it lands on the UDP port.
func bindBoth(addr string) (pc net.PacketConn, l net.Listener, bound string, err error) {
	return bindBothWith(nil, addr)
}

// bindBothWith is bindBoth with a ListenConfig (nil = defaults), used to mark
// the IPv6 wildcard socket V6ONLY so it coexists with the IPv4 listener.
func bindBothWith(lc *net.ListenConfig, addr string) (pc net.PacketConn, l net.Listener, bound string, err error) {
	host, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, nil, "", fmt.Errorf("dns: endereço inválido %s: %w", addr, splitErr)
	}
	if lc == nil {
		lc = &net.ListenConfig{}
	}
	if port != "0" {
		pc, err = lc.ListenPacket(context.Background(), "udp", addr)
		if err != nil {
			return nil, nil, "", fmt.Errorf("dns: udp bind %s: %w", addr, err)
		}
		l, err = lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			_ = pc.Close()
			return nil, nil, "", fmt.Errorf("dns: tcp bind %s: %w", addr, err)
		}
		return pc, l, pc.LocalAddr().String(), nil
	}

	for attempt := 0; attempt < 100; attempt++ {
		pc, err = lc.ListenPacket(context.Background(), "udp", addr)
		if err != nil {
			return nil, nil, "", fmt.Errorf("dns: udp bind %s: %w", addr, err)
		}
		_, p, _ := net.SplitHostPort(pc.LocalAddr().String())
		l, err = lc.Listen(context.Background(), "tcp", net.JoinHostPort(host, p))
		if err == nil {
			return pc, l, pc.LocalAddr().String(), nil
		}
		_ = pc.Close()
	}
	return nil, nil, "", fmt.Errorf("dns: tcp bind %s: porta ephemeral sempre em uso", addr)
}

// Stop closes both listeners and releases the port. Idempotent: stopping a
// stopped server returns nil. In-flight handlers finish on their own (bounded
// by upstreamTimeout); writes to a closed socket during a restart log a
// "use of closed network connection" via writeReply (harmless, but visible
// for diagnostics).
func (s *Server) Stop() error {
	s.mu.Lock()
	udps, tcps := s.udps, s.tcps
	s.udps, s.tcps = nil, nil
	s.addr = ""
	stop := s.statsStop
	s.statsStop = nil
	s.mu.Unlock()
	if stop != nil {
		close(stop) // encerra o loop de contadores (idempotente: nil após a 1ª vez)
	}

	var errs []error
	for _, udp := range udps {
		if udp != nil && udp.PacketConn != nil {
			if err := udp.PacketConn.Close(); err != nil {
				errs = append(errs, fmt.Errorf("dns: fechar listener UDP: %w", err))
			}
		}
	}
	for _, tcp := range tcps {
		if tcp != nil && tcp.Listener != nil {
			if err := tcp.Listener.Close(); err != nil {
				errs = append(errs, fmt.Errorf("dns: fechar listener TCP: %w", err))
			}
		}
	}
	err := errors.Join(errs...)
	if err != nil {
		log.Printf("[FocusGuard DNS] erros ao parar o servidor: %v", err)
	}
	return err
}

// handleDNSRequest serves one query. Every per-request goroutine runs under
// recoverPanic: with the server acting as the LAN resolver, an uncaught panic
// would take down the network's name resolution (spec section 5).
func (s *Server) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	defer s.recoverPanic(w, r)

	s.queries.Add(1)

	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	if r.Opcode != dns.OpcodeQuery || len(r.Question) != 1 {
		// Não é uma query normal (NOTIFY/UPDATE/status ou pacote vazio):
		// responde vazio, mas registra para diagnóstico de scanners/erros.
		log.Printf("[FocusGuard DNS] requisição incomum de %s: opcode=%s questions=%d",
			clientIP(w), dns.OpcodeToString[r.Opcode], len(r.Question))
		s.writeReply(w, r, m)
		return
	}

	q := r.Question[0]
	domain := normalizeDomain(q.Name)

	// Sinkhole: answer OK with a dead address. Returning SERVFAIL/REFUSED (or
	// dropping the packet) would make phones and tablets retry the router's
	// secondary DNS and leak the block (spec section 4.1). O checker com
	// consciência de cliente (Fase 4 — edição Server) decide PRIMEIRO pelo IP
	// de origem: a allow_list de um device pode LIBERAR um domínio que a
	// regra global bloqueia; sem regra por dispositivo, cai na decisão
	// global. O checker clássico (desktop) usa só o domínio.
	blocked := false
	if cac, ok := s.checker.(ClientAwareChecker); ok {
		blocked = cac.IsBlockedFor(domain, clientIP(w))
	} else if s.checker != nil {
		blocked = s.checker.IsBlocked(domain)
	}
	if blocked {
		s.blocked.Add(1)
		m.Authoritative = true
		// Resposta do sinkhole: 0.0.0.0/:: (endereço morto) por padrão, ou o
		// IP local do servidor quando a Interceptor Page está ativa (Fase 3) —
		// aí o navegador conecta no listener HTTP :80 que explica o bloqueio.
		ip := s.interceptIPProvider()
		switch q.Qtype {
		case dns.TypeA:
			a := net.IPv4zero
			if ip != nil && ip.To4() != nil {
				a = ip.To4()
			}
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: sinkholeTTL},
				A:   a,
			})
		case dns.TypeAAAA:
			aaaa := net.IPv6zero
			if ip != nil && ip.To4() == nil {
				aaaa = ip
			}
			m.Answer = append(m.Answer, &dns.AAAA{
				Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: sinkholeTTL},
				AAAA: aaaa,
			})
		}
		s.writeReply(w, r, m)
		// Telemetria (Fase 1.2): avisa o hook com o domínio bloqueado e o IP
		// de origem — fora de qualquer lock do servidor, nunca bloqueia o
		// caminho do DNS.
		if fn := s.onBlockedProvider(); fn != nil {
			fn(domain, clientIP(w))
		}
		return
	}

	resp, err := s.forward(w, r)
	if err != nil {
		// SERVFAIL only for allowed domains: clients then fall back to the
		// router's secondary public DNS, the designed failover when this
		// machine is off or the upstream is unreachable (spec section 3.2).
		// O erro é registrado para diagnóstico: upstream inalcançável, timeout
		// ou rede do servidor caída são as causas clássicas de celulares
		// perdendo o DNS.
		log.Printf("[FocusGuard DNS] falha ao encaminhar %s (cliente %s, upstream %s): %v",
			domain, clientIP(w), s.upstream, err)
		m.Rcode = dns.RcodeServerFailure
		s.writeReply(w, r, m)
		return
	}
	clampTTL(resp)
	s.writeReply(w, r, resp)
}

// writeReply writes m to the client and logs any failure. Um erro de escrita é
// client-side (socket fechado, cliente sumiu, restart do listener) e nunca
// afeta o servidor — mas fica no log para diagnóstico de timeouts na rede.
func (s *Server) writeReply(w dns.ResponseWriter, r *dns.Msg, m *dns.Msg) {
	if err := w.WriteMsg(m); err != nil {
		domain := ""
		if len(r.Question) > 0 {
			domain = normalizeDomain(r.Question[0].Name)
		}
		log.Printf("[FocusGuard DNS] falha ao responder %s (%s): %v", domain, clientIP(w), err)
	}
}

// onBlockedProvider reads the telemetry hook under the lock — the read is
// cheap and the handler runs outside s.mu (the hook itself must never call
// back into the server).
func (s *Server) onBlockedProvider() func(string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onBlocked
}

// interceptIPProvider reads the interceptor answer IP under the lock.
func (s *Server) interceptIPProvider() net.IP {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interceptIP
}

// clientIP extracts the client's IP from the remote address (host:port). The
// IP is what the telemetry needs for per-device attribution.
func clientIP(w dns.ResponseWriter) string {
	host, _, err := net.SplitHostPort(w.RemoteAddr().String())
	if err != nil {
		return w.RemoteAddr().String()
	}
	return host
}

// recoverPanic converts a panic in a request goroutine into a logged SERVFAIL
// so one bad query can never crash the daemon.
func (s *Server) recoverPanic(w dns.ResponseWriter, r *dns.Msg) {
	if rec := recover(); rec != nil {
		log.Printf("[FocusGuard DNS] panic ao processar %v: %v", r.Question, rec)
		m := new(dns.Msg)
		m.SetReply(r)
		m.Rcode = dns.RcodeServerFailure
		s.writeReply(w, r, m)
	}
}

// forward sends the query to the upstream, matching the client's transport and
// retrying over TCP when a UDP answer is truncated.
func (s *Server) forward(w dns.ResponseWriter, r *dns.Msg) (*dns.Msg, error) {
	client := s.udpClient
	if w.RemoteAddr().Network() == "tcp" {
		client = s.tcpClient
	}

	resp, _, err := client.Exchange(r, s.upstream)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Truncated && client == s.udpClient {
		full, _, tcpErr := s.tcpClient.Exchange(r, s.upstream)
		if tcpErr == nil && full != nil {
			return full, nil
		}
		if tcpErr != nil {
			// Resposta UDP truncada E fallback TCP falhou: entrega a resposta
			// truncada mesmo assim (o cliente decide como agir) e registra a
			// falha do retry.
			domain := ""
			if len(r.Question) > 0 {
				domain = normalizeDomain(r.Question[0].Name)
			}
			log.Printf("[FocusGuard DNS] fallback TCP truncado falhou para %s: %v", domain, tcpErr)
		}
	}
	return resp, nil
}

// clampTTL caps every record's TTL at sinkholeTTL, skipping the EDNS OPT
// pseudo-record (whose TTL field is the requestor's UDP payload size).
func clampTTL(m *dns.Msg) {
	for _, section := range [][]dns.RR{m.Answer, m.Ns, m.Extra} {
		for _, rr := range section {
			if rr.Header().Rrtype == dns.TypeOPT {
				continue
			}
			if rr.Header().Ttl > sinkholeTTL {
				rr.Header().Ttl = sinkholeTTL
			}
		}
	}
}

// normalizeDomain lowercases the FQDN from the wire format and drops the
// trailing root dot, matching the lowercase names the scheduler stores.
func normalizeDomain(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

// bindHint appends the most common platform cause of a port-53 bind failure
// so the error surfaced in dns-status/doctor/UI is actionable: Internet
// Connection Sharing (SharedAccess) on Windows, systemd-resolved on Linux
// (bindhint_{windows,linux,other}.go).
func bindHint(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port != "53" {
		return ""
	}
	return platformBindHint()
}
