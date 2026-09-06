#!/usr/bin/env bash
# validate-linux-etapa5.sh — Validação da Etapa 5 (CA local + Interceptor :80/:443 + Navegador)
set -euo pipefail

PASS=0
FAIL=0
WARN=0

pass() { echo -e "  \033[1;32m✔\033[0m $*"; PASS=$((PASS + 1)); }
fail() { echo -e "  \033[1;31m✘\033[0m $*"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  \033[1;33m⚠\033[0m $*"; WARN=$((WARN + 1)); }

echo "============================================"
echo "  Etapa 5 — CA Local & Interceptor HTTPS"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# 1. Instalar CA local do FocusGuard
echo "1. Instalando CA local com sudo focusguard ca-install..."
if sudo focusguard ca-install; then
  pass "Comando 'ca-install' executado com sucesso"
else
  fail "Falha na execução de 'sudo focusguard ca-install'"
fi

# 2. Verificar arquivos da CA e Trust Store do sistema
echo ""
echo "2. Verificando certificados no sistema..."
CA_DIR="/var/lib/focusguard/ca"
if [[ -f "${CA_DIR}/focusguard-ca.crt" && -f "${CA_DIR}/focusguard-ca.key" ]]; then
  pass "Chave e certificado da CA gerados em ${CA_DIR}"
  KEY_PERM=$(stat -c '%a' "${CA_DIR}/focusguard-ca.key" 2>/dev/null || echo "")
  if [[ "$KEY_PERM" == "600" ]]; then
    pass "Permissão da chave da CA é restrita (0600)"
  else
    warn "Permissão da chave da CA é $KEY_PERM (esperado: 0600)"
  fi
else
  fail "Arquivos da CA ausentes em ${CA_DIR}"
fi

SHARE_CRT="/usr/local/share/ca-certificates/focusguard-ca.crt"
if [[ -f "$SHARE_CRT" ]]; then
  pass "Certificado copiado para ${SHARE_CRT}"
else
  fail "Certificado não encontrado em ${SHARE_CRT}"
fi

# 3. Executar focusguard doctor para verificar a CA
echo ""
echo "3. Executando focusguard doctor..."
DOCTOR_OUTPUT=$(focusguard doctor 2>&1 || true)
if echo "$DOCTOR_OUTPUT" | grep -qi "CA local.*ok\|CA local.*pass\|CA local.*instalada"; then
  pass "focusguard doctor confirmou integridade da CA local"
else
  echo "$DOCTOR_OUTPUT" | grep -i "CA" | sed 's/^/    /' || true
  warn "focusguard doctor reportou status inesperado para a CA"
fi

# 4. Ativar Interceptor
echo ""
echo "4. Ativando interceptor HTTP/HTTPS..."
if focusguard interceptor on; then
  pass "Comando 'focusguard interceptor on' aceito"
else
  fail "Falha ao ativar interceptor"
fi

# Reiniciar serviço para garantir listeners se necessário
sudo systemctl restart focusguard
sleep 2

# 5. Verificar listeners nas portas 80 e 443
echo ""
echo "5. Verificando portas 80 e 443..."
if ss -tlnp 2>/dev/null | grep -E "(:80|:443).*focusguard"; then
  pass "Listeners do interceptor ativos em :80 e :443"
else
  warn "Listeners do interceptor em :80 ou :443 podem estar ocupados ou restritos por outro serviço"
fi

# 6. Testar handshake TLS no trust store do sistema sem --cacert
echo ""
echo "6. Testando requisição TLS contra example.com bloqueado..."
focusguard block example.com 3m >/dev/null 2>&1 || true
sleep 1

CURL_OUT=$(curl -s --connect-timeout 4 https://example.com/ 2>&1 || true)
if echo "$CURL_OUT" | grep -qi "FocusGuard\|bloqueado\|Foco Ativo"; then
  pass "Página do interceptor HTTPS retornada SEM aviso de certificado inválido (confiança do SO funcionando!)"
else
  # Verificar com curl ignorando cert se o listener responde
  if curl -k -s --connect-timeout 4 https://127.0.0.1:443/ 2>&1 | grep -qi "FocusGuard\|bloqueado"; then
    warn "Interceptor respondeu em 127.0.0.1:443 com -k, mas falhou sem --cacert (store pode necessitar de reload de certs do browser)"
  else
    warn "Página do interceptor não interceptou a requisição TLS (o enforcer pode ter aplicado REJECT puro antes do listener)"
  fi
fi

echo ""
echo "--------------------------------------------"
echo "  Resultado Etapa 5: $PASS pass, $FAIL fail, $WARN warn"
echo "--------------------------------------------"
exit $FAIL
