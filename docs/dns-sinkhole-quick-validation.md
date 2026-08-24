# Validação Rápida — DNS Sinkhole

> **Objetivo**: ter certeza que o sinkhole funciona antes de colocar o IP no DNS do modem.
> Tempo estimado: 15–20 minutos (máquina) + 10 minutos (rede/celular).

---

## Pré-requisitos

```powershell
# 1. Porta 53 livre (ELEVADO)
sc config SharedAccess start= disabled
net stop SharedAccess
wsl --shutdown                    # libera stub WSL/HNS
netstat -ano | findstr :53        # deve mostrar nada

# 2. Daemon atualizado
go build -o focusguard-daemon.exe ./cmd/focusguard-daemon/
go build -o focusguard.exe ./cmd/focusguard/
focusguard install                # registra serviço com binário novo
focusguard dns start
```

---

## Passo 1 — O daemon escuta? (L1)

```powershell
focusguard dns status
# Esperado: "Estado: Ativo (ouvindo em 0.0.0.0:53, [::]:53)"
#           "Upstream: 1.1.1.2:53"

netstat -ano | findstr :53
# Esperado: UDP e TCP em 0.0.0.0:53 E [::]:53 (PID do daemon)

netsh advfirewall firewall show rule name=FocusGuard_DNS_Inbound_UDP
netsh advfirewall firewall show rule name=FocusGuard_DNS_Inbound_TCP
# Esperado: 2 regras dir=in action=allow
```

✅ **Critério**: porta 53 ocupada pelo daemon + 2 regras inbound.

---

## Passo 2 — Resolve localmente? (L2)

```powershell
nslookup google.com 127.0.0.1
# Esperado: resposta com IPs reais (ex: 142.250.x.x)

nslookup instagram.com 127.0.0.1
# Esperado: 0.0.0.0 (se instagram.com estiver bloqueado no scheduler)

nslookup teste-bloqueio.local 127.0.0.1
# Esperado: 0.0.0.0 (domínio inexistente → sinkhole responde morto)
```

✅ **Critério**: domínios liberados → IPs reais; domínios bloqueados → `0.0.0.0`.

---

## Passo 3 — Rede local (L3)

```powershell
# Descobrir IP da máquina na LAN
ipconfig | findstr "IPv4"
# Ex: 192.168.1.100
```

### No celular (Wi-Fi):

1. Configurações → Wi-Fi → rede conectada → **DNS manual**
2. Colocar `192.168.1.100` (IP do PC)
3. Reconectar o Wi-Fi

```bash
# No celular (termux ou browser):
nslookup google.com 192.168.1.100
# Esperado: IPs reais

nslookup instagram.com 192.168.1.100
# Esperado: 0.0.0.0 (bloqueado)
```

✅ **Critério**: celular resolve via FocusGuard e bloqueia domínios da lista.

---

## Passo 4 — Failover (L3b)

```powershell
# Desligar o daemon temporariamente
taskkill /f /im focusguard-daemon.exe
```

No celular (DNS ainda apontando para o PC):

```
nslookup google.com 192.168.1.100
# Esperado: timeout ou SERVFAIL → celular cai no DNS secundário do roteador
# e continua navegando (failover).
```

✅ **Critério**: rede não para quando o PC cai.

---

## Passo 5 — Operação contínua (L4)

```powershell
# Reiniciar o daemon
focusguard dns start
```

1. Navegar no celular por ~2 minutos
2. Verificar log:
   ```
   [FocusGuard DNS] atividade no intervalo: X queries (Y bloqueadas) · total ...
   ```
3. Verificar tela **Rede** no painel → contadores sobem
4. Kill rápido:
   ```powershell
   taskkill /f /im focusguard-daemon.exe
   Get-Process focusguard-daemon   # deve reaparecer em ~1s (SCM)
   ```

✅ **Critério**: contadores sobem com tráfego real + daemon ressuscita.

---

## Troubleshooting

| Sintoma | Causa | Ação |
|---|---|---|
| `dns status` → Desativado | Flag não persistido | `focusguard dns start` |
| Porta 53 "em uso" | ICS/WSL2/Docker segurando | §Pré-requisitos (elevado) |
| Celular não resolve | Firewall inbound fechado | Verificar regras inbound + perfil Rede **Privada** |
| `nslookup` local ok, celular não | Roteador não aponta DNS | Configurar DHCP no modem (DNS primário → IP do PC) |
| Bloqueio demora no celular | Cache DNS do SO | TTL 60s; reconectar Wi-Fi |

---

## Checklists finais

- [ ] `focusguard dns status` → Ativo (0.0.0.0:53, [::]:53)
- [ ] `netstat` → UDP+TCP em ambas famílias
- [ ] 2 regras inbound no firewall
- [ ] `nslookup` local: liberado → IPs; bloqueado → 0.0.0.0
- [ ] Celular: mesmo resultado via IP do PC
- [ ] Failover: PC desligado → celular continua (DNS secundário)
- [ ] Kill → daemon volta em ~1s
- [ ] Log de atividade aparece com tráfego real

**Quando tudo verde**: colocar o IP do PC no DNS primário do modem e fechar.
