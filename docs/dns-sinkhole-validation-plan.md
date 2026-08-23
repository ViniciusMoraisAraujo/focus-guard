# Plano de Validação — DNS Sinkhole (como ter CERTEZA que funciona)

> Documento de planejamento (14/08/2026). Objetivo: transformar "acho que
> funciona" em **evidência**, em camadas — do código ao celular na rede.
> Complementa `docs/dns-sinkhole-spec.md` (especificação) e
> `docs/dns-sinkhole-lan-fix-plan.md` (fix de binding/firewall/IPv6, já
> implementado). Cobre o que já está provado, o que falta provar e a ordem
> exata de execução com comandos e saídas esperadas.

---

## 1. Estado atual (o que já está PROVADO vs. o que falta)

A implementação está feita e testada; **o que falta é a validação no real
(processo + rede)**, que nesta máquina de dev está bloqueada por 2 condições
ambientais (§4.1).

| Camada | O que está provado | Evidência |
|---|---|---|
| **L0 — Código (unit/integração)** | Bind dual-stack `0.0.0.0:53` + `[::]:53` (UDP+TCP, `IPV6_V6ONLY`, best-effort por família); upstream com fallback UDP→TCP; sinkhole `0.0.0.0`/`::` TTL 60; firewall inbound `FocusGuard_DNS_Inbound_UDP/TCP` (args + idempotência); flush de cache; wiring boot/`dns-start`; logs e contadores | `go test ./...` ✅ (Windows); suíte do daemon + dnsserver + enforcer com `-race` no WSL ✅ |
| **L1 — Processo/socket** | `focusguard dns status` responde; contadores/telemetria da tela Rede | ❌ **não provado no real** (ver §4) |
| **L2 — Local** | `nslookup google.com 127.0.0.1` responde | ❌ **não provado**: hoje a porta 53 está presa pelo ICS e o daemon instalado é o binário ANTIGO |
| **L3 — Rede (celular/TV)** | Dispositivo resolve e é bloqueado via FocusGuard | ❌ **não provado** — depende de L1+L2 + roteador (§4.3) |
| **L4 — Operação contínua** | Watchdog/SCM ressuscitam em ~1s; logs de erro; contadores periódicos | Parcial: config validada (SCM `restart/1000`, systemd `RestartSec=1`); comportamento em falha real não exercitado |

**Conclusão honesta:** o código está sólido; "ter certeza que funciona" =
executar o runbook abaixo e registrar cada passo. O único obstáculo real é
ambiental e tem correção conhecida (§4.1).

---

## 2. Pirâmide de validação (do barato ao caro)

### L0 — Código (já verde, vira regressão a cada release)

```bash
go build ./... && go vet ./...
go test $(go list ./... | grep -v 'cmd/focusguard-daemon')   # Windows
go test -race ./cmd/focusguard-daemon ./internal/infrastructure/dnsserver ./internal/infrastructure/enforcer   # Linux/WSL
cd focusguard-ui && npm run typecheck && npm run test && npm run build
bash scripts/check-session-log.sh --today && go run ./scripts/gen-contract/main.go --check
```

Critério: tudo ✅ antes de tocar em rede.

### L1 — Processo e socket (a máquina escuta?)

```bash
focusguard dns status          # esperado: "Estado: Ativo (ouvindo em 0.0.0.0:53, [::]:53)" + "Upstream: 1.1.1.2:53"
netstat -ano | findstr :53     # esperado: UDP e TCP em 0.0.0.0:53 E [::]:53 (PID do focusguard-daemon)
netsh advfirewall firewall show rule name=FocusGuard_DNS_Inbound_UDP   # + TCP
```

### L2 — Local (a máquina resolve?)

```bash
nslookup google.com 127.0.0.1            # responde com IPs reais (via upstream)
nslookup <dominio-bloqueado> 127.0.0.1   # responde 0.0.0.0 (nunca erro)
powershell Resolve-DnsName -Server 127.0.0.1 google.com
```

