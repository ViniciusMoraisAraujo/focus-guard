package ipc

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// --- nil registry (Server sem registry — dev build / boot parcial) ---

// TestDispatch_NilRegistry_FallsBackToUnknownAction: o Dispatch com registry
// nil (zero value do Server) não pode panicar — cai no fallback legado com
// CodeUnknownAction (comportamento documentado em Dispatch).
func TestDispatch_NilRegistry_FallsBackToUnknownAction(t *testing.T) {
	s := &Server{} // zero value: registry nil
	resp := s.Dispatch(&Request{Action: "ping"})
	if resp.Success || resp.Code != CodeUnknownAction {
		t.Fatalf("esperava CodeUnknownAction com registry nil, got %+v", resp)
	}
	if resp.Message != "Not supported action: ping" {
		t.Errorf("mensagem legada divergiu: %q", resp.Message)
	}
}

// TestHandleConnection_NilRegistry_StillResponds: no wire, um Server sem
// registry responde (e não trava a conexão).
func TestHandleConnection_NilRegistry_StillResponds(t *testing.T) {
	s := &Server{}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go s.handleConnection(serverConn)

	if err := json.NewEncoder(clientConn).Encode(Request{Action: "status"}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp Response
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("sem resposta com registry nil: %v", err)
	}
	if resp.Success || resp.Code != CodeUnknownAction {
		t.Fatalf("esperava CodeUnknownAction, got %+v", resp)
	}
}

// --- ação vazia ---

func TestDispatch_EmptyAction_FallsBackToUnknownAction(t *testing.T) {
	s := setupTestServer(t)
	resp := s.Dispatch(&Request{Action: ""})
	if resp.Success || resp.Code != CodeUnknownAction {
		t.Fatalf("esperava CodeUnknownAction para ação vazia, got %+v", resp)
	}
	if resp.Message != "Not supported action: " {
		t.Errorf("mensagem para ação vazia: %q", resp.Message)
	}
}

func TestHandleConnection_EmptyAction(t *testing.T) {
	s := setupTestServer(t)
	resp := executeRequest(t, s, Request{Action: ""})
	if resp.Success || resp.Code != CodeUnknownAction || resp.Message != "Not supported action: " {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
}

// --- handler que devolve (nil, nil) — edge flagrado no review da Fase 8 ---

// TestDispatch_HandlerNilNil_FallsBackToUnknownAction: um handler registrado
// que devolve (nil, nil) de Handle deixa o resp nil e o roteador cai no
// fallback legado (o switch antigo encodaria JSON "null"). Nenhum handler real
// faz isso hoje — o teste congela o comportamento para o caso aparecer.
func TestDispatch_HandlerNilNil_FallsBackToUnknownAction(t *testing.T) {
	s := setupTestServer(t)
	// Handler intencionalmente sem spec: o Dispatch não chama ValidateSpecs
	// (isso é boot/ValidateRegistry) — o registro aqui é só para exercitar o
	// roteador com um handler que devolve (nil, nil).
	s.Register(funcHandler{
		action: "edge-nil-nil",
		handle: func(context.Context, *Request) (*Response, error) { return nil, nil },
	})
	resp := s.Dispatch(&Request{Action: "edge-nil-nil"})
	if resp.Success || resp.Code != CodeUnknownAction {
		t.Fatalf("handler (nil,nil) deveria cair no fallback legado, got %+v", resp)
	}
	if resp.Message != "Not supported action: edge-nil-nil" {
		t.Errorf("mensagem legada: %q", resp.Message)
	}
}

// --- timeout: handler lento + orçamento do cliente ---

// TestClientSendWithTimeout_SlowHandler_TimesOutAndServerSurvives: o router
// é síncrono (sem timeout próprio — os orçamentos vivem nos handlers e no
// proxy web via spec.Timeout); no wire, quem desiste é o cliente
// (SendWithTimeout → deadline). O teste prova que o estouro não derruba o
// roteador: o daemon continua atendendo conexões novas.
func TestClientSendWithTimeout_SlowHandler_TimesOutAndServerSurvives(t *testing.T) {
	s := setupTestServer(t)
	s.Register(funcHandler{
		action: "edge-slow",
		handle: func(context.Context, *Request) (*Response, error) {
			time.Sleep(500 * time.Millisecond)
			return &Response{Success: true}, nil
		},
	})

	ln := newTestListener(t)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handleConnection(conn)
		}
	}()

	c := NewClient()
	start := time.Now()
	_, err := c.SendWithTimeout(Request{Action: "edge-slow"}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("esperava erro de timeout do cliente")
	}
	// O erro tem que ser o deadline estourado, não qualquer falha — é
	// exatamente o comportamento que a Etapa 2 descreve. O client.go embrulha
	// com %%v (não %%w), então a cadeia do errors.As está quebrada; o texto
	// "i/o timeout" é o padrão do Go para deadline estourado em conexões net,
	// estável nas plataformas. (Trocar %%v por %%w no client.go é hardening
	// futuro — registrado na Etapa 2.)
	if !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("esperava timeout de deadline, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("cliente demorou %v para desistir (orçamento 100ms)", elapsed)
	}

	// Nota: a goroutine do handler lento termina ~400ms depois do teste (o
	// sleep de 500ms). É intencional — ela escreve numa conexão já fechada,
	// falha silenciosamente e sai; não é leak que afete a suíte.

	// O servidor continua saudável: o handler lento não derruba o roteador.
	resp, err := c.Send(Request{Action: "ping"})
	if err != nil || resp == nil || !resp.Success {
		t.Errorf("ping pós-timeout falhou: resp=%+v err=%v", resp, err)
	}
}

