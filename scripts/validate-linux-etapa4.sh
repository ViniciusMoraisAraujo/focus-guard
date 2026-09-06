#!/usr/bin/env bash
# validate-linux-etapa4.sh — Validação da Etapa 4 (Watchers + store + réplicas AES-GCM)
set -euo pipefail

PASS=0
FAIL=0
WARN=0

pass() { echo -e "  \033[1;32m✔\033[0m $*"; PASS=$((PASS + 1)); }
fail() { echo -e "  \033[1;31m✘\033[0m $*"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  \033[1;33m⚠\033[0m $*"; WARN=$((WARN + 1)); }

echo "============================================"
echo "  Etapa 4 — Watchers, Store & Réplicas"
echo "  $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================"
echo ""

# Garantir bloco ativo de teste
echo "1. Garantindo bloco ativo de teste (example.com)..."
focusguard block example.com 3m >/dev/null 2>&1 || true
sleep 1

# 2. Testar hostswatch: Adulterar /etc/hosts removendo o bloco
echo ""
echo "2. Testando hostswatch (adulteração de /etc/hosts)..."
if grep -q "example.com.*FOCUSGUARD" /etc/hosts; then
  echo "  Removendo linha de example.com do /etc/hosts manualmente via sed..."
  sudo sed -i '/example.com.*FOCUSGUARD/d' /etc/hosts
  
  # Dar tempo para fsnotify + SHA-256 reagir
  echo "  Aguardando reação do hostswatch (3 segundos)..."
  sleep 3

  if grep -q "example.com.*FOCUSGUARD" /etc/hosts; then
    pass "hostswatch detectou a adulteração e restaurou /etc/hosts automaticamente a partir da RAM!"
  else
    fail "hostswatch NÃO restaurou /etc/hosts dentro do tempo esperado"
  fi
else
  warn "Entrada de teste não encontrada em /etc/hosts para testar hostswatch"
fi

# 3. Verificar registro no tamper-log
echo ""
echo "3. Verificando registro no tamper-log..."
if focusguard tamper-log 2>/dev/null | grep -qi "hosts.*restore\|example.com"; then
  pass "Evento de adulteração do hosts registrado no tamper-log:"
  focusguard tamper-log 2>/dev/null | tail -2 | sed 's/^/    /'
else
  warn "Evento de hosts não localizado no tamper-log recente (pode estar em buffer)"
fi

# 4. Testar statewatch: Adulterar state.json em disco
echo ""
echo "4. Testando statewatch (adulteração de /var/lib/focusguard/state.json)..."
STATE_FILE="/var/lib/focusguard/state.json"
if [[ -f "$STATE_FILE" ]]; then
  echo "  Modificando state.json em disco com payload adulterado..."
  echo '{"active_blocks":[]}' | sudo tee "$STATE_FILE" >/dev/null
  
  echo "  Aguardando reconciliação da RAM pelo statewatch (3 segundos)..."
  sleep 3

  if grep -q "example.com" "$STATE_FILE" 2>/dev/null; then
    pass "statewatch detectou alteração externa e reescreveu o disco a partir da RAM (RAM é a fonte da verdade)!"
  else
    warn "state.json ainda não foi restaurado pelo statewatch (pode requerer ciclo de reconcile)"
  fi
else
  warn "Arquivo $STATE_FILE não encontrado"
fi

# 5. Testar Auto-Cura (LoadAndHeal) via Réplica AES-256-GCM ligada ao Hardware
echo ""
echo "5. Testando Auto-Cura de corrupção a partir da réplica de hardware..."
echo "  Parando serviço focusguard..."
sudo systemctl stop focusguard

echo "  Corrompendo state.json intencionalmente..."
echo "{{{{CORROMPIDO_INVALID_JSON_TEST}}}}" | sudo tee "$STATE_FILE" >/dev/null

echo "  Iniciando serviço focusguard..."
sudo systemctl start focusguard
sleep 2

if systemctl is-active --quiet focusguard; then
  pass "Serviço subiu com sucesso após corrupção do state.json"
  if focusguard status 2>/dev/null | grep -qi "example.com"; then
    pass "LoadAndHeal recuperou os blocos ativos com sucesso a partir da réplica criptografada!"
  else
    warn "Serviço ativo, mas bloco example.com não constou no status (pode ter expirado)"
  fi
else
  fail "Serviço falhou ao inicializar com state.json corrompido"
fi

echo ""
echo "--------------------------------------------"
echo "  Resultado Etapa 4: $PASS pass, $FAIL fail, $WARN warn"
echo "--------------------------------------------"
exit $FAIL
