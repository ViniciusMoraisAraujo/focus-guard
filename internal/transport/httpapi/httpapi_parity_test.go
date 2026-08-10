package httpapi

// Etapa 5 do bug-hunt — paridade de timeouts e isolamento de métricas. A
// cadeia de orçamentos tem duas pontas: o spec (tabela declarativa B7, que o
// proxy consome) deve ser ≥ o orçamento interno do daemon (trava em
// internal/transport/ipc — TestSpec_ProxyBudgetAtLeastDaemonInternal), e o
// proxy deve usar EXATAMENTE o Timeout do spec — nunca um atalho com o
// proxyTimeout curto, senão um daemon lento-mas-bem-sucedido vira "daemon
// indisponível" falso.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"focusguard/internal/transport/ipc"
	"focusguard/internal/transport/metrics"
)

// TestAction_ProxyUsesSpecTimeoutForEveryAction é a paridade de timeouts do
// critério de saída, tabelada sobre TODAS as ações encaminháveis: o /api/action
// usa exatamente o Timeout da tabela de specs para cada uma. Uma regressão que
// fizesse qualquer ação cair no proxyTimeout (5s) — ex.: uma ação nova ganhar
// spec de 30s mas o handler continuar com timeout curto — quebraria aqui.
// event-subscribe é excluído: ele não passa pelo proxy (o loop SSE fala direto
// com o client e tem paridade própria, TestEvents_PollTimeoutExactParity).
func TestAction_ProxyUsesSpecTimeoutForEveryAction(t *testing.T) {
	for _, action := range ipc.SpecActions() {
		if action == "event-subscribe" {
			continue
		}
		t.Run(action, func(t *testing.T) {
			sc := &stubClient{}
			srv, h := newTestServer(sc, uiFS())
			cookie := adminCookie(t, srv)
			rec := doJSON(t, h, cookie, "POST", "/api/action", "application/json",
				`{"action":"`+action+`"}`, "127.0.0.1:48902")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
			}
			if sc.lastReq.Action != action {
				t.Fatalf("request não repassado: %+v", sc.lastReq)
			}
			spec, _ := ipc.SpecFor(action)
			if sc.withTimeout != spec.Timeout {
				t.Errorf("timeout = %v, want spec %v", sc.withTimeout, spec.Timeout)
			}
		})
	}
}

// TestPing_UsesProxyTimeout_ParityWithSpec congela a paridade da const com a
// tabela: o ping (sinal de conectividade do CLI/web) usa proxyTimeout, que
// precisa ser o MESMO do spec do ping — se um dia o spec mudar sem a const
// (ou vice-versa), o teste quebra.
func TestPing_UsesProxyTimeout_ParityWithSpec(t *testing.T) {
	if proxyTimeout != specTimeout(t, "ping") {
		t.Fatalf("proxyTimeout=%v deve ser igual ao spec de ping (%v)", proxyTimeout, specTimeout(t, "ping"))
	}
	sc := &stubClient{}
	_, h := newTestServer(sc, uiFS())
	rec := doJSON(t, h, nil, "GET", "/api/ping", "", "", "127.0.0.1:48902")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if sc.withTimeout != proxyTimeout {
		t.Errorf("ping timeout = %v, want proxyTimeout %v", sc.withTimeout, proxyTimeout)
	}
}

// TestEvents_PollTimeoutExactParity: o timeout do poll SSE é exatamente o
// orçamento do spec + a margem de ida-e-volta do IPC (eventPollMargin) — nem
// mais (proxies ociosos), nem menos (falso "daemon indisponível" a cada
// heartbeat). O teste existente (TestEventsStreamsAndEchoesRev) só garante ≥;
// este congela a fórmula exata.
func TestEvents_PollTimeoutExactParity(t *testing.T) {
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		return nil, errors.New("done") // encerra após o primeiro poll
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)
	doJSON(t, h, cookie, "GET", "/api/events", "", "", "127.0.0.1:48902")

	want := specTimeout(t, "event-subscribe") + eventPollMargin
	if sc.withTimeout != want {
		t.Errorf("poll timeout = %v, want spec+margin %v", sc.withTimeout, want)
	}
}

