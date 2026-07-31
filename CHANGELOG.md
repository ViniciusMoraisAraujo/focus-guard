# Changelog

Todas as mudanças notáveis do **FocusGuard** serão documentadas neste arquivo.

O formato é baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/),
e este projeto adere ao [Versionamento Semântico](https://semver.org/lang/pt-BR/).

## [0.2.3] - 2026-07-31

### 🆕 System Tray App

- **Novo binário `focusguard-tray`** — ícone na bandeja do sistema com menu de
  ações rápidas: `Status`, `Bloco rápido` (domínios comuns por 4h),
  `Verificar atualização`, `Abrir TUI` e `Sair` (o daemon continua rodando).
- **Tooltip dinâmico** — mostra o estado da proteção
  (`DoH/DoT ATIVA · N regras`) e avisa quando há nova versão disponível.
- **Ícone gerado em runtime** — sem assets binários no repositório.
- **Autostart no login (Windows)** — novos comandos `focusguard install-tray` e
  `focusguard uninstall-tray` registram/removem o tray na chave
  `HKCU\...\CurrentVersion\Run`, sem necessidade de elevação.
- **Pipeline de release** — as releases agora incluem o `focusguard-tray`
  (Windows amd64/arm64; Linux amd64 — requer `libayatana-appindicator3`,
  instalada automaticamente pelo `install-linux.sh` quando o tray estiver
  presente no pacote).

## [0.2.2] - 2026-07-31

### ⚡ Otimização de processamento (continuação)

- **Sem DNS duplicado (G2)**: o enforcer não resolve mais IPs por conta própria —
  o scheduler resolve `domínio` e `www.domínio` uma única vez no bloqueio,
  mantendo a cobertura de firewall do `www.` sem trabalho repetido.
- **Sem eventos próprios (G6)**: gravações do próprio daemon no arquivo `hosts`
  e no state são marcadas, então o hostswatch/statewatch ignoram os eventos de
  `fsnotify` gerados por elas — sem ciclos redundantes de Sync/Reconcile.
- **Sync mais eficiente (G7)**: o `Sync` consulta as regras de firewall existentes
  uma única vez por chamada, em vez de uma vez por regra.
- **Status focado (G3)**: no Windows, o status consulta apenas as regras
  `FocusGuard*` (PowerShell), sem despejar todas as regras do firewall.
- **Cache no TUI (G9)**: `r` repetido em menos de 2s não re-consulta o daemon.

## [0.2.1] - 2026-07-31

### ⚡ Otimização de processamento

- **Watchdog externo**: intervalo de verificação do daemon reduzido de 10s para
  30s, com ping sem goroutine por tentativa (menos alocação e lixo para o GC).
- **Windows**: o resultado de `checkAdmin` é cacheado com
  `sync.Once`, eliminando o spawn de `net session` em toda operação de firewall.
- **Refresh de IPs inteligente**: quando não há bloqueios ativos, o refresh
  periódico (a cada 15min) é pulado — sem leitura do store nem resolução de DNS.
- **Novo `DialTimeout`/`SendWithTimeout`** no IPC para controle de timeout no
  dial e na leitura/escrita.

## [0.2.0] - 2026-07-31

### ✨ Auto-Update (destaque)

- **Verificação automática de novas versões** — o daemon consulta o GitHub Releases
  periodicamente (a cada 24h) em segundo plano e avisa quando há atualização disponível.
- **Novo comando `focusguard update`** — verifica e aplica a atualização no binário
  do daemon de forma transparente, com backup e rollback automático em caso de falha.
- **Aviso no `status`** — `focusguard status` agora exibe
  `🔄 Nova versão disponível: X → Y` quando existe uma versão mais recente.
- **Versão injetada no build** — o GoReleaser define `daemonVersion` via ldflags,
  permitindo que o daemon saiba sua própria versão e detecte atualizações corretamente.
- Auto-update é **desativado em builds de desenvolvimento** (versão `-dev`).

### 🐛 Correções

- **Watchdog**: removida a dependência `depend=FocusGuard` que causava o erro
  `1068` na inicialização do serviço quando o daemon estava desabilitado.
- **Watchdog**: `sc description` movido para fora do `sc create` (corrige o erro
  `1639` no Windows em português).
- **Autostart**: `uninstall` agora para o serviço (`sc stop`) antes de deletá-lo
  (corrige o erro `1072`) e detecta serviço inexistente pelo exit code `1060`
  (idempotente e independente de idioma).

## [0.1.1] - 2026-07-31

### 🚀 Releases por plataforma

- **Releases otimizadas**: cada release agora contém apenas os arquivos essenciais
  por plataforma — executáveis `.exe` (Windows) e binários + scripts de instalação (Linux).
- **`install-linux.sh`**: novo script de instalação para Linux.
- Pipeline de release com GitHub Actions + GoReleaser.

## [0.1.0] - 2026-07-31

### ✨ Primeira release

- **TUI interativa** — modo gráfico no terminal com Bubble Tea.
- **CLI completa** — `block`, `status`, `install`, `uninstall`, `interactive`.
- **Bloqueio em dupla camada** — arquivo `hosts` + regras de firewall
  (`iptables`/`ip6tables` no Linux, `netsh advfirewall` no Windows).
- **Bloqueios temporários** com expiração automática e sem desbloqueio manual.
- **Resolução automática de IPs** (IPv4 e IPv6) com refresh periódico.
- **Daemon em background** com comunicação IPC (Unix socket no Linux, TCP no Windows).
- **Serviço nativo** — Windows (SCM) e Linux (systemd), com watchdog embutido.
- **Proteção DoH/DoT** — bloqueio de DNS-over-HTTPS/TLS por regras de firewall.
- **Persistência de estado** — bloqueios salvos em JSON com gravação atômica.
- **Instalação** — autostart para Windows e Linux, com watchdog externo opcional.
