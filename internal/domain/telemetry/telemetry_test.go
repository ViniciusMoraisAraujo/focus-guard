package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordAndReadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	r := NewRecorder(path)

	now := time.Now().UTC()
	r.Record(BlockedQuery{Domain: "youtube.com", ClientIP: "192.168.1.50", Timestamp: now})
	r.Record(BlockedQuery{Domain: "instagram.com", ClientIP: "192.168.1.60", Timestamp: now.Add(time.Second)})

	qs, err := r.Queries()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("Queries len = %d, want 2", len(qs))
	}
	if qs[0].Domain != "youtube.com" || qs[0].ClientIP != "192.168.1.50" {
		t.Errorf("query[0] = %+v, want youtube.com/192.168.1.50", qs[0])
	}
}

func TestCorruptLineIsSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	r := NewRecorder(path)
	r.Record(BlockedQuery{Domain: "good.com", ClientIP: "10.0.0.1", Timestamp: time.Now().UTC()})

	// Linha corrompida no meio (escrita truncada) + linha boa no fim.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := string(data) + "{linha corrompida\n"
	corrupt += `{"domain":"ok.com","client_ip":"10.0.0.2","timestamp":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(corrupt), 0600); err != nil {
		t.Fatal(err)
	}

	qs, err := r.Queries()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("Queries len = %d, want 2 (linha corrompida pulada)", len(qs))
	}
}

func TestRotationCapsFileSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	r := NewRecorder(path)

	// Enche o arquivo além do budget com uma query grande (a primeira escrita
	// passa — a checagem é antes do append), depois uma segunda query dispara
	// a rotação: o conteúdo anterior vai para .old e o arquivo atual recomeça
	// pequeno.
	big := strings.Repeat("x", 2*1024*1024)
	r.Record(BlockedQuery{Domain: big, ClientIP: "10.0.0.1", Timestamp: time.Now().UTC()})
	r.Record(BlockedQuery{Domain: "small.com", ClientIP: "10.0.0.2", Timestamp: time.Now().UTC()})

	if _, err := os.Stat(path + ".old"); err != nil {
		t.Errorf("esperava rotação para %s.old — arquivo: %v", path, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > MaxFileBytes {
		t.Errorf("arquivo atual %d bytes > budget %d (rotação não reduziu)", fi.Size(), MaxFileBytes)
	}

	// O .old preserva a história (a query grande) e o arquivo atual só tem a
	// segunda query. Queries concatena os dois (.old antes — cronológico), então
	// o histórico rotacionado NÃO some da UI (pendência INFO do
	// verification-plan: antes ficava invisível até a purga do boot).
	qs, err := r.Queries()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 || qs[0].Domain != big || qs[1].Domain != "small.com" {
		t.Errorf("Queries pós-rotação = %d entradas (%s, %s), want [big, small.com] (histórico do .old + atual)",
			len(qs), qs[0].Domain, qs[1].Domain)
	}
}

// TestQueries_ReadsRotatedOldAlone cobre o estado intermediário: o .old
// existente sem o arquivo atual (rotação manual / teste), o histórico do .old
// continua legível.
func TestQueries_ReadsRotatedOldAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	old := path + ".old"
	now := time.Now().UTC()
	data := `{"domain":"rotated.com","client_ip":"10.0.0.9","timestamp":"` + now.Format(time.RFC3339) + `"}` + "\n"
	if err := os.WriteFile(old, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	r := NewRecorder(path)
	qs, err := r.Queries()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 || qs[0].Domain != "rotated.com" {
		t.Errorf("Queries = %+v, want [rotated.com] (lido do .old)", qs)
	}
}

func TestInMemoryRecorder(t *testing.T) {
	r := NewRecorder("") // sem caminho → memória
	r.Record(BlockedQuery{Domain: "a.com", ClientIP: "10.0.0.1", Timestamp: time.Now().UTC()})
	r.Record(BlockedQuery{Domain: "b.com", ClientIP: "10.0.0.2", Timestamp: time.Now().UTC()})

	qs, err := r.Queries()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("Queries len = %d, want 2 (memória)", len(qs))
	}
}

func TestRecentOrdersByTimestampDesc(t *testing.T) {
	base := time.Now().UTC()
	qs := []BlockedQuery{
		{Domain: "old.com", Timestamp: base},
		{Domain: "new.com", Timestamp: base.Add(10 * time.Minute)},
		{Domain: "mid.com", Timestamp: base.Add(5 * time.Minute)},
	}
	recent := Recent(qs, 2)
	if len(recent) != 2 {
		t.Fatalf("Recent len = %d, want 2", len(recent))
	}
	if recent[0].Domain != "new.com" || recent[1].Domain != "mid.com" {
		t.Errorf("Recent = [%s, %s], want [new.com, mid.com]", recent[0].Domain, recent[1].Domain)
	}
	if got := Recent(qs, 0); len(got) != 0 {
		t.Errorf("Recent(limit=0) = %d, want 0", len(got))
	}
}

func TestSummarizeAggregatesByDomain(t *testing.T) {
	now := time.Now().UTC()
	qs := []BlockedQuery{
		{Domain: "youtube.com", ClientIP: "192.168.1.50", Timestamp: now},
		{Domain: "youtube.com", ClientIP: "192.168.1.50", Timestamp: now.Add(time.Minute)},
		{Domain: "youtube.com", ClientIP: "192.168.1.99", Timestamp: now.Add(2 * time.Minute)},
		{Domain: "twitter.com", ClientIP: "192.168.1.60", Timestamp: now},
	}
	sum := Summarize(qs)
	if len(sum) != 2 {
		t.Fatalf("Summarize len = %d, want 2", len(sum))
	}
	// Ordenado por contagem desc.
	if sum[0].Domain != "youtube.com" || sum[0].Count != 3 {
		t.Errorf("sum[0] = %+v, want youtube.com count 3", sum[0])
	}
	if len(sum[0].LastIPs) != 2 {
		t.Errorf("LastIPs len = %d, want 2 (dedup)", len(sum[0].LastIPs))
	}
	if sum[1].Domain != "twitter.com" || sum[1].Count != 1 {
		t.Errorf("sum[1] = %+v, want twitter.com count 1", sum[1])
	}
}

func TestMissingFileYieldsEmpty(t *testing.T) {
	r := NewRecorder(filepath.Join(t.TempDir(), "nope.jsonl"))
	qs, err := r.Queries()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 0 {
		t.Errorf("Queries = %d, want 0 para arquivo ausente", len(qs))
	}
}
