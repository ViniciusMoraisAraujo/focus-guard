#!/usr/bin/env bash
# build-msi.sh — Gera os instaladores .msi do FocusGuard para Windows.
#
# Requisitos (rodar em um ambiente Windows — o go-msi executa o WiX via cmd.exe):
#   - Go (go build dos binários)
#   - go-msi      (go install github.com/mat007/go-msi@4783d3eea8eb18a7819d1d1ffac877c3edd50527
#                  → go-msi.exe em GOPATH/bin). Commit pinado: o template
#                  customizado segue o schema desta versão (v0.0.0-20200224144923).
#   - WiX Toolset 3.10+   (choco install wixtoolset  → "C:\Program Files (x86)\WiX Toolset v3.14\bin")
#
# Uso:
#   ./scripts/build-msi.sh <versão> [arquitetura] [perfil]
#     versão      ex.: 0.9.0   (a versão da release, sem o prefixo 'v')
#     arquitetura amd64 (padrão) ou arm64 — determina o GOARCH e o nome do .msi
#     perfil      desktop (padrão) ou server — decide o manifesto do go-msi:
#                   desktop → focusguard-<versão>-<arquitetura>.msi  (wix.json)
#                   server  → focusguard-server-<versão>-<arquitetura>.msi (wix-server.json)
#
# Edição Server (headless): o wix-server.json instala só daemon + watchdog +
# web + CLI e grava o marcador vazio server.role ao lado do daemon — em
# INSTALAÇÃO LIMPA (sem state.json ainda) o DNS sinkhole nasce habilitado
# ("Rei da Rede"). Numa conversão desktop→server de uma instalação já
# existente, o state.json continua lá e o DNS NÃO liga sozinho: habilite na
# tela Rede ou com `focusguard dns start`. Sem tray, sem atalho no desktop e
# sem chave Run (a máquina é um aparelho). As duas edições compartilham o
# UpgradeCode (sabores do mesmo produto): instalar uma sobre a outra na mesma
# versão é permitido (AllowSameVersionUpgrades no product.wxs) e converte a
# máquina.
#
# Saída: focusguard[-server]-<versão>-<arquitetura>.msi na raiz do repositório.

set -euo pipefail

VERSION="${1:-}"
ARCH="${2:-amd64}"
PROFILE="${3:-desktop}"

if [ -z "$VERSION" ]; then
  echo "Uso: $0 <versão> [amd64|arm64] [desktop|server]" >&2
  exit 1
fi

case "$PROFILE" in
  desktop)
    WIX_JSON="wix.json"
    MSI_PREFIX="focusguard"
    ;;
  server)
    WIX_JSON="wix-server.json"
    MSI_PREFIX="focusguard-server"
    ;;
  *)
    echo "ERRO: perfil inválido \"$PROFILE\" (use desktop ou server)." >&2
    exit 1
    ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
ROOT_WIN="$(pwd -W 2>/dev/null || echo "$ROOT")"

# ---------------------------------------------------------------- validações
command -v go >/dev/null 2>&1 || { echo "ERRO: go não encontrado." >&2; exit 1; }
command -v go-msi >/dev/null 2>&1 || { echo "ERRO: go-msi não encontrado (go install github.com/mat007/go-msi@4783d3eea8eb18a7819d1d1ffac877c3edd50527)." >&2; exit 1; }

WIX_DIRS=(
  "/c/Program Files (x86)/WiX Toolset v3.14/bin"
  "/c/Program Files (x86)/WiX Toolset v3.11/bin"
  "/c/Program Files (x86)/WiX Toolset v3.10/bin"
)
WIX_BIN=""
for d in "${WIX_DIRS[@]}"; do
  if [ -d "$d" ]; then WIX_BIN="$d"; break; fi
done
if [ -z "$WIX_BIN" ]; then
  echo "ERRO: WiX Toolset 3.10+ não encontrado (choco install wixtoolset)." >&2
  exit 1
fi
export PATH="$WIX_BIN:$PATH"

if [ ! -f "$ROOT/focusguard.ico" ]; then
  echo "ERRO: Ícone do MSI ausente em $ROOT/focusguard.ico" >&2
  exit 1
fi
if [ "$PROFILE" = "server" ] && [ ! -f "$ROOT/server.role" ]; then
  echo "ERRO: marcador da edição Server ausente em $ROOT/server.role (arquivo vazio que habilita o DNS no 1º boot em instalação limpa)." >&2
  exit 1
fi

echo "==> Compilando binários Windows (${ARCH}, perfil ${PROFILE})..."
mkdir -p bin
for cmd in focusguard focusguard-watchdog focusguard-web; do
  CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w" -o "bin/${cmd}.exe" "./cmd/${cmd}"
done
# O tray só existe na edição desktop (a Server é headless): pular o build aqui
# evita um binário morto na pasta bin e economiza tempo do CI.
if [ "$PROFILE" = "desktop" ]; then
  # O tray é GUI: -H windowsgui evita a janela de console ao iniciar (mesma
  # flag do GoReleaser no build tray-windows).
  CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -H windowsgui" -o "bin/focusguard-tray.exe" "./cmd/focusguard-tray"
fi
# O daemon injeta a versão via ldflags (espelhando o GoReleaser); sem isso a
# UI/status reportam "0.0.0-dev" e o auto-update é desabilitado.
CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -X main.daemonVersion=${VERSION}" -o "bin/focusguard-daemon.exe" "./cmd/focusguard-daemon"

echo "==> Gerando o .msi com go-msi (${PROFILE})..."
MSI_NAME="${MSI_PREFIX}-${VERSION}-${ARCH}.msi"
# O go-msi resolve os caminhos do wix.json (ex.: bin/focusguard.exe) para
# absolutos (filepath.Abs) e os torna relativos ao diretório de trabalho
# (--out) via filepath.Rel. No Windows, filepath.Rel falha entre unidades
# diferentes: no CI o checkout fica em D: e o diretório temporário do SO em
# C:, o que aborta a geração ("Rel: can't make D:/... relative to C:/...").
# Apontar --out para um diretório dentro do repositório mantém tudo na mesma
# unidade; o instalador continua sendo gravado na raiz (--msi relativo). O
# diretório é POR PERFIL (desktop|server): duas execuções seguidas no CI não
# podem se sujar com artefatos intermediários uma da outra (wixobj, cab...).
MSI_OUT_DIR="$ROOT/build/go-msi/$PROFILE"
mkdir -p "$MSI_OUT_DIR"
MSI_OUT="$(cygpath -w "$MSI_OUT_DIR" 2>/dev/null || echo "$MSI_OUT_DIR")"
go-msi make \
  --path "${ROOT_WIN}/scripts/msi/${WIX_JSON}" \
  --src "${ROOT_WIN}/scripts/msi" \
  --out "$MSI_OUT" \
  --arch "$ARCH" \
  --msi "$MSI_NAME" \
  --version "$VERSION"

echo "==> Instalador gerado: $MSI_NAME"
