# FocusGuard 🛡️

**FocusGuard** é uma ferramenta Go para bloquear sites distractivos e manter o foco. Opera em nível de sistema, utilizando regras de firewall e o arquivo `hosts` para impedir o acesso a domínios indesejados.

---

## Funcionalidades

- 🌐 **Interface Web completa** — painel no navegador (`focusguard` ou `focusguard web`), React + TypeScript, com todas as funcionalidades: bloqueios, pomodoro, agenda, apps, presets, estatísticas e segurança; servido localmente pelo `focusguard-web` em `http://127.0.0.1:48902`
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
- 👁️ **File Watcher** — Monitora `/etc/hosts` e o `state.json` em tempo real com `fsnotify`, detecta adulterações via SHA-256 e restaura automaticamente
- 🍅 **Pomodoro e presets** — ciclos de trabalho/descanso por categoria (`social`, `video`, `news`, `games`); presets personalizados via `preset add/remove`
- 🎯 **Metas diárias e streak** — define uma meta de foco diária (`goal set 4h`); o `stats` mostra a sequência de dias consecutivos com foco
- ⏰ **Agendamento recorrente** — bloqueia categorias em horários fixos dos dias da semana (`schedule add --days seg,ter,qua --start 08:00 --end 12:00`)
- 🚨 **Modo pânico e allowlist** — `block --internet` corta toda a internet de uma vez, com `--allow` para manter ferramentas de trabalho acessíveis
- 📊 **Analytics com export** — `stats` com gráfico ASCII, streak e exportação para CSV/JSON/HTML (`--export`) e resumo semanal (`report`)
- 🛡 **Process guard configurável** — `apps add/remove` gerencia quais processos são encerrados durante o foco (antes era hardcoded)
- 🔏 **Tamper-log** — histórico das tentativas de burla detectadas e revertidas pelos watchers (`tamper-log`)
- 🎯 **Missões nomeadas** — `pomodoro --label "Estudar ENEM"` nomeia a sessão; `mission` e `stats --mission` agregam o foco por missão
- 🍅 **Pomodoro com memória** — `--save` persiste os padrões, `pomodoro-defaults` consulta, beeps nas transições e resumo pós-sessão
- ⏰ **Múltiplas janelas e import iCal** — `schedule --windows` para várias janelas por dia e `schedule import --file horario.ics` para eventos semanais de calendários

---

## Build

```bash
# Interface web (React): compila o frontend e o embute no focusguard-web.
# OBRIGATÓRIO antes de compilar se quiser a UI no binário — sem ele, o
# focusguard-web abre a página "UI não compilada" em http://127.0.0.1:48902
# (make build avisa quando os assets estão vazios).
make ui

# Build tudo (usa Makefile)
make build

# Ou manualmente
go build -o bin/focusguard.exe ./cmd/focusguard
go build -o bin/focusguard-daemon.exe ./cmd/focusguard-daemon
go build -o bin/focusguard-web ./cmd/focusguard-web
```

> No Linux, os binários não terão a extensão `.exe`.

---

## Instalação

### 📦 Localização dos arquivos (v0.3.0+)

