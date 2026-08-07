package ipc

import (
	"testing"

	"focusguard/internal/transport/metrics"
)

// TestMetrics_NotConfigured: sem registry (tests/dev) a ação falha com o
// código estável, como as demais ações de servidor.
func TestMetrics_NotConfigured(t *testing.T) {
	s := setupTestServer(t)
	resp := executeRequest(t, s, Request{Action: "metrics"})
	if resp.Success || resp.Code != CodeNotConfigured {
		t.Fatalf("esperava não configurado, got %+v", resp)
	}
}

// TestMetrics_SnapshotAndReset: com registry, a ação devolve o snapshot e o
// Reset zera antes do snapshot (janela de medição do CLI).
func TestMetrics_SnapshotAndReset(t *testing.T) {
	s := setupTestServer(t)
	reg := metrics.New(16)
	s.SetMetrics(reg)

	// Dispatches de ping (e desconhecida) são registrados pelo próprio server.
	_ = executeRequest(t, s, Request{Action: "ping"})
	_ = executeRequest(t, s, Request{Action: "ping"})

	resp := executeRequest(t, s, Request{Action: "metrics"})
	if !resp.Success || len(resp.Metrics) == 0 {
		t.Fatalf("esperava snapshot, got %+v", resp)
	}
	var ping *metrics.ActionStat
	for i := range resp.Metrics {
		if resp.Metrics[i].Action == "ping" {
			ping = &resp.Metrics[i]
		}
	}
	if ping == nil || ping.Count < 2 {
		t.Fatalf("ping ausente ou contagem baixa: %+v", resp.Metrics)
	}

	// Reset zera o registro — o snapshot seguinte fica vazio (ou só com a
	// própria chamada de metrics, que ainda não foi medida... é: a ação
	// metrics também é medida no dispatch, mas o Reset roda antes do snapshot).
	resp2 := executeRequest(t, s, Request{Action: "metrics", Reset: true})
	if !resp2.Success {
		t.Fatalf("reset falhou: %+v", resp2)
	}
	if len(resp2.Metrics) != 0 {
		t.Fatalf("após Reset esperava snapshot vazio, got %+v", resp2.Metrics)
	}
}

// TestMetrics_RecordsDispatchLatency: o Dispatch mede toda ação conhecida
// (exceto event-subscribe) — o snapshot reflete chamadas feitas ANTES da
// leitura.
func TestMetrics_RecordsDispatchLatency(t *testing.T) {
	s := setupTestServer(t)
	reg := metrics.New(16)
	s.SetMetrics(reg)

	for i := 0; i < 3; i++ {
		_ = executeRequest(t, s, Request{Action: "status"})
	}
	_ = executeRequest(t, s, Request{Action: "event-subscribe", Since: 0}) // long-poll: não medido

	resp := executeRequest(t, s, Request{Action: "metrics"})
	if !resp.Success {
		t.Fatalf("metrics falhou: %+v", resp)
	}
	for _, st := range resp.Metrics {
		if st.Action == "event-subscribe" {
			t.Error("event-subscribe não deveria ser medido (long-poll por design)")
		}
		if st.Action == "status" && st.Count != 3 {
			t.Errorf("status count = %d, want 3", st.Count)
		}
	}
}
