# Plano de Correção — Escuta do DNS Sinkhole na Rede (Binding, Firewall, IPv6)

> Documento de planejamento (pré-implementação). Cobre a Task "Redes: Corrigir
> Escuta do DNS Sinkhole (Binding, Firewall e IPv6 Bypass)" com o diagnóstico
> real do código atual, as mudanças por arquivo, os testes e os cenários de
> falha. Tudo referenciado ao estado atual do repo (14/08/2026).

---

## 1. Diagnóstico (o que já está pronto vs. o que realmente falta)

O ponto mais importante: **parte da Task já está implementada no código atual**.
Planejar sobre premissas erradas geraria trabalho inútil. Estado verificado:

| Premissa da Task | Realidade no código | Veredito |
|---|---|---|
| "O servidor DNS provavelmente escuta só em `127.0.0.1`" | `DefaultBindAddr = "0.0.0.0:53"` (`internal/infrastructure/dnsserver/server.go:28`) e `bindBoth` usa `net.ListenPacket("udp", …)` + `net.Listen("tcp", …)` | **Já corrigido** (IPv4 todas as interfaces) |
| "Escutar em `0.0.0.0:53` **e** `[::]:53`" | Só há listener IPv4 wildcard; nenhum socket IPv6 é aberto | **Falta** (única lacuna real no binding) |
| "Firewall deve abrir a porta 53 inbound (UDP/TCP)" | `enforcer_windows.go` só gerencia regras `dir=out` (bloqueios, DoH/DoT, BlockAll/allow). Nenhuma regra inbound | **Falta** — e é a causa mais provável dos timeouts de celulares/TVs |
| "Se domínio não bloqueado, repassar ao upstream" | `forward()` (`server.go:346`) encaminha ao Cloudflare `1.1.1.2:53` com fallback UDP→TCP em truncation | **Já implementado** (Bug 2 da Task não existe no código atual) |
| "`nslookup google.com 127.0.0.1` deve responder" | Bind em `0.0.0.0` cobre loopback | **Deve funcionar hoje** — vira passo de verificação/regressão |
| "Windows prefere o roteador via IPv6 `fe80::1`" | É comportamento do **cliente/roteador** (RDNSS/DHCPv6 do ZTE), não do servidor. O servidor só pode ajudar escutando em `[::]:53` para quem apontar para o IP IPv6 da máquina | **Mitigação parcial** via código + **resolução real** na config do roteador (ver §5) |

Conclusão: as mudanças de código necessárias são **2** (dual-stack no bind e
regra inbound de firewall) + **1** mudança de documentação/operação (IPv6
bypass). O restante da Task é verificação.

---

## 2. Critérios de Aceite (DoD) → status após o fix

- [ ] **Servidor escuta em `0.0.0.0:53` e `[::]:53`** → implementação §3.1/§3.2
- [ ] **Firewall abre 53 (UDP/TCP) inbound** → implementação §3.3/§3.4/§3.5
- [ ] **Dispositivo externo resolve via FocusGuard** → depende de binding +
      firewall + roteador apontando o DNS + perfil de rede Privada (§5)
- [ ] **`nslookup google.com 127.0.0.1` responde localmente** → já funciona;
      vira regressão (§4.2)

---

## 3. Mudanças por arquivo

### 3.1 `internal/infrastructure/dnsserver/server.go` — bind dual-stack

Objetivo: além do socket IPv4 wildcard, abrir o par IPv6 (`[::]:53`), UDP e
TCP, compartilhando o mesmo `dns.ServeMux`.

- Novo const: `DefaultBindAddrV6 = "[::]:53"` (ao lado de `DefaultBindAddr`).
- **Refatorar o estado do `Server`**: `s.udp *dns.Server` / `s.tcp *dns.Server` /
  `s.addr string` viram slices (`s.udps []*dns.Server`, `s.tcps []*dns.Server`,
  `s.addrs []string`) — `isRunning` e `Stop` passam a iterar as slices. A
  assinatura pública (`Start`, `Addr`, `Stop`) não muda.
- **Derivação do par**: em `Start(addr)`, se o host for wildcard IPv4
  (`0.0.0.0`, o default), derivar também `[::]:<mesma porta>` e bindar os dois.
  Endereços concretos (ex.: `127.0.0.1:0` dos testes) bindam só a família
  pedida — testes existentes continuam determinísticos e sem exigir IPv6.
- **`IPV6_V6ONLY=1` no socket v6**: com wildcards das duas famílias, o default
  do Go (`ipv6only=false`) faz o `[::]` capturar IPv4-mapped e o segundo bind
  falhar com EADDRINUSE no Linux. Usar `net.ListenConfig{Control: …}` setando
  `syscall.IPV6_V6ONLY=1` (disponível em windows e unix) quando o endereço for
  `[::]`, via `net.ListenConfig.ListenPacket/Listen`. Refatorar `bindBoth`
  para aceitar o `ListenConfig` (manter a função original para os testes que
  a chamam direto com `127.0.0.1:0`).
