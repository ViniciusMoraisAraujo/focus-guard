// Package telemetry records the DNS sinkhole's blocked queries (domain + client
// IP + timestamp) as an append-only JSONL file next to the state, and answers
// the aggregation queries the "dns-telemetry" IPC action serves. Only blocked
// queries are logged (the volume is low; allowed queries are noise), so the
// log doubles as an accountability trail: what was cut off, and from where.
// Corrupt or partial lines are skipped on read, mirroring analytics — a torn
// write never aborts a request. The file is capped and rotated: a write that
// would push it past the byte budget rotates it to <name>.old and starts
// fresh, so a scan/attack cannot grow the log unbounded. Queries reads both
// the live file and the rotated <name>.old (in that order), so the history
// stays visible until the daily purge.
package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxFileBytes caps the JSONL size before rotation (~10k lines at ~100B each).
// Exported so the daemon and tests share the same budget.
const MaxFileBytes = 1 << 20 // 1 MiB

// BlockedQuery is one sinkholed DNS request, recorded by the dnsserver hook.
type BlockedQuery struct {
	Domain    string    `json:"domain"`
	ClientIP  string    `json:"client_ip"`
	Timestamp time.Time `json:"timestamp"`
}

// Recorder appends blocked queries to a JSONL file. An empty path keeps the
// recorder in memory (tests and daemon fallback). All methods are safe for
// concurrent use: the DNS server runs one goroutine per request.
type Recorder struct {
	mu     sync.Mutex
	path   string
	memory []BlockedQuery
}

// NewRecorder returns a Recorder writing to path, or an in-memory recorder
// when path is empty.
func NewRecorder(path string) *Recorder {
	return &Recorder{path: path}
}

// Record appends one blocked query to the file (or memory). Best-effort: a
// failed append or rotation is silently dropped — the sinkhole must never
// depend on the telemetry file (spec: never block the DNS path on telemetry
// failure).
func (r *Recorder) Record(q BlockedQuery) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.path == "" {
		r.memory = append(r.memory, q)
		return
	}

	data, err := json.Marshal(q)
	if err != nil {
		return
	}

	if fi, err := os.Stat(r.path); err == nil && fi.Size() > MaxFileBytes {
		r.rotateLocked()
	}

	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// rotateLocked moves the current file to <name>.old, replacing any previous
// .old, so the live log restarts small. Best-effort. The caller holds r.mu.
func (r *Recorder) rotateLocked() {
	old := r.path + ".old"
	_ = os.Remove(old)
	_ = os.Rename(r.path, old)
}

// Queries returns every blocked query read from the file (or memory), skipping
// corrupt lines. A missing file yields an empty list without error. After a
// rotation, the <name>.old history is included BEFORE the live file
// (chronological) — a rotation never hides history from the UI until the
// daily purge (pendência INFO do docs/verification-plan.md).
func (r *Recorder) Queries() ([]BlockedQuery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.path == "" {
		return append([]BlockedQuery(nil), r.memory...), nil
	}

	// O .old é auxiliar e descartável (purga diária no boot): um erro de
	// leitura nele (ex.: permissão) não pode esconder o arquivo atual — a
	// telemetria é best-effort por design. O erro do arquivo ATUAL, sim, é
	// propagado.
	old, _ := readJSONL(r.path + ".old")
	live, err := readJSONL(r.path)
	if err != nil {
		return nil, err
	}
	return append(old, live...), nil
}

// maxJSONLLine é o teto de bytes de uma linha de telemetria (4 MiB). Linhas
// reais são minúsculas — um nome DNS tem ≤ 255 bytes no fio — então o teto só
// existe para o splitter PULAR uma linha monstruosa (tamper/corrupção do
// arquivo) em vez de abortar a leitura inteira com ErrTooLong.
const maxJSONLLine = 4 << 20

