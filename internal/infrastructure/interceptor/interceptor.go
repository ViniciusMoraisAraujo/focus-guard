// Package interceptor serves the Focus Interceptor Page (Fase 3 do
// features-plan): when a user opens a blocked domain, instead of "connection
// refused" or a dead address they see a page explaining the block — which
// domain, why, how long is left — with a motivational phrase to turn the
// interruption into encouragement.
//
// How it works: with the feature enabled, the hosts file points blocked
// domains at 127.0.0.1 (desktop) and/or the DNS sinkhole answers with the
// server's local IP (server edition). Either way, the browser connects to
// port 80 on this machine, where this listener answers. The listener is
// best-effort: a busy port 80 (Apache/IIS/nginx) must never take down the
// daemon — the block keeps working, only the page is missing.
package interceptor

import (
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultBindAddr is where the daemon binds the listener: loopback in desktop
// mode, 0.0.0.0 in server edition (the daemon chooses and passes it).
const DefaultBindAddr = "0.0.0.0:80"

// Checker decides whether a requested host is blocked (satisfied by
// *scheduler.Scheduler.IsBlocked). The page is served only for blocked hosts.
type Checker interface {
	IsBlocked(domain string) bool
	// BlockRemaining returns the remaining time of the active block for
	// domain (0 when not blocked) — the page shows how long is left.
	BlockRemaining(domain string) time.Duration
}

// Page is the data injected into the template: the blocked domain, the
// remaining time and a motivational phrase.
type Page struct {
	Domain    string
	Remaining time.Duration
	Quote     string
}

// motivationalQuotes é a lista de frases motivacionais exibidas na página de
// bloqueio. A frase é escolhida deterministicamente pelo domínio (hash), então
// o mesmo site sempre mostra a mesma frase — reconhecível e pessoal.
var motivationalQuotes = []string{
	"Foco é dizer não para o que você quer agora, para ter o que você quer depois.",
	"Cada vez que você resiste à distração, você fica mais forte.",
	"O sucesso é a soma de pequenos esforços repetidos dia após dia.",
	"Disciplina é a ponte entre seus objetivos e suas conquistas.",
	"Não é sobre ter tempo. É sobre fazer tempo.",
	"Você não precisa de motivação. Você precisa de consistência.",
	"A mente que se abre a uma nova ideia jamais volta ao seu tamanho original.",
	"Grandes coisas nunca vêm da zona de conforto.",
	"Seu futuro eu está torcendo por você agora mesmo.",
	"Uma hora de foco vale mais que um dia de distração.",
	"Proteja seu tempo como se ele fosse seu recurso mais valioso — porque é.",
	"Você já chegou longe. Não pare agora por causa de um clique.",
	"A força não vem da capacidade física, mas de uma vontade inabalável.",
	"Concentre-se no que importa: o resto pode esperar.",
	"Hoje é um bom dia para construir o amanhã que você sonha.",
	"Quem foca, vence. Quem insiste, alcança.",
	"Distração é fácil. Foco é uma escolha poderosa.",
	"Respire fundo. Volte ao que importa. Você consegue.",
	"Cada momento de foco é um tijolo no prédio dos seus sonhos.",
	"O agora é onde o futuro é construído.",
}

// Server is the HTTP listener for the interceptor page. Zero-value is not
// usable; construct with New.
type Server struct {
	checker Checker

	mu   sync.Mutex
	ln   net.Listener
	srv  *http.Server
	addr string
}

// New builds an interceptor server consulting checker (nil checker = page
// never matches, the listener still answers 404).
func New(checker Checker) *Server {
	return &Server{checker: checker}
}

// Start binds addr (default DefaultBindAddr) and begins serving. It is
// synchronous: it returns as soon as the listener is bound, so a bind error
// (port 80 occupied) surfaces immediately for the daemon to log and continue
// (best-effort — the block never depends on this listener). Not idempotent.
func (s *Server) Start(addr string) error {
	if addr == "" {
		addr = DefaultBindAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("interceptor: bind %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	s.mu.Lock()
	if s.ln != nil {
		s.mu.Unlock()
		_ = ln.Close()
		return fmt.Errorf("interceptor: já iniciado")
	}
	s.ln = ln
	s.srv = srv
	s.addr = ln.Addr().String()
	s.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed && s.isRunning() {
			log.Printf("[FocusGuard Interceptor] erro no listener: %v", err)
		}
	}()
	return nil
}

// isRunning reports whether srv is still the active server (false once Stop
// replaced it) — suppresses the expected "closed" error on shutdown.
func (s *Server) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srv != nil
}

// Stop closes the listener. Idempotent.
func (s *Server) Stop() error {
	s.mu.Lock()
	srv, ln := s.srv, s.ln
	s.srv, s.ln = nil, nil
	s.addr = ""
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
	if ln != nil {
		_ = ln.Close()
	}
	return nil
}

// Addr returns the bound address ("" when stopped).
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// handleRequest serves the interceptor page for blocked hosts and 404s
// everything else. The Host header carries the domain the browser tried to
// reach (blocked domains resolve here).
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	host := hostOnly(r.Host)
	if s.checker == nil || !s.checker.IsBlocked(host) {
		http.NotFound(w, r)
		return
	}
	page := Page{
		Domain:    host,
		Remaining: s.checker.BlockRemaining(host),
		Quote:     quoteFor(host),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
	if err := pageTemplate.Execute(w, page); err != nil {
		log.Printf("[FocusGuard Interceptor] erro ao renderizar página: %v", err)
	}
}

// hostOnly strips the port from a Host header (example.com:8080 → example.com).
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(hostport)
}

