# Plano — Reorganização de Diretórios e Arquitetura

> **Status:** documento vivo — **planejamento** (nada executado ainda).
> **Criado em 2026-08-06.** Escopo escolhido: **todas as 3 frentes** —
> (A) organização de docs, (B) assets de build em diretório próprio,
> (C) `internal/` agrupado em camadas.

## Diagnóstico (estado atual)

### Raiz poluída

| Item na raiz | Problema | Destino proposto |
|---|---|---|
| `focusguard.exe`, `focusguard-daemon.exe`, `focusguard-web.exe`, `scheduler.test.exe` | Binários/lixo locais commitados ou esquecidos | **Deletar** (`.gitignore` já cobre `*.exe`/`*.test`) |
| `task.md`, `follow-up-v0.15.1.md`, `plan-new-ui-and-user.md` | Planos antigos já implementados (v0.15.x / F4-F5) soltos na raiz | `docs/archive/` |
| `AGENT.md` + `AGENTS.md` + `docs/AGENT.md` | **3 guias redundantes e divergentes** (EN completo 461 linhas × TL;DR PT 37 linhas × espelho 446 linhas) | Consolidar: **1 canônico** na raiz + TL;DR aponta para ele |
| `versioninfo.json` (raiz), `focusguard-daemon.exe.manifest`, `focusguard.ico/.png`, `server.role`, `install.txt` | Assets de build espalhados na raiz | `packaging/` (ver Frente B) |
| `img/focusguard.png` | Artwork canônico (1024px) | `packaging/artwork/` |
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
    (mapa, conta 23 pacotes), `cmd/AGENT.md`, `scripts/AGENT.md` (tabela de
    assets), `docs/AGENT.md`, `README.md`.

---

## Estrutura alvo

