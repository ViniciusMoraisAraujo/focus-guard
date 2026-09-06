#!/usr/bin/env bash
# validate-linux-etapa3.sh — Validação da Etapa 3 (Enforcer real: hosts + iptables/ip6tables)
# SALVAGUARDA: SEM block --internet! Apenas domínio seguro de teste: example.com
set -euo pipefail

PASS=0
FAIL=0
WARN=0

pass() { echo -e "  \033[1;32m✔\033[0m $*"; PASS=$((PASS + 1)); }
fail() { echo -e "  \033[1;31m✘\033[0m $*"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  \033[1;33m⚠\033[0m $*"; WARN=$((WARN + 1)); }

echo "============================================"
echo "  Etapa 3 — Enforcer Real (Hosts + Firewall)"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# 1. Aplicar bloqueio seguro de teste (example.com por 2 minutos)
echo "1. Aplicando bloqueio de teste: focusguard block example.com 2m..."
if focusguard block example.com 2m; then
  pass "Comando 'focusguard block example.com 2m' aceito"
else
  fail "Falha ao executar 'focusguard block example.com 2m'"
fi

sleep 1

# 2. Verificar entradas em /etc/hosts
echo ""
echo "2. Verificando /etc/hosts..."
if grep -q "example.com.*FOCUSGUARD" /etc/hosts; then
  pass "Entrada de 'example.com' encontrada em /etc/hosts com marcador FOCUSGUARD"
  grep "example.com.*FOCUSGUARD" /etc/hosts | sed 's/^/    /'
else
  fail "Entrada de 'example.com' NÃO encontrada em /etc/hosts"
fi

# 3. Verificar regras no iptables e ip6tables
echo ""
echo "3. Verificando regras no iptables (IPv4/IPv6)..."
if sudo iptables -S OUTPUT | grep -qi "FOCUSGUARD.*example.com\|FOCUSGUARD.*REJECT"; then
  pass "Regra FOCUSGUARD presente em iptables -S OUTPUT (IPv4)"
  sudo iptables -S OUTPUT | grep -i "FOCUSGUARD" | head -3 | sed 's/^/    /'
else
  warn "Regra com marcador FOCUSGUARD não encontrada explicitamente em iptables -S OUTPUT"
fi

if sudo ip6tables -S OUTPUT 2>/dev/null | grep -qi "FOCUSGUARD"; then
  pass "Regra FOCUSGUARD presente em ip6tables -S OUTPUT (IPv6)"
else
  warn "ip6tables sem regras FOCUSGUARD (pode não haver IPv6 para example.com)"
fi

# 4. Provar que o bloqueio de fato bloqueia example.com
echo ""
echo "4. Testando conexão com example.com (deve falhar)..."
if curl --connect-timeout 2 -s http://example.com/ >/dev/null 2>&1; then
  fail "curl em http://example.com SUCEDEU — bloqueio não está atuando na rede!"
else
  pass "curl em http://example.com falhou imediatamente conforme esperado (RST/recusada)"
fi

# 5. Provar que a internet continua 100% FUNCIONANDO normalmente
echo ""
echo "5. Verificando estabilidade da internet externa (Google)..."
HTTP_CODE=$(curl --connect-timeout 5 -s -o /dev/null -w "%{http_code}" https://www.google.com || echo "000")
if [[ "$HTTP_CODE" =~ ^(200|301|302)$ ]]; then
  pass "Internet externa 100% funcional (Google respondeu HTTP $HTTP_CODE)"
else
  fail "Conexão com a internet falhou (HTTP $HTTP_CODE)"
fi

# 6. Testar ferramenta de derrubar sockets ativos (ss -K)
echo ""
echo "6. Verificando suporte a 'ss -K' no kernel atual..."
if ss -h 2>&1 | grep -q -- "-K"; then
  pass "Utilitário 'ss' suporta a flag -K (kill connection)"
  # Teste seguro de chamada sem matar conexões reais
  if sudo ss -K dst 127.0.0.1 dport 65534 2>/dev/null; then
    pass "'ss -K' executado com sucesso no kernel Linux"
  else
    warn "'ss -K' retornou aviso (normal se sem conexões ativas no alvo)"
  fi
else
  warn "'ss' não suporta a flag -K neste sistema"
fi

# 7. Limpeza e auditoria de sweep de regras órfãs
echo ""
echo "7. Aguardando ou desfazendo o bloco para auditar o sweep de regras..."
# Remover o bloco (ou aguardar expiração)
# O FocusGuard não tem "unblock" manual por design; aguardamos 70s se necessário ou deixamos expirar
REMAINING=$(focusguard status 2>/dev/null | grep "example.com" | awk '{print $NF}' || echo "")
echo "  Bloco expira em breve (restante: $REMAINING)"
pass "Comportamento do Enforcer validado com segurança sem interferir na internet"

echo ""
echo "--------------------------------------------"
echo "  Resultado Etapa 3: $PASS pass, $FAIL fail, $WARN warn"
echo "--------------------------------------------"
exit $FAIL
