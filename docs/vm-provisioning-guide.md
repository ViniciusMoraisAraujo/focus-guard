# Guia de Provisionamento da VM Ubuntu

> **Objetivo:** preparar uma VM Ubuntu com desktop para validar as Etapas
> 7–10 do `docs/linux-validation-plan.md` (tray, update, clock guard, web/CLI).

## Pré-requisitos no Windows

- [ ] VirtualBox instalado (https://www.virtualbox.org/wiki/Downloads)
- [ ] ISO do Ubuntu 24.04 LTS Desktop baixada (~5GB)
  https://ubuntu.com/download/desktop
- [ ] 8GB RAM disponível (mínimo 4GB para a VM)
- [ ] 50GB de espaço em disco

---

## 1. Criar a VM

### Configurações recomendadas (VirtualBox)

| Parâmetro | Valor |
|---|---|
| Nome | `focusguard-linux` |
| Tipo | Linux |
| Versão | Ubuntu (64-bit) |
| RAM | 4096 MB (4GB) |
| CPU | 2 cores |
| Disco | 50GB (VDI, alocamento dinâmico) |
| Video | 128MB VRAM, VBoxSVGA |
| Network | **Bridge Adapter** (para testes de rede) |
| Audio | HID Audio (para notificações) |

### Passo a passo

1. VirtualBox → **Novo** → Nome: `focusguard-linux`
2. Tipo: Linux → Versão: Ubuntu (64-bit)
3. Memória: 4096 MB
4. Disco: criar disco virtual (VDI, 50GB, alocamento dinâmico)
5. **Configurar** antes de iniciar:
   - Sistema → Placa-mãe → EFI: **desmarcar** (manter Legacy BIOS)
   - Sistema → Processador → 2 CPUs
   - Tela → Video → 128MB VRAM, VBoxSVGA
   - Rede → Adapter 1 → **Bridge Adapter** (não NAT)
   - Armazenado → Controlador SATA → Adicionar disco óptico → Selecionar ISO Ubuntu

---

## 2. Instalar Ubuntu

1. Iniciar a VM e seguir o instalador
2. **Atualizar o sistema:**
   ```bash
   sudo apt update && sudo apt upgrade -y
   ```
3. **Instalar Open SSH Server** (para copiar arquivos do Windows):
   ```bash
   sudo apt install -y openssh-server
   ```
4. **Configurar SSH** (no Windows, copiar chave):
   ```bash
   # No Windows (Git Bash):
   # Primeiro, descubra o IP da VM:
   # Dentro da VM: ip addr show | grep inet

   # Copie sua chave pública para a VM:
   # ssh-copy-id user@<vm-ip>
   ```

---

## 3. Setup automático

### Opção A: Script automático (recomendado)

```bash
# Dentro da VM:
cd ~

# Copiar o repositório (via git ou SCP)
git clone https://github.com/<user>/focusguard.git
# ou do Windows: scp -r /c/dev/focusguard user@<vm-ip>:~/focusguard

# Executar o setup
cd ~/focusguard
chmod +x scripts/setup-linux-vm.sh
./scripts/setup-linux-vm.sh
```

### Opção B: Manual

```bash
# 1. Dependências
sudo apt install -y \
  golang-go \
  libayatana-appindicator3-dev \
  libayatana-appindicator3-1 \
  libgtk-3-dev \
  libgtk-3-0 \
  libnotify-bin \
  build-essential \
  git curl jq iptables iproute2 ca-certificates

# 2. Compilar binários
cd ~/focusguard
go run ./cmd/focusguard-icon
go build -o bin/focusguard ./cmd/focusguard
go build -o bin/focusguard-daemon ./cmd/focusguard-daemon
go build -o bin/focusguard-watchdog ./cmd/focusguard-watchdog
go build -o bin/focusguard-web ./cmd/focusguard-web
CGO_ENABLED=1 go build -o bin/focusguard-tray ./cmd/focusguard-tray

# 3. Criar staging
mkdir -p /tmp/focusguard-release
cp bin/focusguard* /tmp/focusguard-release/
cp scripts/install-linux.sh scripts/focusguard.service \
   scripts/focusguard-tray.desktop /tmp/focusguard-release/
cp packaging/focusguard.png /tmp/focusguard-release/
chmod +x /tmp/focusguard-release/install-linux.sh

# 4. Instalar
cd /tmp/focusguard-release
sudo ./install-linux.sh install

# 5. Verificar
systemctl status focusguard
ls -la /run/focusguard.sock
```

---

## 4. Validar Etapa 7 (Tray + Notificações)

```bash
# Fazer logout/login primeiro (para pegar o grupo focusguard)
# Ou: newgrp focusguard

# Executar validação
cd ~/focusguard
chmod +x scripts/validate-etapa7.sh
./scripts/validate-etapa7.sh
```

### Checklist manual (se preferir)

- [ ] Tray inicia: `focusguard-tray &` → ícone na bandeja
- [ ] Menu funcional: clique direito → opções aparecem
- [ ] Bloco rápido: menu → "Bloco rápido" → notificação + bloco ativo
- [ ] Notificações: `notify-send "Teste" "Mensagem"` → popup visível
- [ ] Autostart: `cat ~/.config/autostart/focusguard-tray.desktop`
- [ ] Desktop: `ls ~/Desktop/focusguard.desktop`
- [ ] Log: `cat ~/.local/state/focusguard/tray.log`
- [ ] CLI sem sudo: `focusguard status`

---

## 5. Validar Etapa 8 (Update + Smart Recovery)

```bash
# Compilar "versão nova" (mudar algo trivial)
cd ~/focusguard
echo "// v2" >> cmd/focusguard/commands.go
go build -o bin/focusguard-v2 ./cmd/focusguard

# Copiar para staging de teste
cp bin/focusguard-v2 /tmp/focusguard-release/focusguard

# Testar update (via CLI ou tray)
focusguard update

# Verificar
cat /opt/focusguard/*.bak.*  # backups criados
focusguard doctor             # versões consistentes
```

---

## 6. Validar Etapa 9 (Clock Guard)

```bash
# Parar NTP
sudo systemctl stop systemd-timesyncd
sudo systemctl disable systemd-timesyncd
sudo iptables -A OUTPUT -p udp --dport 123 -j DROP

# Adiantar relógio +24h
sudo date -s "$(date -d '+1 day' '+%Y-%m-%d %H:%M:%S')"

# Reiniciar daemon
sudo systemctl restart focusguard

# Verificar lockdown
focusguard status
focusguard tamper-log

# Corrigir relógio
sudo systemctl start systemd-timesyncd
sudo iptables -D OUTPUT -p udp --dport 123 -j DROP
```

---

## 7. Validar Etapa 10 (Web UI + CLI)

```bash
# CLI completa
focusguard status
focusguard block example.com 30m
focusguard stats
focusguard doctor
focusguard tamper-log
focusguard achievements
focusguard presets
focusguard goal --help

# Web UI
focusguard-web &
# Abrir: http://127.0.0.1:48902

# Login padrão: admin (configurar senha no primeiro acesso)
```

---

## Solução de Problemas

### Tray não aparece na bandeja

```bash
# Verificar se o appindicator está disponível
ldconfig -p | grep appindicator

# Verificar se o desktop suporta appindicator
echo $XDG_CURRENT_DESKTOP

# GNOME: instalar extensão
sudo apt install gnome-shell-extension-appindicator

# KDE: geralmente funciona nativamente
```

### Socket "permission denied"

```bash
# Verificar grupo
id $USER  # deve incluir "focusguard"

# Se não incluiu:
sudo usermod -aG focusguard $USER
newgrp focusguard  # ou faça logout/login
```

### Notificações não aparecem

```bash
# Verificar daemon de notificações
dbus-send --print-reply --dest=org.freedesktop.Notifications \
  /org/freedesktop/Notifications \
  org.freedesktop.Notifications.GetServerInformation

# GNOME: geralmente funciona
# KDE: verificar configurações de notificação
# XFCE: pode precisar de xfce4-notifyd
```

### Web UI não abre

```bash
# Verificar se a porta está ocupada
ss -tlnp | grep 48902

# Matar processo stale
pkill -x focusguard-web

# Verificar log
cat ~/.local/state/focusguard/web.log
```

### Daemon não inicia

```bash
# Verificar logs
journalctl -u focusguard -n 50

# Verificar se o binário existe
ls -la /opt/focusguard/focusguard-daemon

# Reiniciar manualmente
sudo systemctl restart focusguard
```

---

## Notas

- **WSL2 não serve para Etapas 7–10** — não tem sessão gráfica completa
- **Bridge Adapter** é necessário para testes de rede (sinkhole, DNS)
- **Snapshot da VM** antes de cada etapa permite reverter facilmente
- O log do setup fica em `~/focusguard-vm-setup.log`
