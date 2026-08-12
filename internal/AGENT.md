# AGENT.md — internal/

> Guia para agentes de IA que trabalham neste diretório. Consulte também o
> **[AGENT.md](../AGENT.md)** na raiz (specs, convenções, armadilhas) — leia-o
> antes de editar qualquer código.

## Propósito

Núcleo do FocusGuard: **34 pacotes** consumidos pelos binários de `cmd/`.
Fonte de verdade em RAM (scheduler), disco como espelho (state.json), watchers
restauram adulterações, IPC é o contrato entre CLI/tray/web ↔ daemon.

## Mapa dos pacotes

> 34 pacotes em 4 camadas — `domain/` (regra de negócio), `infrastructure/`
> (IO de SO), `transport/` (protocolo IPC/HTTP + observabilidade), `system/`
> (ciclo de vida daemon/tray/watchdog) — reorg concluída (`docs/reorg-plan.md` Fase C).

| Pacote | Responsabilidade |
|---|---|
| `domain/analytics` | Histórico JSONL de sessões; streak, stats, exports CSV/JSON/HTML |
| `domain/apps` | Denylist de processos (apps.json) p/ o process guard; fallback `steam, discord` |
| `infrastructure/autostart` | Serviço (`sc`/systemd), autostart do tray (HKCU Run / XDG), atalho desktop + `ExtractIcon` |
| `domain/blocks` | Handlers de domínio das ações `block`/`block-all` (`Blocker`/`Catalog`) |
| `system/daemon` | Ciclo de vida do daemon: `Run(ctx) error` + shutdown ordenado (B10) |
| `infrastructure/dns` | Handlers de domínio do sinkhole DNS (`start`/`stop`/`status`/`set-upstream`) |
| `infrastructure/dnsserver` | Sinkhole DNS embutido (porta 53, miekg/dns) + forward de upstream |
| `infrastructure/enforcer` | Aplica bloqueios no SO: hosts + firewall (`enforcer_linux.go`/`enforcer_windows.go`); `BlockAll`/allowlist; sanitização de domínios |
| `transport/eventhub` | Pub/sub de eventos em processo (ring buffer + long-poll `Wait`) — mudanças de estado |
| `infrastructure/filelog` | Log de arquivo compartilhado (append + rotação) ao lado do executável |
| `infrastructure/fsutil` | SHA-256 de arquivo (watchers) |
| `domain/goal` | Meta diária (goal.json) |
| `infrastructure/hostswatch` | Watcher do `hosts`: fsnotify + hash anti-loop; detecta/reverte adulterações |
| `transport/httpapi` | HTTP da UI: proxy IPC + estático + guardas localhost (Host, Content-Type, CSP) |
| `infrastructure/icon` | Renderiza `packaging/artwork/focusguard.png` em qualquer tamanho; `GenerateICO`/`GeneratePNG` |
| `transport/ipc` | Protocolo JSON sobre socket; `SendWithTimeout`; server/handlers |
| `transport/ipcerr` | Códigos de erro estáveis do IPC (`Error`) — espelho de `internal/transport/ipc/codes.go`, aditivo |
| `transport/metrics` | Registry de latência por ação (ring + percentis) — IPC do daemon e proxy web |
| `domain/policy` | Modelo `Block` (`IsActive`, `CanUnblock`, `RemainingTime`) |
| `domain/pomodoro` | Sessões work/rest/cycles; prefs persistidas; resumo pós-sessão |
| `domain/preset` | Catálogo builtin (social/video/news/games) + personalizados |
| `domain/presets` | Handlers de domínio do catálogo de presets (list/add/remove) |
| `infrastructure/processguard` | Encerra processos da denylist a cada 5s durante sessão ativa |
| `domain/recovery` | Smart Recovery: `FindRecentBackup`, `ShouldRollBack`, `RestoreFromBackup` |
| `domain/schedule` | Regras recorrentes (dias/horários, janelas overnight, import iCal); worker 30s |
| `domain/scheduler` | Ciclo de vida dos bloqueios: `Block`, `Reconcile`, expiração, refresh de IPs (15min) |
| `infrastructure/statewatch` | Watcher do `state.json`: restaura o disco a partir da RAM |
| `infrastructure/store` | Persistência JSON atômica + réplica AES-256-GCM atrelada ao hardware |
| `infrastructure/tamper` | Log JSONL append-only de burlas detectadas/revertidas |
| `system/tray` | Controlador do systray: menu, tooltip dinâmico, notificações, IPC com timeout |
| `infrastructure/update` | Auto-update multi-binário atômico (`UpdateToAll`) com rollback |
| `domain/user` | Armazenamento de contas/senhas (usuário admin, hash) |
| `domain/users` | Handlers de domínio de usuários (add/remove/verify/set-password) |
| `system/watchdog` | Health check systemd (`NOTIFY_SOCKET`) |

