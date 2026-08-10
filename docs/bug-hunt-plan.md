# Plano — Bug Hunt do FocusGuard (pós-v0.16.0)

> **Status:** documento vivo. **Criado em 2026-08-06** após a v0.16.0.
> **Etapas 0–4 ✅ concluídas em 2026-08-10** (baseline, contrato IPC,
> roteador, concorrência/lifecycle e domínios críticos — ver seções
> abaixo). Cada etapa tem escopo, técnicas, comandos e critério de saída;
> marque a etapa com ✅ ao concluir.

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

## Etapa 8 — Fuzz/property + E2E

- `go test -fuzz` no parser de duração (`time.ParseDuration`), janelas do
  `schedule` (overnight/sobreposição), `ics`.
- E2E real: `focusguard status` ↔ daemon ao vivo, `focusguard web` abre a UI,
  bloco → SSE → Dashboard atualiza.

**Critério de saída:** 1 fuzz target sem crash em 30s + 1 smoke E2E documentado.

---

## Regras transversais

- Cada bug encontrado vira um **teste que falha primeiro** (TDD); commit
  `fix(<escopo>): ...`. Nada de "ajustar sem teste".
- As etapas 1–4 são as de maior risco (refatoração interna); 5–6 pegam
  regressão de produto; 7–8 pegam plataforma.
- Flaky novos registrados aqui, com o comando que reproduz isolado.
