# Plano — Reorganização de Diretórios e Arquitetura

> **Status:** documento vivo — **A, B, C1–C7 e Fase D (validação definitiva)
> concluídas** (2026-08-07). **Criado em 2026-08-06.** Escopo escolhido:
> **todas as 3 frentes** — (A) organização de docs, (B) assets de build em
> diretório próprio, (C) `internal/` agrupado em camadas.

## Diagnóstico (estado atual)

### Raiz poluída

| Item na raiz | Problema | Destino proposto |
|---|---|---|
| `focusguard.exe`, `focusguard-daemon.exe`, `focusguard-web.exe`, `scheduler.test.exe` | Binários/lixo locais commitados ou esquecidos | **Deletar** (`.gitignore` já cobre `*.exe`/`*.test`) |
| ~~`task.md`, `follow-up-v0.15.1.md`, `plan-new-ui-and-user.md`~~ | Planos antigos já implementados (v0.15.x / F4-F5) soltos na raiz | ~~`docs/archive/`~~ → **removidos** (decisão do mantenedor, commit `d645321`) ✅ |
| `AGENT.md` + ~~`AGENTS.md`~~ + ~~`docs/AGENT.md`~~ | **3 guias redundantes e divergentes** (EN completo 461 linhas × TL;DR PT 37 linhas × espelho 446 linhas) | Consolidar: **1 canônico** na raiz — `AGENTS.md` removido (`d645321`) e `docs/AGENT.md` removido (Fase A) ✅ |
| ~~`versioninfo.json` (raiz), `focusguard-daemon.exe.manifest`, `focusguard.ico/.png`, `server.role`, `install.txt`~~ | Assets de build espalhados na raiz | `packaging/` (ver Frente B) ✅ |
| ~~`img/focusguard.png`~~ | Artwork canônico (1024px) | `packaging/artwork/` ✅ |
| `docs/` | Já centraliza docs — manter | — |
| `dist/`, `.idea/`, `build/` | Já ignorados | — |

### `internal/` — 28 pacotes flat

Hoje todos em um nível. O mapa do AGENT.md está **desatualizado** (faltam os
pacotes novos da Fase 4/5: `blocks`, `dns`, `ipcerr`, `presets`, `users`,
`daemon`, `eventhub`, `metrics`). Grafo de dependências real (imports internos,
sem testes):

- **Folhas** (não dependem de nada interno): `autostart`, `dnsserver`,
  `enforcer`, `eventhub`, `filelog`, `fsutil`, `icon`, `ipcerr`, `metrics`,
  `policy`, `preset`, `processguard`, `recovery`, `tamper`, `update`, `user`,
  `watchdog`
- **Dependem de poucos**: `analytics→ipcerr` · `apps→ipc` · `blocks→ipc,policy,preset` ·
  `dns→dnsserver,ipc` · `goal→ipc` · `hostswatch→fsutil,policy,tamper` ·
  `httpapi→ipc,metrics,policy` · `pomodoro→analytics,ipcerr,policy,preset` ·
  `presets→ipc,preset` · `schedule→ipcerr,policy,preset` · `scheduler→enforcer,policy,store` ·
  `statewatch→fsutil,tamper` · `store→fsutil,policy` · `tray→ipc,policy,pomodoro,preset` ·
  `users→ipc,user`
- **`ipc` é o hub** (importa quase tudo — transport + domínios); `daemon` é o
  composition root (registra handlers); `cmd/*` importam `ipc` + domínios.

> ⚠️ **Fio solto confirmado da Fase 4:** o `ipc` de produção importa domínios
> em **arquivos `*_handler.go` de referência** (`analytics_handler.go`,
> `pomodoro_handler.go`, `schedule_handler.go`, `update_handler.go` — além de
> `dnsserver` em `server.go`). Ou seja, `transport/ipc` acopla a `domain/*` e
> `infrastructure/*` **em produção**, não só nos testes. Isso é permitido pela
> regra "depender para baixo" (transport → domain/infra), mas é exatamente o
> fio que a Fase 4 deixou para trás: os handlers reais vivem nos domínios e o
> `domain_wiring_test.go` os conecta. **Recomendação:** registrar como item de
> limpeza separado (pós-migração) — remover os `*_handler.go` de referência do
> `ipc` após garantir que o composition root registra os handlers reais dos
> domínios (o `ValidateRegistry` já prova a cobertura).

