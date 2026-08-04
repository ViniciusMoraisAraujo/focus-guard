#!/usr/bin/env bash
# build-msi.sh — Gera o instalador único .msi do FocusGuard para Windows.
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

echo "==> Compilando binários Windows (${ARCH})..."
mkdir -p bin
for cmd in focusguard focusguard-daemon focusguard-watchdog focusguard-tray focusguard-web; do
  CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" go build -trimpath -ldflags "-s -w" -o "bin/${cmd}.exe" "./cmd/${cmd}"
done

echo "==> Gerando o .msi com go-msi..."
MSI_NAME="focusguard-${VERSION}-${ARCH}.msi"
# O go-msi resolve os caminhos do wix.json (ex.: bin/focusguard.exe) para
# absolutos (filepath.Abs) e os torna relativos ao diretório de trabalho
# (--out) via filepath.Rel. No Windows, filepath.Rel falha entre unidades
# diferentes: no CI o checkout fica em D: e o diretório temporário do SO em
# C:, o que aborta a geração ("Rel: can't make D:/... relative to C:/...").
# Apontar --out para um diretório dentro do repositório mantém tudo na mesma
# unidade; o instalador continua sendo gravado na raiz (--msi relativo).
MSI_OUT="$(cygpath -w "$ROOT/build/go-msi" 2>/dev/null || echo "$ROOT/build/go-msi")"
go-msi make \
  --path "${ROOT_WIN}/scripts/msi/wix.json" \
  --src "${ROOT_WIN}/scripts/msi" \
  --out "$MSI_OUT" \
  --arch "$ARCH" \
  --msi "$MSI_NAME" \
  --version "$VERSION"

echo "==> Instalador gerado: $MSI_NAME"
