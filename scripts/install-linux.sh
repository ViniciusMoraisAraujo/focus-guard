#!/usr/bin/env bash
set -euo pipefail

BIN_DIR="/usr/local/bin"
SERVICE_NAME="focusguard"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
STATE_DIR="/var/lib/focusguard"

ACTION="${1:-install}"

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "[FocusGuard] ERRO: execute como root (sudo)." >&2
    exit 1
  fi
}

cmd_exists() {
  command -v "$1" >/dev/null 2>&1
}

script_dir() {
  cd "$(dirname "${BASH_SOURCE[0]}")" && pwd
}

install_binaries() {
  local dir
  dir="$(script_dir)"

  if [[ ! -f "${dir}/focusguard" || ! -f "${dir}/focusguard-daemon" || ! -f "${dir}/focusguard-watchdog" ]]; then
    echo "[FocusGuard] ERRO: binários não encontrados em ${dir}" >&2
    echo "[FocusGuard] Extraia o tar.gz e execute o script de dentro dele." >&2
    exit 1
  fi

  echo "[FocusGuard] Instalando binários em ${BIN_DIR}..."
  install -m 0755 "${dir}/focusguard" "${BIN_DIR}/focusguard"
  install -m 0755 "${dir}/focusguard-daemon" "${BIN_DIR}/focusguard-daemon"
  install -m 0755 "${dir}/focusguard-watchdog" "${BIN_DIR}/focusguard-watchdog"
  if [[ -f "${dir}/focusguard-tray" ]]; then
    install -m 0755 "${dir}/focusguard-tray" "${BIN_DIR}/focusguard-tray"
    install_tray_deps
  fi
}

install_tray_deps() {
  if ldconfig -p 2>/dev/null | grep -q 'libayatana-appindicator3.so'; then
    return 0
  fi
  if ! cmd_exists apt-get; then
    echo "[FocusGuard] Aviso: libayatana-appindicator3 não encontrada. O focusguard-tray precisa dela para rodar." >&2
    return 0
  fi
  echo "[FocusGuard] Instalando dependências do tray (libayatana-appindicator3)..."
  apt-get update -qq >/dev/null 2>&1 || true
  apt-get install -y libayatana-appindicator3-1 libgtk-3-0 \
    || echo "[FocusGuard] Aviso: não foi possível instalar as dependências do tray." >&2
}

install_service() {
  local dir
  dir="$(script_dir)"

  if [[ ! -f "${dir}/focusguard.service" ]]; then
    echo "[FocusGuard] ERRO: focusguard.service não encontrado em ${dir}" >&2
    exit 1
  fi

  echo "[FocusGuard] Instalando unit systemd..."
  install -m 0644 "${dir}/focusguard.service" "${SERVICE_FILE}"
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}"
  systemctl start "${SERVICE_NAME}"
  echo "[FocusGuard] ✔ FocusGuard instalado e iniciado!"
}

do_install() {
  require_root
  if ! cmd_exists systemctl; then
    echo "[FocusGuard] ERRO: systemd não encontrado neste sistema." >&2
    exit 1
  fi
  install_binaries
  install_service
}

do_uninstall() {
  require_root
  echo "[FocusGuard] Parando e desabilitando serviço..."
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
  systemctl daemon-reload
  rm -f "${SERVICE_FILE}"
  echo "[FocusGuard] Removendo binários..."
  rm -f "${BIN_DIR}/focusguard" "${BIN_DIR}/focusguard-daemon" "${BIN_DIR}/focusguard-watchdog" "${BIN_DIR}/focusguard-tray"
  echo "[FocusGuard] ✔ FocusGuard removido. Estado preservado em ${STATE_DIR}"
}

do_status() {
  systemctl status "${SERVICE_NAME}" --no-pager || true
}

case "${ACTION}" in
  install)   do_install ;;
  uninstall) do_uninstall ;;
  status)    do_status ;;
  *)
    echo "Uso: sudo ./install-linux.sh [install|uninstall|status]" >&2
    exit 1
    ;;
esac
