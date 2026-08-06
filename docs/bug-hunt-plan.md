# Plano — Bug Hunt do FocusGuard (pós-v0.16.0)

> **Status:** documento vivo. **Criado em 2026-08-06** após a v0.16.0 — nenhuma
> etapa executada ainda. Cada etapa tem escopo, técnicas, comandos e critério
> de saída; marque a etapa com ✅ ao concluir.

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
