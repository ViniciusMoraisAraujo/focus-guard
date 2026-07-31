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
- 🪟 **Serviço Windows nativo** — Roda como serviço Windows sem console visível
- 👁️ **File Watcher** — Monitora `/etc/hosts` em tempo real com `fsnotify` e reaplica regras se detectar violação

---

## Build

```bash
# Build tudo (usa Makefile)
make build

# Ou manualmente
go build -o bin/focusguard.exe ./cmd/focusguard
go build -o bin/focusguard-daemon.exe ./cmd/focusguard-daemon
```

> No Linux, os binários não terão a extensão `.exe`.

---

## Instalação

### Windows — Serviço nativo

O daemon roda como um **serviço Windows legítimo** (gerenciado pelo Service Control Manager), sem console visível.

#### Via CLI (recomendado)

```powershell
# Como Administrador:
.\bin\focusguard.exe install
```

Isso cria o serviço, copia os binários e inicia o daemon automaticamente.

#### Via PowerShell script

```powershell
# Como Administrador:
.\scripts\install-daemon.ps1 install
```

#### Manualmente com `sc.exe`

```powershell
# Como Administrador:
sc create FocusGuard binPath="C:\caminho\completo\focusguard-daemon.exe" start=auto displayname="FocusGuard Daemon"
sc start FocusGuard
```

#### Gerenciar o serviço

```powershell
# Status
sc query FocusGuard

# Parar
sc stop FocusGuard

# Iniciar
sc start FocusGuard

# Remover
.\bin\focusguard.exe uninstall
# ou
sc stop FocusGuard
sc delete FocusGuard
```

> ✅ O daemon não mostra console — roda em segundo plano como serviço Windows legítimo.
> ⚠️ Todos os comandos de instalação/gerenciamento exigem **Administrador**.

### Linux — Systemd

```bash
# Via CLI (recomendado)
sudo make install
# ou
sudo ./bin/focusguard install

# Verificar status
systemctl status focusguard

# Parar
sudo systemctl stop focusguard

# Remover
sudo make uninstall
```

#### Manualmente

```bash
# Copiar service file
sudo cp scripts/focusguard.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable focusguard
sudo systemctl start focusguard
```

---

## Release (Download)

> 📋 Veja o [CHANGELOG](CHANGELOG.md) para o histórico completo de versões.

Cada release publicada no GitHub contém apenas os arquivos essenciais por plataforma:

| Plataforma | Arquivo | Conteúdo |
|------------|---------|----------|
| 🪟 Windows | `focusguard_<versão>_windows_<arch>.zip` | Apenas os executáveis: `focusguard.exe`, `focusguard-daemon.exe`, `focusguard-watchdog.exe` |
| 🐧 Linux | `focusguard_<versão>_linux_<arch>.tar.gz` | Binários + `focusguard.service` + `install-linux.sh` |

**Windows:**

```powershell
# Como Administrador
Expand-Archive focusguard_1.0.0_windows_amd64.zip -DestinationPath focusguard
cd focusguard
.\focusguard.exe install
```

**Linux:**

```bash
tar -xzf focusguard_1.0.0_linux_amd64.tar.gz
cd focusguard_1.0.0_linux_amd64
sudo ./install-linux.sh install

# Remover
sudo ./install-linux.sh uninstall
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
# Bloquear um domínio (requer daemon rodando)
focusguard block twitter.com --duration 4h
focusguard block youtube.com 30m

# Ver status dos bloqueios
focusguard status

# Instalar/remover da inicialização automática
focusguard install
focusguard uninstall

# Modo interativo
focusguard interactive
focusguard              # (sem argumentos também abre a TUI)
```

> ⚠️ O daemon (`focusguard-daemon`) precisa estar rodando para que `block` e `status` funcionem.

### Makefile

```bash
make build       # Compila CLI e daemon em ./bin/
make install     # Build + instala como serviço (Windows: sc, Linux: systemd)
make uninstall   # Remove da inicialização
make test        # Executa todos os testes
make clean       # Remove artefatos de build
make fmt         # Formata o código Go
make tidy        # go mod tidy
```

---

## Arquitetura

