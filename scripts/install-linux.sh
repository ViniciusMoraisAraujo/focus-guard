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
    install_tray_autostart
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

# tray_user retorna o usuário real que invocou o sudo (ou root como fallback).
# O autostart do tray é por usuário (espelho da chave HKCU Run do Windows).
tray_user() {
  echo "${SUDO_USER:-$(logname 2>/dev/null || echo root)}"
}

tray_user_home() {
  local user home
  user="$(tray_user)"
  # '|| true' evita abortar sob 'set -e' quando getent/awk falham (usuário inexistente)
  if cmd_exists getent; then
    home="$(getent passwd "${user}" 2>/dev/null | cut -d: -f6 || true)"
  else
    home="$(awk -F: -v u="${user}" '$1 == u { print $6 }' /etc/passwd 2>/dev/null || true)"
  fi
  if [[ -z "${home}" ]]; then
    return 1
  fi
  echo "${home}"
}

install_tray_autostart() {
  local dir user home gid autostart_dir desktop
  dir="$(script_dir)"

  if [[ ! -f "${dir}/focusguard-tray.desktop" ]]; then
    echo "[FocusGuard] Aviso: focusguard-tray.desktop não encontrado. Autostart do tray não registrado." >&2
    return 0
  fi

  user="$(tray_user)"
  if ! home="$(tray_user_home)"; then
    echo "[FocusGuard] Aviso: não foi possível determinar o home do usuário ${user}. Autostart do tray não registrado." >&2
    return 0
  fi
  gid="$(id -gn "${user}" 2>/dev/null || echo "${user}")"
  autostart_dir="${home}/.config/autostart"
  desktop="${autostart_dir}/focusguard-tray.desktop"

  echo "[FocusGuard] Registrando autostart do tray para o usuário ${user}..."
  # Best-effort: qualquer falha aqui NÃO deve abortar a instalação do daemon.
  # install -d age como mkdir -p, mas define o dono dos diretórios criados,
  # para o usuário poder gerenciar o autostart sem sudo (espelho da HKCU).
  if ! install -d -m 0755 -o "${user}" -g "${gid}" "${autostart_dir}" ||
     ! sed "s|^Exec=.*|Exec=${BIN_DIR}/focusguard-tray|" \
        "${dir}/focusguard-tray.desktop" > "${desktop}"; then
    echo "[FocusGuard] Aviso: não foi possível registrar o autostart do tray em ${autostart_dir}." >&2
    rm -f "${desktop}" # evita deixar um .desktop parcial/quebrado no autostart do usuário
    return 0
  fi
  chmod 0644 "${desktop}" 2>/dev/null || true
  chown "${user}:${gid}" "${desktop}" 2>/dev/null || true
  echo "[FocusGuard] ✔ Tray registrado para iniciar com o login (${autostart_dir})."
}

remove_tray_autostart() {
  local home autostart_dir desktop
  if ! home="$(tray_user_home)"; then
    return 0
  fi
  autostart_dir="${home}/.config/autostart"
  desktop="${autostart_dir}/focusguard-tray.desktop"
  if [[ -f "${desktop}" ]]; then
    rm -f "${desktop}"
    echo "[FocusGuard] Autostart do tray removido (${desktop})."
  fi
}

# install_desktop_shortcut cria o atalho do FocusGuard no Desktop do usuário
# (.desktop) e instala o ícone no hicolor do usuário (256px, espelho do atalho
# .lnk do Windows). Best-effort: qualquer falha aqui NÃO aborta a instalação do
# daemon — sem o ícone ou sem home, apenas avisa e segue.
install_desktop_shortcut() {
  local dir user home gid icons_dir desktop_file
  dir="$(script_dir)"

  if [[ ! -f "${dir}/focusguard.png" ]]; then
    echo "[FocusGuard] Aviso: focusguard.png não encontrado. Atalho do Desktop não criado." >&2
    return 0
  fi

  user="$(tray_user)"
  if ! home="$(tray_user_home)"; then
    echo "[FocusGuard] Aviso: não foi possível determinar o home do usuário ${user}. Atalho do Desktop não criado." >&2
    return 0
  fi
  gid="$(id -gn "${user}" 2>/dev/null || echo "${user}")"

  echo "[FocusGuard] Criando atalho do FocusGuard no Desktop do usuário ${user}..."
  if ! install -d -m 0755 -o "${user}" -g "${gid}" "${home}/.local/share/icons/hicolor/256x256/apps"; then
    echo "[FocusGuard] Aviso: não foi possível criar o diretório de ícones. Atalho não criado." >&2
    return 0
  fi
  install -m 0644 "${dir}/focusguard.png" "${home}/.local/share/icons/hicolor/256x256/apps/focusguard.png" 2>/dev/null || true

  desktop_dir="${home}/Desktop"
  if ! install -d -m 0755 -o "${user}" -g "${gid}" "${desktop_dir}"; then
    # Sem diretório Desktop, tenta ~/Área de Trabalho (pt-BR) e cai fora se falhar.
    desktop_dir="${home}/Área de Trabalho"
    if ! install -d -m 0755 -o "${user}" -g "${gid}" "${desktop_dir}"; then
      echo "[FocusGuard] Aviso: sem diretório Desktop. Atalho não criado." >&2
      return 0
    fi
  fi

  desktop_file="${desktop_dir}/focusguard.desktop"
  # Terminal=true: a CLI sem argumentos abre a TUI (que exige um terminal).
  # Sem isso, clicar no atalho não mostraria nada ao usuário.
  cat > "${desktop_file}" <<EOF
[Desktop Entry]
Type=Application
Name=FocusGuard
Comment=Bloqueio focado de distrações
Exec=${BIN_DIR}/focusguard
Icon=focusguard
Terminal=true
Categories=Utility;Security;
EOF
  chmod 0755 "${desktop_file}" 2>/dev/null || true
  chown "${user}:${gid}" "${desktop_file}" 2>/dev/null || true
  echo "[FocusGuard] ✔ Atalho do FocusGuard criado (${desktop_file})."
}

remove_desktop_shortcut() {
  local home desktop
  if ! home="$(tray_user_home)"; then
    return 0
  fi
  for desktop in "${home}/Desktop/focusguard.desktop" "${home}/Área de Trabalho/focusguard.desktop"; do
    if [[ -f "${desktop}" ]]; then
      rm -f "${desktop}"
      echo "[FocusGuard] Atalho do FocusGuard removido (${desktop})."
    fi
  done
  rm -f "${home}/.local/share/icons/hicolor/256x256/apps/focusguard.png" 2>/dev/null || true
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
  install_desktop_shortcut
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
  remove_tray_autostart
  remove_desktop_shortcut
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
