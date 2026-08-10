package ipcerr_test

import (
	"errors"
	"fmt"
	"testing"

	"focusguard/internal/domain/ipcerr"
	"focusguard/internal/transport/ipc"
)

// codes é a tabela de paridade domínio ↔ wire ↔ literal estável. Cada código
// do ipcerr (fonte única, usada pelos services de domínio que não podem
// importar ipc) precisa ser re-exportado pelo transport/ipc com o MESMO valor
// (o que chega no Response.Code) e manter o literal exato do contrato externo.
//
// ⚠️ Protocolo aditivo: ao adicionar um código novo, atualize ipcerr.go, o
// re-export em transport/ipc/codes.go E esta tabela — um código esquecido aqui
// passa despercebido (o protocolo não valida contra esta lista).
var codes = []struct {
	name   string
	domain string
	wire   string
	want   string
}{
	{"CodeDurationInvalid", ipcerr.CodeDurationInvalid, ipc.CodeDurationInvalid, "ERR_DURATION_INVALID"},
	{"CodeDomainRequired", ipcerr.CodeDomainRequired, ipc.CodeDomainRequired, "ERR_DOMAIN_REQUIRED"},
	{"CodeDomainConflict", ipcerr.CodeDomainConflict, ipc.CodeDomainConflict, "ERR_DOMAIN_CONFLICT"},
	{"CodeInvalid", ipcerr.CodeInvalid, ipc.CodeInvalid, "ERR_INVALID"},
	{"CodeNotConfigured", ipcerr.CodeNotConfigured, ipc.CodeNotConfigured, "ERR_NOT_CONFIGURED"},
	{"CodeUnknownAction", ipcerr.CodeUnknownAction, ipc.CodeUnknownAction, "ERR_UNKNOWN_ACTION"},
}

// TestWireCodesMatchDomainCodes verifica que o transport re-exporta cada
// código do domínio com o mesmo valor (paridade ipcerr ↔ wire). O compilador
// não pega um drift aqui — este teste pega.
func TestWireCodesMatchDomainCodes(t *testing.T) {
	for _, c := range codes {
		if c.domain != c.wire {
			t.Errorf("%s: ipcerr=%q ≠ ipc=%q — drift entre domínio e wire", c.name, c.domain, c.wire)
		}
		if c.domain == "" {
			t.Errorf("%s: código vazio no domínio", c.name)
		}
		if c.wire == "" {
			t.Errorf("%s: código vazio no wire", c.name)
		}
	}
}

// TestStableLiterals trava os valores EXATOS dos códigos: são o contrato
// externo (a UI/CLI/tray ramificam por eles em runtime). Renomear qualquer um
// é breaking change — o protocolo é aditivo ("additive only"), nunca
// reescrito. Um typo aqui vira bug silencioso de ramificação.
func TestStableLiterals(t *testing.T) {
	for _, c := range codes {
		if c.want == "" {
			t.Errorf("%s: literal esperado vazio na tabela de paridade", c.name)
			continue
		}
		if c.domain != c.want {
			t.Errorf("%s: literal mudou para %q (esperado %q) — protocolo é aditivo, não reescrito", c.name, c.domain, c.want)
		}
		if c.wire != c.want {
			t.Errorf("%s: wire=%q ≠ literal esperado %q", c.name, c.wire, c.want)
		}
	}
}

// TestNew_SetsCodeAndMessage cobre o construtor usado pelos services de domínio.
func TestNew_SetsCodeAndMessage(t *testing.T) {
	e := ipcerr.New(ipcerr.CodeInvalid, "campo inválido")
	if e.Code != ipcerr.CodeInvalid {
		t.Errorf("Code = %q, esperava %q", e.Code, ipcerr.CodeInvalid)
	}
	if e.Message != "campo inválido" {
		t.Errorf("Message = %q, esperava %q", e.Message, "campo inválido")
	}
	if e.Error() != "campo inválido" {
		t.Errorf("Error() = %q, esperava a mensagem", e.Error())
	}
}

// TestError_ImplementsError garante que *ipcerr.Error continua satisfazendo
// error (os handlers devolvem ele pelos construtores de serviço).
func TestError_ImplementsError(t *testing.T) {
	var _ error = (*ipcerr.Error)(nil)
}

// TestError_As verifica o transporte por errors.As — o padrão que os testes
// de domínio e o roteador usam para extrair código + mensagem de erros
// embrulhados (fmt.Errorf com %w).
func TestError_As(t *testing.T) {
	err := fmt.Errorf("wrap: %w", ipcerr.New(ipcerr.CodeNotConfigured, "não configurado"))
	var e *ipcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("errors.As(*ipcerr.Error) falhou para erro embrulhado: %v", err)
	}
	if e.Code != ipcerr.CodeNotConfigured {
		t.Errorf("Code = %q, esperava %q", e.Code, ipcerr.CodeNotConfigured)
	}
}
