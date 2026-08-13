# Desenvolvimento — FocusGuard

Guia para quem vai **compilar, testar ou mexer no código**. Para usar o
FocusGuard, veja o [README](../README.md).

> 📌 **Convenções do projeto:** o [AGENT.md](../AGENT.md) na raiz é a fonte de
> verdade das regras de desenvolvimento (IPC é o contrato, ações no registry,
> daemon exige admin, `make contract` após mudar structs do contrato…). Leia-o
> antes do primeiro PR.

---

## Stack

| Camada | Tecnologia |
|---|---|
| Backend | Go `1.26` (go.mod: `1.26.5`, módulo `focusguard`) |
| Frontend | React + TypeScript + Vite (`focusguard-ui/`) |
| Comunicação | Socket IPC (JSON sobre loopback) + HTTP (`focusguard-web`) |
| Destaques de deps | `go-selfupdate` (auto-update), `fsnotify` (watchers), `systray` (bandeja), `miekg/dns` (sinkhole), `x/crypto` (bcrypt), `x/mod` (semver) |

---

## Layout do repositório

```
focusguard/
├── Makefile / .goreleaser.yaml
├── AGENT.md                     # convenções de desenvolvimento
├── cmd/
│   ├── focusguard/              # CLI (um arquivo por comando + tabela commands)
│   ├── focusguard-daemon/       # serviço em background (privilegiado)
│   ├── focusguard-tray/         # bandeja do sistema (sem admin)
│   ├── focusguard-watchdog/     # health-check externo + smart recovery
│   ├── focusguard-web/          # painel HTTP user-space (proxy das ações IPC)
│   └── focusguard-icon/         # gera ícones (build-time)
├── focusguard-ui/               # frontend React + TypeScript
│   └── src/api/types.ts         # contrato IPC espelhado em TS (gerado!)
├── internal/
│   ├── domain/                  # lógica de negócio (scheduler, schedule, pomodoro, …)
│   ├── infrastructure/          # I/O de SO (enforcer, store, dnsserver, autostart, …)
│   ├── transport/               # IPC/HTTP + observabilidade (ipc, httpapi, eventhub, …)
│   └── system/                  # ciclo de vida (daemon, tray, watchdog)
├── scripts/                     # install-daemon.ps1, install-linux.sh, build-msi.sh, msi/
├── packaging/                   # assets de build (ícones, manifest, server.role, install.txt)
└── docs/                        # planos e decisões (ver índice no fim)
```

As camadas de `internal/` seguem o fluxo de dependência **domain → infrastructure
→ transport/system** (DIP): os pacotes de domínio nunca importam transporte.

---

## Build

**Pré-requisitos**

- Go `1.26+`
- `Node.js`/`npm` — só para `make ui` (frontend embutido)
- `go-winres` — só para `make winres` (recursos `.exe` do Windows)
- Windows + WiX 3.10 — só para `make msi` (instaladores)

**Comandos**

```bash
make ui          # frontend React → cmd/focusguard-web/assets (go:embed)
make build       # icon + CLI + daemon + web em ./bin/
make build-cli   # só o CLI
make build-daemon
make build-web
```

> ⚠️ Sem `make ui`, o `focusguard-web` embute uma pasta vazia e a UI abre a
> página "UI não compilada". O `build-web` avisa quando os assets estão vazios.

**Manual**

```bash
go build -o bin/focusguard.exe ./cmd/focusguard
go build -o bin/focusguard-daemon.exe ./cmd/focusguard-daemon
go build -o bin/focusguard-web ./cmd/focusguard-web
```

> No Linux os binários não têm `.exe`. O CLI localiza os binários irmãos pelo
> nome com extensão do SO atual.

### Alvos do Makefile

| Alvo | O que faz |
|---|---|
| `make all` | build + test + vet |
| `make build` | icon + build-cli + build-daemon + build-web |
| `make build-cli` / `build-daemon` / `build-web` | binários individuais (CLI / daemon / web) |
| `make ui` | compila o frontend e copia o `dist` para `cmd/focusguard-web/assets` |
| `make icon` | regenera `.ico`/`.png` a partir de `packaging/artwork/focusguard.png` |
| `make winres` | gera `rsrc_windows_*.syso` via go-winres (metadados/ícone dos `.exe`) |
| `make contract` | regenera `focusguard-ui/src/api/types.ts` a partir do contrato Go |
| `make contract-check` | falha se `types.ts` divergiu (CI roda antes da release) |
| `make msi VERSION=x.y.z [ARCH=amd64|arm64]` | instalador desktop `.msi` (go-msi + WiX) |
| `make msi-server VERSION=x.y.z` | instalador Server/headless `.msi` |
| `make install` / `make uninstall` | build + instala/remove como serviço |
| `make test` | `go test ./... -count=1 -timeout=60s` |
| `make session-check` | falha se o resumo da sessão de hoje (`docs/session-log/`) não existir — handoff diário (AGENT.md §4.15) |
| `make vet` / `make fmt` / `make tidy` / `make clean` | vet / fmt / tidy / limpeza |
| `make help` | lista os alvos |

---

## Frontend (`focusguard-ui/`)

- **Dev** — `cd focusguard-ui && npm run dev` (Vite com proxy `/api` → `focusguard-web`).
- **Embutido** — `make ui` compila e o `go:embed` coloca no binário do
  `focusguard-web`.
- **Contrato** — `focusguard-ui/src/api/types.ts` é **gerado** (o Go é a fonte
  da verdade). Mudou um struct do contrato IPC → rode `make contract` e altere
  os 4 lados (CLI/tray/web/daemon) no mesmo commit (regra do AGENT.md).
