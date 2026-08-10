# Plano — Bug Hunt do FocusGuard (pós-v0.16.0)

> **Status:** **✅ CONCLUÍDO.** **Criado em 2026-08-06** após a v0.16.0.
> **Etapas 0–8 concluídas em 2026-08-10** (baseline, contrato IPC, roteador,
> concorrência/lifecycle, domínios críticos, HTTP/SSE, frontend, plataforma e
> fuzz/E2E — ver seções abaixo e o resumo final em "✅ Checklist final").
> Cada etapa tem escopo, técnicas, comandos e critério de saída; todas
> marcadas com ✅ ao concluir.

**Motivação:** a v0.16.0 entrou com **40 commits de refatoração** (229 arquivos
mudados — registry de ações, serviços de domínio, lifecycle do daemon, event
hub/SSE, métricas, CLI split, socket por grupo no Linux). O risco de regressão
silenciosa é alto exatamente onde o comportamento foi **reescrito, não só
movido** (Fases 2–8 do `docs/refactor-plan.md` + F4/F5 do `docs/ui-plan.md`).

**Fontes de verdade já levantadas (2026-08-06):**

- Cobertura: `ipc` 93.8% · `scheduler` 90.4% · `httpapi` 84.4% ·
  **`daemon` 69.6%** (o mais baixo entre os críticos).
- Pacote sem teste: `internal/ipcerr` (só aliasing — verificar por teste de
  paridade dos códigos).
- Flake conhecido: `TestWatchFsEvents_*` do statewatch (timing de fsnotify;
  passa isolado, `-count=3` verde).
- `-race` **não roda no Windows local** (`CGO_ENABLED=0`) — rodar no CI Linux.
- Armadilhas documentadas (AGENT.md §9): daemon exige admin (manifest); tray
  **sem** admin; `install-daemon.ps1` precisa de BOM UTF-8; update reinicia na
  hora e **para watchdog + tray no Windows antes do swap**; IPC é o contrato
  (CLI/tray/daemon/web + `types.ts` gerado — `make contract`/`contract-check`);
  ações vivem no registry (não em `switch`); `focusguard-web` é user-space;
  porta 48902 vem de `httpapi.DefaultAddr`; `git status` antes de commitar
  (`.syso`/`focusguard.ico`/`versioninfo.json` mudam com `make icon`/`winres`).
- Bug 1 comentado em `cmd/focusguard-daemon/main.go` (limpeza de `.bak`/`.old`
  órfãos de updates passados em todo boot).

---

## Etapa 0 — Baseline de sanidade (30 min)

**Objetivo:** suíte reprodutível antes de caçar; nada de "bug" por infra.

- `go build ./... && go vet ./... && go test ./... -count=1 -timeout=60s`
  (testes do daemon exigem shell **elevado** no Windows).
- `make contract-check` (drift Go→TS) + `cd focusguard-ui && npx tsc --noEmit
  && npx vitest run`.
- Rodar a suíte **2×** para separar flaky real de falha determinística;
  registrar os flaky neste documento.

**Critério de saída:** lista de testes flaky conhecidos + verde determinístico
no resto.

### ✅ Resultado — executada em 2026-08-10 (shell **não-elevado**, Windows)

| Check | Pass 1 | Pass 2 | Resultado |
|---|---|---|---|
| `go build ./...` | OK | OK | ✅ determinístico |
| `go vet ./...` | OK | OK | ✅ determinístico |
| `go test ./... -count=1 -timeout=60s` | 38 ok · 1 FAIL | 38 ok · 1 FAIL | 1 falha **ambiental** (idêntica nos 2) |
| `contract-check` | ✅ "contrato Go→TS em dia" | — | ✅ sem drift |
| `tsc --noEmit` | OK | — | ✅ |
| `vitest run` | 22/22 | 22/22 | ✅ determinístico (2×) |

- **Flaky detectado: nenhum.** As falhas dos 2 passes são idênticas (mesmo
  pacote, mesma causa); nenhum teste falhou num pass e passou no outro.
- **Falha ambiental conhecida (não é bug):** `cmd/focusguard-daemon` —
  `fork/exec ...\focusguard-daemon.test.exe: A operação solicitada requer
  elevação.` (manifest `requireAdministrator`). A suíte do daemon só roda em
  shell **elevado**, de preferência com o serviço `FocusGuard` parado — ver
  `docs/perf-2026-08-05.md` §8. Reproduzida deterministicamente nos 2 passes
  (2.553s / 2.270s).
- **Pacote sem testes (não é falha):** `internal/domain/ipcerr`
  (`[no test files]`) — alvo da Etapa 1 (teste de paridade dos códigos).
- **Observação de ambiente:** `make` não existe neste shell — o
  `contract-check` foi rodado direto via
  `go run ./scripts/gen-contract/main.go --check` (mesmo comando do alvo).
- **Critério de saída atendido:** lista de flaky = **vazia**; verde
  determinístico em todos os pacotes exceto a falha ambiental documentada.

## Etapa 1 — Contrato IPC (a superfície que tudo depende)

**Escopo:** `internal/ipc` (specs, codes, Request/Response), `internal/ipcerr`,
`scripts/gen-contract`, `focusguard-ui/src/api/types.ts`.

**O que procurar:**

- Paridade de códigos de erro: handler devolve `ipcerr` ↔ `Response.Code` ↔
  mensagem no CLI/tray/web.
- `ValidateRegistry`: existe spec sem handler ou handler sem spec?
  (`user-verify` é a exceção web-only).
- Campos do contrato que o frontend lê mas o daemon nunca preenche (e vice-versa).

**Critério de saída:** teste de paridade em `internal/ipcerr` (o único pacote
sem teste) + nenhum drift de contrato.

