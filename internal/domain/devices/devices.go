// Package devices implements per-device policies for the FocusGuard Server
// edition (Fase 4 do features-plan): a persisted store of network devices
// (keyed by IP) with a policy that overrides the global sinkhole decision.
//
// Priority: a device-specific rule (block_all / allow_list) beats the global
// rule; "inherit" and unknown IPs fall through to the global decision. The
// device is keyed by IP (the primary identifier — MACs change with VPNs and
// are best-effort), matching the client IP the DNS sinkhole sees on queries.
package devices

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Policy is the per-device override. The zero value ("") means inherit.
type Policy string

const (
	// PolicyInherit: no device-specific rule — the global sinkhole decides.
	PolicyInherit Policy = "inherit"
	// PolicyBlockAll: the device is blocked from every domain, even those the
	// global rule allows.
	PolicyBlockAll Policy = "block_all"
	// PolicyAllowList: only AllowedDomains are reachable; everything else is
	// blocked for this device.
	PolicyAllowList Policy = "allow_list"
)

// Valid reports whether p is a known policy value.
func (p Policy) Valid() bool {
	switch p {
	case "", PolicyInherit, PolicyBlockAll, PolicyAllowList:
		return true
	}
	return false
}

// Device is one network device with an optional policy override.
type Device struct {
	IP   string `json:"ip"`
	MAC  string `json:"mac,omitempty"`
	Name string `json:"name,omitempty"`
	// Policy "" e "inherit" são equivalentes (regra global decide).
	Policy Policy `json:"policy,omitempty"`
	// AllowedDomains are the only domains reachable under PolicyAllowList
	// (a device rule never applies unless the policy is allow_list).
	AllowedDomains []string `json:"allowed_domains,omitempty"`
}

// Store persists the device policies to a JSON file next to the state.json
// (devices.json). A missing/corrupt file degrades to an empty catalog —
// per-device rules are an optional Server feature and must never break the
// global sinkhole.
type Store struct {
	mu      sync.Mutex
	path    string
	byIP    map[string]Device
	ordered []string // IPs em ordem de inserção, para listagem estável
}

// NewStore opens (or creates) the device catalog at path. Empty path keeps
// everything in memory (tests).
func NewStore(path string) *Store {
	s := &Store{
		path:    path,
		byIP:    make(map[string]Device),
		ordered: make([]string, 0),
	}
	if path == "" {
		return s
	}
	s.load()
	return s
}

// load reads the catalog from disk (best-effort). A corrupt file is ignored —
// the store stays empty and the next Upsert rewrites it.
func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []Device
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	for _, d := range list {
		d.IP = normalizeIP(d.IP)
		if d.IP == "" || !d.Policy.Valid() {
			continue
		}
		if _, exists := s.byIP[d.IP]; !exists {
			s.ordered = append(s.ordered, d.IP)
		}
		s.byIP[d.IP] = d
	}
}

// save persists the catalog (best-effort — a write failure is reported but
// the in-memory state stays authoritative).
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	list := make([]Device, 0, len(s.ordered))
	for _, ip := range s.ordered {
		list = append(list, s.byIP[ip])
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns the catalog in insertion order (stable for the UI table).
func (s *Store) List() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, 0, len(s.ordered))
	for _, ip := range s.ordered {
		out = append(out, s.byIP[ip])
	}
	return out
}

// Get returns the device for an IP (nil when absent). Matching is normalized
// (127.0.0.1 == 127.000.000.001 textual variants are not normalized beyond
// net.ParseIP canonical form).
func (s *Store) Get(ip string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.byIP[normalizeIP(ip)]
	return d, ok
}

// Upsert adds or replaces the device for IP. Empty/invalid IP or an unknown
// policy is rejected (the caller must not store garbage). When the device
// policy is inherit/"" the rule is inert — IsBlocked ignores it — but the
// device is still listed (the UI may want to name/annotate devices).
func (s *Store) Upsert(d Device) error {
	d.IP = normalizeIP(d.IP)
	if d.IP == "" {
		return errors.New("devices: IP inválido")
	}
	if !d.Policy.Valid() {
		return errors.New("devices: política desconhecida")
	}
	if d.Policy == PolicyAllowList && len(d.AllowedDomains) == 0 {
		return errors.New("devices: política allow_list exige ao menos um domínio permitido")
	}
	s.mu.Lock()
	if _, exists := s.byIP[d.IP]; !exists {
		s.ordered = append(s.ordered, d.IP)
	}
	s.byIP[d.IP] = d
	err := s.save()
	s.mu.Unlock()
	return err
}

// Remove deletes the device for IP (no-op when absent).
func (s *Store) Remove(ip string) error {
	ip = normalizeIP(ip)
	s.mu.Lock()
	if _, exists := s.byIP[ip]; !exists {
		s.mu.Unlock()
		return nil
	}
	delete(s.byIP, ip)
	for i, k := range s.ordered {
		if k == ip {
			s.ordered = append(s.ordered[:i], s.ordered[i+1:]...)
			break
		}
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// IsBlocked resolves the per-device override for (domain, clientIP):
//
//	blocked=true,  decided=true  → the device rule says blocked
//	blocked=false, decided=true  → the device rule says allowed
//	decided=false               → no applicable device rule (inherit/unknown)
//
// The caller (scheduler.IsBlockedFor) falls back to the global rule when
// decided is false. Matching walks parent domains so a device allowlist of
// "example.com" also permits "www.example.com".
func (s *Store) IsBlocked(domain, clientIP string) (blocked bool, decided bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.byIP[normalizeIP(clientIP)]
	if !ok {
		return false, false
	}
	switch d.Policy {
	case PolicyBlockAll:
		return true, true
	case PolicyAllowList:
		return !domainAllowed(d.AllowedDomains, domain), true
	default: // "" / inherit
		return false, false
	}
}

// domainAllowed reports whether domain equals an allowlist entry or sits under
// one (allowlist "example.com" covers "api.example.com").
func domainAllowed(allowlist []string, domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	for _, a := range allowlist {
		a = strings.ToLower(strings.TrimSuffix(a, "."))
		if domain == a || strings.HasSuffix(domain, "."+a) {
			return true
		}
	}
	return false
}

// normalizeIP canonicalizes a client IP: it strips a :port suffix when the
// caller passes host:port (the DNS server passes the bare IP, but defensive
// callers may not), trims spaces and lowercases. IPv6 addresses with a port
// ("[::1]:5353") keep the bracket form untouched — the store keys exactly what
// net.SplitHostPort would produce, matching the DNS server's clientIP().
func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	// Bracketed IPv6 ("[::1]:5353") — keep as-is.
	if strings.HasPrefix(ip, "[") {
		return ip
	}
	// IPv4-with-port: "127.0.0.1:5353" → "127.0.0.1" (the part before the
	// last colon contains a dot). Bare IPv6 ("fe80::1") has no dot before
	// the colon — leave it untouched.
	if i := strings.LastIndexByte(ip, ':'); i > 0 && strings.Contains(ip[:i], ".") {
		return ip[:i]
	}
	return ip
}
