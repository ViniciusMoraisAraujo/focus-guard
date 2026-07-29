# FocusGuard 🛡️

**FocusGuard** é uma biblioteca Go para bloquear sites distractivos e manter o foco. Ela opera em nível de sistema, utilizando regras de firewall (`iptables`/`ip6tables`) e o arquivo `/etc/hosts` para impedir o acesso a domínios indesejados.

> ⚠️ **Status**: Em desenvolvimento ativo. Atualmente com suporte completo apenas para Linux. Suporte para Windows em planejamento.

---

## Funcionalidades

- 🚫 **Bloqueio de domínios** — Impede o acesso a sites distractivos por tempo determinado
- ⏱️ **Bloqueios temporários** — Cada bloqueio tem data de expiração, e não pode ser removido manualmente antes do prazo
- 🔄 **Resolução automática de IPs** — Resolve endereços IPv4 e IPv6 dos domínios bloqueados
- 🧱 **Dupla camada de bloqueio** (Linux):
  - **`/etc/hosts`** — Redireciona o domínio para `127.0.0.1` e `::1`
  - **`iptables`/`ip6tables`** — Regras `DROP` na chain `OUTPUT` para os IPs resolvidos
- 💾 **Persistência de estado** — Armazena bloqueios ativos em arquivo JSON com gravação atômica
- 🔄 **Sincronização** — Reconcilia o estado salvo com as regras do sistema

---

## Arquitetura

```
focusguard/
├── go.mod
├── internal/
│   ├── policy/          # Modelo de dados e regras de negócio
│   │   ├── policy.go        # Definição do Block e métodos de ciclo de vida
│   │   └── policy_test.go   # Testes do ciclo de vida do Block
│   ├── enforcer/        # Aplicação das regras no sistema operacional
│   │   ├── enforcer.go          # Interface Enforcer + utilitário ResolveIPs
│   │   ├── enforcer_linux.go    # Implementação Linux (hosts + iptables)
│   │   └── enforcer_windows.go  # Stub para Windows (em construção)
│   └── store/           # Persistência de estado
│       ├── json.go          # Store com gravação atômica em JSON
│       └── json_test.go     # Testes de save/load do estado
```

### Fluxo de Dados

```
Policy (definição do bloqueio)
    ↓
Store (persistência em JSON)
    ↓
Enforcer (aplicação no SO)
    ├── /etc/hosts (redirecionamento localhost)
    └── iptables/ip6tables (DROP packets)
```

---

## Instalação

### Pré-requisitos

- Go 1.26.5 ou superior
- Linux com `iptables` e `ip6tables` (para funcionalidade completa do enforcer)
- Acesso root/sudo (necessário para manipular firewall e `/etc/hosts`)

### Como dependência

```bash
go get github.com/seu-usuario/focusguard
```

### Build

```bash
git clone https://github.com/seu-usuario/focusguard.git
cd focusguard
go build ./...
```

---

## Uso

> **Nota sobre plataforma**: A implementação do `Enforcer` (`NewEnforcer()`) está disponível apenas em Linux devido às build tags (`//go:build linux`). O stub para Windows existe mas ainda não possui funcionalidades implementadas.

```go
package main

import (
    "focusguard/internal/policy"
    "focusguard/internal/store"
    "time"
)

func main() {
    // Criar um bloqueio
    block := policy.Block{
        Domain:    "youtube.com",
        StartedAt: time.Now(),
        ExpiresAt: time.Now().Add(1 * time.Hour),
    }

    // Persistir o estado
    st := store.NewStore("/etc/focusguard/state.json")
    state, _ := st.Load()
    state.Blocks[block.Domain] = block
    st.Save(state)

    // Nota: NewEnforcer() requer Linux + build tag linux
    // enf := enforcer.NewEnforcer()
    // enf.BlockDomain(block.Domain, block.ResolvedIPs)
}
```

### Conceitos

#### `policy.Block`

Representa um bloqueio individual:

| Campo | Tipo | Descrição |
|-------|------|-----------|
| `Domain` | `string` | Domínio a ser bloqueado |
| `StartedAt` | `time.Time` | Início do bloqueio |
| `ExpiresAt` | `time.Time` | Fim do bloqueio (após essa data, o bloqueio expira) |
| `ResolvedIPs` | `[]string` | IPs resolvidos do domínio |

**Regras de negócio:**
- `IsActive()` → retorna `true` se `time.Now() < ExpiresAt`
- `CanUnblock()` → sempre retorna `false` (bloqueios não podem ser removidos manualmente)
- `RemainingTime()` → retorna o tempo restante do bloqueio

#### `enforcer.Enforcer` (Interface)

```go
type Enforcer interface {
    BlockDomain(domain string, ips []string) error
    UnblockDomain(domain string, ips []string) error
    Sync(activeBlocks map[string][]string) error
}
```

> 💡 A interface também define a constante `HeaderMarker = "# FOCUS GUARD BLOCKS - DO NOT EDIT MANUALLY"` (reservada para uso futuro em marcação de regras). A implementação Linux atual utiliza o marcador `# FOCUSGUARD:` para identificar suas entradas no `/etc/hosts`.

#### `store.Store`

Armazena o estado em formato JSON, com:

- **Gravação atômica**: escreve em arquivo temporário (`os.CreateTemp`) e renomeia (`os.Rename`)
- **Thread-safe**: utiliza `sync.RWMutex` para leitura/escrita concorrente
- **Inicialização segura**: se o arquivo não existir, retorna estado vazio
- **Permissões restritas**: arquivo salvo com `Chmod(0600)`

---

## Plataformas Suportadas

### ✅ Linux (`//go:build linux`)

Implementação completa com:
- Edição do `/etc/hosts` com entradas marcadas (`# FOCUSGUARD:`)
- Regras `iptables` e `ip6tables` para IPv4 e IPv6
- Requer privilégios root (`sudo`)
- Mensagens de erro em português

### 🚧 Windows (`//go:build windows`)

Em desenvolvimento. Atualmente apenas um stub — o arquivo existe com a build tag `windows` mas sem implementação.

---

## Testes

```bash
# Rodar todos os testes
go test ./...

# Rodar testes com cobertura
go test -cover ./...

# Rodar testes de um pacote específico
go test ./internal/policy/...
go test ./internal/store/...
```

---

## Licença

Este projeto está sob a licença MIT. Veja o arquivo [LICENSE](LICENSE) para mais detalhes.

---

## Contribuindo

Contribuições são bem-vindas! Especialmente para:

- 🪟 **Windows**: Implementar o enforcer usando `netsh advfirewall` ou similar
- 🍎 **macOS**: Suporte a `pfctl` para firewall
- ⌨️ **CLI**: Criar uma interface de linha de comando amigável
- 📊 **Relatórios**: Dashboard de bloqueios ativos e histórico

---

> **FocusGuard** — Protegendo seu foco, um bloqueio de cada vez. 🎯