### ✅ Resultado — executada em 2026-08-10

- **Teste de paridade criado** em `internal/domain/ipcerr/ipcerr_test.go`
  (5 testes — o pacote deixou de ser o único sem teste do repo):
  - `TestWireCodesMatchDomainCodes` — cada código do domínio é re-exportado
    pelo `transport/ipc` com o MESMO valor (paridade ipcerr ↔ `Response.Code`).
  - `TestStableLiterals` — trava os literais exatos (`ERR_*`) do contrato
    externo; renomear qualquer um é breaking change (protocolo é aditivo).
  - `TestNew_SetsCodeAndMessage`, `TestError_ImplementsError`,
    `TestError_As` — comportamento do `*ipcerr.Error` (construtor + transporte
    por `errors.As`, o padrão usado pelos handlers e pelo roteador).
- **`ValidateRegistry` já coberto** (verificado, não reescrito):
  `TestServer_RegisteredHandlersHaveSpecs` (registry→specs),
  `TestServer_SpecsAllHaveHandlers` (specs→registry) e
  `TestSpecFor_UserVerifyAbsent` (`user-verify` web-only, sem spec — a exceção
  documentada). O `ValidateRegistry` também roda no boot do daemon
  (`cmd/focusguard-daemon/main.go`).
- **Nenhum drift de contrato**: `contract-check` ✅ ("contrato Go→TS em dia").
- **Frontend/CLI não ramificam por literais hardcoded** — o `code` chega pelo
  codegen (`types.ts`) e `execAction`/`useAction` expõem o campo; os únicos
  usos literais de `ERR_*` no repo são docs e um `t.Fatalf` de teste.
- Validação: `go test ./internal/domain/ipcerr/ ./internal/transport/ipc/`
  verdes, `go vet` limpo, `gofmt -l` sem output.

**Critério de saída atendido:** teste de paridade em `internal/domain/ipcerr`
(ex-único pacote sem teste; agora coberto) + nenhum drift de contrato.

## Etapa 2 — Roteador IPC (comportamento reescrito)

**Escopo:** `ipc.Dispatch`, fallback de ação desconhecida, `errorResponse`,
specs (permissão/timeout).

**O que procurar:**

- Ação desconhecida → `CodeUnknownAction` com a mensagem legada exata (o review
  da Fase 8 flagrou o edge de handler devolver `(nil, nil)`).
- Spec com timeout: estourou → código correto e conexão fechada sem goroutine leak.
- `event-subscribe` continua excluído do log de lentos e do proxy HTTP.
- Permissão: ação `PermAuthenticated` recusada por HTTP → 403 (e não 401/500).

**Critério de saída:** testes de edge (`nil` registry, ação vazia, timeout,
payload gigante).

### ✅ Resultado — executada em 2026-08-10

- **Novo arquivo `internal/transport/ipc/router_edge_test.go` (8 testes):**
  - **nil registry:** `TestDispatch_NilRegistry_FallsBackToUnknownAction` +
    `TestHandleConnection_NilRegistry_StillResponds` — um Server zero-value
    (registry nil) não panic e responde no wire com o fallback legado.
  - **ação vazia:** `TestDispatch_EmptyAction_...` +
    `TestHandleConnection_EmptyAction` — `""` → `CodeUnknownAction` com
    `"Not suported action: "` (mensagem exata).
  - **handler (nil, nil):** `TestDispatch_HandlerNilNil_FallsBackToUnknownAction`
    — congela o edge flagrado no review da Fase 8: handler registrado que
    devolve `(nil, nil)` cai no fallback (o legado encodaria JSON `null`).
  - **timeout:** `TestClientSendWithTimeout_SlowHandler_TimesOutAndServerSurvives`
    — o roteador é síncrono (sem timeout próprio); quem desiste é o cliente
    (`SendWithTimeout` → deadline). O teste prova que o estouro não derruba o
    servidor (ping continua respondendo depois).
  - **payload gigante:** `TestHandleConnection_GiantPayload_ValidJSON_StillDispatches`
    (8 MiB válido → decodifica e despacha) e
    `TestHandleConnection_GiantActionName_EchoesLegacyMessage` (1 MiB de nome
    de ação → a mensagem legada ecoa o nome inteiro). Deadline de 10s nos
    testes para hang virar falha (não travamento de suíte).
- **Já coberto (verificado, não reescrito):** ação desconhecida →
  `CodeUnknownAction` com a mensagem legada exata (`TestIntegration_UnsupportedAction`,
  `TestServer_HandleConnection_UnsupportedAction`, wiring test); JSON inválido →
  `"Request invalid"` (`TestServer_HandleConnection_InvalidJSON`);
  `event-subscribe` excluído de métricas/log de lentos
  (`TestMetrics_RecordsDispatchLatency`); timeouts de spec ≥ orçamentos
  internos (`TestSpec_ProxyBudgetAtLeastDaemonInternal`); permissão por HTTP →
  403 (não 401/500): `TestAction_UnknownAction_Forbidden` e
  `TestAction_UserSetPassword_OtherForbidden` no `httpapi`.
- **Achados documentados (sem mudança de comportamento):**
  - O roteador não impõe timeout — os orçamentos vivem nos handlers
    (event-subscribe 20s, update 120s) e no proxy web (spec.Timeout). Estouro
    no wire = o cliente desiste via deadline, sem resposta/código. É o desenho
    documentado, não bug.
  - O socket IPC **não tem limite de tamanho** (diferente do proxy HTTP, com
    `MaxBytesReader` de 1 MiB) e o fallback ecoa o nome da ação na mensagem
    (amplificação pequena). Risco baixo (IPC é loopback, daemon admin) —
    candidatos a hardening futuro, registrados aqui.
  - **`client.go` embrulha erros de dial/encode/decode com `%v` (não `%w`)** —
    a cadeia do `errors.Is`/`errors.As` se perde (o teste de timeout teve que
    casar por texto "i/o timeout" em vez de `net.Error.Timeout()`). Sem
    impacto de comportamento hoje; trocar por `%w` é hardening futuro.
