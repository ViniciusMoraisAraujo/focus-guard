#!/usr/bin/env bash
# snapshot-vm.sh — Cria snapshot da VM VirtualBox antes da validação Linux.
#
# Uso:
#   ./snapshot-vm.sh [nome-snapshot]
#
# Exemplo:
#   ./snapshot-vm.sh pre-etapa7
#   ./snapshot-vm.sh pre-validacao-2026-08-27
#
# Pré-requisitos:
#   - VirtualBox instalado com VBoxManage no PATH
#   - VM "focusguard-linux" criada e desligada
set -euo pipefail

VM_NAME="focusguard-linux"
SNAPSHOT_NAME="${1:-pre-validacao-$(date +%Y-%m-%d)}"
VBOXMANAGE=""

# ──────────────────────────────────────────────
# Localizar VBoxManage
# ──────────────────────────────────────────────
find_vboxmanage() {
  # Windows (comum)
  if command -v VBoxManage.exe >/dev/null 2>&1; then
    VBOXMANAGE="VBoxManage.exe"
    return
  fi
  # Linux / macOS
  if command -v VBoxManage >/dev/null 2>&1; then
    VBOXMANAGE="VBoxManage"
    return
  fi
  # Caminhos absolutos Windows
  for path in \
    "/c/Program Files/Oracle/VirtualBox/VBoxManage.exe" \
    "/c/Program Files (x86)/Oracle/VirtualBox/VBoxManage.exe" \
    "C:\\Program Files\\Oracle\\VirtualBox\\VBoxManage.exe"; do
    if [[ -x "$path" ]] 2>/dev/null || [[ -f "$path" ]]; then
      VBOXMANAGE="$path"
      return
    fi
  done
  echo "ERRO: VBoxManage não encontrado. Instale o VirtualBox." >&2
  exit 1
}

# ──────────────────────────────────────────────
# Verificar se a VM existe
# ──────────────────────────────────────────────
vm_exists() {
  $VBOXMANAGE list vms 2>/dev/null | grep -q "\"${VM_NAME}\""
}

# ──────────────────────────────────────────────
# Obter estado da VM
# ──────────────────────────────────────────────
vm_state() {
  $VBOXMANAGE showvminfo "$VM_NAME" --machinereadable 2>/dev/null \
    | grep '^VMState=' | cut -d'"' -f2
}

# ──────────────────────────────────────────────
# Listar snapshots existentes
# ──────────────────────────────────────────────
list_snapshots() {
  echo "Snapshots existentes da VM '${VM_NAME}':"
  echo ""
  $VBOXMANAGE snapshot "$VM_NAME" list --machinereadable 2>/dev/null \
    | grep '^SnapshotName' | cut -d'"' -f2 | while read -r name; do
    echo "  - $name"
  done
  echo ""
}

# ──────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────
echo "=== FocusGuard — Snapshot da VM ==="
echo ""

# Encontrar VBoxManage
find_vboxmanage
echo "VBoxManage: $VBOXMANAGE"

# Verificar VM
if ! vm_exists; then
  echo "ERRO: VM '${VM_NAME}' não encontrada." >&2
  echo "" >&2
  echo "VMs disponíveis:" >&2
  $VBOXMANAGE list vms 2>/dev/null | sed 's/^/  /' >&2
  echo "" >&2
  echo "Crie a VM primeiro ou ajuste VM_NAME neste script." >&2
  exit 1
fi
echo "VM: $VM_NAME"

# Estado atual
STATE=$(vm_state)
echo "Estado: $STATE"
echo ""

# Se a VM está rodando, perguntar se quer desligar
if [[ "$STATE" == "running" ]]; then
  echo "⚠ A VM está rodando."
  echo "  Snapshots em VMs rodando são possíveis (snapshot live),"
  echo "  mas o estado pode ficar inconsistente."
  echo ""
  read -p "  Criar snapshot mesmo assim? [s/N]: " CONFIRM
  if [[ ! "$CONFIRM" =~ ^[sS]$ ]]; then
    echo "  Desligando a VM primeiro..."
    $VBOXMANAGE controlvm "$VM_NAME" acpipowerbutton 2>/dev/null || true
    echo "  Aguardando desligamento..."
    for i in $(seq 1 30); do
      if [[ "$(vm_state)" == "poweroff" ]]; then
        break
      fi
      sleep 2
    done
    STATE=$(vm_state)
    if [[ "$STATE" != "poweroff" ]]; then
      echo "  ERRO: VM não desligou. Force com: $VBOXMANAGE controlvm $VM_NAME poweroff" >&2
      exit 1
    fi
    echo "  VM desligada."
  fi
fi

# Listar snapshots existentes
list_snapshots

# Criar snapshot
echo "Criando snapshot: ${SNAPSHOT_NAME}..."
if $VBOXMANAGE snapshot "$VM_NAME" take "$SNAPSHOT_NAME" \
    --description "FocusGuard - antes da validação Linux ($(date '+%Y-%m-%d %H:%M'))" 2>&1; then
  echo ""
  echo "✔ Snapshot criado com sucesso!"
  echo "  Nome: $SNAPSHOT_NAME"
  echo "  VM: $VM_NAME"
  echo ""
  echo "Para restaurar:"
  echo "  $VBOXMANAGE snapshot $VM_NAME restore \"$SNAPSHOT_NAME\""
  echo ""
  echo "Para deletar (depois da validação):"
  echo "  $VBOXMANAGE snapshot $VM_NAME delete \"$SNAPSHOT_NAME\""
else
  echo ""
  echo "✘ Falha ao criar snapshot." >&2
  exit 1
fi

# Listar snapshots atualizados
echo ""
echo "Snapshots atualizados:"
list_snapshots
