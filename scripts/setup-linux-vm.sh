#!/usr/bin/env bash
# setup-linux-vm.sh — Provisionamento automático da VM Ubuntu para validação
# do FocusGuard Linux (Etapas 7–10).
#
# Uso: execute dentro da VM Ubuntu com desktop ( VirtualBox / bare metal ):
#   chmod +x setup-linux-vm.sh
#   ./setup-linux-vm.sh
#
# Pré-requisitos:
#   - Ubuntu 24.04 LTS Desktop (ou superior) com sudo
#   - Acesso a internet
#   - O repositório focusguard clonado em ~/focusguard (ou passe o caminho)
set -euo pipefail

REPO_DIR="${1:-$HOME/focusguard}"
STAGING_DIR="/tmp/focusguard-release"
LOG_FILE="$HOME/focusguard-vm-setup.log"

info()  { echo -e "\033[1;34m[INFO]\033[0m  $*"; }
ok()    { echo -e "\033[1;32m[OK]\033[0m    $*"; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m  $*"; }
fail()  { echo -e "\033[1;31m[FAIL]\033[0m  $*"; exit 1; }

log() { echo "[$(date '+%H:%M:%S')] $*" >> "$LOG_FILE"; }

echo "=== FocusGuard — Setup da VM Ubuntu ===" | tee "$LOG_FILE"
echo "Repo: $REPO_DIR" | tee -a "$LOG_FILE"
echo ""

# ──────────────────────────────────────────────
# 1. Dependências do sistema
# ──────────────────────────────────────────────
info "1/8 Instalando dependências do sistema..."
sudo apt-get update -qq
sudo apt-get install -y -qq \
  golang-go \
  libayatana-appindicator3-dev \
  libayatana-appindicator3-1 \
  libgtk-3-dev \
  libgtk-3-0 \
  libnotify-bin \
  build-essential \
  git \
  curl \
  jq \
  iptables \
  iproute2 \
  ca-certificates \
  2>&1 | tee -a "$LOG_FILE"
ok "Dependências do sistema instaladas"

# ──────────────────────────────────────────────
# 2. Verificar Go
# ──────────────────────────────────────────────
info "2/8 Verificando Go..."
GO_VER=$(go version 2>/dev/null | awk '{print $3}' | sed 's/go//')
if [[ -z "$GO_VER" ]]; then
  fail "Go não encontrado. Instale manualmente: https://go.dev/dl/"
fi
GO_MAJOR=$(echo "$GO_VER" | cut -d. -f1)
GO_MINOR=$(echo "$GO_VER" | cut -d. -f2)
if [[ "$GO_MAJOR" -lt 1 ]] || { [[ "$GO_MAJOR" -eq 1 ]] && [[ "$GO_MINOR" -lt 22 ]]; }; then
  fail "Go $GO_VER muito antigo (mínimo 1.22). Atualize com: sudo snap install go --classic"
fi
ok "Go $GO_VER"

# ──────────────────────────────────────────────
# 3. Verificar repositório
# ──────────────────────────────────────────────
info "3/8 Verificando repositório..."
if [[ ! -d "$REPO_DIR/.git" ]]; then
  fail "Repositório não encontrado em $REPO_DIR. Clone com: git clone <url> $REPO_DIR"
fi
cd "$REPO_DIR"
BRANCH=$(git branch --show-current)
COMMIT=$(git log --oneline -1 | awk '{print $1}')
info "Branch: $BRANCH | Commit: $COMMIT"
ok "Repositório ok"

# ──────────────────────────────────────────────
# 4. Compilar binários (incluindo tray com CGO)
# ──────────────────────────────────────────────
info "4/8 Compilando binários (5/5 com CGO para o tray)..."
mkdir -p bin

# Gerar ícones
go run ./cmd/focusguard-icon 2>&1 | tee -a "$LOG_FILE"

# Compilar cada binário
for cmd in focusguard focusguard-daemon focusguard-watchdog focusguard-web; do
  info "  Compilando $cmd..."
  go build -o "bin/$cmd" "./cmd/$cmd" 2>&1 | tee -a "$LOG_FILE"
  ok "  $cmd OK ($(stat -c%s "bin/$cmd" 2>/dev/null || stat -f%z "bin/$cmd") bytes)"
done

# Tray COM CGO (precisa do appindicator)
info "  Compilando focusguard-tray (CGO=1)..."
CGO_ENABLED=1 go build -o bin/focusguard-tray ./cmd/focusguard-tray 2>&1 | tee -a "$LOG_FILE"
ok "  focusguard-tray OK"

ok "5/5 binários compilados"
ls -lh bin/

# ──────────────────────────────────────────────
# 5. Criar pacote de release
# ──────────────────────────────────────────────
info "5/8 Criando pacote de release..."
rm -rf "$STAGING_DIR"
mkdir -p "$STAGING_DIR"

cp bin/focusguard bin/focusguard-daemon bin/focusguard-watchdog \
   bin/focusguard-web bin/focusguard-tray "$STAGING_DIR/"
cp scripts/install-linux.sh "$STAGING_DIR/"
cp scripts/focusguard.service "$STAGING_DIR/"
cp scripts/focusguard-tray.desktop "$STAGING_DIR/"
cp packaging/focusguard.png "$STAGING_DIR/" 2>/dev/null || true
cp README.md CHANGELOG.md "$STAGING_DIR/"

chmod +x "$STAGING_DIR/install-linux.sh"

cd /tmp
tar czf focusguard-linux-amd64.tar.gz -C "$(dirname "$STAGING_DIR")" "$(basename "$STAGING_DIR")"
ok "Pacote criado: /tmp/focusguard-linux-amd64.tar.gz ($(ls -lh /tmp/focusguard-linux-amd64.tar.gz | awk '{print $5}'))"

# ──────────────────────────────────────────────
# 6. Instalar FocusGuard
# ──────────────────────────────────────────────
info "6/8 Instalando FocusGuard (sudo)..."
cd "$STAGING_DIR"
sudo ./install-linux.sh install 2>&1 | tee -a "$LOG_FILE"
ok "FocusGuard instalado"

# ──────────────────────────────────────────────
# 7. Verificar serviço
# ──────────────────────────────────────────────
info "7/8 Verificando serviço..."
sleep 2
STATUS=$(systemctl is-active focusguard 2>/dev/null || echo "inactive")
if [[ "$STATUS" == "active" ]]; then
  ok "Serviço focusguard: active"
else
  warn "Serviço focusguard: $STATUS (verificar com: journalctl -u focusguard -n 20)"
fi

# Verificar socket
if [[ -S /run/focusguard.sock ]]; then
  PERMS=$(stat -c '%U:%G %a' /run/focusguard.sock 2>/dev/null || echo "desconhecido")
  ok "Socket: /run/focusguard.sock ($PERMS)"
else
  warn "Socket não encontrado em /run/focusguard.sock"
fi

# ──────────────────────────────────────────────
# 8. Verificar dependências do tray
# ──────────────────────────────────────────────
info "8/8 Verificando dependências do tray..."
TRAY_OK=true

if ldconfig -p 2>/dev/null | grep -q 'libayatana-appindicator3.so'; then
  ok "libayatana-appindicator3: OK"
else
  warn "libayatana-appindicator3: AUSENTE (tray pode não funcionar)"
  TRAY_OK=false
fi

if command -v notify-send >/dev/null 2>&1; then
  ok "notify-send: OK"
else
  warn "notify-send: AUSENTE (notificações não funcionarão)"
fi

if command -v dbus-launch >/dev/null 2>&1; then
  ok "dbus-launch: OK"
else
  warn "dbus-launch: AUSENTE (notificações podem falhar)"
fi

# ──────────────────────────────────────────────
# Resumo
# ──────────────────────────────────────────────
echo ""
echo "============================================"
echo "  Setup completo!"
echo "============================================"
echo ""
echo "  Binários: $STAGING_DIR/"
echo "  Pacote:   /tmp/focusguard-linux-amd64.tar.gz"
echo "  Log:      $LOG_FILE"
echo ""
echo "  Serviço:  $(systemctl is-active focusguard 2>/dev/null || echo 'verificar')"
echo "  Socket:   $(test -S /run/focusguard.sock && echo 'OK' || echo 'verificar')"
echo "  Tray deps: $(test "$TRAY_OK" = true && echo 'OK' || echo 'verificar')"
echo ""
echo "  Próximos passos:"
echo "    1. Faça logout/login (para pegar o grupo focusguard)"
echo "    2. Execute: ~/focusguard/scripts/validate-etapa7.sh"
echo ""