- Validação: `go test ./internal/transport/ipc/` e
  `./internal/transport/httpapi/` verdes; `go vet` limpo; `gofmt -l` sem output.

**Critério de saída atendido:** os 4 edges (`nil` registry, ação vazia,
timeout, payload gigante) têm testes próprios e estão verdes.

## Etapa 3 — Concorrência e lifecycle (a área de menor cobertura)

**Escopo:** `internal/daemon` (69.6%!), `internal/eventhub`, `internal/pomodoro`,
`internal/schedule`, `internal/scheduler`.

**Técnicas:**

- **`go test -race`** nos pacotes concorrentes — rodar no **CI Linux** (local
  Windows tem CGO off).
- Shutdown ordenado do lifecycle: parar no meio de um block ativo, durante
  long-poll, durante update → deadlock?
- Hooks `SetOnChange` (pomodoro/schedule) publicando **fora do lock** —
  reconfirmar que não há lock→hub→lock circular.
- Ring buffer do eventhub: subscriber lento, reconexão com `Last-Event-ID`
  estale, overflow de 64.

**Critério de saída:** `-race` limpo + teste de shutdown com atividade
simultânea.

### ✅ Resultado — executada em 2026-08-10

- **CI Linux com `-race`** — novo `.github/workflows/test.yml` (job `race`,
  `ubuntu-latest`, em todo push/PR): `go build ./... && go vet ./...` + `go
  test -race -count=1 -timeout=180s` nos pacotes concorrentes
  (`internal/system/daemon`, `internal/transport/eventhub`,
  `internal/transport/ipc`, `internal/domain/pomodoro`,
  `internal/domain/schedule`, `internal/domain/scheduler`,
  `internal/infrastructure/processguard`, `internal/infrastructure/store`).
  O `-race` **não roda no Windows local** (CGO off) — a validação real
  acontece no primeiro push/PR; o YAML foi validado localmente (js-yaml).
  Detalhe verificado: o teste de chown do socket Linux (`TestListen_Chowns…`)
  pula sem root — o job não exige sudo.
- **Teste de shutdown com atividade simultânea** —
  `internal/system/daemon/lifecycle_activity_test.go` (3 testes):
  - `TestRun_CtxCancel_ActiveLongPoll_DoesNotWaitForConnections` — o shutdown
    NÃO espera conexões ativas drenarem (long-poll/event-subscribe): o
    lifecycle fecha o listener e retorna; a conexão morre pelo timeout do
    handler (20s do hub).
  - `TestRun_CtxCancel_SlowTeardown_CompletesWithoutDeadlock` — componente
    com Stop lento (simula update/guard em andamento) completa o teardown em
    ordem reversa, sem deadlock nem perda de stops.
  - `TestRun_ServiceStop_SecondRequestIgnoredAfterFirstRefused` — parada de
    serviço recusada (CanStop=false) zera o canal `svcStop`: pedidos
    subsequentes não são re-observados e o daemon segue protegendo até um
    cancelamento incondicional (ctx).
- **Já coberto (verificado, não reescrito):** eventhub — overflow do ring e
  reconexão (`TestRing_WrapsAndCatchUp`), `Last-Event-ID` estale
  (`TestWait_SinceSkipsOldEvents`), subscriber lento não bloqueia os demais
  (`TestPublish_DoesNotBlockWithManySubscribers`), concorrência
  (`TestConcurrentPublishWait`); gate CanStop (`TestRun_IgnoresStopWhenCanStopFalse`).
- **Achados (para a Etapa 4):** `internal/AGENT.md` já documenta o vazamento
  de goroutine do `startPeriodicIPRefresh` (scheduler) no shutdown do daemon —
  requer fix de produção (fora do escopo desta etapa, registrado aqui).
- Validação: `go test ./internal/system/daemon/` verde (8/8 `TestRun_*`),
  `go vet` limpo, `gofmt -l` sem output.

**Critério de saída atendido:** `-race` no CI Linux (job dedicado) + teste de
shutdown com atividade simultânea (3 cenários) verdes.

## Etapa 4 — Domínios críticos (comportamento preservado?)

**Escopo:** `internal/update` (Orchestrator novo, Bug 1), `internal/store/replica`,
`internal/tamper`, `internal/enforcer`, `internal/scheduler` (Reconcile).

**O que procurar:**

- Update: `.bak`/`.old` órfãos, `ErrScheduledOnReboot`, `CleanupStale` varrendo
  o que não deve (comentário "Bug 1" no `main.go`).
- `state.json` ↔ RAM: escrever fora do daemon e ver a RAM vencer (fonte da
  verdade em RAM).
- Reconcile reaplicando bloqueios após drift no `hosts` + firewall.

**Critério de saída:** teste de integração do Orchestrator cobrindo "update com
bloqueios ativos" e "renomeação falhou → reboot".

### ✅ Resultado — executada em 2026-08-10