| Plataforma | Binários (pasta protegida) | Estado |
|------------|----------------------------|--------|
| 🐧 Linux | `/opt/focusguard/` (root:root) | `/var/lib/focusguard/` |
| 🪟 Windows | `C:\Program Files\FocusGuard\` | `C:\ProgramData\FocusGuard\` |

> Os binários vivem numa **pasta protegida**, fora do alcance do usuário
> comum — sem permissão de escrita no diretório, não há exclusão acidental.
> A pasta do pacote extraído pode ser apagada após a instalação; para
> remover o FocusGuard use o desinstalador (`install-linux.sh uninstall` /
> `install-daemon.ps1 uninstall`).

### Windows — Serviço nativo

O daemon roda como um **serviço Windows legítimo** (gerenciado pelo Service Control Manager), sem console visível. Os executáveis são copiados para **`C:\Program Files\FocusGuard\`** (ACL padrão: o usuário comum só tem leitura/execução).

#### Via CLI (recomendado)

```powershell
# Como Administrador:
.\bin\focusguard.exe install
```

Isso copia os binários para `C:\Program Files\FocusGuard` (Sistema / Todos os
Usuários), cria o serviço, inicia o daemon, adiciona o atalho **FocusGuard** na
área de trabalho pública (visível para todos os usuários), registra o **tray**
para iniciar com o Windows (HKCU Run) e instala o **watchdog** externo. O
atalho usa o ícone embutido do sistema, extraído do executável
(`ExtractAssociatedIcon`) para um `focusguard.ico` próprio no diretório de
instalação.

#### Via PowerShell script

```powershell
# Como Administrador:
.\scripts\install-daemon.ps1 install
```

O script copia os 4 executáveis para `C:\Program Files\FocusGuard\`, registra o serviço, cria o atalho do Desktop e inicia o daemon.

#### Via instalador único `.msi` (implantação facilitada)

Cada release também publica um instalador **`.msi`** (`focusguard-<versão>-amd64.msi`)
que faz tudo em um clique — basta executá-lo (ou `msiexec /i focusguard_<versão>-amd64.msi`):

- Instala os 5 executáveis em `C:\Program Files\FocusGuard` (Todos os Usuários);
- Registra os serviços **`FocusGuard`** (daemon) e **`FocusGuardWatchdog`** com
  início automático e **recovery** (`sc failure ... restart/5s/10s/30s`);
- Registra o **tray** na inicialização (HKCU Run) e cria o atalho **FocusGuard** no Desktop;
- A desinstalação (Programas e Recursos / `msiexec /x`) remove serviços, atalho
  e pasta de instalação (o estado em `C:\ProgramData\FocusGuard` é preservado).

#### Manualmente com `sc.exe`

```powershell
# Como Administrador:
sc create FocusGuard binPath="C:\Program Files\FocusGuard\focusguard-daemon.exe" start=auto displayname="FocusGuard Daemon"
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

Os binários são instalados em **`/opt/focusguard/`** (root:root, pasta
protegida); a CLI fica disponível via symlink em `/usr/local/bin/focusguard`.
A unit systemd aponta para `/opt/focusguard/focusguard-daemon`.

```bash
# Via instalador (recomendado — instala no /opt/focusguard e migra layouts antigos)
sudo ./install-linux.sh install

# Via CLI
sudo make install
# ou
sudo ./bin/focusguard install

# Verificar status
systemctl status focusguard

# Parar
sudo systemctl stop focusguard

# Remover
sudo ./install-linux.sh uninstall
# ou
sudo make uninstall
```

#### Acesso ao socket (CLI/tray/web sem sudo)

O daemon roda como **root** (systemd) e expõe o IPC em `/run/focusguard.sock`
(`0660`, `root:focusguard`). Para o CLI/tray/web funcionarem **sem sudo**, o
seu usuário precisa pertencer ao grupo **`focusguard`** — o `install-linux.sh`
já cria o grupo e adiciona o usuário que rodou o instalador. Se você foi
adicionado manualmente (ou em outra máquina):

```bash
sudo usermod -aG focusguard $USER
# e faça logout/login (ou: newgrp focusguard) — o grupo só vale em sessões novas
```

Sem o grupo, os comandos falham com "Erro de comunicação" e o próprio erro
sugere o `usermod`. O daemon segue protegido: **apenas membros do grupo (ou
root) falam com ele** — o socket nunca é 0666.

#### Manualmente

```bash
# Copiar service file (o unit aponta para /opt/focusguard/focusguard-daemon —
# prefira o ./install-linux.sh, que instala os binários no lugar certo)
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
| 🪟 Windows | `focusguard_<versão>_windows_<arch>.zip` | Executáveis (`focusguard.exe`, `focusguard-daemon.exe`, `focusguard-watchdog.exe`, `focusguard-tray.exe`) + `install-daemon.ps1` + `install.txt` |
| 🪟 Windows | `focusguard-<versão>-amd64.msi` | Instalador único (serviços + recovery + tray + atalho) |
| 🐧 Linux | `focusguard_<versão>_linux_<arch>.tar.gz` | Binários + `focusguard.service` + `install-linux.sh` + `install.txt` |

**Windows:**

```powershell
# Opção 1 — instalador único (recomendado)
msiexec /i focusguard-1.0.0-amd64.msi

