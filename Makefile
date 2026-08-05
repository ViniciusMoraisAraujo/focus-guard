.PHONY: all build build-cli build-daemon build-web ui icon winres test vet clean install uninstall msi help fmt tidy

GO       := go
BIN_DIR  := bin
DESTDIR  ?= /usr/local/bin

UNAME_S  := $(shell uname -s 2>/dev/null || echo Windows)

# No Windows, `go build -o` explícito NÃO acrescenta .exe — mas o CLI localiza
# os binários irmãos pelo nome com extensão (ex.: focusguard-web.exe). Deriva a
# extensão do alvo do Go (GOOS) para os builds ficarem corretos em ambos os SOs.
GOOS := $(shell $(GO) env GOOS)
ifeq ($(GOOS),windows)
EXE := .exe
else
EXE :=
endif

CLI_BIN  := $(BIN_DIR)/focusguard$(EXE)
DAEMON   := focusguard-daemon$(EXE)

all: build test vet

build: icon build-cli build-daemon build-web

# icon regenera o ícone multi-tamanho a partir do artwork img/focusguard.png:
# o .ico Windows usado pelo go-winres (metadados .exe), o .png Linux do atalho
# do Desktop e o .png 32px do tray (internal/tray/icon_source.png).
icon:
	$(GO) run ./cmd/focusguard-icon

# winres gera os resource .syso (rsrc_windows_*.syso) que o go build embeda
# nos .exe — requer o go-winres instalado (go install github.com/tc-hib/go-winres@latest).
winres:
	cd cmd/focusguard-daemon && go-winres make --in ../../versioninfo.json --arch amd64,arm64
	cd cmd/focusguard && go-winres make --in versioninfo.json --arch amd64,arm64
	cd cmd/focusguard-tray && go-winres make --in versioninfo.json --arch amd64,arm64
	cd cmd/focusguard-watchdog && go-winres make --in versioninfo.json --arch amd64,arm64

build-cli:
	$(GO) build -o $(CLI_BIN) ./cmd/focusguard

build-daemon:
	$(GO) build -o $(BIN_DIR)/$(DAEMON) ./cmd/focusguard-daemon

build-web:
	$(GO) build -o $(BIN_DIR)/focusguard-web$(EXE) ./cmd/focusguard-web

# ui compila o frontend (React + Vite) e copia o dist para dentro de
# cmd/focusguard-web/assets, onde o go:embed o embute no binário. Rode antes
# de compilar o focusguard-web para incluir a última versão da interface.
ui:
	cd focusguard-ui && npm ci && npm run build
	find cmd/focusguard-web/assets -mindepth 1 -maxdepth 1 ! -name '.gitkeep' -exec rm -rf {} +
	cp -r focusguard-ui/dist/. cmd/focusguard-web/assets/

# msi gera o instalador único do Windows (focusguard-<versão>-amd64.msi) via
# go-msi + WiX Toolset. Requer um ambiente Windows (o go-msi chama o WiX via
# cmd.exe) com go-msi e WiX 3.10+ instalados — ver scripts/build-msi.sh.
# Uso: make msi VERSION=0.9.0 [ARCH=amd64|arm64]
msi:
	bash scripts/build-msi.sh $(VERSION) $(ARCH)

install:
	@echo "=== Instalando FocusGuard ==="
	$(GO) build -o $(CLI_BIN) ./cmd/focusguard
	$(GO) build -o $(BIN_DIR)/$(DAEMON) ./cmd/focusguard-daemon
	$(CLI_BIN) install
ifneq ($(findstring Linux,$(UNAME_S)),)
	cp $(CLI_BIN) $(DESTDIR)/focusguard
	cp $(BIN_DIR)/$(DAEMON) $(DESTDIR)/$(DAEMON)
	-systemctl restart focusguard 2>/dev/null || true
	@echo "✔ Binarios copiados para $(DESTDIR)/"
endif
	@echo "✔ Instalacao concluida"

uninstall:
	@echo "=== Desinstalando FocusGuard ==="
	$(GO) build -o $(CLI_BIN) ./cmd/focusguard
	$(CLI_BIN) uninstall
ifneq ($(findstring Linux,$(UNAME_S)),)
	-systemctl stop focusguard 2>/dev/null || true
	systemctl daemon-reload
	rm -f $(DESTDIR)/focusguard $(DESTDIR)/$(DAEMON)
	@echo "✔ Binarios removidos de $(DESTDIR)/"
endif
	@echo "✔ Desinstalacao concluida"

test:
	$(GO) test ./... -count=1 -timeout=60s

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN_DIR)
	rm -f focusguard-daemon.exe focusguard.exe

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

help:
	@echo "FocusGuard - Comandos disponiveis:"
	@echo ""
	@echo "  make build       Compila CLI, daemon e focusguard-web"
	@echo "  make ui          Compila a interface web e embute no focusguard-web"
	@echo "  make msi         Gera o instalador .msi do Windows (go-msi + WiX)"
	@echo "  make install     Compila e instala (Windows: schtasks, Linux: systemd)"
	@echo "  make uninstall   Remove da inicializacao"
	@echo "  make test        Executa todos os testes"
	@echo "  make vet         Verifica com go vet"
	@echo "  make clean       Remove artefatos de build"
	@echo "  make fmt         Formata codigo fonte"
	@echo "  make tidy        go mod tidy"
	@echo ""
	@echo "Variaveis:"
	@echo "  DESTDIR=$(DESTDIR)    Diretorio de instalacao dos binarios"