### Pontos de ruptura conhecidos (o que QUEBRA ao mover)

1. **Imports Go**: `focusguard/internal/<pkg>` em `cmd/*`, `internal/*`,
   `scripts/gen-contract`, `scripts/verifyicon`.
2. **`scripts/gen-contract/main.go`**: paths **hardcoded** para os structs do
   contrato (linhas ~46–59: `internal/policy/policy.go`, `internal/ipc/ipc.go`,
   etc.) — mover qualquer um desses quebra o `make contract`.
3. **`cmd/focusguard-icon/main.go`**: flags default `img/focusguard.png`,
   `focusguard.ico`, `focusguard.png`, `internal/tray/icon_source.png`.
4. **`Makefile`**: `go-winres --in ../../versioninfo.json` (daemon),
   `go run ./cmd/focusguard-icon`, `scripts/gen-contract`, `scripts/build-msi.sh`.
5. **`.goreleaser.yaml`**: `src: install.txt`, `src: focusguard.png`,
   `src: focusguard.ico`, `src: scripts/*`, hook `go-winres --in
   ../../versioninfo.json`, `go run ./cmd/focusguard-icon`.
6. **`scripts/build-msi.sh`**: espera `$ROOT/focusguard.ico` e
   `$ROOT/server.role`.
7. **`scripts/msi/wix.json` / `wix-server.json`**: `"icon": "focusguard.ico"`,
   `"path": "server.role"`.
8. **`scripts/verifyicon/main.go`**: lê `focusguard.ico` da raiz (cwd-relative).
9. **`install.txt`**: embutido nos archives Windows/Linux pelo goreleaser.
10. **Guides**: `AGENT.md` (mapa de pacotes, seções 3/6), `internal/AGENT.md`
    (mapa, 34 pacotes), `cmd/AGENT.md`, `scripts/AGENT.md` (tabela de assets),
    `README.md`.

---

## Estrutura final

```
focusguard/
├── cmd/                      # binários (mantém — já organizado)
│   ├── focusguard/           # CLI
│   ├── focusguard-daemon/    # serviço (composition root)
│   ├── focusguard-tray/
│   ├── focusguard-watchdog/
│   ├── focusguard-web/       # UI + proxy
│   └── focusguard-icon/      # build-time
├── internal/                 # ✅ Frente C — camadas (concluída)
│   ├── domain/               # regras de negócio (sem IO de SO) — 13 pacotes
│   │   ├── policy/  preset/  goal/  analytics/
│   │   ├── pomodoro/  schedule/  scheduler/  blocks/  apps/
│   │   ├── presets/  user/  users/  recovery/
│   ├── infrastructure/       # IO com o SO — 13 pacotes
│   │   ├── enforcer/  store/  fsutil/  tamper/  hostswatch/
│   │   ├── statewatch/  processguard/  dnsserver/  dns/
│   │   ├── autostart/  filelog/  icon/  update/
│   ├── transport/            # superfícies de comunicação — 5 pacotes
│   │   ├── ipc/  ipcerr/  httpapi/  metrics/  eventhub/
│   └── system/               # processos e ciclo de vida — 3 pacotes
│       ├── daemon/  tray/  watchdog/
├── packaging/                # ✅ Frente B — assets de build (concluída)
│   ├── versioninfo-daemon.json      (ex-raiz)
│   ├── focusguard-daemon.exe.manifest
│   ├── focusguard.ico / focusguard.png
│   ├── server.role / install.txt
│   └── artwork/focusguard.png       (ex-img/)
├── internal/system/tray/icon_source.png    # NÃO movido: go:embed exige o arquivo no pacote
├── scripts/                  # mantém (gen-contract, msi, verifyicon)
├── docs/                     # ✅ Frente A
│   ├── archive/              # task.md, follow-up-v0.15.1.md, plan-new-ui-and-user.md
│   ├── refactor-plan.md  ui-plan.md  bug-hunt-plan.md  release.md  ...
│   └── AGENT.md → remover (espelho)
├── focusguard-ui/            # mantém
├── AGENT.md                  # único guia canônico (+ AGENTS.md TL;DR que aponta p/ ele)
├── README.md  CHANGELOG.md  Makefile  .goreleaser.yaml  go.mod  ...
```