# Opção 2 — zip + script (como Administrador)
Expand-Archive focusguard_1.0.0_windows_amd64.zip -DestinationPath focusguard
cd focusguard
.\install-daemon.ps1 install   # copia para C:\Program Files\FocusGuard e inicia o serviço
```

**Linux:**

```bash
tar -xzf focusguard_1.0.0_linux_amd64.tar.gz
cd focusguard_1.0.0_linux_amd64
sudo ./install-linux.sh install   # binários em /opt/focusguard (pasta protegida)

# Remover
sudo ./install-linux.sh uninstall
```

---

## Uso

### Interface Web (navegador)

```
focusguard
```

Sem argumentos, o CLI inicia o `focusguard-web` por demanda (se não estiver
rodando) e abre o painel no navegador padrão em `http://127.0.0.1:48902`
(acessível só via localhost). O painel cobre **todas** as funcionalidades:
status, countdown, meta do dia, bloqueios, modo pânico, pomodoro, agenda,
apps, presets, estatísticas, segurança e atualizações — tudo conversando com
o daemon via proxy local (sem privilégios). A antiga TUI interativa foi
removida.

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

# Abre a interface web no navegador
focusguard
```

#### Bloqueio por categoria e modo pânico (v0.4.0+)

```bash
# Bloquear uma categoria inteira (presets embutidos e personalizados)
focusguard presets                          # listar as categorias disponíveis
focusguard block --preset social --duration 2h

# Modo pânico: bloquear TODA a internet por um período
focusguard block --internet --duration 30m

# Modo pânico com allowlist: só os domínios permitidos continuam acessíveis
focusguard block --internet --allow docs.google.com,drive.google.com --duration 2h
```

#### Presets personalizados (v0.4.0+)

```bash
# Criar um preset próprio e usá-lo em block/pomodoro/schedule
focusguard preset add estudos github.com stackoverflow.com docs.google.com
focusguard block --preset estudos --duration 4h

# Remover um preset personalizado (embutidos não podem ser removidos)
focusguard preset remove estudos
```

#### Agendamento recorrente (v0.4.0+)

```bash
# Bloquear "social" de segunda a sexta, das 08:00 às 12:00
focusguard schedule add --preset social --days seg,ter,qua,qui,sex --start 08:00 --end 12:00 --label "Manhã de foco"

# Dias aceitam inglês ou português: mon,tue,wed / seg,ter,qua
focusguard schedule list                  # listar regras
focusguard schedule remove <id>           # remover uma regra
```

#### Metas, pomodoro e analytics (v0.4.0+)

```bash
# Meta diária de foco
focusguard goal set 4h                    # definir meta (ex: 4h, 90m)
focusguard goal                           # consultar a meta

# Sessão pomodoro sobre uma categoria (25min trabalho + 5min descanso × 4 ciclos)
focusguard pomodoro --preset social --work 25 --rest 5 --cycles 4 --strict
focusguard pomodoro-stop                  # encerrar a sessão (não funciona em --strict)

# Pomodoro com padrões salvos: a sessão seguinte sem flags reutiliza os valores
focusguard pomodoro --preset social --work 50 --rest 10 --cycles 2 --save
focusguard pomodoro-defaults              # mostrar os padrões atuais

# Sessões nomeadas (missões) e filtro por missão
focusguard pomodoro --preset social --label "Estudar ENEM"
focusguard mission                        # total de foco por missão
focusguard stats --mission ENEM           # relatório só da missão

# Relatório de foco: gráfico ASCII + streak + exportação
focusguard stats                          # gráfico de foco (30 dias)
focusguard stats --export csv > focusguard-stats.csv      # exportar CSV
focusguard stats --export json > focusguard-stats.json    # exportar JSON
focusguard stats --export html > relatorio.html           # relatório HTML autossuficiente
focusguard report                         # resumo semanal de foco
```

#### Processos, burla e importação de calendário (v0.5.0+)

```bash
# Process guard: quais apps são encerrados durante sessões de foco
focusguard apps                           # listar a denylist
focusguard apps add spotify.exe           # encerrar durante o foco
focusguard apps remove spotify.exe        # parar de encerrar

# Histórico de tentativas de burla (adulterações detectadas e revertidas)
focusguard tamper-log