```
focusguard/
├── Makefile                       # Build, test, install, uninstall
├── scripts/
│   ├── install-daemon.ps1         # Instalação Windows via PowerShell
│   └── focusguard.service         # Unit file systemd para Linux
├── cmd/
│   ├── focusguard/                # CLI do usuário
│   │   └── main.go                # Entrada: TUI + comandos (block, status, install, uninstall)
│   └── focusguard-daemon/         # Serviço em background
│       ├── main.go                # Inicialização do daemon (store, enforcer, scheduler, IPC)
│       ├── service_windows.go     # Wrapper de serviço Windows (golang.org/x/sys/windows/svc)
│       └── service_other.go       # Stub para Linux/macOS
├── internal/
│   ├── autostart/                 # Gerenciamento de inicialização automática
│   │   ├── autostart.go           # Dispatcher por plataforma + IsInstalled()
│   │   ├── autostart_svc.go       # Windows: sc create/delete/query
│   │   └── autostart_systemd.go   # Linux: systemd service + systemctl
│   ├── policy/                    # Modelo de dados e regras de negócio
│   │   ├── policy.go              # Block, IsActive, CanUnblock, RemainingTime
│   │   └── policy_test.go
│   ├── store/                     # Persistência de estado em JSON
│   │   ├── json.go                # Store com gravação atômica (temp file + rename)
│   │   └── json_test.go
│   ├── enforcer/                  # Aplicação das regras no SO
│   │   ├── enforcer.go            # Interface Enforcer + ResolveIPs
│   │   ├── enforcer_linux.go      # Implementação Linux (hosts + iptables)
│   │   ├── enforcer_windows.go    # Implementação Windows (hosts + netsh)
│   │   └── *test.go
│   ├── scheduler/                 # Gerenciamento de timers e expiração
│   │   ├── scheduler.go           # Block, timer, refresh periódico de IPs
│   │   └── scheduler_test.go
│   ├── hostswatch/                # File watcher para /etc/hosts
│   │   ├── hostswatch.go          # Monitora alterações com fsnotify, reaplica bloqueios
│   │   └── hostswatch_test.go
│   ├── ipc/                       # Comunicação cliente-servidor
│   │   ├── ipc.go                 # Request/Response (JSON sobre Unix socket)
│   │   ├── client.go              # Cliente IPC
│   │   ├── server.go              # Servidor IPC
│   │   ├── ipc_linux.go           # Unix socket (/run/focusguard.sock)
│   │   ├── ipc_windows.go         # Unix socket (%PROGRAMDATA%/FocusGuard/)
│   │   └── *test.go
│   ├── tui/                       # Interface interativa (Bubble Tea)
│   │   ├── tui.go                 # Modelo TUI com tabela + formulário
│   │   └── tui_test.go
│   └── watchdog/                  # Systemd watchdog
│       ├── watchdog.go            # Notificações via NOTIFY_SOCKET
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

                          [HostsWatcher]
                        (fsnotify /etc/hosts)
                         │ se detectar violação
                         ▼
                     reaplica bloqueios
```

### Windows Service

No Windows, o daemon usa `golang.org/x/sys/windows/svc` para rodar como serviço nativo:

```
Service Control Manager (services.msc)
       │
       ▼
svc.Run("FocusGuard", handler)
       │
       ├─ Inicia runDaemon() em goroutine
       ├─ Escuta STOP/SHUTDOWN do SCM
       └─ Fecha serviceStopCh → runDaemon() para o servidor IPC
```

- **Sem console visível** — roda em segundo plano
- **Inicia com o sistema** — `start=auto`
- **Gerenciável** — `sc start/stop/query FocusGuard`
- **Fallback**: Se não detectar o SCM, roda em modo console (útil para debug)

---

## Módulos

### TUI (`internal/tui/`)