### Critério de agrupamento (Frente C)

- `domain/` = pacotes que só implementam regra de negócio; podem depender de
  `domain/*` e `ipcerr`.
- `infrastructure/` = IO com o SO (hosts, firewall, disco, processos, rede);
  pode depender de `domain/*` e `infrastructure/*`.
- `transport/` = protocolo IPC/HTTP e observabilidade; pode depender de
  `domain/*`, `infrastructure/*`, `transport/*`.
- `system/` = processos de sistema (daemon/tray/watchdog) e lifecycle.
- **Nenhum ciclo de import novo**: a regra é "depender para baixo" —
  `domain` ← `infrastructure` ← `transport` ← `system` ← `cmd`.

---

## Fases de execução (cada uma com validação contínua)

> **Regra global:** depois de **cada** commit, rodar
> `go build ./... && go vet ./... && go test ./... -count=1 -timeout=60s &&
> make contract-check` (e `tsc --noEmit` quando tocar no frontend). Nada de
> "mover e compilar depois".

### Fase A — Organização de docs (baixo risco, sem tocar Go) — ✅ concluída (2026-08-06)

1. ✅ Planos antigos da raiz (`task.md`, `follow-up-v0.15.1.md`,
   `plan-new-ui-and-user.md`) **removidos** — o mantenedor optou por deletar em
   vez de arquivar em `docs/archive/` (commit `d645321`); não havia referências
   fora deles próprios.
2. ✅ `docs/AGENT.md` (espelho divergente) **removido** — o canônico fica na raiz.
3. ✅ Guias consolidados: `AGENTS.md` (TL;DR PT) **removido** (`d645321`); mapa
   de pacotes do canônico (seção 3) atualizado para os **34 pacotes** atuais
   (`blocks`, `dns`, `ipcerr`, `presets`, `user`, `users` adicionados) e
   contagem da seção 6 corrigida (25 → 34).
4. ✅ `internal/AGENT.md` atualizado (23 → 34 pacotes, 11 linhas novas);
   `cmd/AGENT.md` e `scripts/AGENT.md` já estavam em dia (só texto, nada movido).
- **Validação:** revisão de links; nenhum build necessário (só texto).

### Fase B — Assets de build → `packaging/` (médio risco) — ✅ concluída (2026-08-06)

1. ✅ Criado `packaging/` (e `packaging/artwork/`); movidos com `git mv`:
   - `versioninfo.json` (raiz) → `packaging/versioninfo-daemon.json`
   - `focusguard-daemon.exe.manifest` → `packaging/`
   - `focusguard.ico`, `focusguard.png` → `packaging/`
   - `server.role`, `install.txt` → `packaging/`
   - `img/focusguard.png` → `packaging/artwork/focusguard.png` (`img/` removida)
   - ⚠️ **`internal/system/tray/icon_source.png` NÃO foi movido** — `go:embed`
     não aceita arquivos fora do diretório do pacote
     (`internal/system/tray/icon.go`), e o tray depende exclusivamente do
     ícone embutido (sem runtime). `packaging/tray/` foi descartado; o default
     do `focusguard-icon` continua gravando em `internal/system/tray/`.
2. ✅ Referências atualizadas:
   - `cmd/focusguard-icon/main.go` (flags default → `packaging/...`)
   - `cmd/focusguard-daemon/main.go` — `serverRoleFileName` é **runtime**
     (lê ao lado do exe): comportamento preservado; só a origem de build
     (`wix-server.json`) mudou para `packaging/server.role`
   - `Makefile` (`winres --in ../../packaging/versioninfo-daemon.json`)
   - `.goreleaser.yaml` (`src:` dos archives + hook do go-winres)
   - `scripts/build-msi.sh` (`$ROOT/packaging/focusguard.ico`,
     `$ROOT/packaging/server.role`)
   - `scripts/msi/wix.json`, `wix-server.json` (paths do ícone/role —
     resolvem por cwd, que é a raiz)
   - `scripts/verifyicon/main.go` (lê `packaging/focusguard.ico`)
   - `cmd/*/versioninfo.json` (4: `.ico`/manifest → `../../packaging/...`;
     o `packaging/versioninfo-daemon.json` mantém paths json-relativos)
   - `cmd/focusguard-daemon/main_test.go` (`TestVersionInfo_GoWinresFormat`
     → `../../packaging/versioninfo-daemon.json`)
   - `scripts/AGENT.md` (tabela: `packaging/server.role`) e `AGENT.md`
     (seção 6). `.gitignore` já não ignorava `packaging/` — nada a fazer
