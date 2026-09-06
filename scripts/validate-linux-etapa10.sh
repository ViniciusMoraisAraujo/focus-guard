#!/usr/bin/env bash
# validate-linux-etapa10.sh — Validação da Etapa 10 (Web UI & CLI user-space sem sudo)
set -euo pipefail

PASS=0
FAIL=0
WARN=0

pass() { echo -e "  \033[1;32m✔\033[0m $*"; PASS=$((PASS + 1)); }
fail() { echo -e "  \033[1;31m✘\033[0m $*"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  \033[1;33m⚠\033[0m $*"; WARN=$((WARN + 1)); }

echo "============================================"
echo "  Etapa 10 — Web UI & CLI User-Space"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# 1. Garantir que estamos rodando como usuário comum (sem sudo)
CURRENT_USER=$(whoami)
echo "1. Usuário atual: $CURRENT_USER (UID: $(id -u))"
if [[ "$(id -u)" -eq 0 ]]; then
  warn "Este script deve ser executado preferencialmente como usuário comum, não como root"
else
  pass "Executando como usuário não-root"
fi

# 2. Iniciar ou verificar focusguard-web na porta 48902
echo ""
echo "2. Verificando servidor da Web UI (focusguard-web na porta 48902)..."
if ! ss -tlnp 2>/dev/null | grep -q ":48902"; then
  echo "  Iniciando focusguard-web em background..."
  /opt/focusguard/focusguard-web &
  WEB_PID=$!
  sleep 2
else
  pass "focusguard-web já está em execução na porta 48902"
fi

# 3. Testar resposta HTTP do focusguard-web
echo ""
echo "3. Testando endpoints HTTP do focusguard-web..."
HTTP_RESP=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:48902/ || echo "000")
if [[ "$HTTP_RESP" =~ ^(200|302|304)$ ]]; then
  pass "Web UI respondeu com HTTP $HTTP_RESP em http://127.0.0.1:48902/"
else
  fail "Web UI retornou HTTP $HTTP_RESP em http://127.0.0.1:48902/"
fi

# 4. Testar proteção anti-DNS rebinding (Host header inválido deve ser rejeitado)
echo ""
echo "4. Testando segurança anti-DNS rebinding..."
REBIND_RESP=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: attacker.com" http://127.0.0.1:48902/ || echo "000")
if [[ "$REBIND_RESP" =~ ^(400|403)$ ]]; then
  pass "Host header malicioso rejeitado com sucesso (HTTP $REBIND_RESP)"
else
  warn "Host header externo retornou HTTP $REBIND_RESP"
fi

# 5. Testar suíte completa de comandos CLI sem sudo
echo ""
echo "5. Testando comandos do CLI sem elevação de privilégios..."

echo "  -> focusguard status:"
if focusguard status >/dev/null 2>&1; then
  pass "focusguard status OK"
else
  fail "focusguard status FALHOU"
fi

echo "  -> focusguard stats:"
if focusguard stats >/dev/null 2>&1; then
  pass "focusguard stats OK"
else
  warn "focusguard stats falhou"
fi

echo "  -> focusguard presets:"
if focusguard presets >/dev/null 2>&1; then
  pass "focusguard presets OK"
else
  fail "focusguard presets FALHOU"
fi

echo "  -> focusguard schedule list:"
if focusguard schedule list >/dev/null 2>&1; then
  pass "focusguard schedule list OK"
else
  fail "focusguard schedule list FALHOU"
fi

echo "  -> focusguard apps list:"
if focusguard apps list >/dev/null 2>&1; then
  pass "focusguard apps list OK"
else
  fail "focusguard apps list FALHOU"
fi

echo "  -> focusguard achievements:"
if focusguard achievements >/dev/null 2>&1; then
  pass "focusguard achievements OK"
else
  fail "focusguard achievements FALHOU"
fi

echo "  -> focusguard metrics:"
if focusguard metrics >/dev/null 2>&1; then
  pass "focusguard metrics OK"
else
  fail "focusguard metrics FALHOU"
fi

echo "  -> focusguard report now:"
if focusguard report now >/dev/null 2>&1; then
  pass "focusguard report now gerou relatório com sucesso"
else
  warn "focusguard report now falhou"
fi

echo ""
echo "--------------------------------------------"
echo "  Resultado Etapa 10: $PASS pass, $FAIL fail, $WARN warn"
echo "--------------------------------------------"
exit $FAIL