## Regras específicas

1. **Fonte de verdade em RAM** — nunca trate state.json como autoridade;
   `Reconcile` sobrescreve o disco quando diverge da RAM.
2. **Escrita atômica + SHA-256 anti-loop** — `store` grava temp+rename e os
   watchers marcam self-writes por hash (`MarkSelfWrite`) para não reagirem a
   self-writes (sem janela cega de tempo).
3. **Best-effort para o SO** — falha de firewall/notificação/autostart loga e
   segue; **nunca** derruba o daemon.
4. **IPC nunca bloqueia o tray/web** — toda chamada usa `SendWithTimeout` (5s).
5. **Arquivos por plataforma** — `_windows.go`/`_linux.go`/`_other.go` com
   interface comum no arquivo-base; comandos externos mockados (`execCommand`,
   `execCommandContext`, `osRename`, `goos`) — nunca rode `sc.exe`/`iptables`/
   `systemctl` reais em teste unitário.
6. **Defensivo** — sanitize domínios (`sanitizeDomain`), tetos (pomodoro
   `--work` ≤ 7 dias, goal ≤ 1440min), rollback atômico no update, sweep de
   regras órfãs no `Sync`.
7. **Resumo de sessão** — ao final da sessão, atualize o
   `../docs/session-log/YYYY-MM-DD.md` (handoff diário para o próximo agente
   — regra do AGENT.md raiz §4.15).

## Bugs e correções potenciais

### ✅ Corrigidos no bug-hunt (2026-08-10) — não regredir

- **`scheduler/scheduler.go` (último bloco expira)** — quando o último
  bloqueio expirava, o sweep de regras órfãs do `Sync` não rodava e regras
  de firewall ficavam para trás (raça com o refresh). Corrigido com teste
  TDD (`expiry_cleanup_test.go`, commit `577b7a5`).
- **`scheduler/scheduler.go` (`BlockDomains`)** — o batch aplicava só o
  conjunto novo ao `Sync`, removendo proteção pré-existente de outros
  bloqueios ativos. Corrigido: passa **todos** os blocos ativos ao `Sync`
  (commit `50b72ef`).
- **`scheduler/scheduler.go` (`startPeriodicIPRefresh`)** — a goroutine de
  refresh (15min) vazava no shutdown do daemon. Corrigido: `Stop()` fecha o
  canal e aguarda a goroutine sair (commit `d39c70e`).
- **`domain/schedule/ics.go` (`icsWindow`)** — o fallback de +1h (DTEND
  ausente/malformado) com DTSTART ≥ 23:00 emitia janela `"24:xx"`, que o
  próprio pacote rejeita (`parseClock` exige h ≤ 23). Corrigido: wrap para
  o dia seguinte (janela overnight já suportada); descoberto pelo review do
  `FuzzParseICS` + seed de regressão (commit `43c9163`).
- **`ipc/server.go` (`default`)** — mensagem de ação desconhecida tinha typo
  (`"Not suported action"` → `"Not supported action"`); corrigido junto com
  os testes que asseravam o texto (`server_test.go`, `integration_test.go`,
  `router_edge_test.go`, `domain_wiring_test.go`).
