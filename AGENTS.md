# AGENTS.md

> Guia de orientação para **agentes de IA** que trabalham neste repositório.
> O guia completo e detalhado está em **[AGENT.md](AGENT.md)** — **leia-o
> antes de editar qualquer código**.

## TL;DR

**FocusGuard** é uma ferramenta Go (Go 1.26.5, módulo `focusguard`) que
bloqueia sites distractivos via `hosts` + regras de firewall, com arquitetura
cliente-servidor: CLI/TUI ↔ daemon via IPC (Unix socket) + tray + watchdog.

- **5 binários** em `cmd/`: `focusguard` (CLI/TUI), `focusguard-daemon`
  (serviço; manifest `requireAdministrator`), `focusguard-tray` (systray,
  **sem** manifest/admin), `focusguard-watchdog` e `focusguard-icon` (build).
- **23 pacotes** em `internal/` (mapa completo na seção 3 do AGENT.md).
- **Idiomas**: código/comentários em inglês; UI, README, CHANGELOG e docs em
  PT-BR; commits em inglês (Conventional Commits com escopo).
- **Plataformas**: Linux (systemd/iptables, `/opt/focusguard`) e Windows
  (serviço nativo `sc`/netsh, `C:\Program Files\FocusGuard`).
- **Regras-chave**: TDD; fonte de verdade em RAM (watchers restauram o disco);
  escrita atômica + SHA-256 anti-loop; best-effort para o SO; IPC com timeout
  no tray; recursos Windows via go-winres (`make icon` / `make winres`).
- **Validação**: `go build ./... && go vet ./... && go test ./... -count=1 -timeout=60s`.
  ⚠️ Os testes do daemon exigem shell **elevado** no Windows (manifest
  `requireAdministrator`).
- **Commits**: `feat|fix|perf|docs|test|ci|chore(<escopo>): descrição` (EN).
- **Release**: SemVer + Keep a Changelog (PT-BR, seções datadas); tag `vX.Y.Z`
  → push → GitHub Actions roda o GoReleaser automaticamente.

## Leia também

- **[AGENT.md](AGENT.md)** — guia completo: specs, linguagem, arquitetura,
  padrões de código, regras atuais, padrões de commit e release e armadilhas
  conhecidas.
- **[README.md](README.md)** — visão geral, uso e instalação.
- **[CHANGELOG.md](CHANGELOG.md)** — histórico de versões.
