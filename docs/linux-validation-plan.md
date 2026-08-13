# Plano — Validação completa no Linux

> **Status:** 🟢 **EM EXECUÇÃO (2026-08-13)** — **Etapa 0 CONCLUÍDA no CI** (run
> 31710267829, 4 jobs verdes) e **Etapa 2 CONCLUÍDA no real** (WSL2/Ubuntu
> com systemd): install/uninstall completos do `install-linux.sh` validados
> ponta a ponta (binários, unit, grupo, socket, autostart, atalho, ícone,
> estado preservado, watchdog). Etapas 1 e 3–10 pendentes. Escopo: validar **toda a stack FocusGuard no
> Linux**, plataforma que nunca foi testada de ponta a ponta em máquina real
> (o Linux existe no papel — AGENT.md, goreleaser, install-linux.sh — e
> parcialmente no CI, mas nunca rodou a suíte completa nem o E2E de SO real).
>
> **Alvo primário:** Ubuntu/Debian (família Debian-style) com systemd, daemon
> como root. **Ambiente:** o CI (`ubuntu-latest`) automatiza o máximo; uma
> máquina real (VM com sessão gráfica e sudo) executa o checklist E2E que
> exige GUI/root (install, tray, navegador, CA, sinkhole, iptables).

## Por que um plano só para Linux

O que já existe hoje (e o que falta):

| Área | Estado atual | Lacuna |
|---|---|---|
| Build/vet | CI `test.yml` roda `go build ./...` + `go vet ./...` no ubuntu-latest | OK |
| Testes | CI roda `-race` em **15 pacotes** (escolhidos) + teste de chown do socket como root | **A suíte completa `go test ./...` NUNCA rodou no Linux** |
| Pacote do daemon | No Windows exige shell **elevado** (manifest) — historicamente nunca rodou em CI | **No Linux não há manifest: a suíte do daemon pode rodar em CI normal** (incl. `interceptor_ipc_test.go`, `ca_test.go`) |
| Cross-compile Windows | Só validado na release (GoReleaser) | Nenhum job CI garante que mudanças Linux não quebram o build Windows |
| Install (`install-linux.sh`) | Escrito, versionado, com testes de unidade do autostart | Nunca executado numa máquina real |
| Enforcer (iptables/hosts) | `enforcer_linux.go` + testes com exec mockado | Regras reais nunca aplicadas/verificadas em kernel Linux |
| CA + interceptor :80/:443 | `store_linux.go` (update-ca-certificates) + testes herméticos | Instalação real no trust store + navegador nunca testadas |
| DNS sinkhole :53 | `dnsserver` + testes | **Conflito conhecido com systemd-resolved** nunca verificado no real |
| Watchdog systemd | `NOTIFY_SOCKET` + unit | Só com systemd real (VM) |
| Tray | cgo appindicator, build no CI | Nunca rodou numa sessão desktop |
| Update | `go-selfupdate` + Orchestrator | Fluxo completo (tar.gz, .bak, swap, rollback) nunca testado no Linux |

---

## Etapa 0 — Baseline automatizada no Linux (CI)

**Objetivo:** fechar a lacuna de CI antes de qualquer validação manual —
descobrir o que a suíte completa faz no Linux.

- [ ] Novo job no `.github/workflows/test.yml` (ubuntu-latest, com deps do
      tray: `libayatana-appindicator3-dev libgtk-3-dev`):
  - [ ] `go build ./...`
  - [ ] `go vet ./...`
  - [ ] `go test ./... -count=1 -timeout=120s` (**suíte completa**, não só os
        15 pacotes do job `race`)
  - [ ] `gofmt -l .` (sem output)
  - [ ] `make contract-check`
- [ ] Ampliar o job `race` para cobrir **todos** os pacotes concorrentes
      restantes (hoje são 15 — comparar com a lista de pacotes e subir o
      timeout se preciso), ou rodar `-race ./...`.
- [ ] Novo job `windows-compile-check`: `GOOS=windows go build ./...`
      (sem os `.syso`? verificar — os recursos Windows são versionados, então
      compila) — protege o build Windows contra regressão vinda de mudanças
      Linux.
- [ ] Rodar o CI e **registrar os primeiros achados** (testes que falham só no
      Linux: paths assumidos de Windows, skips invertidos, binários ausentes
      como `iptables`/`notify-send`, permissões em `/var/lib`, etc.). Corrigir
      com TDD onde for bug, Skip/ajuste onde for ambiental.

**Achados esperados (a confirmar):**