- **Telas** — `src/screens/`: Dashboard, Bloquear, Pomodoro, Agenda, Apps,
  Presets, Estatísticas, Segurança, Configurações, Login. Dados em tempo real
  via SSE (`/api/events`, event hub) com fallback de polling.

---

## Arquitetura

### Componentes

| Binário | Papel | Privilégio |
|---|---|---|
| `focusguard` | CLI (bloquear, status, stats, doctor…) | usuário |
| `focusguard-daemon` | serviço em background: estado, bloqueios, watchers, DNS, update | **root/Administrador** |
| `focusguard-web` | painel HTTP em `http://127.0.0.1:48902` (proxy das ações IPC) | usuário |
| `focusguard-tray` | bandeja do Windows (bloco rápido 4h, status, update) | usuário |
| `focusguard-watchdog` | health-check do daemon + smart recovery de updates | serviço Windows / systemd |

### Fluxo de dados

```
CLI (focusguard) ←→ Daemon (focusguard-daemon)
tray / web ←→ [IPC Server] ←→ [Scheduler] ←→ [Store] (state.json)
                                        └──→ [Enforcer] → hosts + firewall
                                        └──→ [HostsWatcher]/[StateWatcher] (fsnotify)
                                        └──→ [DNSServer] :53 (edição Server)
```

- **Scheduler** — ciclo de vida dos bloqueios (`Block`, expiração por timer,
  reconciliação no boot, refresh de IPs a cada 15 min).
- **Enforcer** — aplica regras no SO: `iptables`/`ip6tables` (Linux) ou
  `netsh advfirewall` (Windows) + edição do arquivo `hosts`.
- **Watchers** — detectam adulteração externa (hash SHA-256) e reaplicam o
  estado a partir da RAM.
- **DNSServer** — sinkhole na porta 53 (`miekg/dns`), upstream Cloudflare
  Security `1.1.1.2`, bloqueio de DoH na porta 853.
- **IPC** — socket em `/run/focusguard.sock` (`0660`, grupo `focusguard`) no
  Linux ou `%PROGRAMDATA%\FocusGuard\focusguard.sock` no Windows. Ações vivem
  no registry (`ipc.Register`), não em `switch`.

### Update (caminho do `focusguard update`)

O **Orchestrator** (`internal/infrastructure/update`) orquestra: baixa a
release UMA vez, faz backup de toda a suíte (`*.bak.<timestamp>`), troca os
binários com rollback atômico, para watchdog/tray no Windows antes do swap,
marca `update.inprogress` (Bug 2), e o daemon reinicia sozinho. Falha no
rename → fallback **move-on-reboot** (`ErrScheduledOnReboot`). O watchdog
restaura o `.bak` recente se a nova versão crash-loope (smart recovery). A
limpeza de backups antigos roda em todo boot (`CleanupStale`, janela de 1h).

---

## Testes

```bash
make test                # go test ./... -count=1 -timeout=60s
go test -cover ./...     # com cobertura
make contract-check      # contrato IPC ↔ types.ts em dia
make vet
```

- **Fuzz** (parser de calendário/duração/relógio) — `internal/domain/schedule/fuzz_test.go`:
  `go test ./internal/domain/schedule/ -run '^$' -fuzz FuzzParseICS -fuzztime=30s`
  (também `FuzzWindowsPairs` e `FuzzParseClock`).
- **Windows** — os testes do daemon exigem shell **elevado** (manifest
  `requireAdministrator`); o resto da suíte roda normal. `-race` não roda no
  Windows local (CGO off).
- **CI** (`.github/workflows/test.yml`) — build+vet em todo push/PR; job
  dedicado com `-race` no Linux; teste de chown do socket roda como root.
- **Release** (`.github/workflows/release.yml`) — tag `v*` → GoReleaser +
  geração dos dois `.msi` (desktop/server).

---

## Release

1. Suba a versão nos `versioninfo.json` de `cmd/*` (e `packaging/versioninfo-daemon.json`);
2. Atualize o `CHANGELOG.md`;
3. Tag `vX.Y.Z` → o CI publica os artefatos (zips, `.tar.gz`, `.msi` desktop e server);
4. Verifique `make contract-check` e a suíte antes (o CI faz isso).

Detalhes do processo em [`docs/release.md`](release.md).

---

## Índice de documentação

| Arquivo | Conteúdo |
|---|---|
| [`../AGENT.md`](../AGENT.md) | Convenções de desenvolvimento (regras por área) |
| [`features-plan.md`](features-plan.md) | Fases do produto (doctor, clock guard, interceptor, devices, relatório, conquistas) |
| [`ui-plan.md`](ui-plan.md) | Plano e roadmap da interface web |
| [`refactor-plan.md`](refactor-plan.md) | Refatoração em fases (reorg, DIP, contract) |
| [`reorg-plan.md`](reorg-plan.md) | Reorganização de pacotes (4 camadas) |
| [`bug-hunt-plan.md`](bug-hunt-plan.md) | Bug hunt pós-v0.16 (etapas, achados, fix) |
| [`dns-sinkhole-spec.md`](dns-sinkhole-spec.md) | Especificação do DNS sinkhole ("Rei da Rede") |
| [`perf-2026-08-05.md`](perf-2026-08-05.md) | Perfil de performance e limitações de ambiente |
| [`release.md`](release.md) | Processo de release |
| [`linux-validation-plan.md`](linux-validation-plan.md) | Validação completa no Linux (planejada — nunca testado em máquina real) |
| [`session-log/`](session-log/) | Resumos diários de sessão (handoff entre agentes — template na pasta; validação via `make session-check` + job `session-log` do CI) |
