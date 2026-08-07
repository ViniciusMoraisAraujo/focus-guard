// GET /api/metrics (Fase 8 — C3): observabilidade de latência por ação. O
// endpoint devolve dois agregados: "ipc" (o snapshot do daemon, via a ação IPC
// "metrics" — a mesma que o CLI `focusguard metrics` lê) e "http" (a latência
// do próprio proxy web, medida no handleAction). Ler os dois no mesmo lugar
// mostra onde está a latência: no daemon (resolver DNS, firewall) ou no
// round-trip do proxy.
package httpapi

import (
	"net/http"
	"time"

	"focusguard/internal/transport/ipc"
	"focusguard/internal/transport/metrics"
)

// slowProxyThreshold é quando o proxy loga uma linha estruturada (Fase 8) —
// mesma ordem do threshold do daemon (bloqueios com resolver lento passam de
// 1s de forma legítima; é exatamente isso que o log quer capturar).
const slowProxyThreshold = 1 * time.Second

// handleMetrics devolve { ipc, http } com os agregados de latência por ação.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	resp, err := s.client.SendWithTimeout(ipc.Request{Action: "metrics"}, proxyTimeout)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"daemon indisponível — verifique se o serviço FocusGuard está rodando")
		return
	}
	ipcStats := resp.Metrics
	if ipcStats == nil {
		ipcStats = []metrics.ActionStat{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ipc":  ipcStats,
		"http": s.metrics.Snapshot(),
	})
}