| Severidade | Área | Suspeita |
|---|---|---|
| ? | `cmd/focusguard-daemon/main_test.go:1742` | Teste que escreve em `/var/lib/focusguard` — Skip sem root; **como root no CI escreveria no /var/lib real do runner** (verificar e tornar hermético com dir injetável) |
| ? | `internal/infrastructure/enforcer/enforcer_linux_test.go` | Testes de firewall real fazem Skip sem `iptables`/`ip6tables`; como root no runner passariam a mexer no firewall do CI (verificar quais usam exec mockado vs binário real) |
| ? | `internal/system/tray` | Pacote cgo — `go test ./...` precisa das libs do tray instaladas (já no job `race`) |
| ? | Testes que assumem `C:\...`/`PROGRAMDATA` | `cmd/focusguard-web/logging_test.go` já faz Skip fora do Windows — conferir se sobra algum |

**Critérios de saída:** suíte completa verde no ubuntu (com skips documentados
e justificados); `-race` cobrindo todos os pacotes; `windows-compile-check`
verde.

---

## Etapa 1 — Suíte do daemon no Linux (a maior lacuna histórica)

**Objetivo:** rodar os testes do `cmd/focusguard-daemon` — que no Windows só
rodam em shell **elevado** (manifest `requireAdministrator`) e por isso nunca
entraram no CI.

- [ ] `go test ./cmd/focusguard-daemon/... -count=1 -v` no ubuntu (job novo ou
      no job da Etapa 0) — inclui `interceptor_ipc_test.go` (TLS com CA real)
      e `ca_test.go`.
- [ ] Registrar e tratar o que falhar: testes que dependem de elevação
      Windows (devem passar sem mudança no Linux), testes que tocam caminhos
      de sistema (tornar herméticos), etc.
- [ ] Depois de verde, **documentar no AGENT.md** que a suíte do daemon roda
      no CI Linux (a nota "daemon tests só em shell elevado" passa a ser
      Windows-only).

**Critérios de saída:** pacote do daemon verde no CI Linux; nota do AGENT.md
atualizada.

---

## Etapa 2 — Instalação real (install-linux.sh + systemd) — máquina real

**Objetivo:** provar o caminho de instalação que o README promete
(`sudo ./install-linux.sh install` dentro do tar.gz).

Pré: extrair o tar.gz da última release (ou `goreleaser release --snapshot`),
com todos os binários + `install-linux.sh` + `focusguard.service` +
`focusguard-tray.desktop` + `focusguard.png`.

> **EXECUTADA em 2026-08-13 (WSL2/Ubuntu, systemd ativo)** — staging do
> release montado com os 5 binários compilados + scripts + ícone; install e
> uninstall rodados como root (com `SUDO_USER=<usuário>`, espelhando o `sudo`
> real). Todos os itens abaixo conferiram:

- [x] `sudo ./install-linux.sh install`:
  - [x] `/opt/focusguard/` root:root 0755 com os 5 binários;
  - [x] symlink `/usr/local/bin/focusguard → /opt/focusguard/focusguard`;
  - [x] unit `focusguard.service` instalada, `enable` + `restart` ok,
        `systemctl is-active focusguard` = active;
  - [x] grupo `focusguard` criado + usuário adicionado (mensagem de logout);
  - [x] autostart do tray em `~/.config/autostart/focusguard-tray.desktop`
        com `Exec=/opt/focusguard/focusguard-tray`;
  - [x] atalho no Desktop (`focusguard.desktop`, Terminal=false) + ícone no
        hicolor;
  - [x] `focusguard status` sem sudo (após re-login) — socket
        `root:focusguard 0660` em `/run/focusguard.sock`.
- [x] `journalctl -u focusguard -f` durante um boot: sem erros, `NOTIFY_SOCKET`
      ativo (WatchdogSec=30) — NRestarts=0 por minutos (watchdog alimentado).
- [x] `sudo ./install-linux.sh status` → systemctl status.
- [x] `sudo ./install-linux.sh uninstall`:
  - [x] serviço parado/desabilitado, unit removida;
  - [x] `/opt/focusguard` e symlinks removidos;
  - [x] autostart + atalho + ícone removidos;
  - [x] estado preservado em `/var/lib/focusguard/` (mensagem do script).

**Achados esperados (a confirmar):**

