package ipc

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
const (
	// CodeDurationInvalid: duração mal formatada, zero ou negativa
	// (block/block-all/pomodoro).
	CodeDurationInvalid = "ERR_DURATION_INVALID"

	// CodeDomainRequired: faltou informar um domínio ou preset para bloquear.
	CodeDomainRequired = "ERR_DOMAIN_REQUIRED"

	// CodeDomainConflict: domínio já bloqueado e o cliente deve decidir entre
	// somar (--extend) ou substituir (--replace) — chega junto com Conflict=true
	// e ConflictBlock.
	CodeDomainConflict = "ERR_DOMAIN_CONFLICT"

	// CodeInvalid: validação genérica de payload (campos inválidos, credenciais
	// erradas, upstream malformado, etc.).
	CodeInvalid = "ERR_INVALID"

	// CodeNotConfigured: a ação exige um componente que não foi injetado no
	// servidor (testes, dev builds, instalação incompleta).
	CodeNotConfigured = "ERR_NOT_CONFIGURED"

	// CodeUnknownAction: ação não reconhecida pelo roteador.
	CodeUnknownAction = "ERR_UNKNOWN_ACTION"
)