- **`scheduler/scheduler.go` (`Block` e `ExtendBlock`)** — na falha do
  `store.Save`, a RAM mantinha o domínio em estado zumbi: no `Block`, o
  domínio ficava ativo sem timer e sem regra (visível no status para sempre);
  no `ExtendBlock`, a extensão ficava na RAM com o timer antigo armado — ao
  disparar, `onExpire` via o bloco ativo e retornava sem re-armar (nunca
  expirava). Corrigido: `Block` reverte (`delete` + `invalidateSnapshot`) e
  `ExtendBlock` restaura o bloco original no erro do Save, como
  `BlockDomains`/`BlockAllInternet` já faziam — testes TDD
  (`save_rollback_test.go`).
- **`scheduler/scheduler.go` (`SetDNSEnabled`/`SetDNSUpstream`)** — na falha
  do `Save`, a RAM mantinha o setting divergente do disco até o próximo boot
  (o daemon lê `DNSEnabled()`/`DNSUpstream()` após o boot). Corrigido: ambos
  revertem ao valor persistido no erro do Save — testes TDD
  (`save_rollback_test.go`).

### Abertos (candidatos a hardening)

- **`ipc/server.go` (update)** — o handler `update` roda `Check(apply=true)`
  dentro de um timeout de 30s; para downloads grandes o `UpdateToAll` (que
  baixa o archive inteiro) pode estourar o deadline e o daemon aborta o update.
  Considerar separar check/apply ou aumentar o teto para o fluxo de aplicação.

- **`store/json.go` (`Load`)** — quando o arquivo não existe, carrega a réplica
  criptografada antes de curar; porém `Load` expõe `s.mu.RLock` + I/O de disco
  sob lock de leitura — ok para o volume atual, mas não copie esse padrão para
  código de hot path (prefira cache em RAM).

- **`recovery/recovery.go` (`FindRecentBackup`)** — o `sort.Slice` re-estatiza
  os candidatos dentro do comparador (`os.Stat` por comparação); para muitos
  `.bak.*` isso é O(n log n) stats. Pré-calcule mtimes uma vez.

- **`update/update.go` (`UpdateToAll`)** — o rollback restaura apenas os
  binários em `okPaths` (índices alinhados com `backups`); se um binário novo
  foi *copiado* mas o `Rename` do `.old` falhou no Windows (arquivo em uso), o
  `os.Remove(oldPath)` best-effort pode deixar `.old` órfão. O sweep de
  `.old` na próxima atualização já cobre, mas documentar como armadilha.

- **`pomodoro/prefs.go`** — `osWriteFile` usa `0o644` (contrastando com os
  `0600` de store/goal/apps); preferências não são sensíveis, mas por
  consistência do pacote vale alinhar para `0600`.

- **`tray/controller.go`** — `startUpdatePolling`/`startPomodoroPolling`
  criam `time.NewTicker` sem `Stop` (goroutines vivas para sempre). Aceitável
  para o processo do tray, mas documentar; se um dia o controller ganhar
  teardown, parar os tickers.

## Testes

- `go test ./internal/... -count=1 -timeout=60s` (não exigem admin — mockam o SO).
- Rodar com `-race` ao tocar em scheduler/pomodoro/processguard (há
  `race_test.go`, `concurrency_test.go`, `benchmark_test.go` dedicados).
- Fuzz: `internal/domain/schedule/fuzz_test.go` tem `FuzzParseICS`,
  `FuzzWindowsPairs` e `FuzzParseClock` (rodar 30s cada sem crash —
  `go test ./internal/domain/schedule/ -run '^$' -fuzz FuzzParseICS -fuzztime=30s`).
- CI (`.github/workflows/test.yml`): `-race` no Linux + teste de chown do
  socket como root (o teste faz `Skip` sem root — o step usa `sudo`).
