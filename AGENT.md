# AGENT.md — Guia do repositório FocusGuard

> Documento de referência para **agentes de IA e desenvolvedores** que vão
> trabalhar neste repositório. Leia antes de editar código. Mantenha este
> arquivo atualizado quando convenções, arquitetura ou regras mudarem.

---

## 1. Specs — O que é este projeto

**FocusGuard** é uma ferramenta Go para **bloquear sites distractivos** e
manter o foco, operando em nível de sistema (arquivo `hosts` + regras de
firewall). É uma aplicação cliente-servidor:

- **CLI** (`focusguard`) — interface de linha de comando; sem argumentos abre
  a interface web no navegador.
- **Daemon** (`focusguard-daemon`) — serviço em background que aplica e mantém
  os bloqueios; comunica-se com a CLI via IPC (Unix socket).
- **Tray** (`focusguard-tray`) — ícone na bandeja do sistema com ações rápidas.
- **Watchdog** (`focusguard-watchdog`) — serviço externo de health-check e
  Smart Recovery (rollback pós-update quebrado).
- **`focusguard-icon`** — comando **somente de build** (stdlib pura) que gera o
  ícone multi-tamanho `focusguard.ico` (Windows) e `focusguard.png` (Linux).

Funcionalidades principais: bloqueios temporários (sem desbloqueio manual),
modo pânico (`block --internet`) com allowlist, presets por categoria, pomodoro,
agendamento recorrente, metas diárias + streak, analytics com exportação,
process guard (encerra processos da denylist), detecção de burla (tamper-log) e
auto-update multi-binário com rollback.

> 🚧 **Interface web (em andamento):** `focusguard-web` (user-space, por
> demanda) serve a UI React + TS (`focusguard-ui/`) e faz **proxy das ações IPC
> para o daemon** em `http://127.0.0.1:48902` — **sem mudanças no daemon**.
> F1 (servidor HTTP) e F2 (4 telas do MVP) implementadas em 2026-08-03; veja o
> plano completo e o roadmap em `docs/ui-plan.md` antes de criar código
> relacionado.

### Plataformas

| | Linux | Windows |
|---|---|---|
| Firewall | `iptables`/`ip6tables` | `netsh advfirewall` |
| Hosts | `/etc/hosts` | `C:\Windows\System32\drivers\etc\hosts` |
| Socket IPC | `/run/focusguard.sock` | `%PROGRAMDATA%\FocusGuard\focusguard.sock` |
| Serviço | systemd (unit + watchdog `NOTIFY_SOCKET`) | Serviço nativo `svc` (SCM), `sc.exe` |
| Instalação | `/opt/focusguard` (root:root) | `C:\Program Files\FocusGuard` (Sistema / Todos os Usuários) |
| Estado | `/var/lib/focusguard/` | `C:\ProgramData\FocusGuard\` |
| Tray autostart | `~/.config/autostart` (XDG) | HKCU `...\CurrentVersion\Run` |

---

## 2. Linguagem e stack

- **Go 1.26.5** (ver `go.mod`, módulo `focusguard`). Sem CGO no Windows e no
  Linux exceto o tray (`tray-linux` requer CGO: appindicator/GTK).
- Dependências principais (todas já em `go.mod` — **evite adicionar novas sem
  necessidade; stdlib primeiro**):
  - `fsnotify` — watchers de arquivo (hosts, state).
  - `getlantern/systray` — bandeja do sistema.
  - `creativeprojects/go-selfupdate` — auto-update (canais beta/stable).
  - `golang.org/x/sys` — serviço Windows (`svc`), etc.
  - `golang.org/x/mod/semver` — comparação de versões no update.
- **`go-winres`** (ferramenta de build, não é dependência Go) — gera os
  metadados `.exe` do Windows (`rsrc_windows_*.syso`) a partir dos
  `versioninfo.json`. Instale com:
  `go install github.com/tc-hib/go-winres@latest`.

### Idiomas

- **Código, identificadores e comentários de código**: inglês.
- **Mensagens de UI/CLI, README, CHANGELOG, docs e este AGENT.md**: português
  (PT-BR).
- **Mensagens de commit**: inglês (Conventional Commits — seção 7).

---

## 3. Arquitetura

### Fluxo de dados

```
CLI (focusguard) ←────── IPC (Unix socket, JSON) ──────→ Daemon (focusguard-daemon)
Tray (focusguard-tray) ──┘                                        │
                                                             [Scheduler]  ← fonte de verdade em RAM
                                                          ┌────────┴────────┐
                                                     [Store]          [Enforcer]
                                               (state.json atômico)   ┌────┴────┐
                                                               /etc/hosts  Firewall
                                                               (hosts)  (iptables/netsh)
                                                          ┌────────┴────────┐
                                                   [HostsWatcher]    [StateWatcher]
                                                  (fsnotify + SHA-256, restauram o disco a partir da RAM)
