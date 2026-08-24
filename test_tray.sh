#!/bin/bash
cd /opt/focusguard

echo "============================================"
echo "  Etapa 7 — Tray Linux (systray + notificações)"
echo "============================================"

echo ""
echo "=== 1. Daemon como root (usuário de sessão) ==="
pkill -9 -f focusguard-daemon 2>/dev/null || true; sleep 1
rm -f /var/lib/focusguard/state.json /var/lib/focusguard/tamper.jsonl /opt/focusguard/focusguard-daemon.log
# Daemon roda como root, mas com user de sessão para o socket
/opt/focusguard/focusguard-daemon --dir /var/lib/focusguard --user vinicius-araujo &
sleep 2
pgrep -f focusguard-daemon | head -1 && echo "DAEMON_ALIVE"
ls -la /run/focusguard.sock 2>/dev/null && echo "SOCKET_OK"

echo ""
echo "=== 2. Teste unitário do notify-send (raiz) ==="
notify-send --app-name=FocusGuard --icon=focusguard "Teste" "Mensagem de teste do tray" 2>&1
echo "notify-send exit: $?"

echo ""
echo "=== 3. Tray sobe como usuário de sessão (WSLg) ==="
echo "DISPLAY=$DISPLAY WAYLAND_DISPLAY=$WAYLAND_DISPLAY"
# Roda o tray como o usuário da sessão (vinicius-araujo), com o display do WSLg
sudo -u vinicius-araujo env DISPLAY=:0 WAYLAND_DISPLAY=wayland-0 XDG_RUNTIME_DIR=/run/user/1000 \
  /opt/focusguard/focusguard-tray > /tmp/tray.out 2>&1 &
TRAYPID=$!
sleep 5
echo "tray pid: $TRAYPID"
if kill -0 $TRAYPID 2>/dev/null; then
  echo "TRAY_ALIVE ✅ (não crashou)"
else
  echo "TRAY_MORTO ❌"
fi
echo "--- log do tray: ---"
cat /tmp/tray.out 2>/dev/null | head -5
cat /home/vinicius-araujo/.local/state/focusguard/focusguard-tray.log 2>/dev/null | tail -8

echo ""
echo "=== 4. Tray conectou ao daemon? (tooltip de status via IPC) ==="
# O tray consulta o status no boot (refreshStatus) — verificar se o socket foi usado
echo "daemon log (conexões do tray):"
grep -i "tray\|conn\|accept" /opt/focusguard/focusguard-daemon.log | tail -5 || echo "  (sem log de conexão específico)"

echo ""
echo "=== 5. Processos do appindicator/gtk ==="
ps aux | grep -E "focusguard-tray" | grep -v grep | head -2

echo ""
echo "=== 6. Cleanup ==="
kill $TRAYPID 2>/dev/null
pkill -9 -f focusguard-tray 2>/dev/null
pkill -9 -f focusguard-daemon 2>/dev/null
echo "done"