- **Best-effort por família** (filosofia do projeto: porta ocupada não derruba
  o daemon):
  - v4 ok, v6 falhou (máquina sem IPv6 / IPv6 desabilitado) → serve só v4, loga
    aviso. **Sem erro.**
  - v4 falhou, v6 ok → serve só v6, loga aviso; `Addr()` mostra só o v6.
  - ambas falharam → erro combinado (mantém o `bindHint` atual).
- **`Addr()`** passa a retornar a união, ex.: `"0.0.0.0:53, [::]:53"` (ordem
  estável, v4 primeiro). `Start` retorna sucesso se **ao menos uma** família
  bindou — o status reflete o que realmente está escutando.

### 3.2 `internal/infrastructure/dnsserver/controller.go` — expor o par

- `NewController` mantém a assinatura; a derivação do twin v6 fica no `Server`.
- `Status.Addr` / `c.addr` passam a carregar a string união do `Server.Addr()`.
- Nada muda no contrato IPC/CLI (o campo já é string livre).

### 3.3 `internal/infrastructure/enforcer/enforcer_windows.go` — regra inbound :53

Novo método **`AllowDNSInbound() error`** (idempotente), seguindo as
convenções existentes de DoH (`BlockDoH`):

- Regras (nomes estáveis, prefixo `FocusGuard_` para o inventário existente
  continuar contando):
  - `FocusGuard_DNS_Inbound_UDP` → `netsh advfirewall firewall add rule
    name=FocusGuard_DNS_Inbound_UDP dir=in action=allow protocol=udp localport=53`
  - `FocusGuard_DNS_Inbound_TCP` → idem com `protocol=tcp`
- Implementação: helper `addDNSPortRuleArgs(name, protocol)` testável (mesmo
  padrão do `addDoTRuleArgs`), execução via `execCommandContext` com timeout,
  **skip se a regra já existe** (reusa `existingFocusGuardRules()`), e
  `invalidateRuleCache()` + `invalidateStatusCache()` após aplicar.
- Sem `profile=` → a regra vale para todos os perfis (Público/Privado/Domain);
  mesmo assim a orientação de perfil Privado permanece (§5) — rede Pública pode
  ter outras restrições de descoberta.
- **Decisão: não remover no `dns-stop`.** Se um celular ainda aponta para a
  máquina, remover a regra derruba o DNS dele silenciosamente — pior que uma
  regra inerte. Regra é permissiva e só habilita o que o daemon já escuta.

### 3.4 `internal/infrastructure/enforcer/enforcer_linux.go` — no-op documentado

- `AllowDNSInbound()` retorna `nil` (host firewalls típicos aceitam INPUT por
  padrão e o daemon já bindou em `0.0.0.0`/`[::]`). Comentário explicando que
  abrir INPUT em `iptables`/`nftables` fica fora de escopo (Task é Windows).

### 3.5 `cmd/focusguard-daemon/main.go` — wiring no boot e no `dns-start`

- **Evitar tocar a interface `Enforcer`** (há ~15 fakes de teste em
  scheduler/ipc/hostswatch/clockguard que quebrariam): o daemon faz
  type-assert de uma interface local opcional
  `interface{ AllowDNSInbound() error }` sobre o `enf` concreto.
- Helper `ensureDNSListenerFirewall(enf)` (best-effort, loga em falha) chamado:
  1. **Boot**: logo após `dnsSrv.Start()` bem-sucedido quando
     `sched.DNSEnabled()` (bloco atual em `main.go:1105`).
  2. **`dns-start` IPC**: estender o hook `onStarted` já passado ao
     `dns.NewStart` (`main.go:1312`, hoje só o `dohHook`) — compor um closure
     que roda `dohHook()` + `ensureDNSListenerFirewall(enf)`.
- `fakeDaemonEnforcer` em `main_test.go` ganha o método para o teste de wiring
  afirmar a chamada.

### 3.6 Documentação

- `README.md` (seção "Porta 53 em uso" / sinkhole): instruções de roteador ZTE
  (§5) + menção às regras inbound automáticas.
- `docs/dns-sinkhole-spec.md`: seção nova "Firewall inbound :53" e atualização
  do §4 (DoH) para citar as regras `FocusGuard_DNS_Inbound_*`.
- `CHANGELOG.md`: entrada do fix.

---

## 4. Testes

### 4.1 Unitários

- `dnsserver/server_test.go`:
  - `TestStart_DualStack` — `Start("0.0.0.0:0")`, assertar que `Addr()` contém
    as duas famílias e que uma query UDP A/AAAA via `[::1]:<porta-v6>`
    responde (upstream fake existente).
  - `TestStart_IPv6Unavailable_StillServesV4` — simular falha do bind v6
    (ex.: injetar um `ListenConfig` de teste que falha no v6) e assertar que o
    servidor sobe em v4 sem erro.
  - Testes existentes (`127.0.0.1:0`) seguem intactos (refatoração de slices
    coberta por eles).
