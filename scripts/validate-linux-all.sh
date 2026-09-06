#!/usr/bin/env bash
# validate-linux-all.sh — Orquestrador Mestre de Validação Completa no Linux
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPORT_LOG="${REPO_DIR}/docs/linux-validation-report.log"

echo "================================================================="
echo "       FOCUSGUARD — VALIDAÇÃO COMPLETA DO MODO LINUX"
echo "                 $(date '+%Y-%m-%d %H:%M:%S')"
echo "================================================================="
echo ""
echo "Salvaguardas ativas: NUNCA desligar a internet | SEM block --internet | SEM clock-guard"
echo ""

# 1. Autenticação sudo com renovação em background (keep-alive)
echo "1. Solicitando credenciais de elevação (sudo)..."
sudo -v

# Loop keep-alive do sudo em background
while true; do
  sudo -n true
  sleep 40
  kill -0 "$$" || exit
done 2>/dev/null &
SUDO_KEEP_ALIVE_PID=$!
trap 'kill "$SUDO_KEEP_ALIVE_PID" 2>/dev/null || true' EXIT
echo "✔ Privilégios elevados autenticados e mantidos em segundo plano."
echo ""

# Redirecionar saída para tee (terminal + arquivo de relatório)
mkdir -p "${REPO_DIR}/docs"
exec > >(tee "$REPORT_LOG") 2>&1

STAGE_FILTER="${1:-all}"
TOTAL_PASS=0
TOTAL_FAIL=0
TOTAL_WARN=0

run_stage() {
  local stage_name="$1"
  local script_path="$2"
  
  if [[ "$STAGE_FILTER" != "all" && "$STAGE_FILTER" != "$stage_name" ]]; then
    return 0
  fi

  echo ""
  echo ">>> EXECUTANDO ETAPA $stage_name: $(basename "$script_path") <<<"
  echo "-----------------------------------------------------------------"
  
  if [[ -x "$script_path" ]]; then
    if "$script_path"; then
      echo ">>> ETAPA $stage_name CONCLUÍDA COM SUCESSO <<<"
    else
      local rc=$?
      echo ">>> ETAPA $stage_name TEVE FALHAS (código $rc) <<<"
      TOTAL_FAIL=$((TOTAL_FAIL + 1))
    fi
  else
    echo "Erro: script $script_path não encontrado ou sem permissão de execução."
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
  fi
}

# Execução ordenada das etapas aprovadas
run_stage "2"  "${SCRIPT_DIR}/validate-linux-etapa2.sh"
run_stage "3"  "${SCRIPT_DIR}/validate-linux-etapa3.sh"
run_stage "4"  "${SCRIPT_DIR}/validate-linux-etapa4.sh"
run_stage "5"  "${SCRIPT_DIR}/validate-linux-etapa5.sh"
run_stage "6"  "${SCRIPT_DIR}/validate-linux-etapa6.sh"
run_stage "7"  "${SCRIPT_DIR}/validate-etapa7.sh"
run_stage "8"  "${SCRIPT_DIR}/validate-linux-etapa8.sh"
run_stage "10" "${SCRIPT_DIR}/validate-linux-etapa10.sh"

echo ""
echo "================================================================="
echo "                RELATÓRIO GERAL DA VALIDAÇÃO"
echo "================================================================="
if [[ $TOTAL_FAIL -eq 0 ]]; then
  echo -e "  STATUS: \033[1;32mSUCESSO TOTAL (0 falhas críticas)\033[0m"
else
  echo -e "  STATUS: \033[1;31mFALHAS ENCONTRADAS ($TOTAL_FAIL etapas com falhas)\033[0m"
fi
echo "  Relatório registrado em: $REPORT_LOG"
echo "================================================================="
exit $TOTAL_FAIL