| Severidade | Área | Suspeita |
|---|---|---|
| ✅ | filelog | Binários **user-space** (tray, web) gravam `<nome>.log` **na pasta do executável** (`filelog.PathFor`) — `/opt/focusguard` é root:root 0755: o log do tray/web como usuário comum **falha ao abrir** | **Resolvido (achado 2) e validado no real**: fallback para `~/.local/state/focusguard/` (XDG state dir) — o web e o tray logam lá com `/opt` root-only; o daemon (root) segue logando ao lado do exe |
| ✅ | systemd unit | `WatchdogSec=30` + `NotifyAccess=main`: conferir que o daemon de fato envia `WATCHDOG=1` e que o systemd não derruba por timeout | **Validado no real**: processo recebe `NOTIFY_SOCKET=/run/systemd/notify` + `WATCHDOG_USEC=30000000`; serviço estável com **NRestarts=0** por minutos (sem timeout) — o daemon alimenta o watchdog (`getWatchdogSec()` + `watchdog.New`) |
| ℹ️ | `install-linux.sh` `tray_user` | O install/uninstall de artefatos do usuário (autostart, atalho, ícone) depende de `$SUDO_USER` (ou `logname`) para achar o home — sem `sudo` (ex.: `wsl -u root`), o script mira `root` e os artefatos do usuário não são removidos | **Confirmado no real (não é bug no fluxo sudo)**: com `sudo` o `$SUDO_USER` é setado e tudo funciona; registrar como nota operacional (rodar sempre via `sudo`, nunca direto como root) |

**Critérios de saída:** install/uninstall limpos, serviço estável, CLI sem
sudo funcionando, achados de filelog resolvidos (fix + TDD se preciso).

---

## Etapa 3 — Enforcer real: hosts + iptables/ip6tables (root)

**Objetivo:** exercitar `enforcer_linux.go` contra o kernel real (hoje só há
testes com `execCommand` mockado).

- [ ] `focusguard block example.com 30m`:
  - [ ] `/etc/hosts` ganha `127.0.0.1 example.com` / `::1 example.com` com
        `# FOCUSGUARD: example.com` (verificação com `grep`);
  - [ ] `iptables -S OUTPUT` mostra `REJECT --reject-with tcp-reset -d <ip>`
        (e `ip6tables` idem, se IPv6 ativo);
  - [ ] `curl -v http://example.com` falha com RST (bloqueio de verdade);
  - [ ] `ss -K` derrubou conexões ativas (best-effort).
- [ ] `focusguard block --internet` (pânico):
  - [ ] catch-all REJECT por família com o marcador (AllBlockMarker);
  - [ ] allowlist (`--allow google.com docs.google.com`): ACCEPT **antes** do
        catch-all, e `curl` nos permitidos funciona;
  - [ ] `focusguard unblock-all`/expiração: varredura por marcador limpa tudo,
        sem regras órfãs.
- [ ] `focusguard dns start` → regras DoH/DoT (`--dport 853` DROP por
      provider); `dns stop` remove.
- [ ] Reboot com bloqueio ativo → reconciliação no boot reaplica hosts+regras;
      regras órfãs de um crash são varridas.
- [ ] Rollback de batch: bloco de N domínios falha no firewall → hosts e
      regras parciais revertidos (já testado unitário; confirmar o caminho
      real).

**Achados esperados (a confirmar):**

| Severidade | Área | Suspeita |
|---|---|---|
| ? | iptables-nft | Ubuntu 22.04+ usa nftables no kernel; `iptables` geralmente existe como **iptables-nft** (compat). Conferir `iptables -V` e o comportamento do `-S`/`-D` com os marcadores — o sweep por replay de spec depende do output estável do `-S` |
| ? | ip6tables ausente | Distros sem IPv6: `availableDoTBins` já ignora; confirmar que hosts com entrada v6 e firewall v6 ausente não quebram o bloco |
| ? | `ss -K` | Requer iproute2 + privilégio (root ok); em `ss` antigo sem `-K`, best-effort já cobre |

**Critérios de saída:** todos os cenários acima verificados na máquina real,
achados resolvidos (fix + TDD quando bug real).

---

## Etapa 4 — Watchers + store + réplicas (root)

- [ ] **hostswatch:** com um bloco ativo, editar `/etc/hosts` à mão (remover a
      entrada) → revertido em segundos (fsnotify + SHA-256); `focusguard
      tamper-log` registra o evento.
- [ ] **statewatch:** editar `state.json` (remover bloco ativo) → revertido a
      partir da RAM.
- [ ] **Réplicas:** apagar/corromper `state.json` → `LoadAndHeal` restaura da
      réplica criptografada atrelada ao hardware (`/etc/machine-id`).
- [ ] **Self-write sem loop:** o daemon grava hosts/state → watcher NÃO reage
      (hash de self-write).

**Critérios de saída:** 4 cenários verdes; tamper-log com entradas reais.

