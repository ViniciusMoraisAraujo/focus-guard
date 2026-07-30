# FocusGuard 🛡️

**FocusGuard** é uma ferramenta Go para bloquear sites distractivos e manter o foco. Opera em nível de sistema, utilizando regras de firewall e o arquivo `hosts` para impedir o acesso a domínios indesejados.

---

## Funcionalidades

- 🖥️ **Interface TUI interativa** — Modo gráfico no terminal com Bubble Tea
- ⌨️ **CLI completa** — Bloqueio rápido via linha de comando
- 🚫 **Bloqueio de domínios** — Impede o acesso a sites distractivos por tempo determinado
- ⏱️ **Bloqueios temporários** — Expiração automática sem possibilidade de desbloqueio manual
- 🔄 **Resolução automática de IPs** — Resolve IPv4 e IPv6 dos domínios bloqueados, com refresh periódico
- 🧱 **Dupla camada de bloqueio:**
  - **`hosts`** — Redireciona o domínio para `127.0.0.1` e `::1`
  - **Firewall** — Regras `iptables`/`ip6tables` (Linux) ou `netsh advfirewall` (Windows)
- 💾 **Persistência de estado** — Armazena bloqueios em JSON com gravação atômica
- 🔄 **Arquitetura Cliente-Servidor** — Daemon em background com IPC via Unix socket
- 🐳 **Systemd Watchdog** — Suporte a health check via `NOTIFY_SOCKET`
- 🪟 **Suporte Windows** — Firewall via `netsh advfirewall` + edição do `hosts`

---

## Instalação

### Pré-requisitos

- Go 1.26.5 ou superior
- Linux com `iptables`/`ip6tables` ou Windows
- Acesso root/admin (necessário para firewall e edição do `hosts`)

### Build

```bash
git clone https://github.com/seu-usuario/focusguard.git
cd focusguard
go build ./...
```

### Binários

```bash
# Daemon (deve rodar como root/admin)
go build -o focusguard-daemon ./cmd/focusguard-daemon/

# CLI
go build -o focusguard ./cmd/focusguard/
```

---

## Uso

### Modo Interativo (TUI)

```
focusguard
```

Navegue pelos bloqueios ativos, adicione novos bloqueios e acompanhe o tempo restante — tudo em uma interface visual no terminal.

**Atalhos:**
- `b` — Bloquear novo domínio
- `r` — Atualizar lista
- `q` — Sair
- `Tab` — Navegar entre campos no formulário
- `Enter` — Confirmar bloqueio
- `Esc` — Cancelar / Voltar

### Linha de Comando

```bash
# Bloquear um domínio
focusguard block twitter.com --duration 4h
focusguard block youtube.com 30m

# Ver status dos bloqueios
focusguard status

# Modo interativo
focusguard interactive
```

> ⚠️ O daemon (`focusguard-daemon`) precisa estar rodando para que os comandos funcionem.

---

## Arquitetura

```
focusguard/
├── cmd/
│   ├── focusguard/              # CLI do usuário
│   │   └── main.go                  # Entrada: TUI + comandos (block, status)
│   └── focusguard-daemon/       # Serviço em background
│       └── main.go                  # Inicializa store, enforcer, scheduler, IPC
├── internal/
│   ├── policy/                  # Modelo de dados e regras de negócio
│   │   ├── policy.go                # Block, IsActive, CanUnblock, RemainingTime
│   │   └── policy_test.go
│   ├── store/                   # Persistência de estado em JSON
│   │   ├── json.go                  # Store com gravação atômica (temp file + rename)
│   │   └── json_test.go
│   ├── enforcer/                # Aplicação das regras no SO
│   │   ├── enforcer.go              # Interface Enforcer + ResolveIPs
│   │   ├── enforcer_linux.go        # Implementação Linux (hosts + iptables)
│   │   ├── enforcer_linux_test.go
│   │   ├── enforcer_windows.go      # Implementação Windows (hosts + netsh)
│   │   └── enforcer_windows_test.go
│   ├── scheduler/               # Gerenciamento de timers e expiração
│   │   ├── scheduler.go             # Block, timer, refresh periódico de IPs
│   │   └── scheduler_test.go
│   ├── ipc/                     # Comunicação cliente-servidor
│   │   ├── ipc.go                   # Request/Response (JSON sobre Unix socket)
│   │   ├── client.go                # Cliente IPC
│   │   ├── server.go                # Servidor IPC
│   │   ├── ipc_linux.go             # Unix socket (/run/focusguard.sock)
│   │   ├── ipc_linux_test.go
│   │   ├── ipc_windows.go           # Unix socket (%PROGRAMDATA%/FocusGuard/)
│   │   ├── ipc_windows_test.go
│   │   └── server_test.go
│   ├── tui/                     # Interface interativa (Bubble Tea)
│   │   ├── tui.go                   # Modelo TUI com tabela + formulário
│   │   └── tui_test.go
│   └── watchdog/                # Systemd watchdog
│       ├── watchdog.go              # Notificações via NOTIFY_SOCKET
│       └── watchdog_test.go
└── go.mod
```