Interface interativa construída com [Bubble Tea](https://github.com/charmbracelet/bubbletea):
- **Tela principal**: Tabela com bloqueios ativos (domínio, início, expiração, tempo restante)
- **Formulário**: Campos para domínio e duração com navegação por `Tab`
- **Feedback visual**: Indicador de carregamento, mensagens de erro/sucesso com cores
- **Estilo profissional**: Tema adaptativo (claro/escuro), bordas arredondadas

### CLI (`cmd/focusguard/`)

| Comando | Descrição |
|---------|-----------|
| `focusguard` | Abre modo interativo (TUI) |
| `focusguard block <domínio> --duration <tempo>` | Bloqueia um domínio |
| `focusguard status` | Lista bloqueios ativos |
| `focusguard install` | Instala daemon como serviço de inicialização |
| `focusguard uninstall` | Remove daemon da inicialização |
| `focusguard interactive` | Abre modo interativo (TUI) |

### Daemon (`cmd/focusguard-daemon/`)

Serviço em background que:
- Inicializa o store, enforcer e scheduler
- Reconcilia bloqueios salvos com o estado atual do sistema
- Expõe servidor IPC para comunicação com a CLI
- Mantém timers de expiração e refresh periódico de IPs
- Executa como **serviço Windows** (sem console) ou **processo Linux** com systemd

### Autostart (`internal/autostart/`)

Gerencia a inicialização automática do daemon:

| Plataforma | Comando | Descrição |
|------------|---------|-----------|
| Windows | `sc create FocusGuard binPath=...` | Cria serviço Windows |
| Windows | `sc delete FocusGuard` | Remove serviço Windows |
| Windows | `sc query FocusGuard` | Verifica se serviço existe |
| Linux | Cria `/etc/systemd/system/focusguard.service` | Cria unit systemd |
| Linux | `systemctl daemon-reload` + `enable` + `start` | Ativa e inicia o serviço |

### HostsWatcher (`internal/hostswatch/`)

Monitora o arquivo `hosts` em tempo real usando `fsnotify`:
- Detecta edições externas (com `sudo`, por exemplo)
- Aplica debounce para evitar múltiplas reações a uma mesma alteração
- Reaplica bloqueios automaticamente se detectar violação
- Roda em background no daemon

### Scheduler (`internal/scheduler/`)

Gerencia o ciclo de vida dos bloqueios:
- `Block()` — Cria bloqueio, persiste estado, aplica regras, inicia timer
- `Start()` — Reconcilia estado salvo na inicialização do daemon
- `onExpire()` — Remove bloqueio quando o tempo expira
- `startPeriodicIPRefresh()` — Atualiza IPs periodicamente (a cada 15 min)
- `HasActiveBlocks()` — Verifica se há bloqueios ativos (usado para bloquear shutdown)

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
# ou
make test

# Rodar com cobertura
go test -cover ./...

# Rodar testes de um pacote específico
go test ./internal/tui/... -v
go test ./internal/scheduler/... -v
```

### Cobertura

| Pacote | Testes |
|--------|--------|
| `autostart` | Instalação/desinstalação Windows (sc.exe) e Linux (systemd), IsInstalled, erros de plataforma |
| `policy` | Ciclo de vida do Block (IsActive, CanUnblock, RemainingTime) |
| `store` | Save e Load com gravação atômica |
| `enforcer` | ResolveIPs, hosts file operations, iptables bin selection |
| `ipc` | Listen/Dial, server handleConnection, invalid JSON, unsupported action |
| `scheduler` | Block/List, boot reconciliation, timer expiration, periodic IP refresh |
| `hostswatch` | New, detectTamper (intact, missing, partial), Start/Stop, eventLoop, debounce |
| `watchdog` | New() config, sendNotification, Start() com health check |
| `tui` | Model Init/Update/View, key handling, state transitions, messages |
| `cmd/focusguard` | printUsage, handleBlockCommand, handleStatusCommand, main com flags, runInteractive |

---

## Plataformas Suportadas

### 🐧 Linux

- Edição do `/etc/hosts` com entradas marcadas (`# FOCUSGUARD:`)
- Regras `iptables` e `ip6tables` para IPv4 e IPv6
- Unix socket em `/run/focusguard.sock`
- Systemd service com watchdog
- Requer privilégios root (`sudo`)

### 🪟 Windows

- Edição do `hosts` em `C:\Windows\System32\drivers\etc\hosts`
- Regras de firewall via `netsh advfirewall`
- Unix socket em `%PROGRAMDATA%/FocusGuard/focusguard.sock`
- **Serviço Windows nativo** (sem console visível) via `golang.org/x/sys/windows/svc`
- Gerenciável via `sc.exe` ou `services.msc`
- Requer privilégios de administrador

---

## Licença

Este projeto está sob a licença MIT.

---

> **FocusGuard** — Protegendo seu foco, um bloqueio de cada vez. 🎯
