# Changelog

Todas as mudanças notáveis do **FocusGuard** serão documentadas neste arquivo.

O formato é baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/),
e este projeto adere ao [Versionamento Semântico](https://semver.org/lang/pt-BR/).

## [Unreleased]

## [0.6.0] - 2026-08-03

### 🧭 TUI, ícones e instalação (integração local)

- **Versão no cabeçalho da TUI** — o cabeçalho do modo interativo mostra a
  versão atual do sistema (ex: `FocusGuard v0.6.0 - Modo Interativo`), e o
  daemon a reporta mesmo antes da primeira verificação de atualização (que
  depende do GitHub).
- **Tray carrega o ícone embutido** — o `focusguard-tray` lê o
  `focusguard.ico` dos recursos do próprio executável (`RT_GROUP_ICON` +
  `RT_ICON`) em vez de renderizar em runtime; o fallback de renderização foi
  removido (o tray também é gerado com `go-winres` no pipeline).
- **Atalho com ícone extraído** — o atalho do Desktop extrai o ícone embutido
  do executável (via `ExtractAssociatedIcon`, wrapper de `ExtractIconEx`) para
  um `focusguard.ico` próprio no diretório de instalação, com fallback para o
  ícone do exe.
- **`install` registra tray e watchdog** — o `focusguard install` (e o
  `install-daemon.ps1`) agora também registram o tray no HKCU Run e instalam o
  serviço `FocusGuardWatchdog` quando os binários estão presentes; o
  `uninstall` remove ambos.
- **`verifyicon`** — script de verificação que confere se o ícone embutido
  nos executáveis corresponde ao `focusguard.ico` (mesmas dimensões e pixels
  por imagem).

## [0.5.0] - 2026-08-02

### 🛡 Processos configuráveis (process guard)

- **`focusguard apps add <processo>` / `apps remove <processo>` / `apps`** —
  gerencia a denylist de processos que são encerrados durante sessões de foco
  (antes era hardcoded `steam.exe, discord.exe`). Persistida em apps.json e
  aplicada pelo guard em tempo real.

### 📊 Relatório HTML e resumo semanal

- **`focusguard stats --export html`** — relatório autossuficiente (CSS
  inline, sem assets externos) com cards, gráfico de barras por dia e ranking
  de domínios.
- **`focusguard report`** — resumo semanal em texto: total de foco, dias
  ativos, média e raia.

### 🛡 Histórico de tentativas de burla

- **`focusguard tamper-log`** — lista as adulterações detectadas nos arquivos
  de bloqueio (hosts/state) e revertidas pelos watchers, com data e detalhe.

### ⏰ Agendamento com múltiplas janelas

- **`focusguard schedule add --windows 08:00-12:00,14:00-18:00`** — uma regra
  pode ter várias janelas por dia (em vez de um único `--start/--end`).

### 🍅 Pomodoro: padrões salvos, beeps e resumo pós-sessão

- **`focusguard pomodoro --save`** — persiste work/rest/cycles como padrão;
  `focusguard pomodoro-defaults` mostra os padrões atuais; uma sessão sem
  flags reutiliza os salvos.
- **Beep de transição** nas mudanças de fase (trabalho/descanso/fim) e
  **resumo pós-sessão** no log do daemon.

### 🎯 Sessões nomeadas e missões

- **`focusguard pomodoro --label "Estudar ENEM"`** — nomeia a sessão;
  **`focusguard mission`** agrega o foco por missão e
  **`focusguard stats --mission <nome>`** filtra o relatório por missão.

### 📅 Import de calendário (iCal)

- **`focusguard schedule import --file <arquivo.ics> --preset <categoria>`** —
  converte os eventos semanais (RRULE FREQ=WEEKLY) de um calendário em regras
  de bloqueio recorrentes.

### 🧭 TUI, ícones e instalação (integração local)

- **Versão no cabeçalho da TUI** — o cabeçalho do modo interativo mostra a
  versão atual do sistema (ex: `FocusGuard v0.6.0 - Modo Interativo`), e o
  daemon a reporta mesmo antes da primeira verificação de atualização (que
  depende do GitHub).
- **Tray carrega o ícone embutido** — o `focusguard-tray` lê o
  `focusguard.ico` dos recursos do próprio executável (`RT_GROUP_ICON` +
  `RT_ICON`) em vez de renderizar em runtime; o fallback de renderização foi
  removido (o tray também é gerado com `go-winres` no pipeline).
- **Atalho com ícone extraído** — o atalho do Desktop extrai o ícone embutido
  do executável (via `ExtractAssociatedIcon`, wrapper de `ExtractIconEx`) para
  um `focusguard.ico` próprio no diretório de instalação, com fallback para o
  ícone do exe.
- **`install` registra tray e watchdog** — o `focusguard install` (e o
  `install-daemon.ps1`) agora também registram o tray no HKCU Run e instalam o
  serviço `FocusGuardWatchdog` quando os binários estão presentes; o
  `uninstall` remove ambos.
## [0.4.0] - 2026-08-02

### 🍅 Presets personalizados

- **`focusguard preset add <nome> <dominio...>`** — cria um preset próprio com
  os domínios que você quiser (ex.: `focusguard preset add estudos
  github.com stackoverflow.com docs.google.com`); o preset passa a aparecer em
  `focusguard presets` e pode ser usado em `block --preset`, `pomodoro
  --preset` e no agendamento recorrente.
- **`focusguard preset remove <nome>`** — remove um preset personalizado
  (presets embutidos `social`, `video`, `news` e `games` não podem ser
  removidos; remover um preset em uso por um agendamento é rejeitado).
- Persistidos pelo daemon ao lado do state.json (best-effort, mesmo padrão do
  resto do estado).

### ⏰ Agendamento recorrente (schedules)

- **`focusguard schedule add --preset <categoria> --days <dias> --start HH:MM
  --end HH:MM`** — bloqueia uma categoria em horários fixos dos dias da semana
  (ex.: `--days seg,ter,qua,qui,sex --start 08:00 --end 12:00`). O worker do
  daemon aplica a regra quando o horário chega e desbloqueia no fim da janela,
  todos os dias, sem intervenção.
- **`focusguard schedule list` / `focusguard schedule remove <id>`** —
  consulta e gerencia as regras recorrentes.
- **Validação robusta (TDD)**: dias e horários validados, janelas overnight
  (`start 22:00 → end 06:00`) tratadas corretamente, aplicação idempotente e
  persistência entre restarts do daemon.

### 🚨 Modo pânico e allowlist (`block --internet`)

- **`focusguard block --internet --duration <tempo>`** — corta **toda** a
  internet de uma vez: regra catch-all de firewall (REJECT com
  `--reject-with tcp-reset` no Linux via iptables/ip6tables, `netsh
  advfirewall` bloqueando qualquer endereço no Windows) + teardown de sockets
  ativos. Expira sozinho pelo timer do scheduler, como qualquer bloqueio.
- **`focusguard block --internet --allow docs.google.com,drive.google.com`**
  — **allowlist**: os domínios permitidos continuam acessíveis (e seus sockets
  não são derrubados) enquanto todo o resto fica bloqueado — deep focus em
  ferramentas de trabalho sem distração.
- **Reconcile ciente do modo pânico (bug fix da revisão)**: ao reiniciar o
  daemon com um `--internet` ativo, os blocos de domínio persistidos também
  são reaplicados — quando o pânico expira, os domínios continuam protegidos
  (antes, o sync de domínios era pulado enquanto o catch-all estivesse ativo).

### 🎯 Metas diárias, streak e exportação de analytics

- **`focusguard goal set 4h` / `focusguard goal`** — define uma meta diária de
  foco persistida; o `status` e a TUI mostram quanto falta (ex.: `Meta: 2h de
  4h`).
- **Streak de dias consecutivos** — o `focusguard stats` agora mostra a sequência
  de dias com foco registrado (dias consecutivos até ontem/hj).
- **`focusguard stats --export csv|json`** — exporta o histórico completo de
  sessões para `focusguard-stats.csv`/`.json` no diretório atual, para
  planilhas ou relatórios.

### 🪟 Tray: presets, pomodoro e tooltip

- **Submenu de presets** — o menu do tray agora lista as categorias
  disponíveis (incluindo os presets personalizados) para bloquear com um
  clique.
- **Notificações de pomodoro** — o tray avisa quando um ciclo de trabalho
  termina e o descanso começa (notificação nativa com dedup por fase).
- **Tooltip com tempo restante** — o tooltip do ícone mostra o tempo restante
  do bloqueio/sessão ativa, em vez de apenas o estado.

## [0.3.0] - 2026-08-02

### 📦 Instalação em pasta protegida (anti-exclusão acidental)

- **Linux — `/opt/focusguard`** — os 4 binários (focusguard, focusguard-daemon,
  focusguard-watchdog e focusguard-tray) passam a viver em `/opt/focusguard`
  (root:root 0755), pasta padrão do FHS para aplicativos de terceiros, fora do
  alcance do usuário comum: sem permissão de escrita no diretório, não há
  exclusão acidental. A CLI continua no PATH via symlink
  (`/usr/local/bin/focusguard`) — apagar o symlink não derruba nada, pois a
  unit systemd, o autostart do tray e o atalho do Desktop usam caminhos
  absolutos da pasta protegida.
- **Windows — `C:\Program Files\FocusGuard`** — o instalador agora COPIA os 4
  executáveis (daemon, CLI, watchdog e tray) para o Program Files, cuja ACL
  padrão dá ao usuário comum apenas leitura/execução (impossível excluir por
  acidente). O serviço é registrado apontando para lá e o atalho do Desktop
  mira o CLI da pasta protegida — o zip extraído vira um instalador
  descartável. `ProgramW6432` evita o redirect 32-bit; o uninstall remove a
  pasta. Chamadas `sc` → `sc.exe` explícitas (evita o alias Set-Content do
  PowerShell na criação do serviço).
- **Migração automática (Linux)** — instalações antigas (≤ 0.2.9) que ficavam
  em `/usr/local/bin` são detectadas e o layout migra para `/opt/focusguard`:
  a unit é reescrita com o novo `ExecStart` e o serviço reinicia no binário da
  pasta protegida.
- **Sem mudança de código Go** — todos os caminhos (CLI, daemon, watchdog,
  tray) são resolvidos dinamicamente via `os.Executable()` + irmãos no mesmo
  diretório; basta os 4 binários ficarem juntos na pasta protegida para o
  update multi-binário e o Smart Recovery continuarem funcionando.
- `install.txt` atualizado com os novos caminhos e a nota de que o pacote
  extraído pode ser excluído após a instalação.

## [0.2.8] - 2026-08-02

### 🔀 Canais de atualização (beta vs. stable)

- **`focusguard update --channel beta`** — opta por **prereleases** (early
  access) sem contaminar quem usa o canal estável: `--channel stable` (e o
  padrão) ignora releases `prerelease` no GitHub. O canal é honrado por
  request no daemon (`SetChannel` no updater → `Config{Prerelease: true}` do
  go-selfupdate), então CLI e tray podem alternar sem reiniciar o serviço.
- **Race-free sob concorrência (TDD)** — o daemon atende cada conexão IPC em
  sua própria goroutine; o `SetChannel` agora é protegido por mutex e cada
  checagem usa um snapshot consistente do updater (sem data race em flips
  rápidos beta↔stable).

### 🖥️ Indicador visual de update na TUI

- **Banner no topo da interface** — quando há versão nova disponível, a TUI
  exibe uma barra destacada `[ UPDATE DISPONÍVEL (vX.X.X) — Pressione 'u' ]`;
  a tecla `u` aplica a atualização ali mesmo, com mensagens de sucesso/erro e
  reset do banner após a aplicação.

### 🔔 Notificações nativas no tray

- **Balão nativo quando há update** — o tray agora consulta o status
  periodicamente (a cada 30min) e, ao detectar uma nova versão, dispara uma
  notificação do sistema: `notify-send` no Linux e `ShowBalloonTip`
  (PowerShell/WinForms) no Windows, com dedup por versão (notifica uma única
  vez por versão). Best-effort — falha na notificação não afeta o tray.

### 🛟 Smart Recovery: rollback automático no watchdog

- **Release quebrada não deixa a proteção morta** — se o daemon crashar logo
  após um update aplicado e houver um `.bak` recente (deixado pelo
  `UpdateToAll`), o watchdog externo restaura a versão anterior antes de
  reiniciar, subindo o binário que funcionava.
- **Sem reverter atualização boa (TDD)** — a decisão compara o **mtime do
  backup** com a última saúde confirmada do daemon: o binário atual só é
  revertido se **nunca** foi confirmado saudável após a substituição (crash
  no boot) e o daemon ficou fora por mais que a janela de graça de 60s (2× o
  intervalo de checagem) — um restart pós-update legítimo (que volta em
  segundos) nunca é revertido, e morte rotineira de um daemon estável também
  não. Crash logo após o primeiro health da versão nova (dentro de 30s)
  também dispara o rollback.
- **Novo pacote `internal/recovery`** com lógica pura testável
  (`FindRecentBackup`, `ShouldRollBack`, `RestoreFromBackup`,
  `RecoverIfNeeded`) + testes anti-regressão no watchdog.

## [0.2.7] - 2026-08-02

### 🔄 Auto-Update confiável (destaque)

- **Atualização multi-binário (corrige update parcial)** — `focusguard update`
  agora atualiza a **suíte inteira** (daemon, CLI, tray e watchdog), não só o
  daemon: um daemon novo conversando por IPC com uma CLI antiga quebraria o
  protocolo permanentemente. A troca é **atômica** (`UpdateToAll`): todos os
  binários são copiados para backup antes de tocar em qualquer um, e se
  qualquer binário falhar, todos os já atualizados são restaurados — nunca
  fica uma versão meio-atualizada. Binários não instalados (ex.: tray
  opcional) são pulados.
- **Restart automático pós-update (corrige o "daemon zumbi")** — depois de
  aplicar um update com sucesso e sem bloqueios/sessão ativos, o daemon
  encerra o processo para o supervisor subir a versão nova imediatamente, em
  vez de rodar o binário antigo em RAM até o usuário reiniciar a máquina.
  - Linux: `systemd Restart=always` já reinicia com qualquer exit code.
  - Windows: o daemon sai com **exit code 1** (o SCM só aplica recovery em
    falha) e o `install-daemon.ps1` agora configura `sc failure ... actions=
    restart` — o serviço volta sozinho em 5s/10s/30s.
  - O hook de restart só dispara **após** a resposta IPC ter sido escrita no
    socket (o CLI recebe o "✔ Atualização aplicada" antes do daemon sair).
- **Comparação de versões com semver real (remove o hack do "dummy release")**
  — `IsNewVersionAvailable` usa `golang.org/x/mod/semver` em vez de montar um
  `Release` fake e chamar `GreaterThan` do go-selfupdate (que dependia do
  parsing interno de `AssetName` da biblioteca e quebraria silenciosamente se
  ela mudasse). Versões inválidas falham fechado (nunca decidem "há update").

### 🐛 Correções de robustez (TDD)

- **Race no canal `done` do pomodoro (poderia crashar o daemon)** — o `run()`
  fechava o canal do struct após liberar o lock; se uma nova sessão começasse
  na janela de finalização, o goroutine antigo fechava o canal da sessão nova
  → `panic: close of closed channel`. Cada sessão agora captura seu próprio
  canal local (mesmo padrão do `stop`) — regression test com recorder
  bloqueante.
- **`Stop()` síncrono no processguard** — o `Stop` fechava o canal e retornava
  na hora, deixando um scan em voo lendo globais durante o teardown (data
  race intermitente detectada pelo `-race`). Agora `Stop` aguarda o goroutine
  sair antes de retornar.
- **Validação endurecida no IPC**: bloqueio com duração `0` rejeitado (criava
  bloqueio que expirava instantâneo mas ainda aplicava/removia regras de
  firewall); tetos defensivos no pomodoro (`--work`/`--rest` até 7 dias,
  `--cycles` até 1000) impedindo overflow/wrap no `int64` (ex.: `--work
  1000000000` virava ~147 anos e passava na validação).

## [0.2.6] - 2026-08-02

### 🍅 Pomodoro, Presets e Analytics (destaque)

- **Novo comando `focusguard pomodoro`** — sessões de foco em ciclos
  trabalho/descanso sobre uma categoria de domínios: `--preset social` (ex.:
  25min de trabalho + 5min de descanso × 4 ciclos, configuráveis com
  `--work`, `--rest` e `--cycles`). Cada fase de trabalho bloqueia os
  domínios do preset pelo scheduler (expiração automática pelos timers; sem
  desbloqueio manual).
- **Sessões estritas (`--strict`)** — não podem ser encerradas
  antecipadamente pelo `pomodoro-stop` nem pelo `Ctrl+C`/parada de serviço
  do daemon: o ciclo sempre roda até o fim (o daemon ignora sinais de
  parada enquanto houver sessão ativa).
- **Presets por categoria** — `focusguard presets` lista os catálogos
  disponíveis (`social`, `video`, `news`, `games`); `focusguard block
  --preset social --duration 2h` bloqueia a categoria inteira de uma vez.
- **`focusguard stats`** — histórico de sessões em JSONL (`analytics.jsonl`
  ao lado do state.json) com gráfico ASCII: sessões registradas, tempo total
  de foco, foco por dia (janela de 30 dias) e domínios mais bloqueados.
  Linhas corrompidas são puladas na leitura — um write parcial nunca aborta
  o relatório. Sem o arquivo, o recorder fica em memória (best-effort).

### 🔒 Process Guard

- **Encerramento de processos da denylist** — enquanto houver sessão de foco
  ativa (`sched.HasActiveBlocks`), o daemon varre a tabela de processos a
  cada 5s e encerra executáveis de entretenimento/comunicação (`steam.exe`,
  `discord.exe`) — o guard não pode ser enganado por extensão/normalização
  (`Discord.exe` ≡ `discord`, `DISCORD.EXE` ≡ `discord`).
- **Multi-plataforma** — `tasklist`/`taskkill` no Windows, `/proc/<pid>/comm`
  + `pkill` no Linux; falhas são best-effort (o próximo scan tenta de novo).

### 🛡️ Réplicas criptografadas do state.json

- **Backup oculto e auto-healing** — cada gravação do state.json também
  escreve uma réplica selada com AES-256-GCM em `.<nome>.replica` no mesmo
  diretório; um state.json apagado/vazio/corrompido é recuperado da réplica
  na inicialização do daemon (restaura o arquivo primário e segue com a RAM
  como fonte de verdade).
- **Atrelada ao hardware** — a chave é derivada do ID de hardware
  (`/etc/machine-id` no Linux, `MachineGuid` no Windows): a réplica só pode
  ser descriptografada na mesma máquina que a selou. Best-effort: sem ID de
  hardware as réplicas apenas ficam desativadas, sem quebrar o fluxo.

### 🖼️ Ícone e recursos do Windows

- **`focusguard-icon`** — novo comando de build (stdlib pura, sem CGO) que
  gera o ícone multi-tamanho `focusguard.ico` (16–256px) e o `focusguard.png`
  (256px), consumidos pelos metadados do `.exe` (go-winres) e pelos atalhos
  de desktop — o tray e o gerador de ícones agora compartilham o mesmo
  artwork (`internal/icon`).
- **go-winres no pipeline** — `versioninfo.json` (raiz e CLI) com ícone,
  manifest `requireAdministrator` e versão do produto; `go-winres make`
  emite os `rsrc_windows_*.syso` embedados automaticamente pelo `go build`
  (novos alvos `make icon`/`make winres`, hooks no GoReleaser).
- **Atalhos de desktop com ícone** — `install-daemon.ps1` cria/remove o
  atalho `FocusGuard.lnk` (ícone do CLI, `$cli,0`); `install-linux.sh`
  instala o ícone 256px em `~/.local/share/icons/hicolor` e o atalho
  `focusguard.desktop` no Desktop (português incluído).

### 🛡️ Regras de rede e firewall: robustez (enforcer)

- **REJECT em vez de DROP** — as regras de firewall agora usam
  `-j REJECT --reject-with tcp-reset` no Linux: conexões bloqueadas falham
  imediatamente (RST) em vez de travarem no timeout do cliente; o sweep
  remove também regras legadas `DROP` de versões anteriores.
- **Teardown de sockets** — `ss -K dst <ip>` encerra as conexões TCP já
  estabelecidas com um IP recém-bloqueado (best-effort), matando
  keep-alives no ato.
- **Sanitização de domínios** — antes de gravar no `/etc/hosts`, o domínio
  recebido é limpo e validado (`sanitizeDomain`): remove scheme
  (`http://`/`https://`) e path, elimina quebras de linha (`\r`, `\n`),
  espaços e tabs (vetores de injeção no arquivo hosts), normaliza para
  minúsculas, colapsa prefixos `www.` repetidos (`www.www.site.com` →
  `site.com`, evitando entradas redundantes) e rejeita caracteres inválidos —
  um domínio malformado não injeta linhas nem aborta o bloqueio.
- **Sweep de regras órfãs no Linux** — `removeFirewallRule` agora executa
  `iptables -D` em loop até o iptables reportar `does a matching rule exist`,
  removendo regras duplicadas/órfãs acumuladas de crashes/races anteriores
  (com teto defensivo de 100 remoções para nunca travar).
- **Testes (TDD)** — cobertura de sanitização (scheme maiúsculo, injeção
  CRLF/espaço/tab, collapse de `www.`), rejeição de injeção no hosts,
  remoção de duplicatas, no-op sem regras, propagação de erros reais, teto
  do loop, `ss -K` por IP e sweep de regras legadas DROP.

## [0.2.5] - 2026-08-01

### 🪟 System Tray: correções de confiabilidade

- **IPC com timeout** — todas as chamadas do tray ao daemon agora usam
  `SendWithTimeout` com limite de 5s (`daemonTimeout`). O `getlantern/systray`
  entrega cliques por canal **não-bloqueante**: se um handler ficasse preso
  num daemon sem resposta, os cliques seguintes eram descartados
  silenciosamente e o tray aparentava morto. Agora nenhum handler trava e
  cada clique é processado.
- **`resp.Success` honrado** — o tray passou a respeitar a resposta do daemon
  em vez de assumir sucesso:
  - Falha ao bloquear exibe no tooltip o motivo retornado pelo daemon;
  - "Verificar atualização" não alega mais "✔ Você está atualizado" quando o
    daemon rejeita (ex.: auto-update não configurado em build de dev);
  - Erro no status mostra tooltip de falha em vez do estado normal.
- **Testes de regressão (TDD)** — 4 novos testes cobrindo falha no bloqueio,
  falha na verificação de atualização, falha no status e o uso de timeout em
  todo o IPC do tray (garantindo que `Send` sem deadline nunca é chamado).

### 🐧 Autostart do tray no Linux (XDG)

- **Autostart por usuário** — o `install-linux.sh` agora registra o tray para
  iniciar com o login gravando um `focusguard-tray.desktop` em
  `~/.config/autostart/` do usuário real (resolvido via `SUDO_USER`,
  espelhando a chave `HKCU Run` do Windows). O diretório é criado com o
  usuário como dono, para que ele possa gerenciar o autostart sem sudo.
- **Best-effort** — qualquer falha no registro do autostart (usuário sem
  entrada no passwd, `getent` ausente, etc.) apenas loga um aviso e nunca
  aborta a instalação do daemon; o uninstall remove o `.desktop`.
- O template `focusguard-tray.desktop` é incluído no pacote de release Linux.

## [0.2.4] - 2026-08-01

### 👁️ Watchers: sem ponto cego de 500ms e event loop assíncrono

- **Ponto cego de 500ms eliminado**: a supressão de eventos do próprio daemon
  deixou de ser baseada em tempo (janela de 500ms) e passou a comparar o
  SHA-256 do conteúdo gravado — `MarkSelfWrite` registra o hash do que o
  daemon acabou de escrever (via `onSave` pós-escrita) e apenas o evento
  `fsnotify` com conteúdo idêntico é ignorado. Uma edição externa que chega
  logo após um self-write agora é detectada e revertida — sem ponto cego.
- **Event loop assíncrono**: `Reconcile` (statewatch) e a detecção de
  adulteração/Sync (hostswatch) rodam em goroutine com trava booleana
  (`running`/`pending`) — uma operação lenta não congela mais o event loop;
  eventos que chegam durante a execução são coalescidos em uma única
  execução de acompanhamento, sem perder nem duplicar trabalho.
- **Exclusão/renomeação monitoradas**: os watchers reagem a `fsnotify.Remove`
  e `fsnotify.Rename`, recriando o arquivo (hosts ou state) a partir da
  memória quando ele é apagado ou renomeado.
- **Restauração do `hosts` apagado**: se o arquivo `hosts` for deletado, o
  `detectTamper` força o `Sync` do enforcer para recriá-lo com os marcadores.
- **Falhas de restauração logadas**: erro do `Sync` no hostswatch agora é
  propagado e registrado em log, em vez de descartado silenciosamente.
- **Testes de integração** no daemon cobrindo a cadeia real
  `store → statewatch → scheduler`: adulteração externa, delete, rename e
  edição imediatamente após um self-write — com restauração do disco a partir
  da RAM e ausência de loop de Sync/Reconcile.

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
