# Resumo Executivo — Validação Linux

> **Consulta rápida** para executar à noite na Linux real.
> Duração estimada: 30–60 min (Etapa 7).

---

## Status Atual

| Etapa | O quê | Status |
|---|---|---|
| 0 | CI baseline (suíte completa + race + cross-compile) | ✅ |
| 1 | Daemon tests no CI Linux | ⏳ (CI confirma) |
| 2 | Install/uninstall (install-linux.sh) | ✅ WSL2 |
| 3 | Enforcer real (hosts + iptables) | ✅ WSL2 |
| 4 | Watchers + réplicas | ✅ WSL2 |
| 5 | CA + interceptor :80/:443 | ✅ WSL2 |
| 6 | DNS sinkhole + telemetria | ✅ WSL2 |
| **7** | **Tray + notificações** | **⬜ HOJE** |
| 8 | Update + smart recovery | ⬜ |
| 9 | Clock guard | ⬜ |
| 10 | Web UI + CLI user-space | ⬜ |

---

## Comandos Rápidos

### Setup (uma vez)
```bash
cd ~/focusguard
./scripts/setup-linux-vm.sh
logout  # ou: newgrp focusguard
```

### Validação Etapa 7
```bash
./scripts/validate-etapa7.sh
```

### Verificar se tudo está ok
```bash
systemctl status focusguard          # serviço ativo?
ls -la /run/focusguard.sock          # socket root:focusguard 0660?
focusguard status                    # CLI sem sudo funciona?
cat ~/.local/state/focusguard/tray.log  # log do tray existe?
```

---

## O que testar (Etapa 7)

| # | Teste | Comando/Ação | Esperado |
|---|---|---|---|
| 1 | Tray inicia | `focusguard-tray &` | Ícone na bandeja |
| 2 | Menu funcional | Clique direito no ícone | 6 opções aparecem |
| 3 | Bloco rápido | Menu → "Bloco rápido" | Notificação + bloco ativo |
| 4 | Notificações | `notify-send "Teste" "Msg"` | Popup visível |
| 5 | Autostart | `cat ~/.config/autostart/focusguard-tray.desktop` | Exec=/opt/focusguard/focusguard-tray |
| 6 | Desktop | `ls ~/Desktop/focusguard.desktop` | Arquivo existe |
| 7 | Log | `cat ~/.local/state/focusguard/tray.log` | Linhas de startup |
| 8 | CLI sem sudo | `focusguard status` | Funciona (grupo focusguard) |

---

## Troubleshooting

| Problema | Solução |
|---|---|
| Tray não inicia | `sudo apt install libayatana-appindicator3-1` |
| "Permission denied" no socket | `sudo usermod -aG focusguard $USER && newgrp focusguard` |
| Notificações não aparecem | `sudo apt install libnotify-bin` |
| Web UI não abre | `pkill -x focusguard-web` |
| Daemon não inicia | `journalctl -u focusguard -n 20` |

---

## Arquivos Importantes

```
scripts/setup-linux-vm.sh      # Setup automático
scripts/validate-etapa7.sh     # Checklist interativo
docs/vm-provisioning-guide.md  # Guia completo
docs/linux-validation-plan.md  # Plano detalhado (11 etapas)
```

---

## Achados Resolvidos (14 total)

| # | Severidade | Bug | Fix |
|---|---|---|---|
| 1 | WARN | bind hint Windows-only no Linux | `platformBindHint()` por SO |
| 2 | WARN | log user-space sem fallback | `filelog.UserLogPath` (XDG) |
| 3 | **ALTA** | ICMPv4 no IPv6/nft | `icmpPortUnreachableType(mask)` |
| 4-11 | MÉDIA | Testes que nunca rodaram no Linux | Corrigidos com TDD |
| 13 | MÉDIA | Data race nos mocks do tray | Mutex nos mocks |
| 14 | MÉDIA | Doctor: "CA corrompida" para permission denied | `fs.ErrPermission` → WARN |

---

*Gerado em 2026-08-27. Consultar `docs/linux-validation-plan.md` para detalhes.*