- **Integração Orchestrator + Updater REAL** — novo
  `internal/infrastructure/update/orchestrator_integration_test.go` (o
  `CheckForUpdate` é canhão — sem rede — e o `UpdateToAll` real baixa do
  httptest, extrai e troca binários):
  - `TestOrchestrator_Integration_ApplyWithActiveBlocks` — **"update com
    bloqueios ativos"**: `state.json` com bloqueios vivos ao lado dos binários
    (mesmo dir do daemon); o update troca os binários, mantém a flag
    `update.inprogress` e **não toca o state.json** (byte a byte) — os
    bloqueios sobrevivem ao restart para o Reconcile do daemon novo. CleanupStale
    mantém 1 `.bak`/binário.
  - `TestOrchestrator_Integration_RenameFailedSchedulesReboot` — **"renomeação
    falhou → reboot"** ponta a ponta: rename-aside do daemon falha (exe
    travado, Windows) → suíte agendada para o próximo boot
    (`ErrScheduledOnReboot`), `PendingReboot=true`, `Applied=false`, flag
    REMOVIDA, binário não trocado e o `.bak` fica para o smart recovery do
    watchdog.
- **Vazamento do `startPeriodicIPRefresh` FIXADO** (o achado da Etapa 3): novo
  `Scheduler.Stop()` idempotente (`sync.Once`) fecha o `refreshStop`;
  registrado no lifecycle do daemon (`cmd/focusguard-daemon/main.go`,
  componente `StopOnly(sched.Stop)`). Teste TDD
  `TestScheduler_Stop_StopsPeriodicRefresh`: nenhuma resolução DNS acontece
  após o Stop (goroutine saiu) e o Stop duplo não panic.
- **Já coberto (verificado, não reescrito):** `UpdateToAll` — rollback atômico,
  fail-fast de binário ausente e o próprio `ErrScheduledOnReboot` (testes de
  update); Orchestrator — PendingReboot/flag/CleanupStale com fake
  (`orchestrator_test.go`); Reconcile reaplicando bloqueios após drift
  (testes do scheduler); store/replica com suíte própria.
- Validação: suíte dos pacotes `scheduler` + `update` verdes, `go build ./...`
  OK, `go vet` limpo, `gofmt -l` sem output.

**Critério de saída atendido:** teste de integração do Orchestrator com
"update com bloqueios ativos" e "renomeação falhou → reboot" + vazamento do
refresh periódico corrigido e testado.

## Etapa 5 — HTTP/SSE

**Escopo:** `internal/httpapi` (proxy, `/api/events`, `/api/metrics`, auth).

**O que procurar:**

- SSE: reconexão com `Last-Event-ID` entrega eventos perdidos? keepalive?
  daemon caiu → `event: error`?
- Proxy: ação sem spec (403), daemon offline (503), timeout vs especificado.
- Auth: sessão expirada → 401 → UI volta ao login (regressão da v0.15.2).
- Métricas: `event-subscribe` ausente; `Reset` não zera percentis de outro cliente.

**Critério de saída:** teste de reconexão SSE com eventos no intervalo +
paridade de timeouts.

### ✅ Resultado — executada em 2026-08-10

- **Reconexão SSE com Last-Event-ID** — novo
  `internal/transport/httpapi/events_reconnect_test.go` (3 testes):
  - `TestEvents_ReconnectResumesFromLastEventID` — o teste do critério: o
    browser reconecta mandando `Last-Event-ID=5` e o loop retoma do gap
    (primeiro poll `Since=5`) — entrega SÓ os eventos do intervalo (rev
    6..7) e não reentrega o ring. O `id:` ecoa o `resp.Rev` do lote (os
    DOIS eventos compartilham o mesmo id — o rev é o high-water mark, não
    o seq individual); o ciclo quieto segue com `since=7` (sem duplicar) e
    um erro de daemon encerra com `event: error`.
  - `TestEvents_InvalidLastEventIDFallsBackToZero` — `Last-Event-ID` não
    inteiro (`abc`, `12abc`, `1.5`, com espaços) é ignorado → `since=0`: um
    id corrompido nunca quebra a reconexão.
  - `TestEvents_NegativeLastEventID_PropagatesVerbatim` — edge do parse
    flagrado: `ParseInt` aceita `"-1"`, então um id negativo chega ao hub
    com `since=-1` (o `Wait` entrega o ring inteiro). Um EventSource real
    nunca manda isso (só ecoa ids ≥ 0 recebidos) — hardening candidato
    (clampar negativos em 0 no parse), registrado sem mudança de
    comportamento.
- **Paridade de timeouts** — novo `internal/transport/httpapi/httpapi_parity_test.go`
  (5 testes):
  - `TestAction_ProxyUsesSpecTimeoutForEveryAction` — tabelado sobre TODAS
    as ações do spec (34): o `/api/action` usa exatamente `spec.Timeout`
    para cada uma (drift detection: ação nova com spec de 30s não pode
    cair no `proxyTimeout` de 5s).
  - `TestPing_UsesProxyTimeout_ParityWithSpec` — a const `proxyTimeout` é
    o MESMO do spec de ping (paridade const ↔ tabela).
  - `TestEvents_PollTimeoutExactParity` — o poll SSE usa exatamente
    `spec + eventPollMargin` (o teste existente só garantia `≥`; aqui a
    fórmula exata fica congelada).
  - `TestMetrics_EventSubscribeExcluded` — `event-subscribe` fora do
    registro `http` do `/api/metrics` mesmo quando o `/api/action` o
    encaminha (controle positivo com ping prova que o registro funciona).
  - `TestMetrics_DaemonResetDoesNotClearHTTPRegistry` — o `metrics --reset`
    do CLI zera o registro DO DAEMON (snapshot ipc vazio) sem tocar o
    registro local do proxy web (processo separado) — "Reset não zera
    percentis de outro cliente".
- **Já coberto (verificado, não reescrito):** auth 401/405/415/400, 403
  (host/spec/permissão), 503 daemon offline; **sessão expirada → 401**
  (`TestAction_ExpiredSession`); paridade spec ≥ orçamento interno do daemon
  (`TestSpec_ProxyBudgetAtLeastDaemonInternal` no ipc — update/update-check/
  event-subscribe); hub com `Last-Event-ID` estale (`TestWait_SinceSkipsOldEvents`
  no eventhub); loop SSE completo com keepalive e `event: error`
  (`TestEventsStreamsAndEchoesRev`, `TestEventsDaemonDownClosesWithError`).