# Agendamento com múltiplas janelas por dia
focusguard schedule add --preset social --days seg,ter,qua,qui,sex --windows 08:00-12:00,14:00-18:00

# Importar eventos semanais de um calendário (.ics) como regras de bloqueio
focusguard schedule import --file horario.ics --preset social
```

#### DNS Sinkhole — "Rei da Rede" (v0.15.0+)

O FocusGuard pode atuar como **servidor DNS da rede inteira**: o daemon escuta
na porta 53 (`0.0.0.0:53`) e responde `0.0.0.0` para domínios bloqueados,
encaminhando as demais consultas ao upstream Cloudflare Security (`1.1.1.2`,
com filtro nativo de malware). Qualquer dispositivo que use o DNS do roteador
fica protegido — celular, TV, console — sem depender de sessão de foco ativa
nem do arquivo `hosts`.

```bash
focusguard dns start        # subir o sinkhole (porta 53)
focusguard dns status       # estado, upstream, consultas e bloqueios
focusguard dns stop         # desligar o sinkhole
```

Configuração "Rei da Rede" no roteador:
1. **IP fixo** — no painel do roteador, reserve o IP do PC que roda o
   FocusGuard (ex: `192.168.1.100`).
2. **DNS primário** — no DHCP do roteador, aponte o DNS primário para o IP do
   PC (`192.168.1.100`).
3. **DNS secundário** — configure um DNS público de confiança (ex: `1.1.1.1`)
   como secundário: se o PC cair, a rede continua navegando normalmente.

Ao ligar o sinkhole, o FocusGuard também bloqueia **DNS-over-HTTPS (porta
853)** para os navegadores não contornarem o bloqueio. QUIC/DoH3 (UDP 443)
ainda não é bloqueado automaticamente nesta versão.

> ⚠️ Porta 53 em uso? A causa mais comum é o ICS do Windows — desative com
> `sc config SharedAccess start= disabled` e `net stop SharedAccess`. O
> diagnóstico completo aparece em `focusguard dns status`.

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
│   │   └── main.go                # Entrada: comandos (block, status, install, uninstall, web)
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
│   │   ├── scheduler.go           # Block, BlockAllInternet, timer, refresh periódico
│   │   └── scheduler_test.go
│   ├── hostswatch/                # File watcher para /etc/hosts
│   │   ├── hostswatch.go          # Monitora alterações com fsnotify, reaplica bloqueios
│   │   └── hostswatch_test.go
│   ├── preset/                    # Catálogo de presets (builtin + personalizados)
│   │   ├── preset.go              # Store persistente: social, video, news, games + custom
│   │   └── preset_test.go
│   ├── schedule/                  # Agendamento recorrente
│   │   ├── schedule.go            # Regras por dia/horário, persistência, worker de aplicação
│   │   └── schedule_test.go
│   ├── pomodoro/                  # Sessões de foco em ciclos trabalho/descanso
│   │   ├── pomodoro.go            # Controller sobre o scheduler (work/rest/cycles, --strict)
│   │   └── pomodoro_test.go
│   ├── goal/                      # Meta diária de foco
│   │   ├── goal.go                # Store persistente (goal.json)
│   │   └── goal_test.go
│   ├── analytics/                 # Histórico de sessões (JSONL)
│   │   ├── analytics.go           # Recorder, Summarize, streak, RenderStats, ExportCSV/JSON
│   │   └── analytics_test.go
│   ├── processguard/              # Encerra processos da denylist (steam/discord)
│   │   ├── processguard.go        # Scan periódico enquanto houver sessão ativa
│   │   └── processguard_test.go
│   ├── recovery/                  # Smart Recovery: rollback pós-update no watchdog
│   │   └── recovery.go            # Restaura .bak se o daemon novo crashar no boot
│   ├── ipc/                       # Comunicação cliente-servidor
│   │   ├── ipc.go                 # Request/Response (JSON sobre Unix socket)
│   │   ├── client.go              # Cliente IPC
│   │   ├── server.go              # Servidor IPC
│   │   ├── ipc_linux.go           # Unix socket (/run/focusguard.sock)
│   │   ├── ipc_windows.go         # Unix socket (%PROGRAMDATA%/FocusGuard/)
│   │   └── *test.go
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

### CLI (`cmd/focusguard/`)

| Comando | Descrição |
|---------|-----------|
| `focusguard` | Abre a interface web no navegador |
| `focusguard block <domínio> --duration <tempo>` | Bloqueia um domínio |
| `focusguard block --preset <categoria> --duration <tempo>` | Bloqueia uma categoria inteira |
| `focusguard block --internet [--allow <d1,d2>] --duration <tempo>` | Modo pânico: bloqueia toda a internet (com allowlist opcional) |
| `focusguard presets` | Lista as categorias de bloqueio disponíveis |
| `focusguard preset add <nome> <dominio...>` | Cria um preset personalizado |
| `focusguard preset remove <nome>` | Remove um preset personalizado |
| `focusguard schedule add --preset <cat> --days <dias> --start HH:MM --end HH:MM` | Cria um agendamento recorrente |
| `focusguard schedule add --windows 08:00-12:00,14:00-18:00` | Várias janelas por dia numa regra |
| `focusguard schedule import --file <arquivo.ics> --preset <cat>` | Importa eventos semanais de um calendário |
| `focusguard schedule list` | Lista os agendamentos |
| `focusguard schedule remove <id>` | Remove um agendamento |
| `focusguard apps` / `apps add <proc>` / `apps remove <proc>` | Gerencia a denylist de processos do guard |
| `focusguard pomodoro --preset <cat> [--work 25] [--rest 5] [--cycles 4] [--strict] [--save] [--label "missão"]` | Sessão pomodoro (ciclos trabalho/descanso) |
| `focusguard pomodoro-defaults` | Mostra os padrões salvos do pomodoro |
| `focusguard pomodoro-stop` | Encerra a sessão pomodoro |
| `focusguard mission` | Total de foco por missão nomeada |
| `focusguard tamper-log` | Histórico de tentativas de burla |
| `focusguard goal set <duração>` | Define a meta diária de foco (ex: 4h) |
| `focusguard goal` | Consulta a meta diária |
| `focusguard stats [--export csv\|json\|html] [--mission <nome>]` | Relatório de foco em ASCII, com exportação e filtro por missão |
| `focusguard report` | Resumo semanal de foco |
| `focusguard status` | Lista bloqueios ativos |
| `focusguard web` | Abre a interface web no navegador |
| `focusguard install` | Instala daemon como serviço de inicialização |
| `focusguard uninstall` | Remove daemon da inicialização |

### Daemon (`cmd/focusguard-daemon/`)

Serviço em background que:
- Inicializa o store, enforcer e scheduler
- Reconcilia bloqueios salvos com o estado atual do sistema
- Expõe servidor IPC para comunicação com a CLI
- Mantém timers de expiração e refresh periódico de IPs
- Executa como **serviço Windows** (sem console) ou **processo Linux** com systemd

### Interface Web (`cmd/focusguard-web/` + `focusguard-ui/`)

Painel no navegador (React + TypeScript + Vite) servido por um binário
**user-space** (`focusguard-web`) que faz **proxy das ações IPC para o daemon**
— o daemon não ganha superfície HTTP e não muda nada.

- **`focusguard web`** — inicia o servidor por demanda (singleton via probe de
  porta) e abre o navegador em `http://127.0.0.1:48902`.
