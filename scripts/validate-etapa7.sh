#!/usr/bin/env bash
# validate-etapa7.sh — Checklist de validação da Etapa 7 (Tray + Notificações)
#
# Execute DEPOIS do setup-linux-vm.sh e de fazer logout/login (para o grupo
# focusguard). Requer sessão desktop (X11 ou Wayland) com display ativo.
#
# Uso:
#   chmod +x validate-etapa7.sh
#   ./validate-etapa7.sh
set -euo pipefail

PASS=0
FAIL=0
WARN=0
SKIP=0
NOTES=()

pass() { echo -e "  \033[1;32m✔\033[0m $*"; PASS=$((PASS + 1)); }
fail() { echo -e "  \033[1;31m✘\033[0m $*"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  \033[1;33m⚠\033[0m $*"; WARN=$((WARN + 1)); }
skip() { echo -e "  \033[1;36m⊘\033[0m $* (skip)"; SKIP=$((SKIP + 1)); }
note() { NOTES+=("$*"); }

echo "============================================"
echo "  Etapa 7 — Tray + Notificações"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# ──────────────────────────────────────────────
# 1. Pré-condições
# ──────────────────────────────────────────────
echo "1. Pré-condições"

# Display
if [[ -n "${DISPLAY:-}" ]] || [[ -n "${WAYLAND_DISPLAY:-}" ]]; then
  pass "Display ativo (${DISPLAY:-}${WAYLAND_DISPLAY:-})"
else
  fail "Sem display — execute numa sessão desktop (não SSH/headless)"
  echo ""
  echo "Resultados: $PASS pass, $FAIL fail, $WARN warn, $SKIP skip"
  exit 1
fi

# Grupo focusguard
if id -nG 2>/dev/null | grep -qw focusguard; then
  pass "Usuário no grupo focusguard"
else
  warn "Usuário NÃO está no grupo focusguard — faça logout/login primeiro"
  note "Execute: sudo usermod -aG focusguard \$USER && newgrp focusguard"
fi

# Binário do tray
TRAY_BIN="/opt/focusguard/focusguard-tray"
if [[ -x "$TRAY_BIN" ]]; then
  pass "Binário do tray: $TRAY_BIN"
else
  fail "Binário do tray não encontrado ou não executável: $TRAY_BIN"
fi

# Socket do daemon
if [[ -S /run/focusguard.sock ]]; then
  pass "Socket do daemon ativo"
else
  warn "Socket não encontrado — daemon pode não estar rodando"
  note "Execute: sudo systemctl start focusguard"
fi

# Serviço
if systemctl is-active --quiet focusguard 2>/dev/null; then
  pass "Serviço focusguard: active"
else
  warn "Serviço focusguard não está active"
fi

echo ""

# ──────────────────────────────────────────────
# 2. Dependências do tray
# ──────────────────────────────────────────────
echo "2. Dependências do tray"

if ldconfig -p 2>/dev/null | grep -q 'libayatana-appindicator3.so'; then
  pass "libayatana-appindicator3 instalada"
else
  fail "libayatana-appindicator3 AUSENTE"
  note "Instale com: sudo apt install libayatana-appindicator3-1"
fi

if ldconfig -p 2>/dev/null | grep -q 'libgtk-3.so'; then
  pass "libgtk-3 instalada"
else
  warn "libgtk-3 pode estar ausente"
fi

echo ""

# ──────────────────────────────────────────────
# 3. Iniciar tray
# ──────────────────────────────────────────────
echo "3. Iniciar tray"

# Matar tray anterior se existir
pkill -x focusguard-tray 2>/dev/null || true
sleep 1

# Iniciar em background
if [[ -x "$TRAY_BIN" ]]; then
  "$TRAY_BIN" &
  TRAY_PID=$!
  sleep 3

  if kill -0 "$TRAY_PID" 2>/dev/null; then
    pass "Tray iniciado (PID: $TRAY_PID)"
  else
    fail "Tray crashou ao iniciar"
    # Verificar log
    LOG_FILE="$HOME/.local/state/focusguard/tray.log"
    if [[ -f "$LOG_FILE" ]]; then
      warn "Últimas linhas do log:"
      tail -10 "$LOG_FILE" | sed 's/^/    /'
    fi
  fi
else
  fail "Não foi possível iniciar o tray (binário ausente)"
fi

echo ""

# ──────────────────────────────────────────────
# 4. Ícone na bandeja
# ──────────────────────────────────────────────
echo "4. Ícone na bandeja"

echo "  Verifique visualmente:"
echo "    - Há um ícone novo na bandeja do sistema (geralmente canto inferior direito)?"
echo "    - O ícone é o do FocusGuard (não um ícone genérico)?"
echo ""
read -p "  O ícone apareceu na bandeja? [s/N]: " TRAY_ICON
if [[ "$TRAY_ICON" =~ ^[sS]$ ]]; then
  pass "Ícone visível na bandeja"
else
  warn "Ícone não visível — verificar se o appindicator está funcionando"
  note "Possíveis causas: desktop sem suporte a appindicator, DE não suporta"
fi

echo ""

# ──────────────────────────────────────────────
# 5. Menu do tray
# ──────────────────────────────────────────────
echo "5. Menu do tray"

echo "  Clique DIREITO no ícone do tray e verifique:"
echo "    - Menu aparece com as opções?"
echo "    - Opções: Status, Bloco rápido, Categorias, Verificar atualização,"
echo "      Abrir painel, Sair"
echo ""
read -p "  O menu aparece corretamente? [s/N]: " TRAY_MENU
if [[ "$TRAY_MENU" =~ ^[sS]$ ]]; then
  pass "Menu do tray funcional"
else
  warn "Menu não verificado ou não funcional"
fi

echo ""

# ──────────────────────────────────────────────
# 6. Bloco rápido
# ──────────────────────────────────────────────
echo "6. Bloco rápido (4h)"

echo "  No menu do tray, clique em 'Bloco rápido' (ou similar)."
echo "  Verifique:"
echo "    - Uma notificação aparece confirmando o bloco?"
echo "    - 'focusguard status' mostra o bloco ativo?"
echo ""
read -p "  Bloco rápido aplicou corretamente? [s/N]: " QUICK_BLOCK
if [[ "$QUICK_BLOCK" =~ ^[sS]$ ]]; then
  pass "Bloco rápido funcional"
  # Verificar via CLI
  if focusguard status 2>/dev/null | grep -qi "bloqueado\|block\|ativo"; then
    pass "CLI confirma bloco ativo"
  else
    warn "CLI não mostra bloco — verificar manualmente"
  fi
else
  warn "Bloco rápido não verificado"
fi

echo ""

# ──────────────────────────────────────────────
# 7. Notificações
# ──────────────────────────────────────────────
echo "7. Notificações (notify-send)"

if command -v notify-send >/dev/null 2>&1; then
  # Testar notificação
  notify-send "FocusGuard Teste" "Se você está vendo esta notificação, está funcionando!" 2>/dev/null
  echo "  Uma notificação foi enviada. Você a viu?"
  read -p "  Notificação visível? [s/N]: " NOTIFY_TEST
  if [[ "$NOTIFY_TEST" =~ ^[sS]$ ]]; then
    pass "notify-send funcionando"
  else
    warn "Notificação não visível — verificar se o daemon de notificações está ativo"
    note "Verifique: dbus-send --print-reply --dest=org.freedesktop.Notifications /org/freedesktop/Notifications org.freedesktop.Notifications.GetServerInformation"
  fi
else
  warn "notify-send ausente — notificações não testáveis"
  note "Instale com: sudo apt install libnotify-bin"
fi

echo ""

# ──────────────────────────────────────────────
# 8. Autostart XDG
# ──────────────────────────────────────────────
echo "8. Autostart XDG"

AUTOSTART="$HOME/.config/autostart/focusguard-tray.desktop"
if [[ -f "$AUTOSTART" ]]; then
  pass "Arquivo de autostart existe: $AUTOSTART"
  # Verificar conteúdo
  if grep -q "Exec=/opt/focusguard/focusguard-tray" "$AUTOSTART"; then
    pass "Autostart aponta para o binário correto"
  else
    warn "Autostart com Exec inesperado"
    cat "$AUTOSTART" | sed 's/^/    /'
  fi
  if grep -q "X-GNOME-Autostart-enabled=true" "$AUTOSTART"; then
    pass "Autostart habilitado (X-GNOME-Autostart-enabled=true)"
  else
    warn "X-GNOME-Autostart-enabled não encontrado"
  fi
else
  warn "Autostart não encontrado em $AUTOSTART"
  note "O autostart é criado pelo install-linux.sh quando o SUDO_USER está configurado"
fi

echo ""

# ──────────────────────────────────────────────
# 9. Atalho Desktop
# ──────────────────────────────────────────────
echo "9. Atalho Desktop"

DESKTOP_FILE="$HOME/Desktop/focusguard.desktop"
if [[ -f "$DESKTOP_FILE" ]]; then
  pass "Atalho existe: $DESKTOP_FILE"
  if grep -q "Terminal=false" "$DESKTOP_FILE"; then
    pass "Atalho configurado (Terminal=false)"
  else
    warn "Atalho pode abrir terminal"
  fi
else
  # Tentar Área de Trabalho (pt-BR)
  DESKTOR_FILE_BR="$HOME/Área de Trabalho/focusguard.desktop"
  if [[ -f "$DESKTOP_FILE_BR" ]]; then
    pass "Atalho existe (Área de Trabalho): $DESKTOP_FILE_BR"
  else
    warn "Atalho não encontrado no Desktop"
  fi
fi

echo ""

# ──────────────────────────────────────────────
# 10. Log do tray
# ──────────────────────────────────────────────
echo "10. Log do tray"

TRAY_LOG="$HOME/.local/state/focusguard/tray.log"
if [[ -f "$TRAY_LOG" ]]; then
  pass "Log do tray existe: $TRAY_LOG"
  LINES=$(wc -l < "$TRAY_LOG")
  pass "Log tem $LINES linhas"
  echo "  Últimas 5 linhas:"
  tail -5 "$TRAY_LOG" | sed 's/^/    /'
else
  warn "Log do tray não encontrado em $TRAY_LOG"
  note "O log pode estar em outro local se XDG_STATE_HOME estiver configurado"
fi

echo ""

# ──────────────────────────────────────────────
# 11. IPC via socket (usuário comum)
# ──────────────────────────────────────────────
echo "11. IPC via socket (sem sudo)"

if focusguard status >/dev/null 2>&1; then
  pass "CLI funciona sem sudo (acesso via grupo focusguard)"
  focusguard status 2>/dev/null | head -5 | sed 's/^/    /'
else
  warn "CLI falhou sem sudo — verificar permissões do socket"
  note "Verifique: ls -la /run/focusguard.sock"
fi

echo ""

# ──────────────────────────────────────────────
# 12. Cleanup
# ──────────────────────────────────────────────
echo "12. Cleanup"

echo "  Deseja encerrar o tray de teste?"
read -p "  Encerrar tray? [s/N]: " KILL_TRAY
if [[ "$KILL_TRAY" =~ ^[sS]$ ]] && [[ -n "${TRAY_PID:-}" ]]; then
  kill "$TRAY_PID" 2>/dev/null || true
  pass "Tray encerrado"
else
  note "Tray continua rodando (PID: ${TRAY_PID:-desconhecido})"
fi

echo ""

# ──────────────────────────────────────────────
# Resumo
# ──────────────────────────────────────────────
echo "============================================"
echo "  Resumo da Validação — Etapa 7"
echo "============================================"
echo ""
echo "  ✔ Pass:  $PASS"
echo "  ✘ Fail:  $FAIL"
echo "  ⚠ Warn:  $WARN"
echo "  ⊘ Skip:  $SKIP"
echo ""

if [[ $FAIL -eq 0 ]]; then
  echo -e "  \033[1;32m✔ Etapa 7 CONCLUÍDA\033[0m (sem failures)"
else
  echo -e "  \033[1;31m✘ Etapa 7 com $FAIL failure(s)\033[0m"
fi

if [[ ${#NOTES[@]} -gt 0 ]]; then
  echo ""
  echo "  Notas:"
  for n in "${NOTES[@]}"; do
    echo "    - $n"
  done
fi

echo ""
echo "  Resultados salvos em: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""