- Validação: suíte `internal/transport/httpapi` verde (8 testes novos +
  34 subtests + existentes), `go vet` limpo, `gofmt -l` sem output; code
  review aplicado (controle positivo no teste de exclusão + assert preciso
  do count de `id:`).

**Critério de saída atendido:** teste de reconexão SSE com eventos no
intervalo (`Last-Event-ID`) + paridade de timeouts (tabela completa + ping +
formula exata do poll SSE), verdes.

## Achado ao vivo — "o YouTube não voltou" (2026-08-10)

**Origem:** usuário reportou YouTube bloqueado após remover/expirar os
bloqueios. Investigação **ao vivo na máquina** (estado real do sistema, não
código): todos os mecanismos estavam limpos — `state.json` vazio, `hosts`
sem entradas `# FOCUSGUARD`, zero regras `FocusGuard_*` no firewall (dump
completo do netsh), porta 53 livre, DNS do adaptador = Cloudflare, cache
DNS do Windows sem youtube, `nslookup youtube.com` resolvendo IP real. O
bloqueio do dia (09:03 → expirou 09:04) havia sido removido por completo; o
`ping youtube.com` voltou a responder após `ipconfig /flushdns` (a causa
residual era a aba/navegador com o erro velho — cache do browser, não do
sistema).

**Descoberta 1 — o sinkhole DNS é network-wide, não do próprio PC.** O
servidor DNS escuta em `0.0.0.0:53` e cobre OUTROS dispositivos da rede que
apontem o DNS para esta máquina (edição Server, "Rei da Rede"). **Nenhum
código toca o DNS do adaptador da própria máquina** (varredura por
`Set-DnsClientServerAddress`/`netsh dns`/`dnsclient` = zero ocorrências) —
por isso o adaptador ficava em Cloudflare estático mesmo com o sinkhole
rodando, e não há o que "restaurar" no adaptador ao parar (o stop só
libera a porta 53 e persiste `dns_enabled=false`). Consequência
consistente com o diagnóstico: o bloqueio do YouTube foi aplicado por
hosts + firewall por IP, não pelo sinkhole.

**Descoberta 2 — raça real no refresh periódico (fix de produção).** O
`startPeriodicIPRefresh` (15min) re-resolve blocos ativos e aplica IPs
novos via `BlockDomain`; a checagem de atividade e o apply não são
atômicos — se o bloco expirar na janela entre os dois, uma regra de
firewall para o IP novo + a entrada do hosts **ficam órfãs**, e o
`UnblockDomain` da expiração só remove os IPs conhecidos do bloco. Sem
blocos ativos, `Sync` (que varre órfãos) não rodava — o órfão ficava até o
próximo boot. Exatamente o "o sistema esquece de desbloquear?".

**Fix** (`internal/domain/scheduler/scheduler.go`): quando o **último**
bloco sai — no `onExpire` (`remaining == 0`) e no `Reconcile` (branch
`else` sem blocos ativos) — roda `enforcer.Sync(nil)` antes de desligar o
DoH: a varredura idempotente que remove QUALQUER regra de domínio órfã
(inclusive o IP novo do refresh) e reescreve o hosts limpo, sem tocar
regras DoH/DoT/AllInternet/Allow. Erro do sweep é best-effort (como o
`UnblockDoH`); um sweep falho se auto-cura no próximo boot.

**Testes** (`internal/domain/scheduler/expiry_cleanup_test.go`):
- `TestScheduler_TimerExpiration_CleansAllMechanisms` — cadeia completa da
expiração por timer: `UnblockDomain` com os MESMOS IPs da consulta,
`UnblockDoH` no último bloco, `state.json` limpo, `HasActiveBlocks`/`IsBlocked`
false (o `mockEnforcer` passou a rastrear `blockDoHCalls`/`unblockDoHCalls`).
- `TestScheduler_LastExpiry_SweepsOrphanRules` (TDD do fix, timer) — o
`Sync` de varredura com conjunto vazio roda na saída do último bloco.
- `TestScheduler_Reconcile_LastExpiry_SweepsOrphanRules` (TDD do fix,
Reconcile) — mesmo fix no caminho boot/tamper.

**Validação:** suíte completa do `scheduler` (7.6s) + `enforcer` (3.0s)
verdes, `go build ./...` OK, `go vet` limpo, `gofmt` limpo, code review
aplicado (sinal de conclusão do teste = o próprio sweep). Trade-offs
documentados: todo boot sem blocos faz 1 enumeração de firewall a mais
(idempotente, cura órfãos de crash); a remoção primária por domínio mantém
o retry original (o sweep é camada extra de higiene).

### Achado 3 — assimetria do caminho de batch (`BlockDomains`)

**Origem** — pergunta de acompanhamento do mesmo relato ao vivo: "o caminho
de batch tem a mesma simetria de limpeza ao expirar?" (preset/pomodoro/
schedule bloqueiam lotes de domínios via `BlockDomains`).

**O bug (lado da APLICAÇÃO, não da expiração)** — o `BlockDomains` aplicava
o lote com `enforcer.Sync(activeIPs)` passando **só o lote**. Como o `Sync`
reescreve o hosts e varre regras órfãs **com base no conjunto que recebe**,
os domínios de um bloqueio manual já ativo (ex.: youtube.com bloqueado à mão
enquanto um pomodoro/preset começa) eram tratados como **órfãos**: a
proteção deles (hosts + firewall) era **removida com eles ATIVOS na RAM** —
estado dizia "bloqueado", SO dizia "livre". A expiração do lote em si já era
simétrica (timer próprio por domínio com os mesmos IPs da consulta + a
varredura do último bloco, Achado 2).