---

## Etapa 5 — CA local + interceptor :80/:443 (root + navegador)

- [ ] `sudo focusguard ca-install`:
  - [ ] CA gerada em `/var/lib/focusguard/ca/` (P-256, 0600 na key);
  - [ ] `focusguard-ca.crt` em `/usr/local/share/ca-certificates/` e cópia
        instalada `/etc/ssl/certs/focusguard-ca.pem` (prova real do
        `update-ca-certificates`);
  - [ ] `focusguard doctor` → checagem da CA = **pass**.
- [ ] `focusguard interceptor-set on` (ou flag persistida):
  - [ ] listeners `127.0.0.1:80`, `[::1]:80`, `127.0.0.1:443`, `[::1]:443`
        ativos (`ss -tlnp`);
  - [ ] site HTTPS bloqueado abre a **página de bloqueio sem aviso** no
        Chromium; Firefox com `security.enterprise_roots.enabled`.
  - [ ] site HTTP bloqueado idem.
  - [ ] porta 80 ocupada (ex.: `nginx`/`apache`): listener degrada, daemon
        segue, bloqueio continua valendo.
- [ ] `sudo focusguard ca-uninstall` → removido do store
      (`update-ca-certificates --fresh`), doctor volta a reportar sem CA;
      `focusguard uninstall` também remove a CA.
- [ ] `focusguard doctor` sem root: WARN de elevação (não FAIL falso);
      exit codes coerentes.

**Achados esperados (a confirmar):**

| Severidade | Área | Suspeita |
|---|---|---|
| ? | `update-ca-certificates` | É **Debian-style** — alvo é Ubuntu/Debian, ok; **Fedora/RHEL (update-ca-trust) fica fora do escopo** e registrado como portabilidade futura |
| ? | `/etc/hosts` + CA | A página HTTPS abre só se o hosts aponta o domínio para o loopback E o listener TLS responde com leaf assinado pela CA — confirmar o fluxo real (o teste hermético já cobre o handshake) |

**Critérios de saída:** CA instalada e removida do store real; navegador abre
a página sem aviso; doctor coerente; achados resolvidos.

---

## Etapa 6 — DNS sinkhole + telemetria (edição Server, root)

> **Validado em 2026-08-13 num Ubuntu 26.04 real (WSL2 com systemd)**, daemon
> compilado para Linux rodando como root. Resumo:
> - ✅ **Conflito confirmado**: `dns start` → `bind: address already in use` no
>   `0.0.0.0:53` com o systemd-resolved segurando `127.0.0.53:53`/`127.0.0.54:53`
>   (a suspeita do plano estava certa).
> - ✅ **Nova mensagem de BindError validada**: o erro exibiu o hint Linux
>   (`systemd-resolved costuma segurar a porta — libere com: sudo systemctl
>   stop systemd-resolved`) em vez do comando Windows ICS (bug corrigido nos
>   riscos 1+2).
> - ⚠️ **Quirk do WSL**: mesmo com o resolved PARADO, o bind `0.0.0.0:53`
>   continua falhando porque o stub DNS do WSL2 segura `10.255.255.254:53`
>   (listener do host, sem PID no guest). Numa máquina Ubuntu real o resolved
>   é o único ocupante — o passo de liberação funciona. Por isso a
>   funcionalidade do sinkhole foi validada com o controller REAL no loopback
>   (`127.0.0.1:53`), que não conflita com o stub.
> - ✅ **Sinkhole funcional**: domínio bloqueado → `0.0.0.0`/`::`; domínio
>   livre → encaminhado ao upstream real (Cloudflare Security 1.1.1.2, IPs
>   públicos reais); `telemetry.jsonl` registrou a query bloqueada (domínio +
>   IP de origem + timestamp).
> - ✅ **Modo pânico (BlockAll)**: catch-all + allowlist nas duas famílias,
>   aceitos pelo backend nft do ip6tables.
> - 🐛 **Bug real corrigido (achado 3)**: `block youtube.com` falhava — ver
>   tabela de Achados (tipo REJECT ICMPv4 inválido no IPv6/nft).

- [x] **Conflito systemd-resolved:** confirmado — bind `0.0.0.0:53` falha com
      EADDRINUSE com o resolved ativo; passo de liberação (`systemctl stop
      systemd-resolved`) validado (no WSL o stub NAT do próprio WSL ainda
      segura a porta — quirk do WSL, não do Ubuntu real).