```

### Binários (`cmd/`)

| Binário | Papel | Recursos Windows |
|---|---|---|
| `focusguard` | CLI (comandos; sem args abre a web) | `cmd/focusguard/versioninfo.json` (ícone, sem manifest) |
| `focusguard-daemon` | Serviço em background | `versioninfo.json` raiz + `focusguard-daemon.exe.manifest` (**`requireAdministrator`**) |
| `focusguard-tray` | Bandeja do sistema | `cmd/focusguard-tray/versioninfo.json` (**só ícone — NUNCA manifest/admin**) |
| `focusguard-watchdog` | Health-check / Smart Recovery | Sem recursos (console app) |
| `focusguard-web` | Serve a UI web + proxy das ações IPC (user-space, por demanda) | Sem manifest — **nunca** adicionar admin |
| `focusguard-icon` | Gera ícones (build-time) | — |

> ⚠️ O daemon embute manifest `requireAdministrator`; por isso **rodar
> `go test ./cmd/focusguard-daemon/...` num shell sem elevação falha** (o
> binário de teste não inicia) — é limitação ambiental, não bug.

### Pacotes internos (`internal/`) — mapa rápido

| Pacote | Responsabilidade |
|---|---|
| `analytics` | Histórico de sessões em JSONL; streak, `stats` (ASCII + export CSV/JSON/HTML), `report` |
| `apps` | Denylist de processos (apps.json) consumida pelo process guard; fallback `steam, discord` |
| `autostart` | Instala/remove serviço (`sc`/systemd), autostart do tray (HKCU Run / XDG) e **atalho de desktop** com ícone extraído (`ExtractIcon`) |
| `enforcer` | Aplica bloqueios no SO: interface + `enforcer_linux.go` (hosts + iptables) / `enforcer_windows.go` (hosts + netsh); `BlockAll`/allowlist; sanitização de domínios |
| `fsutil` | Helpers de filesystem compartilhados pelos watchers (ex.: SHA-256 de arquivo) |
| `goal` | Meta diária de foco (goal.json) |
| `hostswatch` | Watcher do `hosts`: fsnotify + hash SHA-256 dos self-writes; detecta/reverte adulterações |
| `httpapi` | Servidor HTTP da interface web: proxy das ações IPC (`/api/action`, `/api/ping`, `/api/health`) + UI estática com SPA fallback + guardas de segurança localhost (Host, Content-Type, CSP) |
| `icon` | Desenho do escudo/checkmark (stdlib pura); `GenerateICO` multi-tamanho + `GeneratePNG` |
| `ipc` | Protocolo cliente-servidor: `Request`/`Response` JSON sobre Unix socket; `SendWithTimeout` |
| `policy` | Modelo `Block` e regras de negócio (`IsActive`, `CanUnblock`, `RemainingTime`) |
| `pomodoro` | Sessões work/rest/cycles (`--strict`, `--save`/defaults, missões/labels) |
| `preset` | Catálogo de categorias: builtin (`social`, `video`, `news`, `games`) + personalizados |
| `processguard` | Encerra processos da denylist a cada 5s enquanto houver sessão ativa |
| `recovery` | Smart Recovery: `FindRecentBackup`, `ShouldRollBack`, `RestoreFromBackup` (watchdog) |
| `schedule` | Agendamento recorrente (dias/horários, janelas overnight, `--windows`, import iCal); worker 30s |
| `scheduler` | Ciclo de vida dos bloqueios: `Block`, `Start` (reconcile), expiração, refresh de IPs (15min) |
| `statewatch` | Watcher do `state.json`: restaura o disco a partir da RAM (memória é a fonte de verdade) |
| `store` | Persistência JSON atômica (temp file + rename) + réplicas AES-256-GCM atreladas ao hardware |
| `tamper` | Log JSONL append-only de tentativas de burla detectadas/revertidas (`tamper-log`) |
| `tray` | Controlador do systray: menu, tooltip dinâmico, notificações, IPC com timeout |
| `update` | Auto-update via go-selfupdate: canais beta/stable, atualização **multi-binário atômica** (`UpdateToAll`) |
| `watchdog` | Health check systemd (`NOTIFY_SOCKET` → `READY=1`/`WATCHDOG=1`) |

> A maioria desses pacotes tem teste dedicado (`*_test.go` no mesmo diretório).

---

## 4. Padrões de código (regras atuais)

1. **TDD** — escreva testes junto com a feature, no mesmo commit. Os pacotes
   têm cobertura extensa (ver tabela de testes no README). Não quebre testes
   existentes.
2. **Arquivos por plataforma** — use sufixos `_windows.go`, `_linux.go`,
   `_other.go` (build tags implícitas) para código de SO, com uma interface
   comum no arquivo-base (ex.: `enforcer.go`, `systray.go`).
3. **Fonte de verdade em RAM** — o scheduler mantém o estado em memória; o
   disco (state.json) é reflexo. Watchers restauram o disco a partir da RAM
   quando detectam adulteração.
4. **Escrita atômica** — `store` grava em temp file + rename; gravações do
   próprio daemon são marcadas por **SHA-256** para os watchers não reagirem a
   self-writes (sem loop).
5. **Best-effort para o SO** — falha de firewall/notificação/autostart **nunca
   aborta o daemon**: loga, avisa e segue. Padrão: operações auxiliares
   retornam erro sem derrubar o fluxo principal.
6. **IPC nunca bloqueia o tray** — toda chamada do tray ao daemon usa
   `SendWithTimeout` (5s); handlers do systray são não-bloqueantes.
7. **Defensivo** — sanitize domínios (`sanitizeDomain`: remove scheme/CRLF/
   espaços, colapsa `www.`), valide inputs, use tetos (ex.: pomodoro `--work`
   máx 7 dias), rollback atômico no update, sweep de regras de firewall órfãs.
8. **Persistência JSONL append-only** para logs (analytics, tamper) — linhas
   corrompidas são puladas na leitura.
9. **Sem dependências novas sem necessidade** — stdlib primeiro; confira o
   `go.mod` antes de trazer biblioteca.
10. **Recursos Windows via go-winres** — alterou `versioninfo.json` ou o ícone?
    Rode `make winres` (e `make icon` para regenerar `focusguard.ico`/`.png`).
    Os `.syso` **são commitados** (o CI precisa deles).
14. **UI web: `make ui` antes de compilar o `focusguard-web`** — o dist do React
    é copiado para `cmd/focusguard-web/assets` (gitignored, só o `.gitkeep` é
    versionado) e embutido via `go:embed`. Sem `make ui`, o binário serve a
    página "rode make ui". Dev: `go run ./cmd/focusguard-web -assets focusguard-ui/dist`
    ou `cd focusguard-ui && npm run dev` (Vite :5173 com proxy `/api`).
11. **Nunca adicione manifest ao tray** — o tray roda como usuário comum; sem
    elevação. O daemon é o único com `requireAdministrator`.
12. **Arquivos de script** — `*.sh` com EOL LF (`.gitattributes`); o
    `install-daemon.ps1` deve manter **BOM UTF-8** (use escrita com
    `UTF8Encoding($true)` ao re-salvar).
13. **Testes mockam o SO** — os testes dos pacotes que executam comandos
    externos (autostart, enforcer, etc.) mockam `execCommand` e `os.Stat`
    (ex.: helper `fakeFileInfo`) para não depender de binários reais do
    sistema nem de elevação. Siga esse padrão em testes novos — nunca chame
    `sc.exe`/`iptables`/`systemctl` de verdade dentro de um teste unitário.

---

## 5. Testes e validação (checklist para agentes)

Antes de terminar qualquer mudança:

```bash
go build ./...              # compila tudo
go vet ./...                # análise estática
gofmt -l <arquivos mudados> # formatação (ou gofmt -w)
go test ./... -count=1 -timeout=60s   # make test
```

- Testes de pacote específico: `go test ./internal/<pkg>/... -v`.
- Após mexer em ícone/versioninfo: `make icon && make winres` e confira o
  ícone embutido com `go run ./scripts/verifyicon`.
- Após mexer no `install-daemon.ps1`: valide a sintaxe com o parser do
  PowerShell e **confira se o BOM (EF BB BF) foi preservado**.
- ⚠️ Windows: `go test ./cmd/focusguard-daemon/...` exige shell **elevado**
  (manifest `requireAdministrator`). Num shell sem admin, rode apenas os
  pacotes não-elevados.
- Cross-check de linha de comando no Windows: use bash POSIX (nunca `dir`/
  `copy`/`findstr`); caminhos com `/`.

---

## 6. Estrutura de arquivos relevante

```
├── AGENT.md                    # este guia
├── docs/ui-plan.md             # plano da UI web (F1+F2 implementadas, roadmap)
├── Makefile                    # build, icon, winres, ui, test, vet, fmt, tidy, clean, install, uninstall
├── cmd/focusguard-web/         # servidor da interface web (user-space, embed da UI)
├── internal/httpapi/           # HTTP: proxy IPC + estático + segurança localhost
├── focusguard-ui/              # frontend React + Vite + TS (4 telas do MVP)
├── .goreleaser.yaml            # pipeline de release
├── .github/workflows/release.yml  # CI: tag v* → GoReleaser
├── .gitattributes              # *.sh → eol=lf
├── focusguard.ico / .png       # ícone do sistema (gerados por cmd/focusguard-icon)
├── versioninfo.json            # recursos Windows do daemon (ícone + manifest + versão)
├── cmd/
│   ├── focusguard/             # CLI (+ versioninfo.json próprio)
│   ├── focusguard-daemon/      # serviço (+ rsrc_windows_*.syso)
│   ├── focusguard-icon/        # gerador de ícones (build-time)
│   ├── focusguard-tray/        # systray (+ versioninfo.json só-ícone)
│   └── focusguard-watchdog/    # health-check / Smart Recovery
├── internal/                   # 23 pacotes (ver mapa na seção 3)
└── scripts/
    ├── install-daemon.ps1      # instalação Windows (copia p/ Program Files, serviço, atalho, tray, watchdog)
    ├── install-linux.sh        # instalação Linux (/opt/focusguard, systemd, XDG autostart)
    ├── focusguard.service      # unit systemd
    ├── focusguard-tray.desktop # template de atalho do tray (Linux)
    └── verifyicon/             # verifica ícone embutido == focusguard.ico