```
focusguard/
├── cmd/                      # binários (mantém — já organizado)
│   ├── focusguard/           # CLI
│   ├── focusguard-daemon/    # serviço (composition root)
│   ├── focusguard-tray/
│   ├── focusguard-watchdog/
│   ├── focusguard-web/       # UI + proxy
│   └── focusguard-icon/      # build-time
├── internal/                 # ✅ Frente C — camadas
│   ├── domain/               # regras de negócio (sem IO de SO)
│   │   ├── policy/  preset/  goal/  analytics/
│   │   ├── pomodoro/  schedule/  scheduler/  blocks/  apps/
│   │   ├── presets/  user/  users/  recovery/
│   ├── infrastructure/       # IO com o SO
│   │   ├── enforcer/  store/  fsutil/  tamper/  hostswatch/
│   │   ├── statewatch/  processguard/  dnsserver/  dns/
│   │   ├── autostart/  filelog/  icon/  update/
│   └── transport/            # superfícies de comunicação
│       ├── ipc/  ipcerr/  httpapi/  metrics/  eventhub/
│   └── system/               # processos e ciclo de vida
│       ├── daemon/  tray/  watchdog/
├── packaging/                # ✅ Frente B — assets de build
│   ├── versioninfo-daemon.json      (ex-raiz)
│   ├── focusguard-daemon.exe.manifest
│   ├── focusguard.ico / focusguard.png
│   ├── server.role / install.txt
│   ├── artwork/focusguard.png       (ex-img/)
│   └── tray/icon_source.png
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

### Fase A — Organização de docs (baixo risco, sem tocar Go)

1. `docs/archive/`: mover `task.md`, `follow-up-v0.15.1.md`,
   `plan-new-ui-and-user.md`; corrigir links que apontem para eles (não há
   referências fora deles próprios — confirmado).
2. Remover `docs/AGENT.md` (espelho divergente — o canônico fica na raiz).
3. Reconciliar `AGENT.md` (EN, completo) × `AGENTS.md` (TL;DR PT): manter os
   dois, mas o TL;DR vira **resumo + link** para o canônico (sem duplicar
   tabelas). Atualizar o mapa de pacotes do canônico (seção 3) para os 28
   pacotes **atuais** (já com `blocks`, `dns`, `ipcerr`, `presets`, `users`,
   `daemon`, `eventhub`, `metrics`).
4. Atualizar `internal/AGENT.md`, `cmd/AGENT.md`, `scripts/AGENT.md` (tabelas
   de pacotes/arquivos) — só texto, sem mover nada ainda.
- **Validação:** `git mv` + revisão de links; nenhum build necessário.

### Fase B — Assets de build → `packaging/` (médio risco)

1. Criar `packaging/` (e `packaging/artwork/`, `packaging/tray/`); mover:
   - `versioninfo.json` (raiz) → `packaging/versioninfo-daemon.json`
   - `focusguard-daemon.exe.manifest` → `packaging/`
   - `focusguard.ico`, `focusguard.png` → `packaging/`
   - `server.role`, `install.txt` → `packaging/`
   - `img/focusguard.png` → `packaging/artwork/focusguard.png`
   - `internal/tray/icon_source.png` → `packaging/tray/icon_source.png` (o
     gerador `focusguard-icon` grava lá; o tray embute via go:embed — ajustar
     o embed path)
2. Atualizar as referências (checklist completo):
   - `cmd/focusguard-icon/main.go` (flags default)
   - `cmd/focusguard-daemon/main.go` (`serverRoleFileName` é runtime — conferir
     se lê ao lado do exe ou da raiz; **não** mudar o comportamento runtime,
     só o artefato de origem no build)
   - `Makefile` (`winres --in`, `icon` target)
   - `.goreleaser.yaml` (`src:` dos archives + hooks)
   - `scripts/build-msi.sh` (`$ROOT/focusguard.ico`, `$ROOT/server.role`)
   - `scripts/msi/wix.json`, `wix-server.json` (paths do ícone/role)
   - `scripts/verifyicon/main.go` (cwd-relative → flag `-ico packaging/...`)
   - `scripts/AGENT.md` (tabela de assets) e `.gitignore` (garantir que
     `packaging/` NÃO é ignorado; `build/` continua ignorado)
- **Validação:** `make icon && make winres && go build ./...` no Windows +
  `bash -n scripts/build-msi.sh` + revisão do goreleaser (`goreleaser check`).

### Fase C — `internal/` em camadas (alto risco — 28 pacotes)

> Estratégia: mover **por camada, da mais baixa para a mais alta** (folhas
> primeiro), um commit por camada, compilando a cada passo. NUNCA mover tudo de
> uma vez.

1. **C1 — Folhas e domínio puro** → `internal/domain/`: `policy`, `preset`,
   `goal`, `analytics`, `pomodoro`, `schedule`, `scheduler`, `recovery`,
   `user`, `apps`, `blocks`, `presets`, `users` (ajustar paths no
   `gen-contract` à medida que cada struct do contrato mudar de pasta).
2. **C2 — Infraestrutura** → `internal/infrastructure/`: `fsutil`, `store`,
   `tamper`, `enforcer`, `hostswatch`, `statewatch`, `processguard`,
   `dnsserver`, `dns`, `autostart`, `filelog`, `icon`, `update`.
3. **C3 — Transport** → `internal/transport/`: `ipcerr`, `ipc`, `metrics`,
   `eventhub`, `httpapi`.
4. **C4 — System** → `internal/system/`: `daemon`, `tray`, `watchdog`.
5. **C5 — `cmd/*`**: atualizar todos os imports para os novos paths.
6. **C6 — Ferramentas**: `scripts/gen-contract/main.go` (paths do contrato),
   `scripts/verifyicon` (se usa `internal/tray`), `Makefile` (targets que
   referenciam `internal/...`), `.goreleaser.yaml` (main paths dos builds).
7. **C7 — Docs**: reescrever mapas no `AGENT.md`, `internal/AGENT.md`,
   `cmd/AGENT.md`, `docs/README` se existir.

- **Ferramentas de migração:** `git mv` para preservar histórico; sed para
  reescrever imports (`s|focusguard/internal/<pkg>|focusguard/internal/<camada>/<pkg>|g`);
  `goimports -w` para limpar; `go build ./...` como gate a cada commit.
- **Validação:** após cada commit de camada —
  `go build ./... && go vet ./... && go test ./... -count=1 -timeout=60s && make contract-check`.
- **Cuidado especial:** o `ipc` (hub) migrar por último dentro do transport;
  revisar se os imports de domínio no `ipc` de produção são realmente
  necessários ou herança dos testes de referência (fio solto da Fase 4 —
  candidato a limpeza separada, **não** no meio da migração).

### Fase D — Validação final e release

1. Suíte completa 2× (Go + frontend) + `goreleaser check` + `make ui`.
2. Smoke test de build: `make build` (todos os binários) e `make msi` (se o
   host tiver WiX) para provar que packaging/ e os novos imports fecham.
3. Commit final + (opcional) tag de release seguinte.

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
