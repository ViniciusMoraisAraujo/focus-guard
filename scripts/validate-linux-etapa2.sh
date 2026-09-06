#!/usr/bin/env bash
# validate-linux-etapa2.sh — Validação da Etapa 2 (Instalação real, systemd, permissões, socket)
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PASS=0
FAIL=0
WARN=0

pass() { echo -e "  \033[1;32m✔\033[0m $*"; PASS=$((PASS + 1)); }
fail() { echo -e "  \033[1;31m✘\033[0m $*"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  \033[1;33m⚠\033[0m $*"; WARN=$((WARN + 1)); }

echo "============================================"
echo "  Etapa 2 — Instalação Real & Systemd"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# 1. Preparar staging em dist/linux-install com os binários recém-compilados
echo "1. Preparando staging para instalação..."
mkdir -p "${REPO_DIR}/dist/linux-install"
cp -f "${REPO_DIR}/bin/focusguard" "${REPO_DIR}/dist/linux-install/"
cp -f "${REPO_DIR}/bin/focusguard-daemon" "${REPO_DIR}/dist/linux-install/"
cp -f "${REPO_DIR}/bin/focusguard-watchdog" "${REPO_DIR}/dist/linux-install/"
cp -f "${REPO_DIR}/bin/focusguard-web" "${REPO_DIR}/dist/linux-install/"
if [[ -f "${REPO_DIR}/bin/focusguard-tray" ]]; then
  cp -f "${REPO_DIR}/bin/focusguard-tray" "${REPO_DIR}/dist/linux-install/"
fi
cp -f "${REPO_DIR}/scripts/install-linux.sh" "${REPO_DIR}/dist/linux-install/"
cp -f "${REPO_DIR}/scripts/focusguard.service" "${REPO_DIR}/dist/linux-install/"
cp -f "${REPO_DIR}/scripts/focusguard-tray.desktop" "${REPO_DIR}/dist/linux-install/"
cp -f "${REPO_DIR}/packaging/focusguard.png" "${REPO_DIR}/dist/linux-install/"
chmod +x "${REPO_DIR}/dist/linux-install/install-linux.sh"
pass "Staging atualizado em dist/linux-install com os 5 binários"

# 2. Executar instalação via install-linux.sh
echo ""
echo "2. Executando sudo ./install-linux.sh install..."
(cd "${REPO_DIR}/dist/linux-install" && sudo ./install-linux.sh install)

# 3. Verificar arquivos em /opt/focusguard
echo ""
echo "3. Verificando /opt/focusguard..."
for b in focusguard focusguard-daemon focusguard-watchdog focusguard-web focusguard-tray; do
  if [[ -x "/opt/focusguard/${b}" ]]; then
    pass "Binário instalado e executável: /opt/focusguard/${b}"
  else
    fail "Binário ausente ou não executável: /opt/focusguard/${b}"
  fi
done

# 4. Verificar symlink CLI
echo ""
echo "4. Verificando symlink da CLI..."
if [[ -L "/usr/local/bin/focusguard" ]]; then
  pass "Symlink existe: /usr/local/bin/focusguard -> $(readlink -f /usr/local/bin/focusguard)"
else
  fail "Symlink /usr/local/bin/focusguard não encontrado"
fi

# 5. Verificar serviço systemd
echo ""
echo "5. Verificando serviço systemd..."
if systemctl is-active --quiet focusguard; then
  pass "Serviço focusguard está active (running)"
else
  fail "Serviço focusguard NÃO está ativo"
fi

# 6. Verificar socket Unix e permissões
echo ""
echo "6. Verificando socket Unix..."
SOCKET_PATH=""
if [[ -S "/run/focusguard/focusguard.sock" ]]; then
  SOCKET_PATH="/run/focusguard/focusguard.sock"
elif [[ -S "/run/focusguard.sock" ]]; then
  SOCKET_PATH="/run/focusguard.sock"
fi

if [[ -n "$SOCKET_PATH" ]]; then
  pass "Socket encontrado em $SOCKET_PATH"
  SOCK_GROUP=$(stat -c '%G' "$SOCKET_PATH" 2>/dev/null || stat -f '%Sg' "$SOCKET_PATH" 2>/dev/null || echo "unknown")
  SOCK_PERM=$(stat -c '%a' "$SOCKET_PATH" 2>/dev/null || echo "unknown")
  if [[ "$SOCK_GROUP" == "focusguard" ]]; then
    pass "Grupo do socket é 'focusguard'"
  else
    warn "Grupo do socket é '$SOCK_GROUP' (esperado: focusguard)"
  fi
  if [[ "$SOCK_PERM" == "660" ]]; then
    pass "Permissão do socket é 0660"
  else
    warn "Permissão do socket é $SOCK_PERM (esperado: 660)"
  fi
else
  fail "Nenhum socket Unix do FocusGuard encontrado em /run"
fi

# 7. Testar comunicação CLI sem sudo
echo ""
echo "7. Testando CLI sem sudo..."
if focusguard status >/dev/null 2>&1; then
  pass "CLI 'focusguard status' executou com sucesso sem sudo"
else
  fail "CLI 'focusguard status' falhou sem sudo (verificar permissões no grupo focusguard)"
fi

# 8. Verificar watchdog do systemd
echo ""
echo "8. Verificando WatchdogSec do systemd..."
WATCHDOG_USEC=$(systemctl show focusguard -p WatchdogUSec --value 2>/dev/null || echo "0")
if [[ "$WATCHDOG_USEC" -gt 0 ]]; then
  pass "Watchdog configurado no systemd: $((WATCHDOG_USEC / 1000000))s"
else
  warn "WatchdogUSec não configurado na unit do systemd"
fi

echo ""
echo "--------------------------------------------"
echo "  Resultado Etapa 2: $PASS pass, $FAIL fail, $WARN warn"
echo "--------------------------------------------"
exit $FAIL