- **Segurança** — bind loopback, validação de `Host` (anti-DNS-rebinding),
  `Content-Type: application/json` obrigatório (anti-CSRF), headers
  CSP/nosniff/X-Frame e limite de corpo.
- **Build** — `make ui` compila o frontend e o embute no binário
  (`go:embed`); em dev, `cd focusguard-ui && npm run dev` (Vite com proxy `/api`).
- **Telas** — Dashboard, Bloquear, Modo pânico, Pomodoro, Agenda (+
  importação .ics), Apps (denylist), Presets personalizados, Estatísticas
  (gráficos, missões, export CSV/JSON), Segurança (histórico de burla) e
  Configurações (meta, atualizações com canal, proteção).

> O plano completo e o roadmap (real-time via WebSocket) estão em
> [`docs/ui-plan.md`](docs/ui-plan.md).

### System Tray (`cmd/focusguard-tray/`)

Ícone na bandeja do sistema com ações rápidas (`Status`, `Bloco rápido`,
`Verificar atualização` e `Sair` — o daemon continua rodando).

> 🪟 **Comportamento do tray**
>
> **IPC com timeout** — toda chamada ao daemon usa `SendWithTimeout` com
> limite de 5s. O `getlantern/systray` entrega cliques por canal
> não-bloqueante: se um handler ficasse preso num daemon sem resposta, os
> cliques seguintes eram descartados silenciosamente e o tray aparentava
> morto. Com o timeout, nenhum handler trava e cada clique é processado.
>
> **Respeita a resposta do daemon** — falha ao bloquear exibe o motivo
> retornado pelo daemon no tooltip; "Verificar atualização" não alega
> "✔ Você está atualizado" quando o daemon rejeita (ex.: auto-update não
> configurado em build de dev); erro no status mostra tooltip de falha em
> vez do estado normal.

