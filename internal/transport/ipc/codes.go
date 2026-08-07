package ipc

import "focusguard/internal/domain/ipcerr"

// Códigos de erro estáveis, aditivos desde a Fase 2 do plano de refatoração
// (docs/refactor-plan.md). O campo Response.Code é opcional no wire protocol —
// uma resposta com Success:false pode carregar um código para a UI/CLI
// ramificarem sem depender do texto humano (message), que muda de idioma e de
// redação. Códigos novos são sempre aditivos: um cliente antigo que não os
// conhece simplesmente os ignora.
//
// Regra: validações/conflitos/estado conhecidos têm código; erros internos de
// domínio (scheduler, store, etc.) continuam devolvendo apenas a message
// original — o código vazio diz "não ramifique por isso".
//
// Os valores vivem em internal/transport/ipcerr (fonte única — os services de domínio,
// que não podem importar ipc, leem os MESMOS valores); este arquivo apenas os
// re-exporta para o pacote ipc e para o wire.
const (
	// CodeDurationInvalid: duração mal formatada, zero ou negativa
	// (block/block-all/pomodoro).
	CodeDurationInvalid = ipcerr.CodeDurationInvalid

	// CodeDomainRequired: faltou informar um domínio ou preset para bloquear.
	CodeDomainRequired = ipcerr.CodeDomainRequired

	// CodeDomainConflict: domínio já bloqueado e o cliente deve decidir entre
	// somar (--extend) ou substituir (--replace) — chega junto com Conflict=true
	// e ConflictBlock.
	CodeDomainConflict = ipcerr.CodeDomainConflict

	// CodeInvalid: validação genérica de payload (campos inválidos, credenciais
	// erradas, upstream malformado, etc.).
	CodeInvalid = ipcerr.CodeInvalid

	// CodeNotConfigured: a ação exige um componente que não foi injetado no
	// servidor (testes, dev builds, instalação incompleta).
	CodeNotConfigured = ipcerr.CodeNotConfigured

	// CodeUnknownAction: ação não reconhecida pelo roteador.
	CodeUnknownAction = ipcerr.CodeUnknownAction
)