```

---

## 7. Padrões de commit

**Conventional Commits, em inglês, com escopo.** Formato:

```
<tipo>(<escopo>): <descrição curta no imperativo>
```

Tipos usados no histórico: `feat`, `fix`, `perf`, `docs`, `test`, `ci`,
`chore`. Escopos típicos: `tui`, `install`, `icon`, `tray`, `update`, `store`,
`scheduler`, `enforcer`, `watchers`, `daemon`, `ipc`, `autostart`, `focus`,
`changelog`, `readme`, `release`.

Exemplos reais do repositório:

```
feat(tui): show system version in interactive header
feat(install): install to Program Files with desktop shortcut, tray and watchdog
feat(icon): add focusguard.ico embedded in executables, tray and shortcut
fix(enforcer): roll back partial firewall rules on block failure
fix(update): atomic multi-binary update, semver compare and auto-restart
perf(store): drop fsync from state.json saves
docs(changelog): add v0.6.0 release section
ci(release): allow different binary counts in linux archive
test(daemon): cover real store-statewatch-scheduler chain
```

Regras:

- **Um commit por mudança coesa.** Não misture feature + docs + fix no mesmo
  commit.
- Atualização de docs (CHANGELOG/README) vira commit próprio
  (`docs(changelog): ...` / `docs(readme): ...`).
- Descrição curta (≤ ~72 chars), minúscula, no imperativo.
- Não commitar artefatos de build (`bin/`, executáveis compilados) — confira
  `git status` antes. **Porém** `focusguard-daemon.exe.manifest`, os
  `rsrc_windows_*.syso` e os `versioninfo.json` **são versionados de propósito**
  (o `go build` e o CI precisam deles): commite-os normalmente junto do código.
- Commit de código **com testes passando** (seção 5).

---

## 8. Padrões de release

Versionamento **SemVer** (atual: **v0.6.0**). O changelog segue **Keep a
Changelog** em PT-BR, com seções datadas e categorias por emoji
(por exemplo `### 🛡 ...`).