- [x] `focusguard dns start` (sinkhole real via loopback — ver nota acima):
  - [x] domínio bloqueado → `0.0.0.0`/`::`; domínio livre → resposta do
        upstream real (1.1.1.2);
  - [x] `telemetry.jsonl` registra as queries bloqueadas (domínio + IP de
        origem);
  - [ ] `--dport 853` DROP ativo (não exercitado no WSL — o DoH block é
        coberto por teste unitário; fica para a máquina real);
  - [ ] devices: política por IP de origem vence a global (não exercitado no
        WSL — coberto por teste; fica para a máquina real).
- [ ] Máquina cliente resolvendo pelo sinkhole (configurar o cliente para
      apontar para o servidor — fica para a máquina real com rede).
- [x] `focusguard dns stop`/restauração: resolved religado, portas originais,
      hosts/firewall limpos (validação terminou com o ambiente restaurado).

**Achados esperados (validados em 2026-08-13):**

| Severidade | Área | Suspeita | Status |
|---|---|---|---|
| ALTA | `0.0.0.0:53` vs `127.0.0.53:53` | Bind de `0.0.0.0:53` **conflita** com o socket do systemd-resolved | ✅ **Confirmado no real** (EADDRINUSE); hint Linux validado; passo de liberação documentado na mensagem |
| INFO | WSL2 | O stub DNS do WSL (`10.255.255.254:53`) também segura a porta 53 — bloqueia `0.0.0.0:53` mesmo com o resolved parado | ⚠️ Quirk do WSL2 (listener do host), não afeta Ubuntu real — registro para quem validar em WSL |
| ? | resolv.conf | Sem o cliente apontar para o sinkhole, o teste do navegador não passa | ⏳ passo documentado no checklist da máquina real, não bug |

**Critérios de saída:** sinkhole bloqueando de verdade com o resolved
liberado; telemetria funcionando — **cumpridos** (via loopback no WSL);
`--dport 853`/devices/cliente de rede ficam para a máquina real.

---

## Etapa 7 — Tray + notificações (sessão desktop)

- [ ] `focusguard-tray` (cgo, appindicator) sobe numa sessão desktop real:
  - [ ] ícone na bandeja, menu com Status / Bloco rápido / Categorias /
        Verificar atualização / Abrir painel / Sair;
  - [ ] bloco rápido de 4h aplica (hosts + iptables reais);
  - [ ] notificações `notify-send` nas transições do pomodoro e nova versão;
  - [ ] autostart XDG: tray aparece no próximo login.
- [ ] Dependências: `libayatana-appindicator3-1 libgtk-3-0` instaladas pelo
      `install-linux.sh` (apt); em distro sem apt, aviso documentado.

**Achados esperados (a confirmar):** log do tray em `/opt/focusguard` root-only
(ver Etapa 2); `notify-send` ausente em desktops minimalistas → best-effort já
silencioso.

**Critérios de saída:** tray funcional em sessão desktop; ações do menu
aplicam bloqueios reais.

---

## Etapa 8 — Update + smart recovery (E2E real)

- [ ] Release de teste (tag `vX.Y.Z` de um fork/ramo ou `goreleaser
      release --snapshot` com `--skip-publish`) → instalar a versão atual.
- [ ] `focusguard update` (ou bandeja → Verificar atualização):
  - [ ] baixa o tar.gz da release, gera `.bak.<timestamp>` dos 5 binários;
  - [ ] swap atômico, `update.inprogress` escrito/limpo, restart via
        `osExit(1)` → systemd sobe a versão nova (Restart=always);
  - [ ] versões iguais em todos os irmãos (`focusguard doctor` → versões ok);
  - [ ] `CleanupStale` no boot seguinte expira `.bak` com mais de 1h.
- [ ] **Smart recovery:** substituir `focusguard-daemon` por um binário que
      crash-loope → watchdog detecta e restaura o `.bak` dentro da janela.
- [ ] **Watchdog systemd real:** `kill -9` do daemon → `Restart=always` +
      `WATCHDOG=1` mantêm o serviço de pé; sem `NOTIFY_SOCKET` (rodando o
      daemon manualmente) nada quebra.

**Critérios de saída:** update completo aplicado e reversível; rollback do
watchdog comprovado.

---

## Etapa 9 — Clock guard (relógio real, root)

- [ ] `sudo systemctl stop systemd-timesyncd`; adiantar o relógio +24h;
      reiniciar o daemon com o NTP inalcançável (bloquear UDP 123 na saída) →
      lockdown preventivo all-internet (sentinela `clock-guard`, banner na
      tela Segurança).
- [ ] Corrigir o relógio + NTP válido → liberação automática (nunca toca um
      pânico do usuário).