### Autostart (`internal/infrastructure/autostart/`)

Gerencia a inicialização automática do daemon:

| Plataforma | Comando | Descrição |
|------------|---------|-----------|
| Windows | `sc create FocusGuard binPath=...` | Cria serviço Windows |
| Windows | `sc delete FocusGuard` | Remove serviço Windows |
| Windows | `sc query FocusGuard` | Verifica se serviço existe |
| Linux | Cria `/etc/systemd/system/focusguard.service` | Cria unit systemd |
| Linux | `systemctl daemon-reload` + `enable` + `start` | Ativa e inicia o serviço |

### HostsWatcher (`internal/infrastructure/hostswatch/`)

Monitora o arquivo `hosts` em tempo real usando `fsnotify`:
- Detecta edições externas (com `sudo`, por exemplo)
- Aplica debounce para evitar múltiplas reações a uma mesma alteração
- Reaplica bloqueios automaticamente se detectar violação
- Roda em background no daemon

### StateWatcher (`internal/infrastructure/statewatch/`)

Monitora o arquivo de estado `state.json` em tempo real usando `fsnotify`:
- Detecta adulterações, exclusões e renomeações do arquivo de estado
- Aciona o `Reconcile` do scheduler para restaurar o disco a partir da memória
- Roda em background no daemon

> 🛡️ **Comportamento dos watchers (v0.2.4+)**
>
> **Detecção via SHA-256 (sem ponto cego de 500ms)** — gravações feitas pelo
> próprio daemon são marcadas pelo hash do conteúdo gravado (registrado logo
> após a escrita, via `onSave` no store e `SetOnHostsWrite` no enforcer);
> apenas o evento `fsnotify` com conteúdo idêntico é ignorado. Uma edição
> externa é detectada mesmo que chegue imediatamente após um self-write.
>
> **Restauração automática a partir da memória** — se o `hosts` ou o
> `state.json` for adulterado, apagado ou renomeado, ele é recriado a partir
> da RAM do scheduler: o `hosts` ganha novamente os marcadores
> `# FOCUSGUARD:` e o `state.json` é reescrito com os bloqueios ativos.
>
> **Event loop assíncrono** — `Reconcile`/`Sync` rodam em goroutine com trava
> booleana (`running`/`pending`); uma operação lenta não congela o watcher, e
> eventos que chegam durante a execução são coalescidos em uma única execução
> de acompanhamento, sem perder nem duplicar trabalho.

### Scheduler (`internal/domain/scheduler/`)

Gerencia o ciclo de vida dos bloqueios:
- `Block()` — Cria bloqueio, persiste estado, aplica regras, inicia timer
- `Start()` — Reconcilia estado salvo na inicialização do daemon
- `onExpire()` — Remove bloqueio quando o tempo expira
- `startPeriodicIPRefresh()` — Atualiza IPs periodicamente (a cada 15 min)
- `HasActiveBlocks()` — Verifica se há bloqueios ativos (usado para bloquear shutdown)
- `IsBlocked(domain)` — Verifica se um domínio deve ser bloqueado no DNS
  sinkhole (mapa em RAM, case-insensitive, com allowlist do deep-focus)

### DNS Server (`internal/infrastructure/dnsserver/`)

Sinkhole DNS embutido (v0.15.0) usando `miekg/dns`:
- Escuta **UDP+TCP na mesma porta** (`0.0.0.0:53`, bind atômico dos dois)
- Domínio bloqueado → responde `A 0.0.0.0` / `AAAA ::` com **Status OK**
  (nunca SERVFAIL/REFUSED — evita o fallback ao DNS secundário do roteador)