### L3 — Rede (a casa resolve?)

1. Celular: esqueça e reconecte o Wi-Fi (ou DNS manual = IP LAN do PC).
2. `nslookup google.com <IP-do-PC>` no celular → IPs reais (via sinkhole).
3. `nslookup <dominio-bloqueado> <IP-do-PC>` → `0.0.0.0`.
4. **Failover**: desligue o daemon → celular continua navegando pelo DNS
   secundário público (§4.3) em ≤ TTL (~60s).

### L4 — Operação contínua

- Contadores e "Atividade bloqueada" da tela Rede sobem com tráfego real.
- Log do daemon: linha `atividade no intervalo` a cada min com tráfego;
  falha de upstream loga domínio + cliente + upstream.
- Kill o daemon (taskkill) → SCM ressuscita em ~1s (validar com
  `Get-Process focusguard-daemon` antes/depois).

---

## 3. Critérios de aceite (DoD) — checklist final

- [ ] `focusguard dns status` → `Estado: Ativo (ouvindo em 0.0.0.0:53, [::]:53)` e `Upstream: 1.1.1.2:53`
- [ ] `netstat -ano | findstr :53` → UDP+TCP em `0.0.0.0:53` e `[::]:53` (PID do daemon)
- [ ] `netsh advfirewall firewall show rule name=FocusGuard_DNS_Inbound_{UDP,TCP}` → 2 regras `dir=in allow`
- [ ] `nslookup google.com 127.0.0.1` → IPs reais
- [ ] `nslookup <bloqueado> 127.0.0.1` → `0.0.0.0` (sem SERVFAIL)
- [ ] Celular na rede com DNS = IP do PC: liberado resolve, bloqueado → `0.0.0.0`
- [ ] PC desligado → rede navega pelo DNS secundário (failover)
- [ ] `taskkill /f /im focusguard-daemon.exe` → processo volta em ~1s (SCM)

---

## 4. Pré-requisitos do ambiente (sem isto, o DoD falha por motivo errado)

### 4.1 Porta 53 livre (o bloqueio real desta máquina)

Hoje: `netstat` mostra `UDP 0.0.0.0:53` presa pelo **svchost PID 4284 =
SharedAccess (ICS)**, e há o quirk do **WSL2/HNS** (o stub DNS do WSL segura
`0.0.0.0:53` no host). Correção (terminal ELEVADO):

```powershell
sc config SharedAccess start= disabled & net stop SharedAccess
wsl --shutdown          # libera o stub do WSL/HNS até o próximo wsl start
netstat -ano | findstr :53   # deve ficar vazio
```

Alternativa sem desativar o ICS (temporário): `net stop SharedAccess` e
reiniciar depois. Verifique também `dnscache`/Hyper-V/Docker se o erro for
`bind: acesso proibido` (o `bindHint` do `dns-bind-error` já orienta).

### 4.2 Daemon INSTALADO com o binário NOVO

O daemon em execução nesta máquina é **anterior ao fix** (sem `[::]:53`, sem
regras inbound, sem `lan_ip`/`lan_mac`). Reinstale/atualize antes de validar:

```powershell
# reinstalar o serviço com os binários da release (ou focusguard update)
focusguard install          # eleva e registra o serviço
focusguard dns start
```

### 4.3 Roteador (config de ambiente — o usuário só faz isto uma vez)

1. **Reserva DHCP**: MAC do PC → IP fixo (ex.: `192.168.1.100`).
2. **DNS primário do DHCP** → IP fixo do PC (o FocusGuard).
3. **DNS secundário** → `1.1.1.2` (o mesmo upstream do sinkhole) — failover quando o PC cair mantém o filtro de segurança Cloudflare.
4. **IPv6/RDNSS**: desligar o anúncio de DNS IPv6 do roteador (senão os
   aparelhos preferem o `fe80::1` e burlam o sinkhole) ou apontá-lo para o
   IPv6 da máquina.