// quoteFor picks the motivational phrase deterministically from the domain —
// the same blocked site always shows the same phrase, stable across visits.
func quoteFor(domain string) string {
	if len(motivationalQuotes) == 0 {
		return ""
	}
	h := 0
	for _, c := range domain {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return motivationalQuotes[h%len(motivationalQuotes)]
}

// pageTemplate é o template da página de bloqueio — autossuficiente (CSS
// inline), escapa todos os dados dinâmicos (html/template).
var pageTemplate = template.Must(template.New("blocked").Parse(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Bloqueado pelo FocusGuard</title>
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; margin: 0; }
  body {
    font-family: system-ui, -apple-system, sans-serif;
    min-height: 100vh; display: grid; place-items: center;
    background: radial-gradient(1200px 600px at 50% -10%, #1d2b53, #0b1020);
    color: #e6e9f5; padding: 1rem;
  }
  .card {
    max-width: 520px; width: 100%;
    background: rgba(21, 29, 56, .85); border: 1px solid #232c4d;
    border-radius: 18px; padding: 2.5rem 2rem; text-align: center;
    box-shadow: 0 24px 60px rgba(0,0,0,.45);
  }
  .shield {
    width: 64px; height: 64px; margin: 0 auto 1.25rem;
    display: grid; place-items: center;
    background: #ff7849; color: #fff; font-size: 2rem;
    border-radius: 18px;
  }
  h1 { font-size: 1.35rem; margin-bottom: .5rem; }
  .domain {
    display: inline-block; margin: .5rem 0 1rem;
    padding: .3rem .8rem; border-radius: 999px;
    background: rgba(255,255,255,.08); border: 1px solid rgba(255,255,255,.15);
    font-family: ui-monospace, monospace; font-size: .95rem; word-break: break-all;
  }
  .reason { color: #aab2cf; font-size: .95rem; line-height: 1.55; margin-bottom: 1.5rem; }
  .reason strong { color: #e6e9f5; }
  .quote {
    font-style: italic; color: #ffb48a; font-size: 1.02rem; line-height: 1.6;
    padding: 1rem 0; border-top: 1px dashed rgba(255,255,255,.15);
    border-bottom: 1px dashed rgba(255,255,255,.15); margin-bottom: 1.5rem;
  }
  .back {
    display: inline-block; padding: .7rem 1.4rem; border-radius: 10px;
    background: #2d5f9a; color: #fff; text-decoration: none; font-weight: 600;
    transition: background .2s, transform .15s;
  }
  .back:hover { background: #3a74b8; transform: translateY(-1px); }
  .foot { margin-top: 1.25rem; font-size: .8rem; color: #6f7aa3; }
</style>
</head>
<body>
  <div class="card">
    <div class="shield">🛡</div>
    <h1>Momento de foco</h1>
    <span class="domain">{{.Domain}}</span>
    <p class="reason">
      Este site está <strong>bloqueado pelo FocusGuard</strong> até
      <strong>{{.Remaining}}</strong> de descanso da distração.
    </p>
    <p class="quote">“{{.Quote}}”</p>
    <a class="back" href="javascript:history.back()">← Voltar para o que importa</a>
    <p class="foot">Bloqueio ativo do FocusGuard · proteja seu foco</p>
  </div>
</body>
</html>`))