- [ ] Cenário já coberto por teste E2E (`restart_e2e_test.go`); aqui é a prova
      com o wall clock real do SO.

**Critérios de saída:** lockdown preventivo aplicado e liberado no real.

---

## Etapa 10 — Web UI + CLI user-space (sem sudo)

- [ ] `focusguard-web` como usuário comum (grupo `focusguard`):
  - [ ] painel abre em `http://127.0.0.1:48902`, login `admin`/troca de
        senha, SSE em tempo real (bloquear → dashboard atualiza sem refresh);
  - [ ] proxy IPC autenticado; ações sem spec não passam (403 allowlist);
  - [ ] anti-DNS-rebinding (Host header) no `httpapi`;
  - [ ] `pkill -x focusguard-web` do stale na porta 48902 (reabrir painel).
- [ ] CLI completo sem sudo: `status`, `block`, `stats`, `doctor`, `tamper-log`,
      `achievements`, `report now` (gera HTML+JSON — caminho expandido com
      `~`), `presets`, `goal`, `pomodoro`, `dns-status`, `metrics`.
- [ ] Relatório semanal automático: `reports-config-set` + daemon ligado no
      minuto agendado gera o arquivo (worker real).

**Critérios de saída:** painel e CLI 100% funcionais como usuário comum.

---

## Etapa 11 — CI permanente (depois que o manual validar)

- [ ] Consolidar no `.github/workflows/test.yml`:
  - [ ] job `linux-full-suite` (Etapa 0) — suíte completa + daemon + `-race`
        ampliado;
  - [ ] job `windows-compile-check` (Etapa 0);
  - [ ] manter o teste de chown do socket como root (já existe);
  - [ ] se algum fix das Etapas 2–10 pedir binário/ambiente específico
        (ex.: teste do sinkhole com resolved liberado), avaliar job dedicado
        com `systemctl stop systemd-resolved`.
- [ ] Atualizar `docs/development.md` (Linux: como rodar a suíte, notas de
      sistema) e o AGENT.md (suíte do daemon no CI Linux, achados
      transversais).

**Critérios de saída:** CI verde com a validação Linux permanente; docs
atualizadas.

---

## Achados — tabela de acompanhamento

