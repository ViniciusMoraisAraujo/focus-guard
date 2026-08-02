.PHONY: all build build-cli build-daemon icon winres test vet clean install uninstall help fmt tidy

GO       := go
BIN_DIR  := bin
CLI_BIN  := $(BIN_DIR)/focusguard
DAEMON   := focusguard-daemon
DESTDIR  ?= /usr/local/bin

UNAME_S  := $(shell uname -s 2>/dev/null || echo Windows)

all: build test vet

build: icon build-cli build-daemon

# icon regenera o ícone multi-tamanho (.ico Windows + .png Linux) usado pelo
# go-winres (metadados .exe) e pelo atalho do Desktop nos installers.
icon:
	$(GO) run ./cmd/focusguard-icon

# winres gera os resource .syso (rsrc_windows_*.syso) que o go build embeda
# nos .exe — requer o go-winres instalado (go install github.com/tc-hib/go-winres@latest).
winres:
	cd cmd/focusguard-daemon && go-winres make --in ../../versioninfo.json --arch amd64,arm64
	cd cmd/focusguard && go-winres make --in versioninfo.json --arch amd64,arm64

build-cli:
	$(GO) build -o $(CLI_BIN) ./cmd/focusguard

build-daemon:
	$(GO) build -o $(BIN_DIR)/$(DAEMON) ./cmd/focusguard-daemon

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
	@echo "  make build       Compila CLI e daemon"
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
