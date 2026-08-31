#!/usr/bin/env bash
# build-msi.sh — Gera o instalador .msi do FocusGuard para Windows.
#
# Requisitos (rodar em um ambiente Windows — o go-msi executa o WiX via cmd.exe):
#   - Go (go build dos binários)
#   - go-msi      (go install github.com/mat007/go-msi@4783d3eea8eb18a7819d1d1ffac877c3edd50527
#                  → go-msi.exe em GOPATH/bin). Commit pinado: o template
#                  customizado segue o schema desta versão (v0.0.0-20200224144923).
#   - WiX Toolset 3.10+   (choco install wixtoolset  → "C:\Program Files (x86)\WiX Toolset v3.14\bin")
#
# Uso:
#   ./scripts/build-msi.sh <versão> [arquitetura]
#     versão      ex.: 0.9.0   (a versão da release, sem o prefixo 'v')
#     arquitetura amd64 (padrão) ou arm64 — determina o GOARCH e o nome do .msi
#
# Saída: focusguard-<versão>-<arquitetura>.msi na raiz do repositório.

set -euo pipefail

VERSION="${1:-}"
ARCH="${2:-amd64}"

if [ -z "$VERSION" ]; then
  echo "Uso: $0 <versão> [amd64|arm64]" >&2
  exit 1
fi

WIX_JSON="wix.json"
MSI_PREFIX="focusguard"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
ROOT_WIN="$(pwd -W 2>/dev/null || echo "$ROOT")"

# ---------------------------------------------------------------- validações
command -v go >/dev/null 2>&1 || { echo "ERRO: go não encontrado." >&2; exit 1; }
if [ -n "$(command -v go)" ]; then
  GOPATH_BIN="$(go env GOPATH 2>/dev/null)/bin"
  [ -d "$GOPATH_BIN" ] && export PATH="$PATH:$GOPATH_BIN"
fi
for ugo in /c/Users/*/go/bin; do
  [ -d "$ugo" ] && export PATH="$PATH:$ugo"
done
[ -d "$HOME/go/bin" ] && export PATH="$PATH:$HOME/go/bin"
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

if [ ! -f "$ROOT/packaging/focusguard.ico" ]; then
  echo "ERRO: Ícone do MSI ausente em $ROOT/packaging/focusguard.ico" >&2
  exit 1
fi

echo "==> Compilando binários Windows (${ARCH})..."
mkdir -p bin
for cmd in focusguard focusguard-watchdog focusguard-web; do
  CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w" -o "bin/${cmd}.exe" "./cmd/${cmd}"
done
# O tray é GUI: -H windowsgui evita a janela de console ao iniciar (mesma
# flag do GoReleaser no build tray-windows).
CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -H windowsgui" -o "bin/focusguard-tray.exe" "./cmd/focusguard-tray"
# O daemon injeta a versão via ldflags (espelhando o GoReleaser); sem isso a
# UI/status reportam "0.0.0-dev" e o auto-update é desabilitado.
CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -X main.daemonVersion=${VERSION}" -o "bin/focusguard-daemon.exe" "./cmd/focusguard-daemon"

echo "==> Gerando o .msi com go-msi..."
MSI_NAME="${MSI_PREFIX}-${VERSION}-${ARCH}.msi"
MSI_OUT_DIR="$ROOT/build/go-msi"
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