| # | Severidade | Área | Achado | Ação |
|---|---|---|---|---|
| ✅ 1 | WARN | dnsserver (Linux) | `bindHint` era Windows-only: no Linux, um bind :53 falho mostrava o comando do ICS do Windows (`sc config SharedAccess...`) — inútil/enganoso | **Corrigido (TDD)**: `platformBindHint()` por plataforma — Linux menciona `systemd-resolved` + comando de liberação (`bindhint_{windows,linux,other}.go` + testes por SO); a confirmação do conflito real segue na Etapa 6 |
| ✅ 2 | WARN | filelog (user-space) | No Linux o fallback do web apontava para `/var/lib/focusguard` (root-only) → log sumia de vez; o tray não tinha fallback nenhum (nem no Windows) | **Corrigido (TDD)**: novo `filelog.UserLogPath` (Linux → `$XDG_STATE_HOME/focusguard` / `~/.local/state/focusguard`; Windows → `%PROGRAMDATA%\FocusGuard`); web e tray caem nele quando o diretório de instalação não é gravável — testes por SO. **Validado na máquina real (WSL)**: `/opt/focusguard` root-only → web+tray logam em `~/.local/state/focusguard/` (dir criado, dono = usuário, 1ª linha explica); `XDG_STATE_HOME` respeitado; daemon (root) inalterado (log ao lado do exe) |
| ✅ 3 | ALTA | enforcer (Linux) | `block youtube.com` FALHAVA no Ubuntu 26.04 (WSL, iptables nft): `ip6tables-restore: unknown reject type "icmp-port-unreachable"` — os scripts IPv6 usavam o tipo REJECT ICMPv4; domínios com IPs IPv6 não bloqueavam de jeito nenhum (rollback do bloco) | **Corrigido (TDD)**: `icmpPortUnreachableType(mask)` — v6 usa `icmp6-port-unreachable` (buildRestoreScript, buildBlockAllScript, removeFirewallRule). Testes: `TestBuildRestoreScript_V6UsesICMPv6RejectType`, `TestBuildBlockAllScript_V6...`, `TestRemoveFirewallRule_V6...`, `TestAddFirewallRulesBatch_MixedFamilies` atualizado, `TestBuildRestoreScript/v6` atualizado. **Validado ao vivo**: `block youtube.com` ✔ com regras v6 (`icmp6-port-unreachable`) aceitas pelo nft; pânico + allowlist ✔ nas duas famílias |
| ✅ 4 | WARN | enforcer (teste) | `TestSyncLocked_SweepsOrphans` assertava "exactly 1 orphan -D" mas o próprio comentário e o código descrevem 4 invocações (1 remoção + 3 probes no-match) — bug do teste que nunca rodou (a suíte Linux nunca foi executada) | **Corrigido (teste)**: asserção casa com o comportamento real — 4 invocações, exatamente 1 com sucesso, sempre no órfão, nunca no DoH/ativo |
| ✅ 5 | MÉDIA | daemon (teste) | `TestIntegration_InterceptorSetOn_GeneratesCAAndSignsLeafs` falhava no Linux: o server IPC do teste nunca registrou o handler `block` ("Not supported action: block" → 404 na página). No Windows o pacote nem roda (manifest exige elevação), então o teste nunca tinha sido executado | **Corrigido (teste)**: o teste registra o DomainAction `block` com a MESMA wiring do composition root (blocks.New + preset store) |
| ✅ 6 | MÉDIA | daemon (teste) | `TestProbeDaemonAlive_NoDaemon`/`_DaemonResponds` usavam `ipc.TestDialAddr` (TCP) — o hook existe só no Windows; no Linux o cliente diala o **unix socket**, então o probe nunca falava com o listener do teste | **Corrigido (teste)**: helper `testProbeEndpoint` cross-platform (unix socket em temp dir no Linux, TCP no Windows) |
| ✅ 7 | MÉDIA | doctor (Linux) | `queryService` só reconhecia "not found" como serviço ausente; a saída real do systemd é "Unit ... could not be **found**" (exit 4) → teste simulando unit ausente virava "instalado, mas parado" (fail errado) | **Corrigido (código)**: check por "could not be found"/"not found" (exit 4 → `serviceMissing`); exit 3/inactive/failed → `serviceInstalled`; outras falhas de execução → erro (doctor degrada para warn). Testes ajustados por plataforma |
| ✅ 8 | MÉDIA | logging (testes) | `TestLogPathFor_NextToExecutable`/`_WindowsInstallDir`/`TestPathFor_NextToExecutable` (daemon, tray, watchdog, web, filelog) passavam caminho Windows (`\`) para `filepath.Dir`/`Base` — no Linux `\` não é separador (nome puro) → asserção quebrada | **Corrigido (teste)**: skip fora do Windows (o caso Linux é coberto por `TestLogPathFor_LinuxInstallDir` etc.) |
| ✅ 9 | MÉDIA | autostart (teste) | `TestInstallDir_*` e `TestCreateDesktopShortcut_CallsPowerShell` idem — expectativa/mocks com caminho Windows; no Linux o mock de `osStat` nunca casava com o daemon → extração de ícone pulada (1 comando PowerShell em vez de 2) | **Corrigido (teste)**: expectativas e mocks com `filepath.Join` do SO atual |
| ✅ 10 | MÉDIA | update (código) | `CleanupStale` escolhia o .bak mais novo por **ModTime** — backups criados em rajada (um por binário) têm ModTime idêntico no Linux → vencedor não-determinístico (o teste pegou o backup mais velho) | **Corrigido (código)**: comparação pelo timestamp no **nome** (`backupTime()`, 14 dígitos — ordem lexicográfica = cronológica); `newerFile` removido |
| ✅ 11 | MÉDIA | update (teste) | `TestIncludesBinary_TrayDecision` idem (`filepath.Base` com `\` no Linux) | **Corrigido (teste)**: caminhos com `filepath.Join` do SO atual + subteste Linux (tray sem .exe casa por base name) + teste Windows-only de case-insensitivity |
| ℹ️ 12 | INFO | statewatch (teste) | `TestWatchFsEvents_ExternalChangeAfterSelfWrite_Detected` falhou 1x na suíte completa (race de timing: eventos do bootstrap do reconciler chegando depois do baseline) | Sem mudança: 5/5 passando isolado e na suíte completa subsequente; monitorar no CI (`-race`) |
| ✅ 13 | MÉDIA | tray (teste) | `-race ./...` revelou DATA RACE em 3 testes do `internal/system/tray`: `populatePresetMenu` (goroutine de background) escrevia `title`/`children` dos `mockMenuItem` enquanto o teste lia sem lock — mocks não thread-safe (o race só aparece sob `-race`, que o CI agora roda em `./...`) | **Corrigido (teste)**: mutex em todos os campos mutáveis dos mocks + accessors sincronizados (`getTitle`/`getTooltip`/`childrenCount`/`child`/`iconCallCount`/`itemByTitle`). `go test -race ./...` 100% verde no Linux |

**Suspeitas iniciais (status em 2026-08-13):**

| Área | Suspeita | Status |
|---|---|---|
| Porta 53 | Bind `0.0.0.0:53` vs systemd-resolved (`127.0.0.53:53`) — provável EADDRINUSE no Ubuntu | ✅ **Confirmado no real (Etapa 6 executada)**: EADDRINUSE + hint validado; no WSL o stub NAT do próprio WSL também segura :53 (quirk) |
| filelog user-space | Tray/web escrevem `.log` em `/opt/focusguard` (root-only) — falha silenciosa ou log perdido | ✅ resolvido (achado 2) e **validado na máquina real** (WSL): web+tray caem em `~/.local/state/focusguard/` com `/opt` root-only; `XDG_STATE_HOME` respeitado; daemon (root) segue logando ao lado do exe |
| iptables-nft | Ubuntu 22.04+: `iptables` pode ser o wrapper nft — validar `-S`/`-D` com marcadores | ⏳ Etapa 3 |
| CA cross-distro | `update-ca-certificates` é Debian-style — Fedora (`update-ca-trust`) fora do escopo desta rodada | ⏳ Etapa 5 (escopo Ubuntu/Debian) |
| `main_test.go:1742` | Teste do daemon que escreve em `/var/lib/focusguard` — precisa de dir hermético antes de rodar como root no CI | ⏳ runner do CI é não-root (Skip ativo) — virar hermético só se rodar a suíte como root |
| Testes do enforcer com root | Alguns testes Linux fazem Skip sem root; **como root no CI mexeriam no firewall real do runner** — revisar quais usam exec mockado | ⏳ CI roda como não-root (Skips ativos) — revisar se um dia rodar como root |

---

## Regras transversais

- **TDD** — achado que é bug real → teste que falha primeiro → fix → verde
  (padrão do bug-hunt e do verification-plan). Nenhum comportamento de
  bloqueio/produto muda sem teste.
- **Best-effort** — falha de SO (firewall, notificação, autostart) nunca
  derruba o daemon; achados nessa linha viram WARN/INFO, não mudança de
  arquitetura.
- **`_linux.go`/`_windows.go`** — qualquer fix de plataforma segue a convenção
  de arquivos por SO com interface no base.
- **Session-log** — cada sessão de trabalho deste plano escreve/atualiza
  `docs/session-log/YYYY-MM-DD.md` (`make session-check` no fim).
- **Achados na tabela** — preencher a tabela acima a cada etapa; WARNINGs de
  robustez entram com proposta de correção, INFOs ficam registradas.
- **Escopo distro** — Ubuntu/Debian é o alvo; variações (Fedora/RHEL,
  Arch, distros sem systemd) são portabilidade futura, registradas apenas.

---

## ✅ Checklist final (Definition of Done do plano)

- [x] **Etapa 0** — Suíte completa + `-race` completo + cross-compile Windows verdes no CI (run 31710267829, 4 jobs ✅ na primeira execução; achados 1–13 da tabela corrigidos).
- [ ] **Etapa 1** — Pacote do daemon verde no CI Linux; AGENT.md atualizado.
- [x] **Etapa 2** — `install-linux.sh` install/uninstall/status limpos em máquina real (WSL2/Ubuntu, 2026-08-13); achado de filelog já resolvido (achado 2).
- [ ] **Etapa 3** — Enforcer real: hosts + iptables/ip6tables + pânico + allowlist + DoH + rollback verificados.
- [ ] **Etapa 4** — Watchers (hosts/state) + réplicas + self-write verificados.
- [ ] **Etapa 5** — CA no trust store real + interceptor HTTPS sem aviso no navegador + uninstall.
- [ ] **Etapa 6** — Sinkhole real (:53 liberado do resolved) + telemetria + devices.
- [ ] **Etapa 7** — Tray + notificações em sessão desktop.
- [ ] **Etapa 8** — Update E2E (tar.gz, .bak, swap, restart) + smart recovery + watchdog systemd.
- [ ] **Etapa 9** — Clock guard com relógio real.
- [ ] **Etapa 10** — Web UI + CLI 100% funcionais como usuário comum.
- [ ] **Etapa 11** — CI permanente consolidado + docs (development.md/AGENT.md) atualizadas.
- [ ] Achados da tabela resolvidos (ou WARN/INFO justificados).
- [ ] Release Linux (tar.gz) publicada e testada a partir dela (README promete `install-linux.sh`).