### Checklist de release

1. **CHANGELOG** — mova o conteúdo de `## [Unreleased]` para uma nova seção
   `## [x.y.z] - AAAA-MM-DD`, com resumo em PT-BR por tema (emoji + bullets),
   e deixe um `## [Unreleased]` vazio no topo.
2. **Commits** — faça os commits convencionais (incl. `docs(changelog)`).
3. **Tag** — crie tag anotada e faça push da branch + tag:
   ```bash
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin main
   git push origin vX.Y.Z
   ```
4. **CI gera a release** — o push de tag `v*` dispara `.github/workflows/
   release.yml`, que roda o GoReleaser (hooks: `go mod tidy`, regenera o ícone
   via `go run ./cmd/focusguard-icon`, roda `go-winres make` para
   daemon/CLI/tray, e compila a **UI web** com `npm ci && npm run build` —
   exige Node.js no runner). A release sai **automaticamente** no GitHub com os
   arquivos por plataforma.

### O que a release contém

- **Windows** (`focusguard_<v>_windows_<arch>.zip`): `focusguard.exe`,
  `focusguard-daemon.exe`, `focusguard-watchdog.exe`, `focusguard-tray.exe`,
  `focusguard-web.exe` + `install-daemon.ps1` + `install.txt`.
- **Linux** (`focusguard_<v>_linux_<arch>.tar.gz`): binários (incl.
  `focusguard-web`) + `focusguard.service` + `install-linux.sh` +
  `focusguard-tray.desktop` + README/CHANGELOG + `focusguard.ico`/`.png` +
  `install.txt`.

