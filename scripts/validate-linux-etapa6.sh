#!/usr/bin/env bash
# validate-linux-etapa6.sh — Validação da Etapa 6 (Resolução DNS & Integridade do Host)
# SALVAGUARDA: systemd-resolved NÃO é tocado! A internet do usuário continua 100% estável.
set -euo pipefail

PASS=0
FAIL=0
WARN=0

pass() { echo -e "  \033[1;32m✔\033[0m $*"; PASS=$((PASS + 1)); }
fail() { echo -e "  \033[1;31m✘\033[0m $*"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  \033[1;33m⚠\033[0m $*"; WARN=$((WARN + 1)); }

echo "============================================"
echo "  Etapa 6 — Resolução DNS & Integridade"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# 1. Provar que a resolução DNS do sistema operacional continua 100% funcionando
echo "1. Verificando integridade da resolução DNS do sistema..."
if getent hosts google.com >/dev/null 2>&1 || host google.com >/dev/null 2>&1; then
  pass "Resolução DNS para domínios públicos (google.com) operacional"
else
  fail "Resolução DNS do sistema falhou"
fi

if getent hosts github.com >/dev/null 2>&1 || host github.com >/dev/null 2>&1; then
  pass "Resolução DNS para domínios externos (github.com) operacional"
else
  fail "Resolução DNS secundária falhou"
fi

# 2. Verificar resolução de loopback para domínios sob controle do hosts
echo ""
echo "2. Verificando resolução do hosts para domínios locais..."
if getent hosts localhost | grep -q "127.0.0.1"; then
  pass "Resolução local (localhost -> 127.0.0.1) preservada e íntegra"
else
  fail "Resolução de localhost comprometida"
fi

# 3. Verificar estado do systemd-resolved
echo ""
echo "3. Verificando status do serviço systemd-resolved..."
if systemctl is-active --quiet systemd-resolved 2>/dev/null; then
  pass "systemd-resolved ativo e fornecendo DNS sem interferências"
else
  warn "systemd-resolved não está em execução (DNS resolvido por outro mecanismo)"
fi

echo ""
echo "--------------------------------------------"
echo "  Resultado Etapa 6: $PASS pass, $FAIL fail, $WARN warn"
echo "--------------------------------------------"
exit $FAIL
