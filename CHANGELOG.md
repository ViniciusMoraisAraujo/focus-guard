# Changelog

Todas as mudanças notáveis do **FocusGuard** serão documentadas neste arquivo.

O formato é baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/),
e este projeto adere ao [Versionamento Semântico](https://semver.org/lang/pt-BR/).

## [Unreleased]

## [0.18.0] - 2026-08-11

### ✨ Novas funcionalidades

- **Página de bloqueio HTTPS sem aviso de certificado (CA local)** — o
  interceptor HTTPS (:443) passou a assinar os certificados por domínio com
  uma **CA própria persistente** (ECDSA P-256, em
  `%PROGRAMDATA%\FocusGuard\ca` no Windows / `/var/lib/focusguard/ca` no
  Linux) instalada no trust store do sistema quando o interceptor é ativado
  (Windows: `certutil -addstore Root`; Linux: `update-ca-certificates`).
  Resultado: o navegador abre a página motivacional **direto, sem o aviso
  "sua conexão não é privada"**. Sem CA instalada, mantém o fallback
  auto-assinado histórico (zero regressão). O **Firefox** usa trust store
  próprio: ative `security.enterprise_roots.enabled` no `about:config` ou
  importe a CA com `focusguard ca-install`.
- **`focusguard ca-install` / `ca-uninstall`** (elevados) — gerar/instalar ou
  remover a CA local manualmente; o `focusguard uninstall` agora também remove
  a CA do trust store (higiene da âncora).
- **Doctor: checagem da CA local** — ausente = pass (config); gerada sem
  instalar = WARN com o passo da correção.
- **Update: os `.bak` da versão antiga agora são expirados** — o `CleanupStale`
  remove o backup mais novo por binário quando ele passa da janela de retenção
  do smart recovery (`recovery.BackupMaxAge`, **1h**): a cópia da versão antiga
  sai da pasta de instalação no primeiro boot após o update, em vez de ficar
  acumulada o dia inteiro.

### 🐛 Correções

- **`focusguard block <domínio> --duration <tempo>` falhava com "Duration
  invalid"** — o `flag` do Go para no primeiro argumento posicional, então
  `--duration` depois do domínio virava `Arg(1)` e era enviado como duração
  (encontrado na validação ao vivo). O split pré-Parse agora extrai
  `--duration`/`-d` (e as formas com `=`/traço simples) de **qualquer posição**
  junto com `--extend`/`--replace` (TDD com 9 casos).
- **`focusguard doctor` acusava FAIL "state.json não é gravável" num shell não
  elevado** — falso positivo: o CLI comum não abre o arquivo para escrita mesmo
  com a instalação saudável (o daemon elevado é quem grava). Agora degrada para
  WARN quando o erro é de permissão e o shell não é elevado (TDD nos dois
  sentidos).
- **Rollback do watchdog inalcançável no ramo "crash após boot saudável"** — o
  `crashWindow` era igual ao `checkInterval` (30s): a queda só é detectada no
  check seguinte ao último ping, então a condição `now-lastHealthy <
  crashWindow` nunca satisfazia no loop real. Agora `crashWindow = 2 ×
  checkInterval` (60s), cobrindo o primeiro check após a queda com folga. A
  janela de retenção do backup espelha `recovery.BackupMaxAge` (fonte única da
  verdade) e um teste trava a relação "1h nunca trunca uma decisão de
  rollback".

### 🧪 Testes

- **Integração do daemon via IPC real** — `interceptor-set on` pelo socket
  gera e persiste a CA e o handshake TLS valida o leaf **contra a CA apenas**
  (prova que é assinado por ela, não auto-assinado), com a página de bloqueio
  servida por HTTPS; `interceptor-set off` derruba o listener preservando a
  CA. A wiring do handler foi extraída para um helper compartilhado com o
  composition root (sem divergência futura).
- **tlsca** — persistência/idempotência da CA, SAN correta por domínio, chain
  valida contra a CA, limpeza do `.cer` temporário órfão preservando os
  artefatos reais.

## [0.17.1] - 2026-08-10

### ✨ Novas funcionalidades

- **Interceptor Page agora cobre sites HTTPS (porta 443)** — sites HTTPS-only
  (YouTube, Instagram…) forçam TLS via HSTS e o navegador nunca cai no HTTP
  :80, então a página de bloqueio não aparecia para eles (só "connection
  refused"). O interceptor agora também escuta na porta 443 e responde o
  handshake com um **certificado auto-assinado gerado sob demanda pelo SNI**
  (com a SAN exata do domínio): o navegador mostra o aviso de certificado
  usual e o usuário continua (Firefox: **Avançado → Continuar**) para a
  página motivacional. Desktop: loopback dual-stack `127.0.0.1:443` +
  `[::1]:443`; Server: `0.0.0.0:443`. Porta 443 ocupada degrada só a página
  dos sites HTTPS — o bloqueio segue valendo (best-effort, como o :80).

### 🎨 Interface web

- **Alerta de Clock Guard no Painel** — quando o NTP confirma uma burla de
  relógio e o bloqueio preventivo é aplicado, o Dashboard exibe o alert
  destrutivo *"Inconsistência de relógio detectada"* (a Fase 2 do
  features-plan previa o aviso; o guard já gravava o evento no tamper-log —
  o front agora o lê, sem IPC novo). A tela Segurança também mostra os
  eventos de relógio com badge próprio (**relógio / bloqueio preventivo**).

### 🐛 Correções

- **Contrato `TamperEvent` aceita os valores do Clock Guard** — o
  `gen-contract` restringia `source` a `hosts|state`; agora inclui `clock`
  (e `lockdown` em `action`), refletindo o que o daemon já emitia.

### 🧪 Testes

- TDD do TLS: página servida via HTTPS (cliente confiando no cert
  auto-assinado), SAN do certificado escopada ao SNI, bind 443 ocupado falha
  sem derrubar o daemon.
- 4 testes do alerta de Clock Guard no Dashboard (renderiza com evento
  recente, ignora antigo/outra fonte/falha) — 36 testes vitest no total.

## [0.17.0] - 2026-08-10

### ✨ Novas funcionalidades

- **`focusguard doctor`** — diagnóstico completo da instalação com exit code
  (`0` ok / `1` problemas / `2` erro): elevação, serviços, IPC, `state.json`,
  regras de firewall órfãs, hosts vs RAM, versões da suíte e status do DNS
  (saída PT-BR + `--json`).
- **Telemetria do sinkhole** — o DNS registra cada query bloqueada (domínio +
  IP de origem + timestamp) em um JSONL rotacionado (cap 1 MiB, purga no
  boot); a tela Rede ganhou a seção "Atividade bloqueada" (domínio × contagem
  × últimos IPs). O hook é chamado fora de lock e é best-effort — nunca
  atrasa o caminho do DNS.
- **Clock Tamper Protection** — novo cliente NTP (stdlib puro, UDP :123,
  timeout 3s) + guard no daemon: `|now − lastKnown|` além de 5 min (nos dois
  sentidos) dispara suspeita; o NTP confirma e aplica **bloqueio preventivo
  all-internet** + registro no tamper-log, re-ancorando a referência no
  horário real.
- **Focus Interceptor Page** — ao abrir um site bloqueado, o navegador vê uma
  página explicando o bloqueio (domínio, tempo restante e **frase
  motivacional** determinística por domínio) em vez de "connection refused".
  Funciona no **desktop** (hosts → `127.0.0.1` + `[::1]`, listener :80
  dual-stack loopback) e no **Server** (DNS responde o IP local, listener
  `0.0.0.0:80`). Porta 80 ocupada degrada só a página — o bloqueio continua
  valendo (best-effort). Novo switch "Página de bloqueio" em Configurações.
- **Regras por dispositivo (edição Server)** — catálogo persistido
  (`devices.json`) com políticas `block_all` / `allow_list` / `inherit` por
  IP: a regra do dispositivo decide **antes** da regra global (allowlist de
  um device libera domínio bloqueado globalmente). Ações IPC
  `devices-list`/`devices-upsert`/`devices-remove`, comando
  `focusguard devices` e seção "Dispositivos" na tela Rede.
- **Relatório semanal automático** — agendamento persistido (dia, hora,
  pasta de export; default domingo 23:59, desligado): o daemon gera o HTML +
  JSON autossuficientes do analytics no horário, sem reinício. Comandos
  `focusguard report auto|now` e card "Relatório semanal" em Configurações.
- **Gamificação (conquistas)** — catálogo puro de 12 badges derivadas das
  stats (streak ≥ 7, foco ≥ 10h, madrugada, missões, maratonas…), calculadas
  na leitura — sem estado persistido. Ação `achievements-get`, comando
  `focusguard achievements` e seção "Conquistas" em Estatísticas (grid com
  progresso por badge).

### 🐛 Correções

- **Respostas IPC não vazam mais campos de struct nova** — os campos
  `device`/`report_config` passaram a ponteiros no wire (`omitempty` não
  funciona em structs no `encoding/json`); `httpapi` volta a responder o
  corpo exato esperado.

### 🧪 Testes e CI

- TDD em todos os pacotes novos: `telemetry` (rotação/leitura com linha
  corrompida), `ntp` (socket UDP mockado), `clockguard` (clock fake nos dois
  sentidos), `interceptor` (bind falho + página IPv6 dual-stack), `devices`
  (precedência device > global), `reports` (agendamento/generação) e
  `achievements` (cálculo puro).
- Suíte completa verde: 44 pacotes Go + `contract-check` + `tsc` + 32 testes
  vitest.

## [0.16.4] - 2026-08-10

### 🪟 Windows

- **Upgrade do MSI com o tray rodando não quebra mais o ícone** — o hook
  que inicia o `focusguard-tray.exe` tinha condição `NOT Installed` (só
  instalação limpa): num upgrade o tray nunca voltava a subir e, durante o
  `RemoveExistingProducts`, o exe em execução ficava travado → remoção
  deferida/reboot e a mensagem do Windows "não pode encontrar
  focusguard-tray.exe". O instalador agora **encerra o tray antes da troca
  de arquivos** (taskkill no início da sequência, mesmo padrão do
  `stopForBinarySwap` do update) e o hook de start passou a rodar também em
  upgrades (`NOT REMOVE`), trazendo o ícone de volta imediatamente após
  atualizar.
- **Metadados de versão dos executáveis corrigidos** — o bump para 0.16.3
  atualizou apenas o bloco `fixed` do `versioninfo.json`, deixando a string
  table (o que o Explorer/PowerShell exibem) em 0.16.1. Agora `FileVersion`/
  `ProductVersion` reportam **0.16.4** de forma consistente.

### 🐛 Correções

- **`Block`/`ExtendBlock` deixavam domínio "zumbi" na RAM se o `store.Save`
  falhasse** — o bloqueio ficava visível no `status` sem timer nem regra de
  firewall (nada o expirava); no `ExtendBlock` o timer antigo disparava e o
  bloqueio nunca mais expirava. Agora ambos revertem a RAM na falha de
  persistência (TDD).
- **`SetDNSEnabled`/`SetDNSUpstream` mantinham o setting divergente do disco
  na falha do `Save`** — revertem ao valor persistido (TDD).
- **Mensagem de erro IPC corrigida** — `Not suported action` → `Not
  supported action`.

### 🧪 Testes e CI

- **Job `-race` do CI Linux passou a rodar de verdade** — o plain scalar do
  YAML dobrava `\` + newline em `\ ` (espaço colado no path → "malformed
  import path"). Troca para bloco literal; com o detector ativo, o flake do
  `TestClientSend_DecodeError` (broken pipe sob instrumentação) foi
  corrigido — zero data races nos 8 pacotes concorrentes.

## [0.16.3] - 2026-08-10

### 🐛 Correções

- **Regras de firewall órfãs quando o último bloqueio expira** — o sweep do
  `Sync` não rodava quando o último bloco ativo terminava (raça com o refresh
  periódico), deixando regras para trás no hosts/firewall. O fim do último
  bloqueio agora dispara a varredura de órfãos (TDD).
- **`block --preset` (batch) removia proteção pré-existente** — o
  `BlockDomains` aplicava apenas o conjunto novo ao `Sync`, derrubando regras
  de outros bloqueios ativos. Agora o batch passa **todos** os blocos ativos
  ao sync (TDD).
- **Goroutine do refresh de IPs vazava no shutdown** — o `Stop()` do daemon
  agora encerra e aguarda o goroutine periódico de 15min (TDD).
- **Janela de agenda importada do iCal cruzando a meia-noite** — o fallback
  de +1h (DTEND ausente) com início ≥ 23:00 gerava janela `"24:xx"` inválida;
  agora faz wrap para o dia seguinte (`23:59-00:59`, overnight já suportado).
  Encontrado pelo fuzz do `ParseICS` (TDD).
- **Instalador Windows: serviço do watchdog nunca era instalado/removido** —
  o `install-daemon.ps1` usava `$WatchdogServiceName` sem declará-lo
  (string vazia no PowerShell → `sc.exe create` sem nome). A variável agora é
  declarada (`FocusGuardWatchdog`, alinhada ao autostart/MSI).
- **Atalho Linux abria um terminal à toa** — o `.desktop` da CLI usava
  `Terminal=true` (resquício da TUI); como a CLI abre a interface web no
  navegador, agora é `Terminal=false`.

### 🧪 Testes e CI

- **Bug-hunt concluído (Etapas 0–8)** — plano completo em
  `docs/bug-hunt-plan.md` com checklist final: paridade de códigos do
  `ipcerr`, edge cases do roteador IPC, shutdown/races, teste de integração
  do update Orchestrator, reconexão SSE com `Last-Event-ID`, paridade de
  timeouts no httpapi, testes de UI (grade semanal + contexto) e fuzz/E2E.
- **Fuzz targets do agendamento** — `FuzzParseICS`, `FuzzWindowsPairs` e
  `FuzzParseClock` (30s cada, ~4,1M execs, sem crash) em
  `internal/domain/schedule/fuzz_test.go`.
- **CI Linux reforçado** — job `race` (`go test -race` nos pacotes
  concorrentes) e o teste de chown do socket (`root:focusguard 0660`) rodando
  como root via `sudo`, com guard contra falso-verde de `-run` sem match.

### 📝 Documentação

- `README.md` e todos os `AGENT.md` atualizados — estrutura pós-reorg,
  interface web completa (12 telas), bugs corrigidos × abertos nos guias e
  novo pipeline de CI (`test.yml`).

## [0.16.2] - 2026-08-07

### 🏗️ Arquitetura (refatoração interna — sem mudança de comportamento)

- **`internal/` reorganizado em camadas** — os 34 pacotes foram movidos para
  `domain/` (lógica de negócio), `infrastructure/` (I/O de SO), `transport/`
  (IPC/HTTP + observabilidade) e `system/` (ciclo de vida de daemon/tray/
  watchdog); assets de build consolidados em `packaging/` e guias
  consolidados em `docs/`.
- **Inversão de dependências (DIP) no IPC** — apps, blocks, goal, presets,
  users, analytics, pomodoro, schedule, dns e update **não importam mais** o
  `transport/ipc`: cada ação virou um handler de domínio com tipos próprios
  (`ipc.DomainAction`), adaptado ao wire pelo composition root do daemon. Os
  handlers de referência do transport foram removidos (viraram test-only) e o
  `transport/ipc` ganhou tipo próprio de status DNS (`DNSStatus`), sem
  depender mais do `dnsserver`.
- **Contrato do wire inalterado** — `ipc.Request/Response`, mensagens PT-BR,
  códigos estáveis e o `types.ts` do frontend permanecem idênticos; o
  `domain_wiring_test.go` compõe os 31 handlers reais e o `ValidateRegistry`
  fecha specs↔registry no boot do daemon.

## [0.16.1] - 2026-08-06

### 🪟 Windows

- **Atalho no desktop para a edição Server** — o instalador
  `focusguard-server-*.msi` agora também cria o atalho "FocusGuard Server" na
  Área de Trabalho (antes só no Menu Iniciar), apontando para o
  `focusguard.exe`, que abre a interface web.

### 🐛 Correções

- **Interface web com a tela de login nas instalações** — os pacotes .msi
  gerados localmente embutiam um bundle antigo (anterior à autenticação), que
  abria o painel sem pedir login e mostrava "daemon desligado" mesmo com o
  serviço ativo. Os instaladores agora embutem a UI atual (com o fluxo de
  login).

### 📝 Documentação

- **Plano de caça a bugs pós-refatoração** — `docs/bug-hunt-plan.md`, com 9
  etapas progressivas (contrato IPC → roteador → concorrência → domínios →
  HTTP/SSE → frontend → plataforma → fuzz/E2E) e critérios de saída por etapa.
- **Plano de reorganização de diretórios/arquitetura** — `docs/reorg-plan.md`,
  com as 3 frentes (docs/archive, `internal/` em camadas, `packaging/`),
  pontos de ruptura mapeados e fases A→D.
- **Docs antigos da raiz removidos** (`task.md`, `follow-up-v0.15.1.md`,
  `plan-new-ui-and-user.md`, `AGENTS.md`) — conteúdo absorvido por `docs/`.

## [0.16.0] - 2026-08-06

### ✨ Novas funcionalidades

- **Registry de ações com specs declarativas** — o `ipc.Server` virou um
  roteador fino: cada ação declara permissão, timeout e handler via
  `ActionSpec` (33 ações), eliminando o `switch` legado (Fases 2–3 do
  refactor-plan).
- **Serviços de domínio** — analytics, pomodoro, schedule, update e apps
  viraram pacotes com interfaces estreitas, compostos no composition root do
  daemon com `ValidateRegistry` no boot (Fases 4–5).
- **Eventos em tempo real (SSE)** — event hub no daemon + long-poll
  `event-subscribe` + `GET /api/events`; o frontend usa `EventSource` com
  fallback de polling; expiração de bloqueios/pomodoro/agenda viram eventos
  (Fase 7).
- **Observabilidade** — `internal/metrics` mede latência por ação IPC/HTTP
  (percentis p50/p95/p99), nova ação `metrics` + `GET /api/metrics` e comando
  `focusguard metrics [--reset]` (Fase 8).
- **CLI por comando** — `cmd/focusguard/main.go` dividido em um arquivo por
  comando com tabela `commands`/`usageOrder` validada por teste (Fase 6).
- **Acesso ao socket por grupo no Linux (F5 do ui-plan)** — o daemon chowna
  `/run/focusguard.sock` para `root:focusguard 0660`; `install-linux.sh` cria
  o grupo `focusguard` e adiciona o usuário; o CLI sugere o grupo no erro de
  conexão.

### 🎨 Interface web

- **Polimento visual (F4 do ui-plan)** — anel do Pomodoro com gradiente SVG,
  glow e dots de ciclo; grade semanal da Agenda com dia atual em pill e tint
  de fim de semana; gráficos de Estatísticas com gradiente e animação de
  crescimento.

### 🛠️ Build

- **Versioninfo atualizado para 0.16.0** nos binários
  (focusguard, focusguard-daemon, focusguard-watchdog).

## [0.15.2] - 2026-08-05

### 🐛 Correções

- **Sessão expirada agora devolve à tela de login** — com o TTL de 12h, uma
  sessão vencida deixava o painel aberto com toda ação falhando com 401. Agora,
  quando `/api/action` responde 401, a UI re-consulta `/api/auth/status` e, se
  o servidor confirma que a sessão não existe mais, volta para a tela de login
  (limpando os dados do usuário anterior). Vale para ações manuais e para os
  polls de fundo; o re-check é silencioso (sem toast duplicado).

### 🧪 Testes

- **`TestWebExePath` tolera o sufixo `.test` do binário de teste** — sob
  `go test`, `os.Executable()` aponta para `focusguard.test`, então o nome
  derivado ganhava `.test` (`focusguard-web.test`) e a assertiva falhava no
  Linux (no Windows passava, pois o binário de teste é `focusguard.test.exe`).
  O teste agora ignora o sufixo `.test` antes de validar o nome do irmão.

## [0.15.1] - 2026-08-05

### 🐛 Correções

- **UI presa no splash de carregamento (tela de login inalcançável)** — o gate
  de autenticação da v0.15.0 usava `null` tanto para "ainda checando a sessão"
  quanto para "não autenticado": sem cookie de sessão, a UI ficava para sempre
  no ícone pulsando (o ping continuava respondendo ok) e a tela de login nunca
  aparecia — o logout tinha o mesmo defeito. O estado "não autenticado" agora é
  um objeto real (`authenticated: false`), distinto de `null` ("checando"), e o
  logout devolve à tela de login como previsto.

### 🛠️ Build

- **Aviso de assets vazios no `make build-web`** — compilar o `focusguard-web`
  sem `make ui` antes embutia uma pasta de assets vazia e a UI não abria
  (página "UI não compilada" na raiz). O alvo agora imprime um aviso claro
  quando `cmd/focusguard-web/assets` não contém `index.html`, e o README
  documenta `make ui` antes de `make build`.

## [0.15.0] - 2026-08-05

### ✨ Novas funcionalidades

- **DNS Sinkhole — "Rei da Rede"** — o FocusGuard passa a atuar como
  servidor DNS da rede inteira: o daemon escuta na porta 53 e responde
  `0.0.0.0` para domínios bloqueados, encaminhando as demais consultas ao
  upstream Cloudflare Security (`1.1.1.2`). Não depende de sessão de foco
  ativa nem do arquivo `hosts` — qualquer dispositivo que use o DNS do
  roteador fica protegido. Novos comandos: `focusguard dns start|stop|status`.
- **Bloqueio de DNS-over-HTTPS ao ligar o sinkhole** — ao subir o servidor
  DNS, o daemon também bloqueia a porta 853 (TCP/UDP) para navegadores não
  contornarem o bloqueio. QUIC/DoH3 (UDP 443) não é bloqueado nesta versão.
- **Edição Server (headless) com instalador próprio** — a release passa a
  publicar **dois instaladores Windows**: `focusguard-<v>-amd64.msi`
  (desktop) e `focusguard-server-<v>-amd64.msi` (Server). A edição Server é
  um "aparelho" para rodar o sinkhole 24/7: instala apenas daemon + watchdog
  + interface web + CLI (sem tray, sem atalho no desktop e sem chave Run) e
  grava o marcador `server.role` ao lado do daemon — em **instalação limpa**
  o DNS já nasce habilitado no primeiro boot. As duas edições compartilham o
  UpgradeCode: instalar uma sobre a outra converte a máquina (numa
  instalação existente, o DNS é habilitado na tela Rede ou com
  `focusguard dns start`).
- **Upstream DNS configurável** — o resolver do sinkhole deixa de ser fixo:
  `focusguard dns upstream <host[:porta]>` ou o painel web (tela Rede)
  escolhem entre Cloudflare, Google, Quad9, AdGuard ou um servidor custom,
  com validação de host/porta, persistência no `state.json` e troca em tempo
  real (restart instantâneo quando o sinkhole está ligado).
- **Usuários da interface web** — novo `user.json` ao lado do `state.json`
  com senhas em **bcrypt** (nunca texto puro no disco). O admin (`admin`) é
  garantido no primeiro boot e não pode ser removido; o admin cria/remove
  usuários e troca senhas de qualquer conta (ações IPC `user-list`/`user-add`/
  `user-remove`/`user-set-password`).
- **Login e sessões na interface web** — a UI agora exige autenticação: tela
  de login, sessões em memória com cookie HttpOnly SameSite=Strict (12h),
  rate limit de login (5 falhas → 30s) contra brute force e permissão por
  usuário (gestão de usuários só para o admin; usuário comum troca a própria
  senha). Todas as ações do painel (`/api/action`) passaram a exigir sessão.
  O primeiro acesso usa o usuário `admin` com a senha padrão definida na
  instalação — troque-a logo após o login (Configurações → Usuários).
- **Painel web: tela Rede com upstream + card Usuários** — a tela Rede ganhou
  o seletor de upstream (chips Cloudflare/Google/Quad9/AdGuard + campo
  custom, com aviso de que trocar reinicia o servidor e zera os contadores);
  o Configurações ganhou o card Usuários (listar, adicionar, remover, trocar
  senha) para o admin e o card "Minha conta" para usuários comuns.

## [0.14.0] - 2026-08-05

### 🛠️ Correções

- **Atualização no Windows ("Acesso negado")** — o `rename` dos binários
  falhava porque o tray e o watchdog estavam em execução (file lock) e o
  daemon trocava o próprio exe sem plano B. Agora, antes do swap: o daemon
  para o serviço `FocusGuardWatchdog`, encerra o `focusguard-tray.exe` e
  aguarda os handles liberarem; o rename ganhou retry (3× 500 ms) para locks
  transitórios de antivírus; e o daemon é trocado **primeiro**, preservando o
  all-or-nothing (se o próprio rename falhar, nada mais foi alterado).
- **Update agendado para o reboot** — se o rename do daemon falhar mesmo com
  retry, o FocusGuard não deixa a suíte pela metade: agenda a substituição
  completa via `MoveFileEx` + `MOVEFILE_DELAY_UNTIL_REBOOT`, continua rodando
  a versão antiga e avisa "atualização será concluída no próximo reinício" na
  CLI, no tray e no painel web (novo estado `PendingReboot` no IPC). Após o
  swap, o watchdog é religado; o tray reaparece no próximo login (autostart
  via chave Run do Windows).
- **Validação de duração custom (web)** — o botão de bloquear agora valida o
  valor digitado antes de submeter, evitando janelas de bloqueio inválidas.
- **Grid semanal com janelas overnight (web)** — horários de agenda que
  atravessam a meia-noite agora são renderizados corretamente no grid.
- **Contador de foco ao vivo (web)** — o contador da sessão ativa no dashboard
  não congela mais; permanece atualizado enquanto a sessão roda.
- **Presets (web)** — o rótulo do preset usa o nome original em vez do
  slug/nome de exibição quando o fallback é acionado.
- **Meta do foco (web)** — o botão de meta no dashboard só fica destacado
  quando o valor exato da meta é atingido.

## [0.13.0] - 2026-08-05

### ✨ Novas funcionalidades

- **Bloqueio duplicado: somar ou substituir** — bloquear um domínio que já
  possui bloqueio ativo agora retorna conflito em vez de sobrescrever
  silenciosamente. A CLI ganhou `--extend` (soma o tempo ao término atual) e
  `--replace` (reinicia o bloqueio); o painel web exibe um diálogo com
  "cancelar", "somar" ou "substituir" e o tempo restante do bloqueio vigente.

### 🛠️ Correções

- **Enforcer (Windows)** — os IPs são validados/canonicalizados antes do
  `remoteip=` do `netsh`, descartando domínios e entradas inválidas que
  faziam o batch falhar com `exit status 1`; a migração de regras legadas
  IPv6 saiu do script e virou delete tolerante pré-lote (não aborta mais o
  `BlockAll` quando a regra antiga não existe).
- **Web vs Daemon** — o status "daemon ligado" do painel agora é decidido pelo
  ping IPC real (e não pela resposta de `status`, que falhava com poucos
  bloqueios) e cada ação passou a ter orçamento de timeout próprio (status
  15 s, mutações 30 s, update 150 s), eliminando o falso "daemon desligado".
- **Limpeza pós-update** — cada atualização deixava um `.bak.<timestamp>` por
  binário para sempre. Agora o daemon varre a pasta de instalação após o
  update e no boot: mantém apenas o backup mais recente de cada binário (o
  que o smart recovery do watchdog ainda precisa) e remove órfãos
  `.old`/`.trash`/`*-new.exe`.
- **Watchdog vs update** — o daemon grava a flag `update.inprogress` antes de
  aplicar a atualização e a nova versão a remove ao concluir um boot saudável.
  Durante a janela o watchdog não mata nem desfaz o update (tetos: `updateGrace`
  de 3 minutos — um update travado não silencia a proteção para sempre).

## [0.12.1] - 2026-08-05

### ✨ Novas funcionalidades

- **Ação `sessions` e sessões recentes na UI** — nova ação IPC `sessions`
  (histórico das sessões de foco concluídas) e nova seção "sessões recentes"
  nas estatísticas do painel web.
- **`Block.Extend`** — estende a expiração de um bloqueio ativo sem removê-lo.
- **Logs de arquivo** — watchdog, tray e web agora gravam logs junto ao
  executável (`internal/filelog`).
- **Ícone no watchdog** — o binário do watchdog embute o ícone do FocusGuard
  (ícone próprio na tarefa/atalho).
- **Endpoint de profiling** — o daemon expõe `/debug/pprof/` via env `FG_PPROF`
  (loopback, stdlib, com suporte a `seconds=` e `?gc=1`).

### ⚡ Performance (P0/P1 de `docs/perf-2026-08-05.md`)

- **Store** — `Save` marshala o `state.json` fora do lock; só a escrita atômica
  fica serializada (região crítica menor sob concorrência).
- **Enforcer (Windows)** — cache da enumeração de regras netsh (TTL 10 s),
  invalidado nas mutações; tira a enumeração de `FocusGuard-*` (17% do CPU do
  perfil real) da maioria dos `Sync`.
- **Scheduler** — `ListBlocks` lê um snapshot atômico invalidado nas mutações
  (≈2,2× em 1000 blocos; `Status` IPC ≈3×) e cache DNS TTL 60 s para domínios
  repetidos.
- **Analytics** — `Sessions` incremental com cache por fingerprint do arquivo
  (chamadas repetidas de `stats` ≈125× mais baratas a 10k sessões; mudança
  externa força releitura integral).
- **httpapi** — respostas gzip quando o cliente envia `Accept-Encoding: gzip`
  (−98% no payload de status com 1000 bloqueios).
- **processguard** — intervalo padrão do scan elevado de 5 s para 15 s.

### 🐛 Correções

- **Daemon** — guards contra construtores de watcher nil e erros de bind nil /
  Windows (panic pré-existente do restart).

## [0.12.0] - 2026-08-04

### 🪟 Tray: correções de instalação e robustez

- **Run key com aspas no caminho** — o valor gravado em
  `HKCU\...\CurrentVersion\Run` agora é `"C:\Program Files\FocusGuard\focusguard-tray.exe"`
  (com aspas), corrigindo a falha silenciosa no boot: o Windows tentava executar
  `C:\Program` por causa do espaço no caminho e o tray nunca iniciava. Corrigido
  nos 3 pontos que gravam a chave: `wix.json` (MSI), `install-daemon.ps1` e
  `autostart_run.go` (auto-registro do tray).
- **MSI inicia o tray após instalar** — o instalador agora executa o tray na
  sessão do usuário logo após a instalação (CustomAction `WixQuietExec` com
  `Impersonate="yes"`, que roda oculto por natureza), quebrando o ciclo em que
  o tray nunca iniciava e, por isso, nunca se auto-registrava. O auto-registro do
  tray (`ensureTrayAutostart`) continua como rede de segurança para logins
  seguintes.
- **Tray do .msi sem janela de console** — o `build-msi.sh` agora compila o
  `focusguard-tray.exe` com `-H windowsgui` (o GoReleaser já usava), eliminando a
  janela preta que aparecia ao iniciar o tray instalado pelo instalador.
- **Notificação nativa assíncrona (Windows)** — o balão de notificação
  (PowerShell/WinForms com `Start-Sleep 9`) não trava mais o polling do pomodoro:
  o processo é iniciado de forma assíncrona (`Start()` em vez de `Run()`), então
  a checagem de fase continua fluida.

## [0.11.1] - 2026-08-04

### 🐛 Instalador .msi: versão do daemon injetada

- **Daemon instalado via `.msi` agora reporta a versão correta** — o
  `build-msi.sh` compilava o `focusguard-daemon.exe` sem injetar a versão via
  ldflags (o GoReleaser faz isso com `-X main.daemonVersion`), então a UI/status
  exibiam `0.0.0-dev` e o auto-update ficava desabilitado. O script agora
  injeta `-X main.daemonVersion=${VERSION}` no build do daemon, igual ao
  pipeline de release.

## [0.11.0] - 2026-08-04

### ✨ Tray abre o painel web e tema claro/escuro na UI

- **Tray: "Abrir painel" em vez de TUI** — o item do menu agora abre a
  interface web no navegador (via `focusguard web`), em vez de tentar abrir
  uma TUI de terminal. No Windows o CLI é iniciado sem janela de console
  (`HideWindow`); no Linux, em nova sessão (`setsid`), sem deixar terminal
  visível preso ao tray.
- **Tema claro/escuro na UI** — novo `ThemeToggle` (sol/lua) no cabeçalho
  (desktop e mobile). A escolha é persistida (`localStorage "theme"`), com
  script anti-flash no `index.html` e `color-scheme: dark light`.
- **Pomodoro: parâmetros corrigidos** — a UI envia `work_min`/`rest_min`
  (alinhado ao contrato Go), não mais `work`/`rest`.

## [0.10.3] - 2026-08-04

### 🐛 Instalador .msi: interface web agora é embutida

- **UI incluída no `.msi`** — o job `windows-msi` não compilava a interface
  web (React) antes do `go-msi`, então o `focusguard-web.exe` instalado
  embutia uma pasta de assets vazia e a UI não abria (página 404). O pipeline
  agora roda `npm ci && npm run build` e copia o `dist` para
  `cmd/focusguard-web/assets` (embutido via `go:embed`) antes de gerar o
  instalador, espelhando o hook do GoReleaser.

## [0.10.2] - 2026-08-04

### 🐛 Instalador .msi: correção do caminho do ícone

- **Ícone do instalador resolvido a partir da raiz do repositório** — o
  `go-msi` resolve os caminhos relativos do `wix.json` em relação ao diretório
  de trabalho do processo (a raiz do checkout), não em relação à pasta do
  `wix.json`. O `../../focusguard.ico` anterior apontava dois níveis acima do
  checkout (ex.: `D:\a\focusguard.ico`), o que fazia o `light` do WiX abortar
  com `LGHT0103` nas linhas 30 e 143 do `product.wxs`. Agora o `icon` e o ícone
  do atalho usam `focusguard.ico` (relativo à raiz), e o `build-msi.sh` falha
  cedo com mensagem clara se o ícone não existir.

## [0.10.1] - 2026-08-04

### 🐛 Instalador .msi: correção do job `windows-msi` no CI

- **Correção do cross-drive `filepath.Rel`** — o `go-msi` resolve os caminhos
  do `wix.json` para absolutos e os torna relativos ao diretório de trabalho
  (`--out`). No runner do CI o checkout fica em `D:` e o diretório temporário
  do SO em `C:`, o que abortava a geração com
  `Rel: can't make D:/... relative to C:/...`. O `build-msi.sh` agora aponta
  `--out` para um diretório dentro do repositório (`build/go-msi`),
  mantendo tudo na mesma unidade — junto com `--path`/`--src` em caminhos
  absolutos Windows, o .msi volta a ser gerado e anexado à release.

## [0.10.0] - 2026-08-04

### 📦 Instalador único .msi (Windows)

- **Instalador `.msi` em um clique** — cada release agora também publica o
  `focusguard-<versão>-amd64.msi`, gerado no CI (job `windows-msi` com
  go-msi + WiX Toolset). Basta executar o arquivo (ou
  `msiexec /i focusguard_<versão>-amd64.msi`): instala os 5 executáveis em
  `C:\Program Files\FocusGuard` (Todos os Usuários), registra os serviços
  `FocusGuard` e `FocusGuardWatchdog` com início automático e recovery
  (`sc failure ... restart`), registra o tray na inicialização e cria o
  atalho do FocusGuard no Desktop.
- **Desinstalação limpa** — a remoção (Programas e Recursos ou
  `msiexec /x`) para/remove os serviços, o atalho e a pasta de instalação;
  o estado em `C:\ProgramData\FocusGuard` é preservado.
- **Pipeline e build local** — novo job `windows-msi` no release.yml e alvo
  `make msi VERSION=x.y.z [ARCH=amd64|arm64]` (script `scripts/build-msi.sh`
  com go-msi pinado + template WiX próprio em `scripts/msi/`).

### 🪟 Tray: autostart auto-registrado

- **Tray se registra no HKCU Run no primeiro launch** — como o .msi roda como
  SYSTEM (instalação per-machine), ele não consegue gravar a chave Run do
  usuário real; agora o próprio tray registra seu autostart ao iniciar
  (best-effort, sem elevação), garantindo que volte a abrir com o Windows
  para aquele usuário.

## [0.9.0] - 2026-08-04

### 🐛 Daemon: crash-loop na porta IPC (correção crítica)

- **Singleton no daemon** — se outra instância do FocusGuard já estiver
  atendendo a porta IPC (48901), a nova instância encerra limpo com uma mensagem
  clara, em vez de crash-loopar com `bind: address already in use` +
  `Reiniciando...` para sempre. Antes, duas instâncias (ex.: serviço SCM + uma
  segunda iniciada à mão, ou uma sobreposição de restart) brigavam pela porta e
  o log virava um ciclo sem fim de falhas de bind a cada ~6–10s.
- **Fim do flood `Sinal <nil> ignorado`** — quando o servidor IPC falhava ao
  iniciar, o `close(sigChan)` deixava o goroutine de sinais lendo `nil` de um
  canal fechado em hot-loop, inundando o log com centenas de linhas idênticas
  por segundo. O goroutine agora reconhece o canal fechado e encerra.
- **Parada de serviço sem hot-loop nem panic** — o canal de parada do serviço é
  tratado apenas na primeira notificação (um canal fechado fica "pronto" para
  sempre no select); o SCM pode pedir Stop mais de uma vez e o `close()` é
  guardado por `sync.Once`, eliminando um `panic: close of closed channel`
  latente no Windows.
- **Log honesto do servidor IPC** — a mensagem de "ativo e aguardando
  requisições" deixa de ser impressa antes do bind realmente acontecer.

### 📝 Logs do daemon

- **Log em arquivo na pasta de instalação** — o `focusguard-daemon` agora grava
  seu log em `focusguard-daemon.log` ao lado do executável (`C:\Program Files\
  FocusGuard` no Windows, `/opt/focusguard` no Linux), com rotação automática
  quando o arquivo passa de 1 MiB (o anterior vira `focusguard-daemon.log.1`).
  Se a pasta não for gravável, o log continua no stderr (best-effort).

### 🔄 Update reinicia na hora

- **Update de versão reinicia imediatamente** — ao aplicar um update, o daemon
  encerra a sessão pomodoro e reinicia na hora com a versão nova (antes, o
  restart ficava pendente até os bloqueios expirarem). Os bloqueios não são
  tocados: ficam no state.json e o boot da nova versão os restaura — proteção
  contínua.

### ⏳ Expiração de bloqueios confiável

- **Bloqueio expirado só sai do estado após o SO confirmar** — a remoção de
  bloqueios expirados (RAM + state.json) agora só acontece depois que o enforcer
  confirma a remoção das regras de hosts/firewall; em falha transitória (ex.:
  netsh ainda subindo após o boot), o bloqueio permanece com timer de retry em
  vez de o estado declarar "limpo" com regras órfãs.

### 🌐 Interface web

- **Grade semanal na Agenda** — visualização das regras recorrentes em um grid
  7×24h com blocos coloridos por categoria, lane stacking para janelas
  sobrepostas, marcador "agora" no dia atual e legenda. Comporta rolagem
  horizontal no mobile.
- **Anel visual no Pomodoro** — o contador da sessão ativa virou um anel de
  progresso SVG (verde no foco, azul no descanso) com o tempo restante no
  centro.

## [0.8.0] - 2026-08-03

### 🌐 Interface web — todas as funcionalidades

- **Todas as features no navegador** — novas telas: Pomodoro (ciclos, strict,
  missões, encerramento), Agenda (agendamentos recorrentes + importação .ics),
  Apps (denylist de processos), Presets personalizados, Estatísticas (gráficos
  por dia/domínio, streak, missões, export CSV/JSON) e Segurança (histórico de
  burla). A Configuração ganhou aplicar atualização com canal stable/beta.

### 🗑️ TUI removida

- **TUI interativa removida** — a interface web cobre tudo. `focusguard` sem
  argumentos agora abre a interface web no navegador; o pacote `internal/tui`
  e as dependências Bubble Tea saíram do projeto.

### 🔧 Atualização atômica e confiável

- **Update multi-binário atômico** — substituição dos executáveis com rollback
  seguro em falha, restauração Windows-safe e reinício confiável após a aplicação.

## [0.7.0] - 2026-08-03

### 🎨 Ícone do sistema

- **Artwork real no ícone** — o `focusguard.ico` (multi-tamanho, embutido nos
  `.exe` via go-winres) e o `focusguard.png` (atalho do Desktop Linux) agora
  são gerados a partir do artwork **`img/focusguard.png`** (1024px) em vez do
  shield desenhado proceduralmente. O `cmd/focusguard-icon` redimensiona com
  alta qualidade (CatmullRom via `golang.org/x/image`) e o ícone do tray
  (Linux) passou a ser embutido no binário a partir do mesmo artwork — um único
  design em todos os pontos.

### 🔄 Sincronização e status do firewall

- **Sweep de regras órfãs no `sync`** — se o daemon for morto (crash/sigkill)
  antes de um `unblock`, as regras de firewall do domínio ficavam para trás e o
  `sync` apenas adicionava as novas. Agora o `sync` remove regras de domínio que
  não pertencem a nenhum bloco ativo, nos dois sistemas — no Windows preservando
  regras DoH/DoT, allow e o catch-all do modo pânico.
- **`status` fora do lock de mutação** — a consulta de status (lenta, via
  `netsh show`/`iptables -S`) não trava mais as operações de bloqueio. O
  resultado é cacheado por 15s e invalidado a cada mutação, mantendo o
  `status` rápido e o lock curto.
- **Nome de regra IPv6 normalizado no Windows** — regras de domínio IPv6 agora
  usam `FocusGuard_2606_4700_4700__1111` (dois-pontos substituídos, como já
  acontecia nas regras DoH/allow). No primeiro `block` o nome legado com
  dois-pontos é removido antes de criar o novo, e o `unblock` apaga os dois
  formatos.
- **`unblock --internet` enumerando antes de remover** — as allow rules são
  listadas antes do delete do catch-all: se um crash ocorrer no meio, o
  bloqueio de internet já foi removido (nada fica inacessível); sobra apenas
  lixo inerte, varrido no próximo `block --internet`.

### 🛡️ Correção de fuga do bloqueio (Firefox/QUIC)

- **DoH bloqueado em TCP e UDP** — as regras de bloqueio de
  DNS-over-HTTPS (porta 443) agora cobrem também o protocolo UDP, fechando o
  vazamento via QUIC/HTTP/3 que o Firefox usa por padrão. Sem isso, o Firefox
  resolvia os domínios pelo próprio DoH, ignorava o arquivo `hosts` e alcançava
  o site por um IP diferente do bloqueado.
- **REJECT válido no iptables** — as regras de domínio (e o catch-all do
  `block --internet`) agora emitem `-p tcp -j REJECT --reject-with tcp-reset`
  seguido de `-j REJECT --reject-with icmp-port-unreachable`. O `tcp-reset`
  sem `-p tcp` era recusado pelo iptables — o bloqueio de firewall de domínios
  e o modo pânico nunca eram aplicados no Linux, e tráfego UDP/QUIC escapava.
- **Migração no Windows** — o `BlockDoH` substitui as regras antigas (tcp-only)
  por um par `tcp`/`udp` por resolver (`FocusGuard_DoH_<ip>_tcp/_udp`).

### 🌐 Interface web (focusguard-web)

- **`focusguard web`** — abre a interface web no navegador: inicia o
  `focusguard-web` por demanda (singleton via probe de porta) e abre
  `http://127.0.0.1:48902`. Servidor em user-space, sem privilégios, que faz
  proxy das ações IPC para o daemon — **nenhuma mudança no daemon**.
- **Painel web (React + TypeScript + Vite)** — `focusguard-ui/` com 4 telas:
  Dashboard (status, countdown ao vivo, meta do dia, bloqueios ativos), Bloquear
  (presets + duração), Modo pânico (allowlist + confirmação) e Configurações
  (meta diária, atualizações, proteção). Tema dark com a identidade do escudo.
- **Servidor HTTP seguro** — `internal/httpapi`: bind loopback apenas,
  validação de Host (anti-DNS-rebinding), `Content-Type` obrigatório
  (anti-CSRF), headers CSP/nosniff/X-Frame e limite de corpo. UI embutida no
  binário via `go:embed` (`make ui`).
- **Release e instalação** — `focusguard-web` entra nos arquivos das duas
  plataformas (amd64+arm64), nos installers (ps1/sh) e no update multi-binário.

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