// --- payload gigante ---

// TestHandleConnection_GiantPayload_ValidJSON_StillDispatches: um payload de
// 8 MiB válido (campo gigante) decodifica e a ação roda normalmente — sem
// hang nem crash. Deadline de 10s no cliente para transformar um hang em
// falha de teste (em vez de travar a suíte).
func TestHandleConnection_GiantPayload_ValidJSON_StillDispatches(t *testing.T) {
	s := setupTestServer(t)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go s.handleConnection(serverConn)

	big := strings.Repeat("x", 8<<20) // 8 MiB
	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(clientConn).Encode(Request{Action: "ping", Domain: big}); err != nil {
		t.Fatalf("encode de payload gigante: %v", err)
	}
	var resp Response
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("sem resposta para payload gigante (hang?): %v", err)
	}
	if !resp.Success {
		t.Errorf("ping deveria responder ok mesmo com payload gigante, got %+v", resp)
	}
}

// TestHandleConnection_GiantActionName_EchoesLegacyMessage trava o eco da ação
// desconhecida na mensagem (comportamento legado): o fallback devolve
// "Not supported action: <nome>" — o nome inteiro volta na resposta. O socket
// IPC não tem limite de tamanho (diferente do proxy HTTP, que tem
// MaxBytesReader de 1 MiB) — a nota fica registrada como candidata a
// hardening, não como bug (IPC é loopback, daemon admin).
func TestHandleConnection_GiantActionName_EchoesLegacyMessage(t *testing.T) {
	s := setupTestServer(t)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go s.handleConnection(serverConn)

	action := strings.Repeat("a", 1<<20) // 1 MiB de nome de ação
	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(clientConn).Encode(Request{Action: action}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp Response
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("sem resposta: %v", err)
	}
	if resp.Code != CodeUnknownAction {
		t.Fatalf("esperava CodeUnknownAction, got %+v", resp)
	}
	want := "Not supported action: " + action
	if resp.Message != want {
		t.Errorf("eco da mensagem legada divergiu (len=%d, want %d)", len(resp.Message), len(want))
	}
}