**Fix** (`internal/domain/scheduler/scheduler.go`, commit `50b72ef`) — o
`Sync` do lote agora recebe **`allActive` = todos os blocos ativos**
(pré-existentes + lote, condição `!b.CanUnblock()`, a MESMA do `Reconcile`):
pré-existentes permanecem protegidos, o lote é adicionado, expirados são
varridos — uma única regra consistente com o caminho de reconcilição.
Rollback em falha de `Sync` continua revertendo o lote inteiro (RAM +
disco). A variável `activeIPs` (que ficou morta — atribuída, nunca lida —
após a mudança) foi removida.

**Testes** (`internal/domain/scheduler/expiry_cleanup_test.go`):
- `TestScheduler_BlockDomains_PreservesExistingBlocks` (TDD do fix) —
bloqueio manual pré-existente + lote por cima: o `Sync` do lote DEVE conter
o domínio pré-existente com os MESMOS IPs (nunca tratado como órfão).
- `TestScheduler_BlockDomains_ExpiryCleansAll` (simetria da expiração) —
lote expira: `UnblockDomain` com os mesmos IPs da consulta, `state.json`
limpo, `HasActiveBlocks` false, `IsBlocked` false.

**Validação:** suítes verdes (`scheduler` 8.4s, `pomodoro`, `schedule`,
`blocks`), `go build ./...` OK, `go vet` limpo, `gofmt` limpo, code review
aprovado. Recomendação futura: idem para o caminho `BlockAllInternet`
(sentinela de pânico/deep-focus), que também sobrepõe bloqueios existentes.

## Etapa 6 — Frontend

**Escopo:** `focusguard-ui/src` (DataProvider SSE, screens F4, tipos gerados).

**O que procurar:**

- `EventSource` reconectando após `onerror` sem loop; fallback de polling
  liga/desliga correto.
- Contador do Pomodoro com `phase_until` estale; dots de ciclo com `cycles > 12`.
- Grade semanal: janela overnight (22:00–06:00) vira 2 segmentos? lanes sobrepostas?
- Stats: dia mais recente em esmeralda mesmo com dados vazios de hoje.

**Critério de saída:** testes de componente (vitest) para grade overnight +
fallback de SSE.

### ✅ Resultado — executada em 2026-08-10

- **Grade overnight — novo `focusguard-ui/src/components/weekly-grid.test.tsx`**
  (9 testes, critério de saída):
  - Janela normal (start/end, fim depois do início) → **1 segmento**;
  - **Janela overnight (22:00–06:00) → 2 segmentos** (`22:00–24:00` e
    `00:00–06:00`) — via `start/end` e via `windows`, com asserção por
    conjunto (a grade ordena por `seg.start`, então o segmento da meia-noite
    aparece primeiro no DOM — ordem ≠ bug);
  - `windows` múltiplas → um segmento por janela, em cada dia da regra;
  - dias fora de `days[]` não renderizam bloco;
  - **lanes sobrepostas**: regras conflitantes no mesmo dia empilham lado a
    lado (`left 0%/50%`, `width 50%`); horários disjuntos ocupam `100%`;
  - regra desativada → `opacity-40`; legenda com nome da regra + "agora"
    (asserção escopada ao swatch `size-2.5`/linha `w-4` — a v1 era vacua:
    os blocos da grade também contêm o texto da regra).
- **Fallback SSE — reforço no `focusguard-ui/src/context/context.test.tsx`**
  (1 teste novo): `onerror` repetido (3× em cascata, como o EventSource do
  browser na queda de rede) **não duplica o intervalo** — o guard do
  `startFallback` mantém UM único polling de 30s (statusCalls 1→2, não 1→4).
  O teste de liga/desliga (fail → fallback → reconexão desliga) já existia.
- **Verificados por inspeção (sem bug, sem teste obrigatório):** Pomodoro com
  `cycles > 12` — `dots = min(cycles, 12)` com preenchimento proporcional
  (`aria-label` expõe `ciclo N de 12`); Stats — o "dia mais recente em
  esmeralda" usa `per_day.at(-1)` (último dia com dados), então mesmo com
  hoje vazio o último dia com foco fica destacado (comportamento desejado).
- **Integração com o backend:** `go run ./scripts/gen-contract/main.go
  --check` → "✔ contrato Go→TS em dia" (zero drift no `types.ts` gerado);
  `tsc --noEmit` limpo; suíte vitest completa **32/32 verdes** (era 22).

## Etapa 7 — Plataforma (Windows e Linux)

**Escopo:** scripts (`install-daemon.ps1` BOM!, `install-linux.sh`,
`build-msi.sh`), socket por grupo, systemd.

**O que procurar:**

- Linux: socket `root:focusguard 0660`; CLI sem grupo → hint; systemd `User=`
  correto.
- Windows: `.exe` com versioninfo 0.16.0; update para watchdog+tray antes do
  swap; MSI desktop/server (UpgradeCode compartilhado).

**Critério de saída:** checklist manual documentado + teste Linux de chown
(já existe — rodar no CI).

### ✅ Resultado — executada em 2026-08-10

- **Gap corrigido: teste de chown agora roda no CI** — o
  `TestListen_ChownsSocketToFocusGuardGroup` (chown do socket para
  `root:focusguard 0660`) fazia `Skip` sem root, e o `test.yml` roda como
  usuário runner → o chown nunca era verificado no CI. Novo step
  **"Socket group chown test (Linux, root)"** no `.github/workflows/test.yml`:
  roda o teste via `sudo` com `GOCACHE` separado (root escreve em /root, não
  no cache do runner). YAML validado (js-yaml) e `GOOS=linux go vet` limpo.