- **Validação:** `go build ./...` + `go vet ./...` + testes não-admin +
  `bash -n scripts/build-msi.sh` + `make icon`/`make winres` manuais
  (go-winres validou os paths dos 4 versioninfo).

### Fase C — `internal/` em camadas (alto risco — 28 pacotes)

> Estratégia: mover **por camada, da mais baixa para a mais alta** (folhas
> primeiro), um commit por camada, compilando a cada passo. NUNCA mover tudo de
> uma vez.

1. ✅ **C1 — Folhas e domínio puro** → `internal/domain/` (commit `7fc690a`):
   `policy`, `preset`, `goal`, `analytics`, `pomodoro`, `schedule`,
   `scheduler`, `recovery`, `user`, `apps`, `blocks`, `presets`, `users`;
   imports reescritos por sed (word-boundary) e paths do `gen-contract`
   atualizados (policy/preset/pomodoro/analytics/schedule). `go build`,
   `go vet`, testes e `contract-check` verdes. Fio solto documentado:
   `goal/apps/blocks/presets/users → ipc` (handlers usam tipos do transport)
   — limpeza separada, **não** no meio da migração.
2. ✅ **C2 — Infraestrutura** (concluída, commit `65d6dbe`) →
   `internal/infrastructure/`: `fsutil`, `store`, `tamper`, `enforcer`,
   `hostswatch`, `statewatch`, `processguard`, `dnsserver`, `dns`, `autostart`,
   `filelog`, `icon`, `update`; imports reescritos; build/vet/testes verdes.
3. ✅ **C3 — Transport** (concluída, commit `a5cc7a5`) → `internal/transport/`:
   `ipcerr`, `ipc`, `metrics`, `eventhub`, `httpapi`; imports reescritos (sed
   word-boundary + gofmt); paths do `gen-contract` atualizados (ipc/metrics) e
   `types.ts` regenerado; build/vet/testes/`contract-check` verdes.
4. ✅ **C4 — System** (concluída, commit `1722e01`) → `internal/system/`:
   `daemon`, `tray`, `watchdog`; imports reescritos; `verifyicon` atualizado
   (`internal/system/tray`); build/vet/testes verdes.
5. ✅ **C5 — `cmd/*`** (concluída): todos os imports de `cmd/*` já foram
   reescritos pelos seds de C3/C4; `grep` confirmou zero imports flat
   restantes.
6. ✅ **C6 — Ferramentas** (concluída): `gen-contract` atualizado na C3 (paths
   do contrato + header gerado), `verifyicon` na C4, comentários do `Makefile`
   corrigidos; `.goreleaser.yaml` e `release.yml` não referenciam `internal/`.
7. ✅ **C7 — Docs** (concluída): mapas das 4 camadas atualizados no `AGENT.md`
   (raiz), `internal/AGENT.md` e `cmd/AGENT.md`; este documento marcado como
   concluído.

- **Ferramentas de migração:** `git mv` para preservar histórico; sed para
  reescrever imports (`s|focusguard/internal/<pkg>|focusguard/internal/<camada>/<pkg>|g`);
  `goimports -w` para limpar; `go build ./...` como gate a cada commit.
- **Validação:** após cada commit de camada —
  `go build ./... && go vet ./... && go test ./... -count=1 -timeout=60s && make contract-check`.
- **Cuidado especial:** o `ipc` (hub) migrar por último dentro do transport;
  revisar se os imports de domínio no `ipc` de produção são realmente
  necessários ou herança dos testes de referência (fio solto da Fase 4 —
  candidato a limpeza separada, **não** no meio da migração).

### Fase D — Validação final e release — ✅ concluída como **validação definitiva** (2026-08-07)