- Domínio permitido → encaminha ao upstream Cloudflare Security (`1.1.1.2`)
- TTL curto (60s) nas respostas de autoridade para o cache dos celulares
  renovar rápido; panic recovery em cada conexão (a internet da rede não cai)
- `Scheduler.IsBlocked` é o verificador de política; o flag `dns_enabled` é
  persistido no `state.json` e o servidor sobe junto com o daemon quando ativo
- Ao subir, o daemon também aplica o bloqueio de DoH (porta 853) via enforcer

### Enforcer (`internal/infrastructure/enforcer/`)

Interface para aplicação de regras no sistema operacional:

| Plataforma | Arquivo | Firewall | Hosts |
|------------|---------|----------|-------|
| Linux | `enforcer_linux.go` | `iptables`/`ip6tables` | `/etc/hosts` |
| Windows | `enforcer_windows.go` | `netsh advfirewall` | `C:\Windows\System32\drivers\etc\hosts` |

- **`BlockAll`/`UnblockAll` (v0.4.0)** — regra *catch-all* que corta toda a
  internet: `REJECT --reject-with tcp-reset` no Linux e bloqueio de qualquer
  endereço no Windows. Sustenta o modo pânico (`block --internet`) e a
  allowlist (os IPs permitidos continuam acessíveis e não têm sockets derrubados).

### Presets (`internal/domain/preset/`)

Catálogo de categorias persistido ao lado do state.json:
- **Embutidos** — `social`, `video`, `news` e `games` (não removíveis)
- **Personalizados** — `focusguard preset add/remove`, usados em `block
  --preset`, `pomodoro --preset` e `schedule add --preset`

### Agendamento (`internal/domain/schedule/`)

Regras recorrentes de bloqueio por dia da semana e horário:
- Janelas de trabalho/descanso com suporte a horários **overnight**
  (`start 22:00 → end 06:00`)
- Worker no daemon reavalia as regras a cada 30s (e no boot) e aplica as
  janelas vencidas via `ApplyActiveRules` (idempotente)
- Persistência em `schedules.json`; IPC `schedule-add/list/remove`

### Metas e Analytics (`internal/domain/goal/` + `internal/domain/analytics/`)

- **Meta diária** — `goal.json` define a meta (ex: 4h/dia); exibida no `status`
  e na interface web
- **Streak** — dias consecutivos com foco, calculado por `ComputeStreak`
- **Exportação** — `ExportCSV`/`ExportJSON` alimentam o `stats --export`
  (`focusguard-stats.csv`/`.json`)

### IPC (`internal/transport/ipc/`)

Comunicação entre CLI e Daemon via Unix socket:
- **Request**: `{ action, domain, duration }`
- **Response**: `{ success, message, blocks }`
- Socket em `/run/focusguard.sock` (Linux) ou `%PROGRAMDATA%/FocusGuard/focusguard.sock` (Windows)

### Watchdog (`internal/system/watchdog/`)

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
go test ./internal/domain/scheduler/... -v
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
| `preset` | Store persistente, builtin não removíveis, add/remove com validação |
| `schedule` | Regras, validação de dias/horários, janelas overnight, persistência, ApplyActiveRules |
| `pomodoro` | Ciclos work/rest, expiração automática, sessão estrita (--strict) |
| `goal` | Set/Get persistido, validação de duração |
| `analytics` | Recorder JSONL, Summarize, streak, RenderStats, ExportCSV/JSON, linhas corrompidas puladas |
| `dnsserver` | Sinkhole (A/AAAA 0.0.0.0/::), upstream forwarding, TCP/UDP same-port bind, upstream-down SERVFAIL, controller Start/Stop idempotente |
| `recovery` | FindRecentBackup, ShouldRollBack, RestoreFromBackup, RecoverIfNeeded |
| `watchdog` | New() config, sendNotification, Start() com health check |
| `cmd/focusguard` | printUsage, handleBlockCommand (domínio/preset/internet), schedule add/list/remove, preset add/remove, goal set/get, stats --export, main com flags (sem args abre a web) |

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