// splitJSONLLine é o bufio.SplitFunc da leitura: isola linhas terminadas em
// '\n' e pula POR INTEIRO qualquer linha maior que maxJSONLLine (a próxima
// chamada recomeça depois dela) — a política de "linha corrompida nunca
// aborta a leitura" vale também para o tamanho.
func splitJSONLLine(data []byte, atEOF bool) (int, []byte, error) {
	// Fim da entrada (mesmo contrato do ScanLines): sem dados, encerra — um
	// token vazio NÃO-nil aqui faria o Scanner devolver true para sempre.
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		if i > maxJSONLLine {
			return i + 1, nil, nil
		}
		return i + 1, data[:i], nil
	}
	// Sem newline no bloco atual: se o buffer já estourou o teto, a linha é
	// gigante — consome o bloco e segue (o restante dela também será pulado).
	if len(data) >= maxJSONLLine {
		return len(data), nil, nil
	}
	if atEOF {
		return len(data), data, nil // última linha sem '\n' final
	}
	return 0, nil, nil // precisa de mais dados
}

// readJSONL lê um arquivo JSONL de telemetria pulando linhas corrompidas,
// parciais ou monstruosas (uma escrita truncada ou um arquivo adulterado nunca
// aborta a leitura). Um arquivo ausente é vazio sem erro — o .old nem sempre
// existe.
func readJSONL(path string) ([]BlockedQuery, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []BlockedQuery
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxJSONLLine)
	sc.Split(splitJSONLLine)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var q BlockedQuery
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			continue // linha corrompida/parcial — nunca aborta a leitura
		}
		out = append(out, q)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Recent returns up to limit queries ordered by Timestamp descending (newest
// first). A non-positive limit yields an empty slice.
func Recent(queries []BlockedQuery, limit int) []BlockedQuery {
	if limit <= 0 {
		return []BlockedQuery{}
	}
	sorted := append([]BlockedQuery(nil), queries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// Summary aggregates blocked queries per domain: count, last client IPs (up
// to 5, deduplicated, most recent first) and the most recent timestamp. The
// panel's "Atividade bloqueada" screen renders this.
type Summary struct {
	Domain      string    `json:"domain"`
	Count       int       `json:"count"`
	LastIPs     []string  `json:"last_ips"`
	LastBlocked time.Time `json:"last_blocked,omitempty"`
}

// Summarize aggregates queries grouped by domain, sorted by count descending.
func Summarize(queries []BlockedQuery) []Summary {
	byDomain := make(map[string]*Summary)
	var order []string
	for _, q := range queries {
		s, ok := byDomain[q.Domain]
		if !ok {
			s = &Summary{Domain: q.Domain}
			byDomain[q.Domain] = s
			order = append(order, q.Domain)
		}
		s.Count++
		if q.Timestamp.After(s.LastBlocked) {
			s.LastBlocked = q.Timestamp
		}
		s.LastIPs = appendUnique(s.LastIPs, q.ClientIP)
		if len(s.LastIPs) > 5 {
			s.LastIPs = s.LastIPs[:5]
		}
	}
	out := make([]Summary, 0, len(order))
	for _, d := range order {
		out = append(out, *byDomain[d])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Domain < out[j].Domain
	})
	return out
}

// appendUnique appends v to s when absent (case-sensitive — IPs are canonical).
func appendUnique(s []string, v string) []string {
	if v == "" {
		return s
	}
	for _, e := range s {
		if e == v {
			return s
		}
	}
	return append(s, v)
}

// FormatEntry renders a human line for the CLI (not wired yet — the UI owns
// the telemetry surface; kept for tests and future CLI parity).
func FormatEntry(q BlockedQuery) string {
	return fmt.Sprintf("%s  %s  %s", q.Timestamp.Local().Format("2006-01-02 15:04:05"), q.ClientIP, q.Domain)
}

// PurgeOld clears rotated logs older than a day next to the live file — called
// at daemon boot (best-effort) so old .old files never accumulate.
func (r *Recorder) PurgeOld() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path == "" {
		return
	}
	old := r.path + ".old"
	if fi, err := os.Stat(old); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
		_ = os.Remove(old)
	}
	// Remove arquivos <name>.old.N antigos (rotação manual/teste) — nunca o
	// .old único atual.
	matches, _ := filepath.Glob(r.path + ".old.*")
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
			_ = os.Remove(m)
		}
	}
}