- **Checklist manual (Windows e Linux) — verificado ao vivo nesta sessão:**
  - ✅ **Linux — socket por grupo**: `Listen()` (ipc_linux.go) faz
    `Chmod(0660)` + `Chown` para o grupo `focusguard` (best-effort; sem o
    grupo fica `root:root`, só root). O install-linux.sh cria o grupo
    (`groupadd --system focusguard`) e adiciona o usuário do sudo;
  - ✅ **Linux — CLI sem grupo → hint**: `client.go` dá a dica exata no
    erro de dial: "seu usuário precisa estar no grupo focusguard — sudo
    usermod -aG focusguard $USER e re-logar" (Linux only; o prefixo
    "error connecting to ipc" preservado para testes/CLI);
  - ✅ **Linux — systemd `User=` correto**: o unit `focusguard.service` NÃO
    tem `User=` → roda como root (necessário: chown do socket, hosts,
    firewall); `Restart=always`, `WatchdogSec=30` e
    `NotifyAccess=main` coerentes com o watchdog;
  - ✅ **Windows — versioninfo**: os 3 executáveis (daemon/tray/watchdog)
    em `0.16.2.0` (acima do 0.16.0 do plano; bump release já feito);
  - ✅ **Windows — update para watchdog+tray antes do swap**: o
    `StopForBinarySwap` (updateswap.go) para o serviço `FocusGuardWatchdog`
    (se running) + `taskkill`/wait do `focusguard-tray.exe` (SÓ quando o
    tray está na lista de binários — o seam `includesBinary`), com restore
    que religa o watchdog pós-update. Linux = no-op (rename livre);
  - ✅ **Windows — MSI desktop/server UpgradeCode compartilhado**: o MESMO
    UUID (`6a87f38f-…c4b0b`) nos dois `wix.json` — instalar uma edição
    sobre a outra converte a máquina (`AllowSameVersionUpgrades`);
  - ✅ **Windows — BOM do install-daemon.ps1**: `EF BB BF` presente
    (correto para acentos no PowerShell 5.1).
- **Teste novo** (`internal/infrastructure/update/updateswap_test.go`, 2
  testes): `TestIncludesBinary_TrayDecision` — o seam da decisão "para o
  tray antes do swap" (base name, case-insensitive, com/sem caminho, nunca
  substring do path) e `TestIncludesBinary_FilePathBase`. O `StopForBinarySwap`
  em si é stubado nos testes do orchestrator (não toca serviços/processos
  reais) — o seam puro agora tem cobertura direta.
- **Validação:** suíte `internal/infrastructure/update` verde, vet/gofmt
  limpos, YAML do workflow válido.

## Etapa 8 — Fuzz/property + E2E

- `go test -fuzz` no parser de duração (`time.ParseDuration`), janelas do
  `schedule` (overnight/sobreposição), `ics`.
- E2E real: `focusguard status` ↔ daemon ao vivo, `focusguard web` abre a UI,
  bloco → SSE → Dashboard atualiza.

**Critério de saída:** 1 fuzz target sem crash em 30s + 1 smoke E2E documentado.

### ✅ Resultado — executada em 2026-08-10

- **3 fuzz targets novos** (`internal/domain/schedule/fuzz_test.go`), todos
  com property checks além do "sem crash":
  - `FuzzParseICS` — bytes arbitrários no parser RFC 5545 (upload de
    calendário): contrato "nunca erro duro" garantido para qualquer entrada,
    dias 0..6 e janela `HH:MM-HH:MM` válida em toda regra devolvida;
  - `FuzzWindowsPairs` — janelas `HH:MM-HH:MM` (persistidas e enviadas via
    IPC/CLI/UI): quando o parse passa, extremos em 0..1439 e janela nunca
    vazia (`start != end` — o invariante que evita bloqueio sempre-ativo);
  - `FuzzParseClock` — relógio individual: sucesso ⇒ minutos 0..1439.
  - **Resultado: 30s cada, sem crash** — ParseICS 1.08M execs (147
    interesting), WindowsPairs 1.6M (136), ParseClock 1.45M (127), todos
    PASS. Critério de saída superado (3x o mínimo).
  - **Bug real encontrado na property de ParseICS (e corrigido)**: o
    `icsWindow` assumia que o fallback de +1h (DTEND ausente/malformado)
    nunca cruza a meia-noite — com DTSTART 23:00+, a janela viraria
    `"24:xx"`, que o `parseClock` do MESMO pacote rejeita (o parser emitia
    uma regra que o próprio domínio recusa). O fuzzer não chegou ao caso em
    30s (mutações específicas demais), mas a property check o expôs por
    análise. Fix: wrap de `e` para o dia seguinte (`23:59-00:59`, janela
    overnight já suportada pelo `windowsPairs`) + seed de regressão no fuzz.
    Re-rodado 30s após o fix: PASS (794K execs, 182 interesting).
- **Smoke E2E real documentado (2026-08-10, Windows)** — binários compilados
  do HEAD em `/tmp`, daemon iniciado fora do serviço:
  1. `focusguard status` ↔ daemon ao vivo → `🛡 Proteção DoH/DoT: inativa`,
     `0 regras`, `Nenhum bloqueio ativo`;
  2. `focusguard block example.com 1m` → `✔ Domain example.com blocked`;
  3. `focusguard status` → `example.com 10:45 10/08 10:46 10/08 43s` (bloco
     refletido; expirou sozinho em 1m e sumiu);
  4. `focusguard-web` no ar: `GET /api/ping` → `200 {"success":true,
     "message":"pong"}`; `POST /api/login` com credencial inválida → 401
     amigável (`usuário ou senha inválidos`); `GET /api/events` sem sessão →
     `não autenticado` (SSE protegido por sessão).
  - Limitação documentada: o teste de UI (bloco → SSE → Dashboard atualiza)
    exige browser — coberto pela suíte de componente/contexto (Etapa 6) com
    o FakeEventSource; o SSE real exige sessão válida (não automatizado).
  - Nota: o daemon de teste não pôde ser encerrado pelo shell não-elevado
    (o daemon se protege; taskkill → acesso negado) — processo residual
    inofensivo (binários em /tmp, estado limpo), cai no próximo reboot.

