# AGENT.md — cmd/

> Guia para agentes de IA que trabalham neste diretório. Consulte também o
> **[AGENT.md](../AGENT.md)** na raiz (specs, convenções, armadilhas) — leia-o
> antes de editar qualquer código.

## Propósito

Diretório dos **binários** do FocusGuard (6 programas `package main`). A
maioria é fina: parseia argumentos, monta `ipc.Request`, chama o daemon e
renderiza a resposta. A lógica real vive em `internal/`.

| Binário | Papel | Recursos Windows |
|---|---|---|
| `focusguard/` | CLI (sem args → abre a web via `focusguard-web`) | `versioninfo.json` próprio (ícone, sem manifest) |
| `focusguard-daemon/` | Serviço em background (privilegiado) | `packaging/versioninfo-daemon.json` + manifest `requireAdministrator` |
| `focusguard-tray/` | Bandeja do sistema | `versioninfo.json` **só ícone — nunca manifest** |
| `focusguard-watchdog/` | Health-check / Smart Recovery | `versioninfo.json` próprio (ícone + versão, sem manifest) |
| `focusguard-web/` | Serve a UI + proxy das ações IPC (user-space) | **sem manifest** — nunca adicionar admin |
| `focusguard-icon/` | Gera `focusguard.ico`/`.png` (build-time, stdlib pura) | — |

## Regras específicas

1. **Comentários e mensagens de CLI em PT-BR**; identificadores em inglês.
2. `os.Exit` é sempre via `var osExit = os.Exit` (stubbable nos testes) em
   `main.go` que exige `osExit`; no `focusguard-web` usa-se `log.Fatalf` mesmo.
3. `focusguard-web` compila o React via `go:embed all:assets` — sem `make ui`
   o binário serve a página "rode make ui" (comportamento esperado, testado).
4. Tudo que toca rede/processos/navegador é injetável (`probeWebServerFn`,
   `spawnWebServerFn`, `daemonResponds`, `killDaemon`, `osExecutable`, etc.) —
   **mantenha o padrão** em testes novos; nunca dispare navegador/SCM real em
   teste unitário.
5. `focusguard` localiza os irmãos (`focusguard-web`, `-daemon`, `-tray`,
   `-watchdog`) **ao lado do próprio executável** (`os.Executable()` +
   `filepath.Dir`) — mudar esse contrato quebra install/update.
6. **Logs em arquivo** — todo binário grava `<nome>.log` na **mesma pasta do
   daemon** (ao lado do executável), via `internal/filelog` (append + rotação
   de 1 MiB, best-effort: falha cai para stderr sem abortar). O padrão é um
   `logging.go` com `logFileName`, `maxLogSizeBeforeRotate`,
   `setupLoggingAt`/`setupLogging` e `var osExecutable = os.Executable`
   (stubbable nos testes).

## Bugs e correções potenciais

- **`focusguard/main.go` (≈ linha 1187)** — mensagem de sucesso do `update`
  diz *"A nova versão será usada na próxima reinicialização do daemon."* mas o
  daemon **reinicia imediatamente** após aplicar (exit 1 → supervisor sobe a
  versão nova). Texto desatualizado/enganoso; ajuste para refletir o restart
  imediato (o teste `main_test.go:425` ainda depende do trecho — atualizar
  junto).

## Testes

- `go test ./cmd/focusguard/... ./cmd/focusguard-watchdog/...` (não exigem admin).
- ⚠️ `go test ./cmd/focusguard-daemon/...` exige shell **elevado** no Windows
  (manifest `requireAdministrator`) — é limitação ambiental, não bug.
- `go build ./... && go vet ./...` antes de terminar.