> Re-executada após a C7 com o grafo completo em camadas (domain →
> infrastructure → transport → system).

1. ✅ Suíte Go completa 2× (`go build` + `go vet` + testes não-admin, 2
   passes verdes) + `contract-check` em dia; frontend `tsc --noEmit` +
   `vitest run` verdes. Nota: `go test ./cmd/focusguard-daemon/...` falha em
   shell não-elevado (manifest `requireAdministrator`) — limitação ambiental,
   não bug da migração.
2. ✅ Smoke de build: binários compilados; `goreleaser check` validou o
   `.goreleaser.yaml`; MSIs regenerados em `dist/` com os paths de
   `packaging/` e os novos imports das camadas (quando o ambiente WiX está
   disponível).
3. ✅ Docs sincronizados (mapas das 4 camadas em `AGENT.md`, `internal/AGENT.md`
   e `cmd/AGENT.md`) e commits por camada (C3 `a5cc7a5`, C4 `1722e01`, docs
   C7). Tag de release seguinte: pendente (não pedida).

---

## Pós-reorg (próximos passos — fora do escopo da migração)

A migração em camadas está concluída, mas deixou fios soltos documentados que
ficam **fora** do escopo de mover pacotes (não introduzem ciclos — o build
prova —, mas acoplam camadas):

1. **`transport/ipc` importa `domain/*` e `infrastructure/dnsserver` em
   produção** — os `*_handler.go` de referência (`analytics_handler.go`,
   `pomodoro_handler.go`, `schedule_handler.go`, `update_handler.go`) e o
   `server.go` (dnsserver) são herança da Fase 4. O composition root
   (`system/daemon`) já registra os handlers reais dos domínios e o
   `ValidateRegistry` prova a cobertura → os arquivos de referência podem ser
   removidos. **(pendente — item 1)**
2. ✅ **Violações de camada (depender "para cima")** — resolvido (2026-08-07):
   `domain/{apps,blocks,goal,presets,users}` e `infrastructure/dns` **não
   importam mais** `transport/ipc` (e os 3 domínios que usavam `ipcerr`
   passaram a importar `internal/domain/ipcerr`). O DIP foi aplicado com o
   adaptador genérico `ipc.DomainAction[In, Out]` (`transport/ipc/adapt.go`):
   cada pacote define seus próprios tipos de entrada/saída, o composition root
   (`cmd/focusguard-daemon`) traduz o wire (Decode/Encode) e o roteador
   continua convertendo erros (`*ipcerr.Error` → `Success:false + Code`). O
   wire (`ipc.Request/Response`) e o `types.ts` **não mudaram**. Commits:
   `e7c7a81` (ipcerr→domain), `20ecdc6` (adapter), `2cbcf4e` (apps),
   `0cde265` (goal+presets), `4db92b9` (users+blocks), `1f39a4c` (dns).

---

## Riscos e mitigações

| Risco | Mitigação |
|---|---|
| Imports quebrados em massa | Migração por camada + `go build` gate a cada commit; sed + `goimports` |
| `make contract` quebra (paths hardcoded do gen-contract) | Atualizar `scripts/gen-contract` na MESMA etapa que move cada struct do contrato; `make contract-check` no CI da release |
| Icones/versioninfo somem do build | `make icon && make winres` como validação obrigatória da Fase B |
| MSI quebra (wix paths) | `goreleaser check` + revisão dos `wix*.json`; testar `build-msi.sh` com `-n` |
| Guias divergentes de novo | Único canônico na raiz; TL;DR vira link; mapa de pacotes atualizado na Fase A |
| Perda de histórico (git mv) | Sempre `git mv`, nunca mover via `os.rename`/recriar |
| Ciclo de import novo | Regra "depender para baixo"; `go build` detecta; revisar o grafo antes de cada camada |

## O que NÃO será feito

- Não criar subpastas dentro de `domain/` por domínio (política x preset são
  pacotes, não pastas).
- Não renomear pacotes Go (só mover) — `package` name preservado.
- Não mudar o comportamento runtime (`server.role`, paths de instalação,
  socket) — a Fase B mexe só em **origem de build**, não em paths de runtime.
- Não tocar em `focusguard-ui/` nem `scripts/msi/product.wxs` além do
  necessário para paths de assets.