5. Reconectar os dispositivos (`ipconfig /renew` no PC; Wi-Fi off/on nos
   celulares).

Guias por fabricante (ZTE, TP-Link, Huawei, Intelbras, D-Link, Asus): tela
**Guia** do painel (também mostra o IP/MAC da máquina, com copiar).

### 4.4 Windows: perfil de rede **Privada** + regras inbound automáticas

- Perfil Público trata a conexão como não confiável e pode bloquear o
  inbound mesmo com a regra: `ncpa.cpl` → rede → **Rede privada**.
- As regras `FocusGuard_DNS_Inbound_UDP/TCP` são criadas no boot e no
  `dns-start` (binário novo, §4.2) — conferir com o comando do L1.

---

## 5. Diagnóstico rápido (sintoma → causa → ação)

| Sintoma | Causa provável | Ação |
|---|---|---|
| `dns status` → `Estado: Desativado` | Flag persistido `dns_enabled=false` | `focusguard dns start` |
| Porta 53 "em uso" no status | ICS/WSL2-HNS/`dnscache`/Hyper-V segurando :53 | §4.1 (elevado) |
| `nslookup 127.0.0.1` não responde | Porta presa ou daemon não escuta (binário antigo) | §4.1 + §4.2 |
| Regras inbound ausentes | Daemon instalado é o binário antigo | Reinstalar (§4.2), `dns start` |
| Celular sem internet no Wi-Fi | Perfil Público / roteador sem DNS apontado / upstream fora | §4.4 + §4.3; conferir log de upstream |
| Bloqueio demora a pegar no celular | Cache/TTL do SO do dispositivo | TTL 60s; reconectar o Wi-Fi |
| Navegador fura o bloqueio | DoH/DoT/QUIC do navegador | Regras DoH/DoT/QUIC do enforcer (já existem) + bloquear por IP nos browsers |
| Aparelho ignora o IPv4 e usa `fe80::1` | RDNSS/DHCPv6 do roteador se anunciando | Desligar RDNSS no roteador (§4.3.4) |
| `netstat` mostra `[::]:53` ausente | Máquina sem IPv6 (ou binário antigo) | Normal se IPv6 desabilitado (best-effort v4); atualizar binário |

---

## 6. Ordem de execução nesta máquina (runbook)

> Requer shell **elevado** nos passos 1–3 e acesso ao painel do roteador ZTE
> no passo 6. Registrar a saída de cada passo no session-log.

1. **Preparar ambiente**: §4.1 (parar ICS + `wsl --shutdown`) → porta 53 vazia.
2. **Atualizar binários**: reinstalar daemon novo (§4.2); confirmar versão no
   `focusguard dns status` / tela Configurações.
3. **Subir o sinkhole**: `focusguard dns start` → `dns status` (L1).
4. **Validar local (L2)**: nslookups liberado/bloqueado; confirmar regras
   inbound; perfil Privada (§4.4).
5. **Validar roteador (L3 parte 1)**: reserva DHCP + DNS primário → IP do PC +
   RDNSS off (§4.3); reconectar dispositivos.
6. **Validar rede (L3 parte 2)**: nslookup do celular (liberado + bloqueado);
   failover com daemon desligado.
7. **Validar operação (L4)**: contadores na tela Rede, log periódico, kill →
   ressuscitação ~1s.
8. **Fechar**: checklist §3 todo ✅; registrar no session-log do dia.

---

## 7. Fora de escopo / riscos

- **Não** é escopo automatizar a config do roteador (é ambiente).
- **Não** validar com o celular da operadora em rede móvel (precisa estar no
  mesmo Wi-Fi).
- Risco conhecido: celular com DoH nativo (Chrome/Firefox) pode ignorar o DNS
  da rede — cobrir no passo 6 (testar também pelo navegador).
- Esta máquina de dev tem o WSL2 ativo: o `wsl --shutdown` libera a porta,
  mas qualquer `wsl` que suba depois re-ocupa — repetir §4.1 se o bind falhar
  de novo.