// TestMetrics_EventSubscribeExcluded: o long-poll de eventos nunca entra no
// registro de latência do proxy — nem quando o /api/action o encaminha (a ação
// tem spec). Um long-poll de 20s não é sinal de regressão (é o desenho), então
// medi-lo poluiria o /api/metrics. O POST de ping é o controle positivo: prova
// que o registro funciona e que só a exclusão está sendo verificada.
func TestMetrics_EventSubscribeExcluded(t *testing.T) {
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		if req.Action == "metrics" {
			return &ipc.Response{Success: true, Metrics: []metrics.ActionStat{}}, nil
		}
		return &ipc.Response{Success: true}, nil
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	for _, action := range []string{"ping", "event-subscribe"} {
		rec := doJSON(t, h, cookie, "POST", "/api/action", "application/json",
			`{"action":"`+action+`"}`, "127.0.0.1:48902")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", action, rec.Code)
		}
	}

	rec := doJSON(t, h, cookie, "GET", "/api/metrics", "", "", "127.0.0.1:48902")
	var body struct {
		HTTP []metrics.ActionStat `json:"http"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("metrics não é JSON: %v", err)
	}
	foundPing := false
	for _, st := range body.HTTP {
		if st.Action == "event-subscribe" {
			t.Fatalf("event-subscribe não deveria aparecer no registro http: %+v", body.HTTP)
		}
		if st.Action == "ping" && st.Count >= 1 {
			foundPing = true
		}
	}
	if !foundPing {
		t.Fatalf("controle positivo ausente: ping deveria estar no registro http: %+v", body.HTTP)
	}
}

// TestMetrics_DaemonResetDoesNotClearHTTPRegistry: o "metrics --reset" do CLI
// zera o registro DO DAEMON (o snapshot ipc volta vazio) — o registro local do
// proxy web vive em outro processo e não pode ser afetado. O endpoint devolve
// os dois lados no mesmo payload; este teste garante que o lado http sobrevive
// a um reset do lado ipc.
func TestMetrics_DaemonResetDoesNotClearHTTPRegistry(t *testing.T) {
	daemonReset := false
	sc := &stubClient{fn: func(req ipc.Request) (*ipc.Response, error) {
		if req.Action == "metrics" {
			if daemonReset {
				return &ipc.Response{Success: true, Metrics: []metrics.ActionStat{}}, nil
			}
			return &ipc.Response{Success: true, Metrics: []metrics.ActionStat{
				{Action: "block", Count: 1},
			}}, nil
		}
		return &ipc.Response{Success: true}, nil
	}}
	srv, h := newTestServer(sc, uiFS())
	cookie := adminCookie(t, srv)

	// Dispara um proxy para o registro local ter uma amostra.
	doJSON(t, h, cookie, "POST", "/api/action", "application/json",
		`{"action":"ping"}`, "127.0.0.1:48902")

	hasLocalPing := func(t *testing.T) bool {
		t.Helper()
		rec := doJSON(t, h, cookie, "GET", "/api/metrics", "", "", "127.0.0.1:48902")
		var body struct {
			HTTP []metrics.ActionStat `json:"http"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("metrics não é JSON: %v", err)
		}
		for _, st := range body.HTTP {
			if st.Action == "ping" && st.Count >= 1 {
				return true
			}
		}
		return false
	}

	if !hasLocalPing(t) {
		t.Fatal("registro local deveria ter ping antes do reset do daemon")
	}
	// Outro cliente (CLI) reseta o registro do daemon entre as leituras.
	daemonReset = true
	if !hasLocalPing(t) {
		t.Fatal("reset do registro do daemon zerou o registro local do proxy web")
	}
}