### Detalhes

- A **versão do daemon é injetada via ldflags** no GoReleaser:
  `-X main.daemonVersion={{ .Version }}`. Builds de dev sem ldflags ficam com
  `0.0.0-dev` → auto-update desativado (comportamento esperado).
- Os `versioninfo.json` têm campos `file_version`/`product_version` fixos
  (atualmente defasados) — são **informativos** nos metadados do `.exe`;
  a versão funcional vem do tag/ldflags. Atualize-os junto da release se quiser
  metadados corretos.
- O changelog do GoReleaser exclui commits `docs:`, `test:` e `chore:`.
- **Requisito local para `make winres` / hooks**: `go-winres` instalado
  (`go install github.com/tc-hib/go-winres@latest`).

---

## 9. Armadilhas conhecidas

- **Daemon exige admin** — manifest `requireAdministrator`; testes do daemon
  não rodam em shell sem elevação (Windows). Não "corrija" isso: é intencional.
- **Tray sem admin** — o tray **não pode** ganhar manifest; ele roda como
  usuário comum e não pode exigir elevação.
- **BOM do ps1** — editar `install-daemon.ps1` e re-salvar sem BOM UTF-8 quebra
  acentos no PowerShell 5.1. Preserve `EF BB BF`.
- **Não regresse o fallback de ícone** — o tray usa **exclusivamente** o ícone
  embutido (`RT_GROUP_ICON` + `RT_ICON`); não reintroduza renderização em
  runtime nem dependa de assets externos em runtime.
- **goreleaser hook sem shell** — o hook do `go-winres` usa `sh -c` porque o
  GoReleaser executa hooks sem shell; mantenha o condicional ao mexer.
- **IPC é o contrato** — CLI/tray/daemon/**web** conversam pelo mesmo protocolo;
  mudar o payload do `internal/ipc` exige atualizar os três lados + o
  `focusguard-ui/src/api/types.ts`.
- **`focusguard-web` é user-space** — nunca adicione manifest/admin a ele; o
  daemon é o único processo privilegiado. O web apenas faz proxy via `ipc.Client`.
- **Porta 48902** — `httpapi.DefaultAddr` é a fonte única (server + CLI probe);
  não espalhe a porta por literais.
- **`git status` antes de commit** — `.syso`, `focusguard.ico/.png` e
  `versioninfo.json` são versionados e mudam ao rodar `make icon`/`make winres`;
  não os ignore nem os commite por engano em commits de código.
