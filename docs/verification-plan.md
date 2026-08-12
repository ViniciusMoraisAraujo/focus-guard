# Plano — Verificação das features v0.17.0 → v0.18.1

> **Status:** ✅ **CONCLUÍDO em 2026-08-11** (review linha a linha + baseline +
> suítes). Escopo: tudo que entrou depois do bug-hunt (v0.16.x) — Fases 1–5
> do features-plan (v0.17.0), interceptor HTTPS :443 + CA local
> (v0.17.1/v0.18.0), fixes de update/watchdog/CLI (v0.18.0) e a UI
> correspondente.
>
> **Veredito geral: as features estão corretas e cobertas por testes.** A
> suíte está verde e os pontos fracos encontrados são 3 divergências de
> design/robustez (WARNING) + observações menores (INFO) — **nenhum bug de
> correção de comportamento óbvia foi encontrado**. As **3 WARNINGs de
> robustez foram corrigidas com TDD** no follow-up do mesmo dia e a **WARN
> de design do clock guard foi resolvida** (lockdown preventivo na suspeita,
> alinhado ao features-plan — ver [Correções aplicadas](#correções-aplicadas));
> as INFOs ficam registradas como melhorias futuras.

**Fontes de verdade (verificadas nesta sessão):**

| Check | Resultado |
|---|---|
| `go build ./...` / `go vet ./...` | ✅ / ✅ |
| `go test ./... -count=1` | ✅ exceto `cmd/focusguard-daemon` (falha ambiental: manifest `requireAdministrator` em shell não-elevado — idêntica à do bug-hunt, não é bug) |
| 13 pacotes afetados (tlsca, interceptor, clockguard, telemetry, devices, reports, achievements, scheduler, dnsserver, update, recovery, cmd/focusguard, cmd/focusguard-watchdog) | ✅ todos `ok` |
| `contract-check` (Go→TS) | ✅ "contrato Go→TS em dia" |
| `tsc --noEmit` (UI) | ✅ |
| `vitest run` (UI) | ✅ **36/36** (4 arquivos) |
| `gofmt -l` nas áreas alteradas | ✅ limpo |

Commits no escopo: `bd8e93f` (Fases 1–5) · `45b7875` (alerta Clock Guard +
eventos de relógio + contrato `TamperEvent`) · `a873c35` (listener HTTPS
:443 SNI) · `415114f` (CA local) · `337fb91` (.bak 1h + crashWindow
2×checkInterval) · `34330b4` (block `--duration` + doctor WARN elevado) ·
`1fe3327` (script `update-daemon-443.bat`).

---

## Etapa 0 — Baseline de sanidade ✅

Executada em 2026-08-11 (shell não-elevado, Windows). **Flaky: nenhum.** A
única falha é a ambiental do daemon (idêntica nos 2 passs, documentada no
AGENT.md — os testes do daemon, incluindo o novo `interceptor_ipc_test.go`
e o `ca_test.go`, só rodam em shell elevado ou no CI/Windows Admin).

---

## Etapa 1 — CA local (`tlsca`) + interceptor HTTPS ✅

**Review linha a linha concluído** (`tlsca/ca.go`, `store*.go`,
`interceptor/interceptor.go`, `cmd/focusguard/ca.go`, `install.go`, wiring
do daemon, `ca_test.go`/`interceptor_ipc_test.go`).

**Segurança da CA — ok:**
- Key ECDSA P-256, escrita com **0600** no Linux; no Windows a proteção vem
  das ACLs de `%PROGRAMDATA%\FocusGuard` (SYSTEM/Admins — documentado).
- Leafs: ECDSA P-256, serial aleatório (120 bits), validade 1 ano, **SAN
  exata do host** (DNS ou IP), EKU **somente ServerAuth**, chain leaf+CA
  (navegador valida pela âncora sem intermediates).
- CA 10 anos, nunca regenerada se existir (idempotente); `Exists()` sem
  efeitos colaterais; `CleanupTempCER` cirúrgico.
- Trust store: `certutil -addstore -f Root` / `update-ca-certificates`,
  idempotente via `IsInStore`; `ca-uninstall` + `uninstall` do produto
  removem a CA do store (higiene da âncora).
- Fallback auto-assinado por SNI preservado (sem CA → v0.17.1 intacto);
  `interceptor-set off` derruba listeners **preservando** a CA.
- Testes: handshake TLS valida contra a CA apenas (prova assinatura, não
  auto-assinado) + página servida por HTTPS; bind 443 ocupado degrada sem
  derrubar o daemon; wiring compartilhada entre composition root e teste
  (`registerInterceptorSet`).
- Interceptor: dual-stack loopback desktop (`127.0.0.1` + `[::1]` :80/:443),
  `0.0.0.0` no Server; `ReadHeaderTimeout 5s`; página só para host bloqueado
  (404 no resto); `html/template` escapa tudo; resposta enviada antes do
  hook de telemetria.

**Achados (nenhum bloqueante):**
- **WARN (robustez)** — `LoadOrCreate`: cert existe mas key ausente (escrita
  da key falhou no 1º boot) → **erro duro sem auto-cura**; o cert órfão fica
  para sempre e a CA nunca regenera sozinha (daemon cai para self-signed;
  doctor dá o fix manual). Proposta: tratar cert-órfão como lixo e
  regenerar.
- **WARN (edge)** — Linux `IsInStore` checa a presença do arquivo em
  `/usr/local/share/ca-certificates`, não a entrada real no store: se o
  `update-ca-certificates` falhar após o write, `IsInStore` reporta true com
  a instalação inexistente.
- **INFO** — certCache do interceptor + cache de leafs da CA sem teto (Server
  mode: SNIs arbitrários podem crescer a memória; risco baixo — listener
  local/rede).
- **INFO** — `ca-install` gera a CA antes de checar elevação (mensagem de
  erro menos clara em shell não-elevado; sem efeito prático — o write falha
  por permissão antes).
- **INFO** — KeyUsage da CA inclui KeyEncipherment (inócuo em certificado CA).

---

## Etapa 2 — Clock Guard (NTP) + tamper-log ✅

**Review concluído** (`clockguard.go` + testes, `ntp/client`, wiring do
daemon, `tamper.go`, `gen-contract`, UI Dashboard/Segurança).

**Ok:** detecção de gap nos **dois sentidos** (|now − lastKnown| > 5 min,
janela de graça p/ DST/NTP); NTP timeout 3s (nunca segura o boot); suspeita
+ NTP falho → mantida (não confirma, não libera); burla confirmada →
**bloqueio preventivo all-internet** (1h, reusa a maquinaria de expiração do
scheduler) + tamper-log `source=clock action=lockdown` + re-ancoragem do
`LastKnownTime` no horário real; worker com Check no boot + ticker 10 min e
stop limpo; contrato `gen-contract` aceita `clock`/`lockdown` (sem drift);
alerta do Dashboard com janela de 1h e 4 testes cobrindo os **4 sentidos**
(recente renderiza, antigo ignora, outra fonte ignora, falha = ausência);
badge na tela Segurança mapeia `restore`/`lockdown`/`reconcile` corretamente.

**Achados:**
- **WARN (divergência de design vs features-plan) — RESOLVIDA** — o plano
  pedia "bloquear tudo preventivamente até NTP validar" (lockdown na
  **suspeita**); o código só aplicava lockdown após a **confirmação**.
  Cenário descoberto: adiantar o relógio + **sem rede** + restart expira
  bloqueios cedo sem proteção (só logava a suspeita). Decisão de design
  aplicada (ver [Correções aplicadas](#correções-aplicadas) §4): o bloqueio
  preventivo é aplicado na suspeita quando o NTP **não decide** (offline,
  falha ou confirmando a burla) e **liberado** quando o NTP valida o relógio
  local (ou o gap normaliza). Custo conhecido: relógio correto + NTP
  inalcançável (ex.: boot após >5 min fora com UDP 123 filtrado) aplica o
  lockdown até o NTP voltar — o primeiro Check com NTP válido libera.
- **INFO** — "Ajustar expirações pendentes pelo gap em qualquer direção" (do
  plano) não foi implementado — `Lockdown` só tem `BlockAllInternet`; o gap é
  mitigado pelo bloqueio preventivo de 1h.
- **INFO** — cliente NTP aceita qualquer datagrama UDP 48B sem validar
  stratum (spoof local possível; fora do modelo de ameaça — tolerância 5 min
  e o atacante é o próprio usuário).

---

## Etapa 3 — Doctor ✅

**Review concluído** (`doctor.go` + 3 arquivos de plataforma + testes).
9 checagens (elevação, serviços, IPC, estado, hosts, firewall, versões, DNS,
**CA**) com pass/warn/fail + sugestão de fix; exit codes 0/1/2; `--json`
estável; cada checagem isolada (falha não aborta as demais); degrau para
warn quando não dá para verificar. **Fix do v0.18.0 confirmado:** `state.json`
não-gravável em shell não-elevado → **WARN** (não FAIL), TDD nos dois
sentidos. Checagem da CA: ausente = pass (config), gerada-sem-instalar =
WARN com o passo (`ca-install`), corrompida = WARN com o fix manual. **Sem
achados.**

---

## Etapa 4 — Telemetria do sinkhole + Devices ✅

**Review concluído** (`telemetry.go`, `devices.go`, `dnsserver/server.go`,
`scheduler.IsBlockedFor`, wiring do daemon).

**Ok:** telemetria JSONL append-only, cap 1 MiB + rotação para `.old`, purga
no boot (`.old` > 24h), linha corrompida/parcial pulada na leitura; hook
chamado **após** o `WriteMsg` (a resposta do DNS nunca atrasa) e fora de
lock, best-effort; devices persistidos atomicamente (`.tmp` + rename),
precedência **device > global**, `block_all`/`allow_list`/`inherit`,
allowlist do device cobre subdomínios (`example.com` → `www.example.com`),
IP como chave primária; `IsBlockedFor` consulta o device antes da regra
global e cai para `IsBlocked`; DNS responde o **IP local** (rota default,
`localIP4`) quando o interceptor está ativo, `0.0.0.0`/`::` senão;
`0.0.0.0:53` (IPv4-only) evita o problema de IPv4-mapped-IPv6 na
identificação de clientes.

**Achados:**
- **INFO** — após a rotação, a UI (e `Queries`) só lê o arquivo atual: o
  histórico rotacionado fica invisível até a purga (design: log curto).
- **INFO** — o comentário do `server.go` menciona "channel" para a telemetria,
  mas o wiring real é **direto** (síncrono) no `Recorder` — sem impacto
  (resposta já enviada antes do hook).
- **INFO** — o sinkhole só atende IPv4 (`0.0.0.0:53`): clientes IPv6 puros
  caem no DNS do roteador (limitação da edição Server, pré-existente ao
  escopo).

---

## Etapa 5 — Relatório semanal + Conquistas ✅

**Review concluído** (`reports.go`, `achievements.go`, handlers, worker do
daemon, CLI).

**Ok:** agendamento persistido em `reports.json` (dia/hora/pasta, default
domingo 23:59 **desligado**, validado 0-6/0-23/0-59); worker acorda no
próximo agendamento, **relê a config a cada ciclo** (mudança via
`reports-config-set` vale sem reinício) e é best-effort; geração HTML+JSON
autossuficientes reusando o export do analytics (nome ISO week); conquistas:
catálogo **puro** (12 badges), sem estado persistido, progresso 0-100 com
clamp e sem divisão por zero.

**Achados:**
- **WARN (comportamento)** — se o daemon estiver **fora** no minuto agendado,
  o relatório daquela semana **não é gerado** (o `NextRun` pula para a semana
  seguinte; só o on-demand cobre o atraso). Os comentários de `NextRun` e do
  worker se contradizem sobre quem cobre o atraso. Proposta: gerar no boot se
  o horário passou **no mesmo dia** (janela curta).
- **INFO** — `expandHome("~")` devolve `"~"` → `MkdirAll` cria uma pasta
  literal `~` no cwd do daemon (só se o usuário setar o caminho como `~`).
- **INFO** — badge "Mês de Foco" calcula `TotalFocus` **total** (≥ 40h no
  total), mas a descrição diz "40h em um mês" — desbloqueia antes do
  descrito (ajustar a descrição ou implementar agregação mensal).
- **INFO** — "Guardião da Madrugada" conta sessões com `Start` zero (1970)
  como madrugada (só ocorre com dados corrompidos do JSONL).

---

## Etapa 6 — Update (.bak 1h) + watchdog crashWindow ✅

**Review concluído** (`update.go CleanupStale`, `recovery.go`,
`cmd/focusguard-watchdog/main.go` + testes).

**Ok e verificado na matemática:** `CleanupStale` mantém **1 `.bak` por
binário** (o mais novo), varre `.old`/`.trash`/`focusguard-daemon-new*` e
expira o `.bak` mais novo após `recovery.BackupMaxAge` (**1h — fonte única
da verdade**, espelhada no watchdog `backupMaxAge`); os estágios
`.<name>.new` do move-on-reboot **não** são varridos (o
`PendingFileRenameOperations` precisa do source até o reboot — comentado e
correto). `crashWindow = 2 × checkInterval` (60s): o pior caso real
(markHealthy + crash imediato + detecção no tick seguinte = 60s−ε) cabe na
janela **com folga**; o teste `TestBackupMaxAge_OutlivesRollbackDecisionWindow`
trava "1h nunca trunca uma decisão de rollback". **Sem achados.**

---

## Etapa 7 — CLI fixes (block `--duration` + doctor elevado) ✅

**Review concluído** (`block.go`, `commands_test.go` 88 linhas, `doctor_test.go`
46 linhas). O `splitBlockFlags` extrai `--duration`/`-d` (com `=` e traço
simples) de **qualquer posição** junto com `--extend`/`--replace` (9 casos
TDD); duração inválida continua falhando com mensagem clara; o legado
`block <domínio> <duração>` (Arg(1)) preservado. Doctor: WARN (não FAIL)
para `state.json` não-gravável em shell não-elevado, TDD nos dois sentidos.

**Achados:**
- **INFO** — `-d-90m` (sem espaço) não casa com nenhum caso e vira o
  **domínio** posicional → erro confuso "duração deve ser informada" (não é
  crash). Adicionar o prefixo `-d`/`-duration` sem `=` como caso de extração.

---

## Etapa 8 — UI + contrato ✅

**Review concluído** (Dashboard, Segurança, Rede, Configurações,
Estatísticas, `client.ts`, `types.ts`, `dashboard.test.tsx`).
`contract-check` ✅ (zero drift), `tsc` ✅, **vitest 36/36** ✅. Alerta do
Dashboard: filtro `source=clock` + `action=lockdown` na janela de 1h, polling
30s limpo no unmount, detalhe renderizado; os 4 testes cobrem exatamente os 4
sentidos do contrato. Tela Segurança: badges corretos para
hosts/clock/estado × restore/reconcile/lockdown (o `reconcile` existe no
daemon — não é código morto). **Sem achados relevantes** (INFO: badge default
"estado" para source desconhecido — cosmético).

---

## Etapa 9 — Checklist manual (plataforma + E2E) ⏳

**Cobertura automatizada:** o cenário E2E do **clock guard** (adiantar o
relógio + restart com rede bloqueada → lockdown + liberação) foi validado
sem elevação em `internal/domain/clockguard/restart_e2e_test.go` (componentes
reais de produção, fronteira de restart — ver [Correções aplicadas §4](#4-clockguard--bloqueio-preventivo-na-suspeita--liberação-no-ntp-válido-internaldomainclockguard--internaldomainscheduler--internaldomainpolicy--daemon)).

**Não executável neste shell** (sem elevação — os comandos abaixo exigem
admin/root e navegador). Checklist para o usuário rodar na máquina real:

- [ ] `focusguard ca-install` (elevado) → CA no trust store (`certutil -store Root`
      / `/etc/ssl/certs`); `doctor` mostra pass.
- [ ] Site HTTPS bloqueado abre a página **sem aviso** (Chrome/Edge); Firefox
      com `security.enterprise_roots.enabled`.
- [ ] `focusguard ca-uninstall` remove do store; `uninstall` do produto também.
- [ ] `focusguard doctor` em shell não-elevado: nenhum FAIL falso de
      gravabilidade; exit code coerente.
- [x] (clock guard — **coberto por teste automatizado**
      `restart_e2e_test.go`, sem elevação) adiantar o relógio, reiniciar o
      daemon com a rede bloqueada → bloqueio preventivo ativo (banner na tela
      Segurança); corrigir o relógio → liberação automática.
- [ ] `focusguard block example.com --duration 45m` (duração depois do domínio).
- [ ] `focusguard report now` gera HTML+JSON; `report auto` agenda e gera no
      horário (daemon ligado no minuto agendado).
- [ ] `focusguard achievements` lista badges; stats batem.
- [ ] Sinkhole bloqueia domínio → "Atividade bloqueada" registra (Rede).
- [ ] Devices: política de device vence a global (Server).
- [ ] Update aplicado → `.bak` expira após 1h; rollback do watchdog possível
      na janela.

---

## Correções aplicadas (follow-up 2026-08-11)

As 3 WARNINGs de robustez foram corrigidas com TDD (teste que falha primeiro
→ fix → verde). Nenhum comportamento de bloqueio/produto foi alterado.

### 1. tlsca — CA corrompida/órfã se auto-cura (`internal/infrastructure/tlsca/ca.go`)

- `LoadOrCreate` agora **regenera** a CA quando os artefatos persistidos estão
  inutilizáveis: cert sem key (escrita da key falhou no 1º boot), PEM
  corrompido, ou **par descasado key↔cert** (nova verificação no `loadCA`).
  Antes, esses casos eram erro duro no boot para sempre (daemon caía para
  self-signed; o doctor pedia remoção manual). CA sadia continua sendo
  reutilizada — nunca regenerada (idempotência preservada).
- Testes: `TestLoadOrCreate_OrphanCertWithoutKey_Regenerates`,
  `TestLoadOrCreate_CorruptKeyPair_Regenerates`,
  `TestLoadOrCreate_MismatchedKey_Regenerates`.
- **Fechamento do ciclo (review)**: como a regeneração agora existe, a
  detecção no trust store do Windows passou de **CN** para **serial** (o CN é
  constante entre gerações — o cert antigo no store faria a reinstalação ser
  pulada e o navegador rejeitaria os leafs novos). `IsInStore` (Windows)
  compara o serial com o output do `certutil -store Root` normalizado (hex
  em minúsculas, ignorando espaços/prefixo `00`) — colisão com outro cert é
  nula (serial aleatório de 128 bits). Linux já era por identidade (DER).
  Testes: `TestIsInStore_Windows_SerialBased` (inclui o cenário
  "mesmo CN, serial diferente" = CA regenerada), `TestIsInStore_Windows_NormalizedSerial`.

### 2. reports — relatório da semana não é mais pulado (`internal/domain/reports/reports.go` + `cmd/focusguard-daemon/main.go`)

- Novo `Config.MissedToday(now)`: o horário agendado de **hoje** já passou
  (inclui o minuto exato — boot às HH:MM exatas).
- O worker (`startWeeklyReportWorker`) gera **no boot** quando isso ocorre
  (catch-up fora do loop: o disparo normal do timer não re-gera a mesma
  semana). Comentários de `NextRun`/worker alinhados (antes se contradiziam
  e nenhum dos dois comportamentos existia).
- Testes: `TestMissedToday_FalseBeforeTime`, `_TrueAfterTimeSameDay`,
  `_TrueAtExactTime`, `_FalseOtherDay`, `_FalseEarlierWeekday`.

### 3. tlsca (Linux) — `IsInStore` prova a instalação real (`internal/infrastructure/tlsca/store_linux.go`)

- `IsInStore` agora exige a **cópia instalada** em
  `/etc/ssl/certs/focusguard-ca.pem` (o `update-ca-certificates` copia a
  âncora local para lá) **e** que ela seja o nosso certificado (DER
  idêntico): só o arquivo-fonte em `/usr/local/share/ca-certificates` não
  conta (antes, um `update-ca-certificates` que falhava após o write era
  reportado como instalado e a reinstalação era pulada para sempre); uma
  âncora estranha/antiga de CA regenerada também não conta.
- `caCertsDir`/`storeInstalledDir` viraram vars injetáveis — os testes
  Linux agora são **herméticos** (diretórios temporários, nunca tocam o
  store real; antes escreviam em `/usr/local/share/ca-certificates` de
  verdade).
- Testes: `TestIsInStore_Linux_InstalledCopy`, `TestIsInStore_Linux_ForeignOrStaleCert`
  (+ `TestInstallIntoStore_Linux`/`TestRemoveFromStore_Linux` ajustados para
  simular o efeito do `update-ca-certificates`).

### 4. clockguard — bloqueio preventivo na SUSPEITA + liberação no NTP válido (`internal/domain/clockguard` + `internal/domain/scheduler` + `internal/domain/policy` + daemon)

- `Check()` aplica o lockdown all-internet **na suspeita** (gap > tolerância)
  quando o NTP **não decide**: indisponível (nil), falhou, ou **confirmando**
  a burla — a proteção fica no ar mesmo com NTP offline (o cenário "relógio
  adiantado + sem rede + restart" não expira mais os bloqueios sem defesa).
  Com NTP disponível, o veredito dele decide: **valida** o relógio local →
  sem bloqueio (libera um pendente e re-ancora a referência); **confirma** →
  bloqueia e registra no tamper-log. Um relógio consistente também libera um
  lockdown pendente (usuário corrigiu o relógio enquanto o NTP estava
  offline).
- **Por que não "aplicar antes de consultar o NTP" (revisão)**: o
  `CheckInterval` (10 min) é maior que a `Tolerance` (5 min) e todo veredito
  re-ancora `lastKnown ≈ agora` — em sistema saudável o gap do próximo ciclo
  seria sempre > tolerância, tornando o ramo "consistente" inalcançável e
  reescrevendo o firewall (BlockAll + UnblockAll) a cada 10 min (com NTP
  lento/filtrado, um bloqueio real de até 3s a cada 10 min; na edição
  Server, blip na rede inteira). Reservar o bloqueio imediato aos casos em
  que o NTP não decide preserva o cenário alvo (offline) sem o churn.
- **Segurança da liberação**: o `policy.Block` ganhou `Source` (`user` /
  `clock-guard` — aditivo, state.json antigo carrega como `user`); o
  scheduler só remove o sentinela do **próprio guard**
  (`ReleaseClockLockdown`) e `ApplyClockLockdown` **não substitui** um
  bloqueio all-internet do usuário ativo — o modo pânico/deep-focus nunca é
  roubado nem liberado pela validação do NTP. Contrato atualizado
  (`source?: "user" | "clock-guard"` no `Block` do types.ts, regenerado).
- Testes: guard — `TestClockAdvancedWithNTPOffline_LockdownsAtSuspicion`
  (cenário exato do plano), `TestConsistentClockAfterSuspicion_ReleasesLockdown`,
  `TestClockJumpValidatedByNTPIsLegit` (NTP válido NÃO bloqueia), `TestNilNTP_SuspicionAppliesLockdown`,
  `TestNTPFailureKeepsSuspicionUnresolved`; scheduler —
  `TestScheduler_ApplyClockLockdown_{AppliesWithGuardSource,SkipsActiveUserBlock}`,
  `TestScheduler_ReleaseClockLockdown_{RemovesGuardLockdown,DoesNotTouchUserPanic,WithoutSentinelIsNoop,UnblockFailureKeepsBlock,AfterBootstrap}`.
- **Bônus (achado do run da suíte)**: `TestDoctor_CAInstalledPasses` quebrou
  com a detecção por serial do fix 1 (o fake devolvia só o CN) — ajustado
  para devolver o output do `certutil -store` com o **serial** (novo
  `CA.SerialHex()`) e Skip no Linux (o `IsInStore` de lá lê a cópia real em
  /etc/ssl/certs — não simulável via runner).
- **UI (follow-up)**: a tela **Segurança** ganhou um banner destrutivo quando
  o lockdown do clock guard está **ativo** (sentinela `*all-internet*` com
  `source: "clock-guard"` e não expirado, lido do status do data-context —
  tempo real via SSE `blocks-changed`, sem fetch novo; o tamper-log só
  registra burlas confirmadas por NTP, então o lockdown de suspeita sem
  evento só aparece pelo estado). O **Painel** parou de rotular o lockdown
  como "Modo pânico ativo": distingue "Bloqueio preventivo do relógio"
  (clock guard) de "Modo pânico" (intencional do usuário) pelo `source`.
  Testes vitest: `seguranca.test.tsx` (banner ativo / pânico do usuário /
  expirado / badge do tamper-log) + 1 caso no `dashboard.test.tsx` (41
  testes no total, todos verdes).
- **E2E (validação do cenário pedido — sem elevação)**: o cenário "adiantar
  o relógio, reiniciar com a rede bloqueada e conferir lockdown + tamper-log,
  depois corrigir o relógio e ver a liberação" foi validado com componentes
  **reais de produção** atravessando a **fronteira de restart** em
  `internal/domain/clockguard/restart_e2e_test.go`
  (`TestClockGuard_RestartE2E_AdvanceOfflineConfirmRelease`): store
  persistido + scheduler + guard + tamper-recorder, com relógio injetável (o
  daemon real usa `time.Now`; mudar o relógio do SO exigiria admin). O teste
  percorre o ciclo completo — boot saudável → restart com relógio +24h e NTP
  offline (lockdown na suspeita, sentinela persistido com
  `source=clock-guard`, **nenhum** tamper-log — suspeita não confirmada é
  estado vivo, não evento) → NTP volta e confirma (tamper-log
  `clock/lockdown` "confirmado por NTP", bloqueio mantido) → relógio
  corrigido (liberação automática: `UnblockAll` chamado, sentinela sai do
  RAM e do state.json). A Etapa 9 manual continua pendente apenas para os
  itens que dependem do SO real (CA no trust store, navegador, sinkhole).

### Validação

`go build ./...` ✅ · `go vet ./...` ✅ · `gofmt` ✅ · `contract-check` ✅ ·
`go test` tlsca + reports ✅ (Windows, incl. os testes novos por serial) ·
`GOOS=linux go vet` + `go test -c` do tlsca ✅ (os testes Linux rodam no CI
Linux). Suíte completa: `go test ./... -count=1` ✅ (exceto o pacote do
daemon, que exige shell elevado).

---

## ✅ Checklist final — verificação concluída (2026-08-11)

- [x] **Etapa 0** — Baseline (build/vet/testes/contract/tsc/vitest/gofmt) verde.
- [x] **Etapa 1** — CA local + interceptor HTTPS (review linha a linha, 2 WARN + 3 INFO).
- [x] **Etapa 2** — Clock Guard + tamper-log (1 WARN de design + 2 INFO).
- [x] **Etapa 3** — Doctor (sem achados).
- [x] **Etapa 4** — Telemetria + Devices (3 INFO).
- [x] **Etapa 5** — Relatório + Conquistas (1 WARN + 3 INFO).
- [x] **Etapa 6** — Update + watchdog (sem achados; matemática do crashWindow verificada).
- [x] **Etapa 7** — CLI fixes (1 INFO).
- [x] **Etapa 8** — UI + contrato (sem achados relevantes).
- [ ] **Etapa 9** — Manual/E2E (o cenário E2E do clock guard foi coberto
      por teste automatizado `restart_e2e_test.go`; os itens que dependem do
      SO real — CA no trust store, navegador, sinkhole — permanecem
      pendentes para shell elevado).

### Achados

| Severidade | Área | Achado | Ação proposta |
|---|---|---|---|
| ✅ WARN | tlsca | ~~Cert órfão sem key → erro duro sem auto-cura~~ | **Corrigido (TDD)**: `LoadOrCreate` regenera cert órfão / par corrompido / descasado — `TestLoadOrCreate_{OrphanCertWithoutKey,CorruptKeyPair,MismatchedKey}_Regenerates` |
| ✅ WARN | reports | ~~Relatório pulado se o daemon ficar fora no minuto agendado; comentários contraditórios~~ | **Corrigido (TDD)**: `Config.MissedToday` + catch-up no boot do worker — `TestMissedToday_*`; comentários alinhados |
| ✅ WARN | tlsca (Linux) | ~~`IsInStore` checava só o arquivo-fonte~~ | **Corrigido (TDD)**: `IsInStore` exige a cópia instalada em `/etc/ssl/certs/focusguard-ca.pem` **e** que seja o nosso DER — `TestIsInStore_Linux_{InstalledCopy,ForeignOrStaleCert}` |
| ✅ WARN | clockguard | ~~Lockdown só após confirmação NTP (o plano pedia na suspeita)~~ | **Corrigido (design, TDD)**: lockdown preventivo na **suspeita** + liberação quando o NTP valida (ou o gap normaliza); a liberação nunca toca o pânico do usuário (Source no Block) — `TestClockAdvancedWithNTPOffline_LockdownsAtSuspicion` + testes de scheduler |
| ✅ INFO | achievements | ~~"Mês de Foco" usa foco **total**, descrição diz "em um mês"~~ | **Corrigido (TDD)**: janela real dos últimos 30 dias — `TestCalculate_FocusedMonth_{Last30Days,IgnoresOldSessions,PartialProgress}` |
| ✅ INFO | reports | ~~`expandHome("~")` cria pasta literal `~`~~ | **Corrigido (TDD)**: `~` sozinho resolve para o home — `TestExpandHome_BareTilde` |
| ✅ INFO | block.go | ~~`-d-90m` (sem espaço) vira domínio~~ | **Corrigido (TDD)**: prefixo `-d`/`-duration` sem `=` extrai o valor — casos novos no `TestSplitBlockFlags_DurationAnywhere` |
| ✅ INFO | interceptor + tlsca | ~~Caches de certs sem teto (Server mode)~~ | **Corrigido (TDD)**: LRU com teto compartilhado (`internal/infrastructure/lru`, stdlib) — `TestCertCache_Cap_EvictsLeastRecentlyUsed` + `TestLeafFor_CacheCap_EvictsLeastRecentlyUsed` |
| ✅ INFO | telemetry | ~~Histórico rotacionado invisível na UI~~ | **Corrigido (TDD)**: `Queries` lê também o `<name>.old` (concatenado antes do atual, cronológico) — `TestRotationCapsFileSize` atualizado + `TestQueries_ReadsRotatedOldAlone`; `splitJSONLLine` pula linha > 4 MiB em vez de abortar a leitura |
| ✅ INFO | ca-install | ~~Gera a CA antes de checar elevação~~ | **Corrigido (TDD)**: elevação checada ANTES de gerar/instalar a CA (mesma ordem no `ca-uninstall`) — `TestCAInstall_NotElevated_DoesNotGenerateCA` (dir injetável, hermetico) |

### Pendências

- Testes do `cmd/focusguard-daemon` (incl. `interceptor_ipc_test.go`,
  `ca_test.go`) só rodam em shell **elevado** (Windows) — validar no CI ou
  numa sessão admin.
- **Resolvido** — o job `race` do `test.yml` agora cobre também os pacotes
  novos das Fases 1–5: `clockguard`, `tlsca`, `interceptor`, `telemetry`,
  `devices`, `reports` e `achievements` (o `scheduler` já estava na lista);
  timeout do job subiu de 180s para 300s (15 pacotes sob `-race`). Os testes
  do interceptor usam portas efêmeras (`127.0.0.1:0`) e o `tlsca` Linux é
  hermético (temp dirs) — CI-safe no runner não-root; compile-check
  `GOOS=linux` dos 7 pacotes confirmado.

---

## Regras transversais

- Achados seguem o padrão do bug-hunt: bug real → teste TDD que falha
  primeiro. As WARNINGs de robustez e a WARN de design do clock guard foram
  resolvidas com TDD nos follow-ups do mesmo dia (ver
  [Correções aplicadas](#correções-aplicadas)) e **todas as INFOs foram
  resolvidas com TDD nos follow-ups de 08-11/08-12** (ver tabela de Achados
  — última linha marcada ✅).
- Etapa 9 (manual) requer shell elevado — fica como pendência operacional.