### Fluxo de Dados

```
CLI (focusguard) ←→ Daemon (focusguard-daemon)
                          │
                     [IPC Server]
                          │
                    [Scheduler]
                     ┌─────┴─────┐
                     │           │
               [Store]     [Enforcer]
                (JSON)     ┌───┴───┐
                           │       │
                       /etc/hosts  Firewall
                       (hosts)   (iptables/netsh)
```

---

## Módulos

### TUI (`internal/tui/`)

Interface interativa construída com [Bubble Tea](https://github.com/charmbracelet/bubbletea):
- **Tela principal**: Tabela com bloqueios ativos (domínio, início, expiração, tempo restante)
- **Formulário**: Campos para domínio e duração com navegação por `Tab`
- **Feedback visual**: Indicador de carregamento, mensagens de erro/sucesso com cores
- **Estilo profissional**: Tema adaptativo (claro/escuro), bordas arredondadas

### CLI (`cmd/focusguard/`)

Comandos disponíveis:
| Comando | Descrição |
|---------|-----------|
| `focusguard` | Abre modo interativo (TUI) |
| `focusguard block <domínio> --duration <tempo>` | Bloqueia um domínio |
| `focusguard status` | Lista bloqueios ativos |
| `focusguard interactive` | Abre modo interativo (TUI) |

### Daemon (`cmd/focusguard-daemon/`)

Serviço em background que:
- Inicializa o store, enforcer e scheduler
- Reconcilia bloqueios salvos com o estado atual do sistema
- Expõe servidor IPC para comunicação com a CLI
- Mantém timers de expiração e refresh periódico de IPs

### Scheduler (`internal/scheduler/`)

Gerencia o ciclo de vida dos bloqueios:
- `Block()` — Cria bloqueio, persiste estado, aplica regras, inicia timer
- `Start()` — Reconcilia estado salvo na inicialização do daemon
- `onExpire()` — Remove bloqueio quando o tempo expira
- `startPeriodicIPRefresh()` — Atualiza IPs periodicamente (a cada 15 min)

### Enforcer (`internal/enforcer/`)

Interface para aplicação de regras no sistema operacional:

| Plataforma | Arquivo | Firewall | Hosts |
|------------|---------|----------|-------|
| Linux | `enforcer_linux.go` | `iptables`/`ip6tables` | `/etc/hosts` |
| Windows | `enforcer_windows.go` | `netsh advfirewall` | `C:\Windows\System32\drivers\etc\hosts` |

### IPC (`internal/ipc/`)

Comunicação entre CLI e Daemon via Unix socket:
- **Request**: `{ action, domain, duration }`
- **Response**: `{ success, message, blocks }`
- Socket em `/run/focusguard.sock` (Linux) ou `%PROGRAMDATA%/FocusGuard/focusguard.sock` (Windows)

### Watchdog (`internal/watchdog/`)

Integração com systemd:
- Envia `READY=1` na inicialização
- Envia `WATCHDOG=1` periodicamente
- Usa `NOTIFY_SOCKET` para comunicação

---

## Testes

```bash
# Rodar todos os testes
go test ./...

# Rodar com cobertura
go test -cover ./...

# Rodar testes de um pacote específico
go test ./internal/tui/... -v
go test ./internal/scheduler/... -v
```

### Cobertura

| Pacote | Testes |
|--------|--------|
| `policy` | Ciclo de vida do Block (IsActive, CanUnblock, RemainingTime) |
| `store` | Save e Load com gravação atômica |
| `enforcer` | ResolveIPs, hosts file operations, iptables bin selection |
| `ipc` | Listen/Dial, server handleConnection, invalid JSON, unsupported action |
| `scheduler` | Block/List, boot reconciliation, timer expiration, periodic IP refresh |
| `watchdog` | New() config, sendNotification, Start() com health check |
| `tui` | Model Init/Update/View, key handling, state transitions, messages |

---

## Plataformas Suportadas

### ✅ Linux

Implementação completa com:
- Edição do `/etc/hosts` com entradas marcadas (`# FOCUSGUARD:`)
- Regras `iptables` e `ip6tables` para IPv4 e IPv6
- Unix socket em `/run/focusguard.sock`
- Requer privilégios root (`sudo`)

### ✅ Windows

Implementação completa com:
- Edição do `hosts` em `C:\Windows\System32\drivers\etc\hosts`
- Regras de firewall via `netsh advfirewall`
- Unix socket em `%PROGRAMDATA%/FocusGuard/focusguard.sock`
- Requer privilégios de administrador

---

## Licença

Este projeto está sob a licença MIT.

---

> **FocusGuard** — Protegendo seu foco, um bloqueio de cada vez. 🎯
