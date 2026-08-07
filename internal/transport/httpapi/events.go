// Eventos em tempo real (Fase 7): GET /api/events é um stream SSE que relê o
// long-poll IPC event-subscribe do daemon e entrega as mudanças de estado ao
// navegador — no lugar do polling do frontend. O web é o único subscriber
// (uma conexão por aba); o daemon bloqueia até publicar algo ou estourar o
// orçamento interno (20s), então o loop escreve um keepalive comentado por
// ciclo quieto para manter a conexão viva através de proxies.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"focusguard/internal/transport/ipc"
)

// eventPollMargin cobre a latência de ida-e-volta do IPC por cima do orçamento
// interno do daemon (o Timeout do spec event-subscribe), para o long-poll
// nunca estourar do lado do cliente enquanto o daemon ainda está dentro do
// próprio orçamento.
const eventPollMargin = 5 * time.Second

// handleEvents streams daemon state-change events as Server-Sent Events. The
// stream is authenticated like /api/action (the EventSource carries the same
// session cookie — same origin). A daemon failure closes the stream with an
// error event; the browser's EventSource auto-reconnects and, when the daemon
// is back, the loop restarts with since=0 and catches up from the hub ring.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming não suportado")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// X-Accel-Buffering: nginx e proxies locais não devem bufferizar o stream.
	w.Header().Set("X-Accel-Buffering", "no")
	fl.Flush()

	spec, ok := ipc.SpecFor("event-subscribe")
	if !ok {
		// Defensivo (drift): usa o orçamento interno conhecido do daemon
		// (eventSubscribeTimeout=20s) + margem, para não dar timeout
		// prematuro no long-poll.
		spec = ipc.ActionSpec{Timeout: 25 * time.Second}
	}
	pollTimeout := spec.Timeout + eventPollMargin

	// Na reconexão automática o browser manda o Last-Event-ID (o rev do último
	// lote entregue) — o loop retoma dali em vez de reentregar o ring inteiro.
	since := int64(0)
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			since = n
		}
	}

	for {
		resp, err := s.client.SendWithTimeout(ipc.Request{Action: "event-subscribe", Since: since}, pollTimeout)
		if err != nil {
			// Daemon fora do ar (ou falha de IPC): avisa o cliente e encerra —
			// o EventSource reconecta sozinho e o loop recomeça com since=0.
			fmt.Fprint(w, "event: error\ndata: daemon indisponível\n\n")
			fl.Flush()
			return
		}
		for _, ev := range resp.Events {
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", ev.Type, resp.Rev, data)
		}
		if len(resp.Events) == 0 {
			// Ciclo quieto (orçamento expirou sem mudanças): o comentário
			// mantém a conexão viva (proxies cortam conexões ociosas).
			fmt.Fprint(w, ": keepalive\n\n")
		}
		since = resp.Rev
		fl.Flush()

		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}