- `dnsserver/controller_test.go`: `Status().Addr` com o par.
- `enforcer/enforcer_windows_test.go` (build tag windows):
  - `TestAddDNSPortRuleArgs` — args exatos do netsh (`dir=in action=allow
    protocol=udp|tcp localport=53`), mesmo estilo do `TestAddDoTRuleArgs_*`.
  - `TestAllowDNSInbound_Idempotent` — com `execCommandContext` stubado, 2ª
    chamada não re-adiciona (verifica skip via inventário).
- `cmd/focusguard-daemon/main_test.go`: `fakeDaemonEnforcer` registra a chamada
  de `AllowDNSInbound` no boot com `DNSEnabled` e no fluxo `dns-start`.

### 4.2 Manuais (DoD)

1. `go build ./... && go vet ./... && go test -race ./...` verdes.
2. `netstat -ano | findstr :53` → mostra `0.0.0.0:53` **e** `[::]:53` (UDP+TCP).
3. `netsh advfirewall firewall show rule name=FocusGuard_DNS_Inbound_UDP` (e TCP).
4. `nslookup google.com 127.0.0.1` → responde (regressão).
5. `nslookup google.com ::1` (ou `Resolve-DnsName -Server ::1 google.com`) →
   responde via v6 quando o host tem IPv6.
6. Celular desconectado/reconectado ao Wi-Fi com DNS manual = IP LAN do
   servidor → resolve domínio bloqueado para `0.0.0.0` e liberado via upstream.
7. Máquina **sem** IPv6 (desabilitado no adaptador): daemon sobe, `dns-status`
   mostra só o v4, sem erro no boot.

---

## 5. IPv6 bypass (`fe80::1`) — o que o código resolve e o que é config

O `fe80::1` é o **roteador ZTE se anunciando como DNS** via RDNSS (Router
Advertisement) ou DHCPv6 — o cliente nunca consulta o FocusGuard. Nenhum
listener no servidor muda isso. Resolução por camada:

1. **Roteador (fix definitivo)**: no ZTE, desligar o anúncio de DNS IPv6
   (RDNSS) ou apontar o DNS IPv6 da LAN para o endereço IPv6 (ULA/GUA) da
   máquina FocusGuard — assim os clientes que preferem v6 usam o sinkhole.
2. **Servidor (mitigação em código, §3.1)**: o listener `[::]:53` passa a
   atender quem apontar para o IPv6 da máquina.
3. **Ambiente de teste (workaround da própria Task)**: desabilitar IPv6 no
   adaptador (`ncpa.cpl` → desmarcar TCP/IPv6) + `ipconfig /flushdns` +
   `ipconfig /renew` — vale para validar o fluxo v4 imediatamente.
4. **Firewall Windows**: perfil de rede **Privada** + regras inbound §3.3.

---

## 6. Cenários de falha previstos (troubleshooting)

| Sintoma | Causa provável | Ação |
|---|---|---|
| `bind: Only one usage of each socket address…` | Porta 53 ocupada (ICS/`dnscache`, Hyper-V/Docker, stub do WSL) | `netstat -ano \| findstr :53`, parar o PID/serviço; hint do `bindHint` já orienta (`sc config SharedAccess start= disabled`) |
| Celular "sem conexão" ao usar o FocusGuard | Firewall inbound fechado (mais provável) ou upstream inalcançável | Validar regras §4.2.3; `focusguard dns-set-upstream 1.1.1.2`; testar `nslookup` de outro host |
| `nslookup` local ok, rede não | Perfil Público / regra inbound ausente / roteador não aponta o DNS | Perfil Privada; reexecutar `dns-start`; configurar DHCP do ZTE |
| Bind v6 falha na inicialização | IPv6 desabilitado na máquina | Esperado: servidor sobe só v4 com aviso no log (best-effort §3.1) |
| Dois wildcards conflitam no Linux | `[::]` sem `IPV6_V6ONLY` captura v4-mapped | Coberto pelo `ListenConfig.Control` (§3.1) — teste obrigatório no Linux |
| `FirewallRules` do status cresce +2 | Inventário `FocusGuard_*` passa a incluir as regras inbound | Cosmético e desejado; auditar fixtures de `countFocusGuardRules` |

---

## 7. Ordem de implementação (checklist)

1. `server.go` — refatorar slices + dual-stack + `ListenConfig`/V6ONLY + best-effort.
2. `server_test.go` / `controller_test.go` — dual-stack + família ausente.
3. `enforcer_windows.go` — `AllowDNSInbound` + helper de args; `enforcer_linux.go` no-op.
4. `enforcer_windows_test.go` — args + idempotência.
5. `main.go` — helper + wiring boot/`dns-start`; `main_test.go` fake.
6. `go test -race ./...` (todo o repo) + vet/build.
7. Documentação (README, spec, CHANGELOG) + verificação manual §4.2.
8. Validação no real: celular + roteador ZTE (etapa de QA, com workaround v6 §5.3).

---

## 8. Fora de escopo (decisões registradas)

- **Não** remover as regras inbound em `dns-stop` (ver §3.3).
- **Não** alterar a interface `Enforcer` (type-assert local no daemon).
- **Não** abrir INPUT no firewall Linux (no-op documentado).
- **Não** mexer no DHCP/roteador via código — é configuração do ambiente.