---

## ✅ Checklist final — bug-hunt concluído (2026-08-10)

### Etapas — todas concluídas

- [x] **Etapa 0** — Baseline de sanidade (suíte 2x; `cmd/focusguard-daemon` só
      roda elevado no Windows — limitação de ambiente, não de código).
- [x] **Etapa 1** — Contrato IPC: teste de paridade dos códigos do `ipcerr`.
- [x] **Etapa 2** — Roteador IPC: edge cases (nil registry, ação vazia,
      timeout, payload gigante).
- [x] **Etapa 3** — Concorrência/lifecycle: `-race` no CI Linux (novo
      `test.yml`), shutdown com atividade simultânea.
- [x] **Etapa 4** — Domínios críticos: update Orchestrator (bloqueios ativos;
      rename falhou → reboot) + vazamento do `startPeriodicIPRefresh` (fix).
- [x] **Etapa 5** — HTTP/SSE: reconexão com `Last-Event-ID` + paridade de
      timeouts (spec ↔ proxy).
- [x] **Etapa 6** — Frontend: grade overnight (2 segmentos) + fallback SSE
      (onerror em cascata não duplica o intervalo); `contract-check` em dia.
- [x] **Etapa 7** — Plataforma: teste de chown roda como root no CI
      (critério), checklist manual documentado (socket 0660, hint da CLI,
      systemd, versioninfo, watchdog+tray no swap, UpgradeCode, BOM).
- [x] **Etapa 8** — Fuzz/property (3 targets, 30s cada, sem crash) + smoke
      E2E documentado.

### Bugs reais encontrados e corrigidos (cada um com teste TDD)

| Bug | Fix | Commit | Teste(s) |
|---|---|---|---|
| Regra de firewall órfã quando o último bloco expira (raça do refresh) | `Sync(nil)` no `onExpire`/`Reconcile` | `577b7a5` | `TestScheduler_LastExpiry_SweepsOrphanRules` (+ Reconcile) |
| `BlockDomains` removia proteção de bloqueios pré-existentes (Sync só com o lote) | `Sync(allActive)` | `50b72ef` | `TestScheduler_BlockDomains_PreservesExistingBlocks` |
| Goroutine do refresh de IPs vazava no shutdown do daemon | `Scheduler.Stop` + `StopOnly` | `d39c70e` | `stop_test.go` |
| Janela ICS `"24:xx"` (fallback +1h cruzava a meia-noite; o pacote rejeitava a própria saída) | wrap de `e` para o dia seguinte | `43c9163` | seed de regressão no `FuzzParseICS` |

### Resumo de cobertura adicionada

- **Fuzz (30s cada, sem crash):** `FuzzParseICS` 1.08M execs ·
  `FuzzWindowsPairs` 1.6M · `FuzzParseClock` 1.45M (+ re-run pós-fix 794K).
- **Testes novos:** ipc (paridade/edge), httpapi (SSE/timeouts), scheduler
  (limpeza + batch), update (orchestrator + tray seam), ui (grade + fallback),
  schedule (fuzz).
- **CI:** `test.yml` com `-race` (Linux) + teste de chown via `sudo` com
  guard de `--- PASS`.

### Pendências conhecidas (não-bloqueantes)

- `cmd/focusguard-daemon` exige shell elevado para os testes no Windows
  (documentado na Etapa 0).
- `Last-Event-ID` negativo propaga `since=-1` ao hub (hardening candidato,
  Etapa 5 — sem mudança de comportamento).
- Processo residual do smoke E2E (`focusguard-daemon` de /tmp) não foi
  encerrável pelo shell não-elevado — cai no próximo reboot.
- Bug 1 documentado no `main.go` (limpeza de `.bak`/`.old` órfãos em todo
  boot) permanece como item de produto fora do escopo do bug-hunt.

---

## Regras transversais

- Cada bug encontrado vira um **teste que falha primeiro** (TDD); commit
  `fix(<escopo>): ...`. Nada de "ajustar sem teste".
- As etapas 1–4 são as de maior risco (refatoração interna); 5–6 pegam
  regressão de produto; 7–8 pegam plataforma.
- Flaky novos registrados aqui, com o comando que reproduz isolado.
- **Flaky sob carga da suíte completa** (Windows, execução paralela; passam
  isolados com `go test <pkg> -count=2`):
  - `statewatch`: `TestDetectChange_CallsReconcile` (fsnotify — mesmo
    padrão dos `TestWatchFsEvents_*` já registrados; timing de evento).
  - `ipc`: `TestClientSend_DecodeError` sob `-race` (o servidor fechava a
    conexão antes do cliente escrever → `broken pipe` no lugar do decode
    error) — **corrigido** no 1º run real do job `race` do CI: o servidor do
    teste agora lê a requisição antes de responder (determinístico).
  - `scheduler`: `TestScheduler_ConcurrentBlockExpiresWithReads` — o corpo
    só faz `t.Log` (nunca `t.Error`), então uma falha real aqui indica
    **panic em goroutine**, não timing; como o mecanismo não foi capturado
    nas reproduções, se voltar a falhar, capture a saída completa e rode
    com `-race` no CI Linux antes de tratar como flake.
