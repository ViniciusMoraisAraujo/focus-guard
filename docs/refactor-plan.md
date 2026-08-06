# Plano de Refatoração — FocusGuard (SOLID + System Design)

> **Status:** **Fases 0, 1, 2, 3, 4 e 5 concluídas** (Fase 5 em 2026-08-06) — ver seção 5;
> próximas: lifecycle do daemon (`internal/daemon` + `Run(ctx)`) e Fase 6 (CLI por comando).
> **Revisão (2026-08-05):** prioridades reordenadas — o frontend (antiga Fase 6)
> passa a ser executado antes do núcleo Go (ver seção 5).
> **Revisão (2026-08-05):** execução da Fase 0 (caracterização — vitest, 19
> testes) e Fase 1 (split `AuthProvider`/`DataProvider` + `useAction` + toast
> em módulo) — ver seção 5 e 9.
> **Revisão (2026-08-06):** execução da Fase 2 (codegen `make contract` +
> códigos de erro aditivos `Response.Code`) — ver seção 5 e 9.
> **Revisão (2026-08-06):** execução da Fase 3 (action registry: `Handler` +
> `Registry` + `ActionSpec`/`Permission` declarativos; `ipc.Server` vira
> roteador com fallback legado; `httpapi` consome `SpecFor` — B2/B6/B7) —
> ver seção 5 e 9.
> **Revisão (2026-08-06):** execução da Fase 5 (composition root: handlers de
> domínio registrados no daemon; `ipc.Server` vira transport — B3/B4-parcial) —
> ver seção 5 e 9.
> Escopo: visão completa (backend Go + frontend React + contrato IPC), com
> fases priorizadas por impacto × risco. Nenhum código foi alterado para
> produzir este documento; os números vêm de leitura do estado atual do repo
> (v0.15.2).
>
> **Regra de ouro:** refatoração é **comportamento preservado**. Nada aqui
> muda o wire protocol, o modelo de segurança (daemon admin / web user-space)
> nem os contratos de disco sem um plano explícito de compatibilidade.

---

## 1. Objetivo e princípios

O FocusGuard cresceu por acréscimo de features (25 pacotes em `internal/`,
5 binários, UI React com 12 telas) e o código está **funcional e bem testado**,
mas com pontos de acoplamento que encarecem cada feature nova:

- Adicionar uma ação IPC exige tocar **6+ arquivos em 3 camadas** (Go, TS, docs).
- Os "god objects" concentram responsabilidades e crescem a cada release.
- O contrato IPC é *string-typed*: o compilador não valida ação × payload.

Os princípios que guiam o plano:

| Princípio | Aplicação no FocusGuard |
|---|---|
| **SRP** | Cada tipo/função com um motivo único para mudar — hoje violado no `ipc.Server`, nos `main.go` e no `AppProvider` |
| **OCP** | Adicionar feature sem editar código existente — hoje toda ação nova edita os switches gigantes |
| **LSP/ISP** | Depender de interfaces mínimas e consistentes — hoje `ipc.Server` depende de `*scheduler.Scheduler` concreto e o daemon usa type-assertion ad-hoc |
| **DIP** | Módulos de alto nível não dependem de implementação — parcialmente ok (interfaces `Set*`), quebrado no scheduler e no frontend |
| **System design** | Contratos explícitos, latência de atualização, observabilidade, ciclo de vida gerenciado |

> **O que já está bom (não mexer):** interfaces `Set*` no `ipc.Server`,
> `stateStore` no scheduler, `DaemonClient` no httpapi, `enforcer.Enforcer`,
> snapshot lock-free no scheduler, testes por pacote, escrita atômica + SHA-256
> dos watchers. O plano **preserva** essas decisões.

---

## 2. Estado atual (mapa de referência)

```
CLI/Tray/Web ── IPC (JSON over socket; Request/Response universais) ──→ Daemon
                                                         │ ipc.Server (1127 linhas, switch de 34 ações)
                                                         ├─ Scheduler (fonte da verdade em RAM)
                                                         ├─ Enforcer (hosts + firewall)
                                                         ├─ Store (state.json atômico + réplica)
                                                         └─ satélites: pomodoro, preset, schedule, user,
                                                            apps, analytics, goal, tamper, dns, update
```

| Artefato | Tamanho | Papel |
|---|---|---|
| `internal/ipc/server.go` | 1127 linhas, **34 `case`** | Roteador + executor de TODAS as ações |
| `cmd/focusguard/main.go` | 1447 linhas, **41 `case`** | CLI: parse + implementação de todos os comandos |
| `cmd/focusguard-daemon/main.go` | ~900 linhas | Composition root + orquestração de update + workers |
| `internal/scheduler/scheduler.go` | ~900 linhas | Ciclo de vida de blocos + cache DNS + snapshot + refresh + DNS settings |
| `internal/httpapi/httpapi.go` + `auth.go` | ~700 linhas | Web server: estático + proxy IPC + auth + rate limit |
| `focusguard-ui/src/context.tsx` | 214 linhas | **God provider**: auth + daemonUp + status + presets + stats + toast + sessão expirada |
| `focusguard-ui/src/api/client.ts` + `types.ts` | ~450 linhas | Espelho manual do contrato Go (sem codegen) |
| `ipc.Request` / `ipc.Response` | ~40 / ~35 campos | DTO universal: `action` como string livre |

---

## 3. Diagnóstico — violações SOLID e problemas de design

### 3.1 Backend Go

| # | Onde | Princípio | Problema concreto |
|---|---|---|---|
| B1 | `internal/ipc/server.go` | **SRP** | O `Server` é roteador + validador + executor + agregador de 34 ações (bloqueio, pomodoro, presets, schedule, apps, users, DNS, update, analytics). Cada feature nova cresce o mesmo switch. |
| B2 | `internal/ipc/server.go` | **OCP** | Ação nova = editar o switch (e o `Request`/`Response`, e o CLI, e o `types.ts`). Não existe ponto de extensão: é uma cadeia de `case` que todos editam. |
| B3 | `internal/ipc/server.go` | **DIP** | Depende de `*scheduler.Scheduler` **concreto** (único dep não-injetado por interface), enquanto os satélites usam interfaces `Set*`. Testar o Server com um scheduler fake é impossível sem criar um real. |
| B4 | `cmd/focusguard-daemon/main.go` | **SRP** | O `main` acumula: composition root, `daemonUpdater.Check` (orquestração completa de update: flag, stop de guards, swap, cleanup), `guardApps`, `persistPomodoroSummary`, workers. Lógica de negócio vivendo no pacote `main` = intestável isoladamente (testes do daemon exigem shell elevado no Windows). |
| B5 | `cmd/focusguard/main.go` | **SRP/OCP** | 41 `case` de comandos num arquivo só; validação e mensagens espalhadas; comando novo = editar o gigante. |
| B6 | `internal/httpapi/httpapi.go` | **OCP** | O gate de permissão por ação (`user-list/user-add/user-remove` → admin) é um `switch` hardcoded dentro do `handleAction` — deveria ser **metadado declarativo** da própria ação (ver Fase 3 — registry). |
| B7 | `internal/httpapi/httpapi.go` | **DRY/sistema** | `actionTimeoutFor` duplica os orçamentos de timeout que o daemon já tem por ação — dois lugares para manter sincronizados. |
| B8 | `cmd/focusguard-daemon/main.go` | **LSP** | `enf.(interface{ SetOnHostsWrite(func()) })` — type assertion ad-hoc para estender o enforcer; quebra silenciosa se a implementação não suportar. Interface mínima explícita resolveria. |
| B9 | `internal/scheduler/scheduler.go` | **SRP (leve)** | Coeso, mas acumula 5 papéis (blocos, cache DNS, snapshot, refresh periódico, DNS settings). O refresh e o cache DNS podem extrair sem mudar comportamento. |
| B10 | `cmd/focusguard-daemon/main.go` | **Ciclo de vida** | `runDaemon()` é um `main` "tudo na mão": stop channels manuais, `defer` por recurso, retorno `bool shouldExit` ambíguo. Sem um lifecycle explícito (`Run(ctx) error`), a ordem de shutdown não é testável. |
| B11 | `internal/ipc/ipc.go` | **Contrato** | `Request`/`Response` universais com `action` string: o compilador não valida payload × ação, nem documenta os campos por ação. Erro no `types.ts` vira bug em runtime (o bug do splash da v0.15.1 e o da sessão v0.15.2 nasceram dessa borda manual). |
| B12 | `internal/ipc/ipc.go` | **Contrato** | Erros são `success:false` + `message` humano. A UI depende de mensagens de texto em vez de códigos de erro estáveis (`ERR_DOMAIN_CONFLICT`, `ERR_DURATION_INVALID`...) — frágil para i18n e para lógica condicional da UI. |
| B13 | `internal/store/json.go` | **Schema** | `State.Version` fixo em 1 e campos só-aditivos. Funciona, mas sem versão real não há migração formal quando um campo mudar de semântica. |

### 3.2 Frontend React

| # | Onde | Princípio | Problema concreto |
|---|---|---|---|
| F1 | `focusguard-ui/src/context.tsx` | **SRP** | `AppProvider` concentra 6 responsabilidades (auth, daemonUp, status polling, presets, stats polling, toast, sessão expirada) — qualquer mudança re-renderiza o app inteiro e o provider é intestável isoladamente. |
| F2 | `focusguard-ui/src/api/client.ts` + `types.ts` | **DIP/contrato** | Espelho **manual** do Go: todo campo novo no `ipc.Response` exige edição manual aqui (regra do AGENT.md: mudou o IPC, muda os 4 lados no mesmo commit). Sem codegen, o desalinhamento é questão de tempo. |
| F3 | `focusguard-ui/src/screens/*.tsx` | **DRY** | Padrão repetido em toda tela: `busy` local + toast de `execAction` + tratamento de erro — sem hook compartilhado. |
| F4 | `focusguard-ui/src/App.tsx` | **System design** | Navegação por `useState` + array `NAV` (ok para o tamanho atual), mas sem lazy-loading das telas: o bundle único de 464 kB cresce com cada tela. |
| F5 | `focusguard-ui/src/context.tsx` | **System design** | **Polling** 10s/60s em vez de eventos (o `docs/ui-plan.md` já prevê F3 com `/ws`). Latência de estado e tráfego desnecessário — piora com `status` pesado (1,4 MB com 1000 blocos, mitigado por gzip). |

### 3.3 Contrato e processo

| # | Onde | Problema |
|---|---|---|
| C1 | `internal/ipc` ↔ `types.ts` | Sincronização manual Go↔TS (pitfall documentado no AGENT.md). |
| C2 | `AGENT.md` §8 / `docs/release.md` | Release manual (tag → CI). Refatorações grandes por commit aumentam o risco de regressão — o plano usa **commits por ação**, cada um com a suíte verde. |
| C3 | Observabilidade | Só log de arquivo + pprof opt-in (`FG_PPROF`). Sem métricas de latência por ação IPC/HTTP para detectar regressões de performance nas refatorações. |

---

## 4. Arquitetura-alvo

### 4.1 Núcleo: action registry (substitui o switch)

Cada ação vira um `Handler` autocontido — **OCP de verdade**:

```go
// internal/ipc/spec.go (novo) — metadados declarativos por ação, importados
// pelo daemon (execução) E pelo focusguard-web (authz + timeout): os dois
// binários compartilham o pacote, então a tabela é fonte única de verdade.
type ActionSpec struct {
    Action     string
    Permission Permission // PermPublic / PermAuthenticated / PermSelf / PermAdmin
    SelfField  string     // só para PermSelf: campo do Request que é o "recurso próprio"
    Timeout    time.Duration
}

// internal/ipc/action.go (novo) — executor registrado no daemon.
type Handler interface {
    Action() string                          // "block", "user-set-password", ...
    Validate(*Request) error                 // validação pura por ação (payload × campos)
    Handle(ctx context.Context, req *Request) (*Response, error)
}

// internal/ipc/registry.go (novo)
type Registry struct{ m map[string]Handler }
func (r *Registry) Register(h Handler)      // um lugar só para registrar
func (r *Registry) Get(action string) (Handler, bool)
```

- `ipc.Server` vira **roteador fino**: decodifica → `registry.Get` → `Validate` → `Handle`. O Server deixa de conhecer o `*scheduler.Scheduler` concreto (B3) — cada handler recebe por construtor só as interfaces de que precisa (DIP).
- Adicionar ação = criar `*_handler.go` + 1 linha na tabela `specs` + `registry.Register` no boot. **Nada** mais muda (sem tocar `Server`, `httpapi`, CLI ou `types.ts`).
- `httpapi` consome `ipc.SpecFor(action)` (a mesma tabela — daemon e web importam `internal/ipc`) — elimina `actionTimeoutFor` (B7) e o `switch` de permissão (B6) **sem duplicação** e sem mudar a fronteira de segurança (o web continua sendo quem impõe authz).
- Implementação completa com as ações `block` e `user-set-password` na **seção 8**.

### 4.2 Serviços de domínio (SRP)

Os casos do switch migram, um a um, para serviços coesos (padrão *strangler*):

| Serviço | Ações | Fonte atual |
|---|---|---|
| `blocks.Service` | `block`, `block-all`, `extend/replace` | `ipc.Server` + `scheduler` |
| `pomodoro.Service` | `pomodoro`, `pomodoro-stop`, `pomodoro-defaults` | `ipc.Server` + `pomodoro` |
| `schedule.Service` | `schedule-list/add/remove/import` | `ipc.Server` + `schedule` |
| `users.Service` | `user-*` | `ipc.Server` + `user` |
| `dns.Service` | `dns-*` | `ipc.Server` + `dnsserver` |
| `presets.Service` | `presets`, `preset-add/remove` | `ipc.Server` + `preset` |
| `apps.Service` | `apps-list/add/remove` | `ipc.Server` + `apps` |
| `analytics.Service` | `stats`, `missions`, `sessions`, `tamper-log` | `ipc.Server` + `analytics` |
| `update.Service` | `update`, `update-check` | `ipc.Server` + `update` + **main do daemon** |

Cada serviço tem: validação própria, testes unitários próprios (sem daemon elevado)
e um `Handle` que recebe só o que precisa (DIP — deps por interface).

### 4.3 Composition root enxuto (daemon)

```go
// cmd/focusguard-daemon/main.go → runDaemon() vira:
func runDaemon(ctx context.Context) error {
    cfg := loadConfig()                       // paths, porta, versão (estruturado)
    d := daemon.New(cfg)                      // internal/daemon (novo): monta e roda
    return d.Run(ctx)                         // lifecycle: start ordenado, shutdown ordenado
}
```

- Lógica de update sai de `main` → `internal/update` (já existe o pacote): `daemonUpdater` vira `update.Orchestrator`.
- Workers (schedule, pomodoro-completion, update-check) ganham um lifecycle comum (início/parada explícita, testável).
- O `switch` de permissão e os `Set*` viram registro declarativo no boot.

### 4.4 CLI (cmd/focusguard)

- `main.go` (1447 linhas) → `commands/` com **um arquivo por comando** (`block.go`, `pomodoro.go`, `schedule.go`, `user.go`, `web.go`, `update.go`...), cada um com teste próprio.
- Tabela de comandos `map[string]Command{Name, Run(ctx, args), Usage}` — comando novo = novo arquivo + registro (OCP).

### 4.5 Contrato IPC v2 (compatível)

- **Wire protocol idêntico** (mesmo JSON) — mudança interna apenas: `Handler` valida e tipa por ação.
- **Códigos de erro** aditivos: `Response.Code string` (`"ERR_DOMAIN_CONFLICT"`, `"ERR_DURATION_INVALID"`, `"ERR_ALREADY_BLOCKED"`...) mantendo `message` humano. A UI passa a checar código, não texto.
- **Codegen Go → TS**: script pequeno (stdlib, em `scripts/`, igual `verifyicon`) que gera `types.ts` a partir dos structs Go — elimina a borda manual (C1) e o pitfall do AGENT.md vira checagem de CI (`make contract` + diff).

### 4.6 Frontend

- `AppProvider` → separar em **`AuthProvider`** + **`DataProvider`** (hooks `useAuth`, `useDaemonStatus`, `useStatus`, `usePresets`, `useStats`), cada um com escopo e ciclo de vida próprios (F1).
- Hook compartilhado `useAction()` que encapsula `busy` + toast + `execAction` (F3).
- **Eventos em tempo real (F3 do ui-plan — executada na Fase 7 deste plano)**: `/ws` ou SSE com eventos de mudança (block expirou, pomodoro, schedule) no lugar do polling agressivo (F5). Depende do event hub no daemon; o `context` atual já isola a decisão, então a troca é localizada e fica para depois do núcleo Go.
- Lazy-load das telas (`React.lazy`) para conter o bundle (F4) — baixa prioridade.

---

## 5. Fases de implementação (ordem recomendada)

> Cada fase termina com **a suíte verde** e um commit convencional por mudança
> coesa (regra do AGENT.md §7). O wire protocol é **preservado em todas as
> fases** — as mudanças de contrato da Fase 2 são **aditivas** (campos novos
> opcionais; mesmo JSON).

| Fase | Entrega | Impacto | Risco | Esforço |
|---|---|---|---|---|
| **0. Caracterização** ✅ | Testes que congelam o comportamento atual — frontend (`client.ts`/`AppProvider`) e backend (casos do switch) | — | baixo | M |
| **1. Frontend — responsabilidades** ✅ ⭐ | `AuthProvider`/`DataProvider` + hooks (`useAuth`, `useData`) + `useAction()` compartilhado + toast em módulo | Alto (F1/F3) | **baixo** (independe do núcleo Go; valida com `tsc`) | M |
| **2. Frontend — contrato assistido** ✅ | Codegen Go→TS (`make contract` + checagem no CI) + códigos de erro aditivos (`Response.Code`) | Alto (F2/B12/C1) | baixo (aditivo, retrocompat) | M |
| **3. Registry de ações** ✅ | `Handler` + `Registry` + `ActionSpec`/`Permission` declarativos (`spec.go`); `ipc.Server` vira roteador (registry-first + fallback legado); `httpapi` consome `SpecFor` (B2/B6/B7) | Alto (OCP B2/B6/B7) | **baixo** (mesmo contrato; testes por ação) | M |
| **4. Serviços de domínio** | Migrar cada `case` → serviço coeso (strangler, um por commit) | Alto (SRP B1/B3) | médio (mexe em lógica quente: block) | G |
| **5. Composition root** ✅ | Handlers de domínio registrados no daemon (composition root) — `ipc.Server` vira transport (B3, OCP). Falta o lifecycle (`internal/daemon` + `Run(ctx)`, B4/B10) | Alto | médio | G |
| **6. CLI por comando** | `commands/` com um arquivo por comando + tabela | Médio (B5) | baixo | M |
| **7. Eventos em tempo real** | `/ws` ou SSE no lugar do polling — **depende do event hub no daemon** (F3 do ui-plan) | Médio (F5) | médio | G |
| **8. Observabilidade** (opcional) | Métricas de latência por ação IPC/HTTP, logs estruturados leves | Médio (C3) | baixo | P |

**Ordem sugerida de execução:** ~~0 → 1 → 2 → 3~~ (concluídas) → 4 (começando
pelas ações de menor risco: `user-*`, `dns-*`, depois
`presets`/`apps`/`schedule`, e por último `block`/`pomodoro`) → 5 → 6 → 7 → 8.

> ⭐ **Frontend primeiro (decisão 2026-08-05):** as Fases 1-2 não dependem do
> núcleo Go (o contrato HTTP atual é estável) e atacam a dívida que mais
> aparece em produção hoje (espelho manual do `types.ts`, provider com 6
> responsabilidades). O `/ws` (Fase 7) fica para depois porque exige o event
> hub no daemon — até lá, o polling atual segue como está.
>
> A Fase 2 é **aditiva** (campo `code` opcional no JSON; o codegen apenas
> gera o `types.ts` — o Go continua sendo a fonte de verdade). Retrocompat
> mantida: daemon novo ↔ CLI/web velhos continuam falando o mesmo JSON.

---

## 6. Estratégia de validação e Definition of Done

Por fase (manter as regras do AGENT.md §5):

- [ ] `go build ./... && go vet ./...` verdes
- [ ] `gofmt -l` sem output
- [ ] `go test ./... -count=1 -timeout=60s` verdes (elevado no Windows para o daemon)
- [ ] `cd focusguard-ui && npx tsc --noEmit` verde (quando tocar no front)
- [ ] Testes de caracterização (Fase 0) **inalterados** após cada refatoração
- [ ] Nenhum artefato de build commitado (`bin/`, `.exe`)
- [ ] Commit convencional por mudança coesa; sem misturar docs + code
- [ ] Ao final de cada fase, uma **release patch** opcional para isolar risco (padrão já usado: v0.15.1/v0.15.2)

**Métricas de sucesso do programa:**

| Métrica | Hoje | Alvo |
|---|---|---|
| `internal/ipc/server.go` | 1127 linhas / 34 casos | < 350 (roteador + registry) |
| `cmd/focusguard/main.go` | 1447 linhas / 41 casos | < 200 (parse + tabela) |
| `cmd/focusguard-daemon/main.go` | ~900 linhas | < 300 (boot enxuto) |
| Linhas para adicionar uma ação nova | 6+ arquivos (Go ×3, TS ×2, docs) | 1 handler + 1 registro (+ codegen automático) |
| Contrato Go↔TS | manual | gerado por `make contract` |

---

## 7. Riscos e não-objetivos

**Riscos da refatoração:**

| Risco | Mitigação |
|---|---|
| Regressão na lógica quente (`block`, `pomodoro`, expiração) | Fase 0 (caracterização) + migração strangler ação por ação, commit atômico |
| `Request`/`Response` universais encorajam acoplamento | Handlers recebem `*Services` tipado por domínio, não o DTO universal |
| Mudança de contrato quebra CLI/tray/web antigos | Fase 2 aditiva (campo `code` opcional; mesmo JSON); CI valida os 4 lados no mesmo commit |
| Testes do daemon exigem shell elevado (Windows) | Serviços testáveis sem daemon (Fase 4) reduzem a dependência da suíte elevada |
| Frontend à frente do núcleo Go não atrasa o backend | As Fases 1-2 são independentes e pequenas (M); o backend continua com rede de segurança (Fase 0) |
| Refatoração vira "reescrita" (scope creep) | Cada fase é **comportamento preservado**; ganhos de arquitetura vêm depois de cada marco |

**Não-objetivos (respeitar AGENT.md §0/§9):**

- ❌ Não reintroduzir `pendingRestart`/watcher no fluxo de update.
- ❌ Não mudar o modelo de segurança (daemon = único processo admin; web/tray user-space, sem manifest).
- ❌ Não adicionar dependências novas sem necessidade — **stdlib first** (registry, lifecycle e codegen são stdlib).
- ❌ Não mexer em `.syso`, `versioninfo.json`, `focusguard.ico/png` (só via `make icon`/`make winres`).
- ❌ Não criar "desbloqueio manual" nem atalho para desfazer bloqueio antes do fim.
- ❌ Não mudar o schema `state.json` sem plano de migração (e o plano não prevê mudança de schema).

---

## 8. Exemplo detalhado — Handler do action registry (`block` e `user-set-password`)

> Refinamento do esboço da seção 4.1. Mostra a forma final (metadados
> compartilhados com o `focusguard-web` via `internal/ipc`) e como o `case`
> atual de cada ação vira um handler isolado **sem mudar o wire protocol nem
> as mensagens** — comportamento preservado.

### 8.1 Contrato compartilhado (`internal/ipc`)

```go
// internal/ipc/spec.go
package ipc

type Permission int

const (
	// PermPublic: sem sessão (health, ping, login, auth-status).
	PermPublic Permission = iota
	// PermAuthenticated: qualquer sessão válida.
	PermAuthenticated
	// PermSelf: sessão válida; não-admin só sobre o recurso de SelfField
	// (ex.: user-set-password); o admin passa sempre.
	PermSelf
	// PermAdmin: só o admin (user-list/user-add/user-remove).
	PermAdmin
)

type ActionSpec struct {
	Action     string
	Permission Permission
	SelfField  string        // apenas para PermSelf: campo do Request que é o "recurso próprio"
	Timeout    time.Duration // orçamento do proxy web (substitui actionTimeoutFor)
}

// specs é a única fonte de verdade das metainformações. O daemon valida no
// boot que todo handler registrado tem spec (e vice-versa) — drift vira erro
// de boot, não bug em runtime.
var specs = map[string]ActionSpec{
	"block":             {Action: "block", Permission: PermAuthenticated, Timeout: 30 * time.Second},
	"block-all":         {Action: "block-all", Permission: PermAuthenticated, Timeout: 30 * time.Second},
	"status":            {Action: "status", Permission: PermAuthenticated, Timeout: 15 * time.Second},
	"user-list":         {Action: "user-list", Permission: PermAdmin, Timeout: 5 * time.Second},
	"user-add":          {Action: "user-add", Permission: PermAdmin, Timeout: 5 * time.Second},
	"user-remove":       {Action: "user-remove", Permission: PermAdmin, Timeout: 5 * time.Second},
	"user-set-password": {Action: "user-set-password", Permission: PermSelf, SelfField: "user_name", Timeout: 5 * time.Second},
	"update":            {Action: "update", Permission: PermAuthenticated, Timeout: 150 * time.Second},
	// ... as demais (presets, apps, schedule, dns, pomodoro, stats, tamper-log ...)
}

// SpecFor devolve os metadados da ação. Ausência = ação web-only
// (user-verify) ou desconhecida → o proxy responde 403/404 (allowlist).
func SpecFor(action string) (ActionSpec, bool) {
	s, ok := specs[action]
	return s, ok
}
```

```go
// internal/ipc/action.go
package ipc

// Handler é o executor de UMA ação, registrado no daemon. Validate é puro
// (sem dependências nem efeitos); Handle recebe as interfaces por construtor.
type Handler interface {
	Action() string
	Validate(*Request) error
	Handle(ctx context.Context, req *Request) (*Response, error)
}

// ActionError carrega o código estável + a mensagem humana (B12). O roteador
// converte para Response{Success:false, Code, Message}; a UI passa a checar o
// código em vez do texto (códigos aditivos desde a Fase 2 — o campo Code pode
// ficar vazio nas fases iniciais sem quebrar nada).
type ActionError struct {
	Code    string
	Message string
}

func (e *ActionError) Error() string { return e.Message }

func Err(code, message string) *ActionError { return &ActionError{Code: code, Message: message} }

// Códigos estáveis (aditivos).
const (
	CodeDurationInvalid = "ERR_DURATION_INVALID"
	CodeDomainRequired  = "ERR_DOMAIN_REQUIRED"
	CodeDomainConflict  = "ERR_DOMAIN_CONFLICT"
	CodeInvalid         = "ERR_INVALID"
	CodeNotConfigured   = "ERR_NOT_CONFIGURED"
	CodeUnknownAction   = "ERR_UNKNOWN_ACTION"
)
```

```go
// internal/ipc/registry.go
package ipc

type Registry struct {
	mu   sync.RWMutex
	byID map[string]Handler
}

func NewRegistry() *Registry { return &Registry{byID: make(map[string]Handler)} }

func (r *Registry) Register(h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[h.Action()] = h
}

func (r *Registry) Get(action string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.byID[action]
	return h, ok
}
```

### 8.2 Ação `block` (hoje: `internal/ipc/server.go` linhas 389–461)

O caso atual faz 4 coisas num bloco: valida duração, resolve preset em lote,
`--extend`, e o conflito *ask-first* com `--replace`. O handler reproduz 1:1
(ordem de validação e mensagens idênticas) com as dependências injetadas:

```go
// internal/blocks/handler.go (novo — Fase 5)
package blocks

import (
	"context"
	"fmt"
	"time"

	"focusguard/internal/ipc"
	"focusguard/internal/policy"
	"focusguard/internal/preset"
)

// Blocker é a superfície mínima que o handler exige do scheduler (DIP): em
// vez de depender do *scheduler.Scheduler concreto (B3), só os 4 métodos que
// a ação block usa. O *scheduler.Scheduler satisfaz esta interface por
// estrutura — nenhuma mudança nele.
type Blocker interface {
	Block(domain string, duration time.Duration) (*policy.Block, error)
	BlockDomains(domains []string, duration time.Duration) ([]policy.Block, error)
	ExtendBlock(domain string, duration time.Duration) (*policy.Block, error)
	ActiveBlock(domain string) *policy.Block
}

// Catalog resolve presets pelo nome (mesma superfície do ipc.PresetManager).
type Catalog interface {
	Resolve(name string) (preset.Preset, error)
}

type Handler struct {
	blocks  Blocker
	catalog Catalog
}

func New(blocks Blocker, catalog Catalog) *Handler {
	return &Handler{blocks: blocks, catalog: catalog}
}

func (h *Handler) Action() string { return "block" }

// Validate é puro e preserva a ORDEM de erro do switch atual: duração é
// validada antes do alvo (um request com ambos inválidos devolve o erro de
// duração, como hoje). Preset é alvo válido sozinho (sem domain).
func (h *Handler) Validate(req *ipc.Request) error {
	d, err := time.ParseDuration(req.Duration)
	if err != nil || d <= 0 {
		return ipc.Err(ipc.CodeDurationInvalid, "Duration invalid. Ex: --duration 4h, 30m")
	}
	if req.Preset == "" && req.Domain == "" {
		return ipc.Err(ipc.CodeDomainRequired, "Informe um domínio ou --preset para bloquear.")
	}
	return nil
}

func (h *Handler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	// Validate garantiu o parse; aqui re-parse é barato e mantém o Handle
	// independente caso alguém chame sem Validate.
	d, _ := time.ParseDuration(req.Duration)

	switch {
	case req.Preset != "":
		return h.blockPreset(req, d)
	case req.Extend:
		return h.extend(req, d)
	default:
		return h.blockOrConflict(req, d)
	}
}

func (h *Handler) blockPreset(req *ipc.Request, d time.Duration) (*ipc.Response, error) {
	p, err := h.catalog.Resolve(req.Preset)
	if err != nil {
		return nil, err
	}
	blocks, err := h.blocks.BlockDomains(p.Domains, d)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		// Defensivo: nunca indexar blocks[0] (pânico se um dia vier vazio).
		return &ipc.Response{Success: true, Message: fmt.Sprintf("Preset %s: nenhum domínio novo bloqueado", p.Name)}, nil
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf(
		"Preset %s bloqueado (%d domínios) até %s", p.Name, len(blocks),
		blocks[0].ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}

func (h *Handler) extend(req *ipc.Request, d time.Duration) (*ipc.Response, error) {
	block, err := h.blocks.ExtendBlock(req.Domain, d)
	if err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf(
		"Domain %s extended until %s", block.Domain,
		block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}

func (h *Handler) blockOrConflict(req *ipc.Request, d time.Duration) (*ipc.Response, error) {
	// Ask-first: domínio já bloqueado é CONFLITO para o usuário resolver
	// (somar/substituir), não sobrescrita silenciosa. --replace pula.
	if !req.Replace {
		if existing := h.blocks.ActiveBlock(req.Domain); existing != nil {
			return &ipc.Response{
				Success:       false,
				Code:          ipc.CodeDomainConflict, // aditivo (Fase 2)
				Conflict:      true,
				ConflictBlock: existing,
				Message: fmt.Sprintf("Domínio já bloqueado até %s. Use --extend para somar ou --replace para reiniciar.",
					existing.ExpiresAt.Local().Format("15:04:05 02/01/2006")),
			}, nil
		}
	}
	block, err := h.blocks.Block(req.Domain, d)
	if err != nil {
		return nil, err
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf(
		"Domain %s blocked  %s", block.Domain,
		block.ExpiresAt.Local().Format("15:04:05 02/01/2006"))}, nil
}
```

### 8.3 Ação `user-set-password` (hoje: `internal/ipc/server.go` linhas 613–625)

```go
// internal/users/handler.go (novo — Fase 5)
package users

import (
	"context"
	"fmt"
	"strings"

	"focusguard/internal/ipc"
)

// minPasswordLen espelha a regra do user.Store (minPasswordLen=8) — aqui é só
// fail-fast sem chamar o daemon; o store continua a autoridade final.
const minPasswordLen = 8

// UserStore é a superfície mínima (DIP): só o que esta ação usa.
type UserStore interface {
	SetPassword(username, password string) error
}

type Handler struct{ store UserStore }

func New(store UserStore) *Handler { return &Handler{store: store} }

func (h *Handler) Action() string { return "user-set-password" }

func (h *Handler) Validate(req *ipc.Request) error {
	if strings.TrimSpace(req.UserName) == "" {
		return ipc.Err(ipc.CodeInvalid, "informe o nome de usuário")
	}
	if len(req.UserPassword) < minPasswordLen {
		return ipc.Err(ipc.CodeInvalid, fmt.Sprintf("a senha precisa de ao menos %d caracteres", minPasswordLen))
	}
	return nil
}

func (h *Handler) Handle(ctx context.Context, req *ipc.Request) (*ipc.Response, error) {
	if h.store == nil {
		return nil, ipc.Err(ipc.CodeNotConfigured, "usuários não configurados")
	}
	if err := h.store.SetPassword(req.UserName, req.UserPassword); err != nil {
		return nil, err // user.Store já devolve mensagens PT-BR prontas
	}
	return &ipc.Response{Success: true, Message: fmt.Sprintf("Senha de %s atualizada", req.UserName)}, nil
}
```

> **Regra `PermSelf` (authz fica no web, como hoje):** o daemon continua
> confiando no IPC local (CLI/tray/web têm o mesmo nível de acesso); quem
> impõe "não-admin só troca a própria senha" é o proxy web, que lê
> `SelfField: "user_name"` do spec — mesma lógica do `switch` atual do
> `httpapi.handleAction`, agora declarativa.

### 8.4 Roteador pós-refatoração (`ipc.Server.handleConnection`)

```go
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(&Response{Success: false, Message: "Request invalid"})
		return
	}

	h, ok := s.registry.Get(req.Action)
	if !ok {
		_ = json.NewEncoder(conn).Encode(&Response{
			Success: false, Code: CodeUnknownAction, Message: "ação desconhecida: " + req.Action,
		})
		return
	}
	if err := h.Validate(&req); err != nil {
		writeError(conn, err)
		return
	}
	resp, err := h.Handle(context.Background(), &req)
	if err != nil {
		writeError(conn, err)
		return
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// writeError converte ActionError em Response com código; erros comuns
// viram success:false com a mensagem original (comportamento atual).
func writeError(conn net.Conn, err error) {
	var ae *ActionError
	if errors.As(err, &ae) {
		_ = json.NewEncoder(conn).Encode(&Response{Success: false, Code: ae.Code, Message: ae.Message})
		return
	}
	_ = json.NewEncoder(conn).Encode(&Response{Success: false, Message: err.Error()})
}
```

> O conflito `ask-first` não é erro: o handler devolve a `Response` com
> `Conflict:true` + `ConflictBlock` diretamente (como o switch faz hoje).

### 8.5 Proxy web (`httpapi.handleAction`): authz + timeout declarativos

```go
// handleAction (após a Fase 3 — registry): o switch de permissão e o actionTimeoutFor
// somem — os metadados vêm de ipc.SpecFor (B6/B7 resolvidos).
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	// ... Content-Type + decode idênticos aos de hoje ...

	spec, ok := ipc.SpecFor(req.Action)
	if !ok {
		// user-verify (web-only) e ações desconhecidas: o proxy deixa de
		// encaminhar (vira allowlist por spec) — 403, como o case de hoje.
		writeJSONError(w, http.StatusForbidden, "use /api/login para autenticar")
		return
	}

	sess, ok := sessionFrom(r.Context()) // requireAuth já garantiu a sessão
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "não autenticado — faça login")
		return
	}
	switch spec.Permission {
	case ipc.PermAdmin:
		if !sess.isAdmin {
			writeJSONError(w, http.StatusForbidden, "apenas o administrador gerencia usuários")
			return
		}
	case ipc.PermSelf:
		if !sess.isAdmin && !strings.EqualFold(req.UserName, sess.username) {
			writeJSONError(w, http.StatusForbidden, "você só pode alterar a própria senha")
			return
		}
	}

	resp, err := s.client.SendWithTimeout(req, spec.Timeout) // antes: actionTimeoutFor(req)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"daemon indisponível — verifique se o serviço FocusGuard está rodando")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
```

> **Nota de timeout:** o `Timeout` do spec deve ser **≥ o orçamento interno do
> daemon** para a mesma ação — o proxy só espera a resposta chegar (hoje:
> update 150s no proxy vs 120s no daemon). Isso evita "daemon indisponível"
> falso quando o daemon ainda está processando.

### 8.6 Registro no boot do daemon

```go
// cmd/focusguard-daemon/main.go (após Fases 4-5) — os 9 Set* e o switch somem.
reg := ipc.NewRegistry()
reg.Register(blocks.New(sched, presetStore))   // *scheduler.Scheduler implementa blocks.Blocker
reg.Register(users.New(userStore))
reg.Register(presets.New(presetStore))
reg.Register(apps.New(appsStore, pg))
reg.Register(dns.New(dnsSrv, sched))
reg.Register(pomodoro.NewService(pomo, pomoPrefs))
reg.Register(schedule.NewService(scheduleMgr, presetResolver{store: presetStore}, sched))
reg.Register(analytics.NewService(rec))
reg.Register(update.NewService(newDaemonUpdater(binaryPath, newUpdater(updateOwner, updateRepo))))

server := ipc.NewServer(reg) // em vez de NewServer(sched) + Set*
```

O boot valida o fechamento: todo handler registrado tem `SpecFor` (e vice-versa)
— um handler esquecido na tabela `specs` quebra o boot com mensagem clara, em
vez de virar 403 silencioso no web.

### 8.7 Testes dos handlers (sem daemon elevado — a suíte deixa de depender da shell admin)

```go
// internal/users/handler_test.go
package users

import (
	"context"
	"errors"
	"testing"

	"focusguard/internal/ipc"
)

type fakeStore struct{ lastUser string }

func (f *fakeStore) SetPassword(username, password string) error {
	f.lastUser = username
	return nil
}

func TestUserSetPassword_RejeitaSemUsuario(t *testing.T) {
	h := New(&fakeStore{})
	err := h.Validate(&ipc.Request{UserPassword: "nova-senha-123"})
	var ae *ipc.ActionError
	if !errors.As(err, &ae) || ae.Code != ipc.CodeInvalid {
		t.Fatalf("esperava ERR_INVALID, got %v", err)
	}
}

func TestUserSetPassword_OK(t *testing.T) {
	st := &fakeStore{}
	h := New(st)
	resp, err := h.Handle(context.Background(), &ipc.Request{
		UserName: "joao", UserPassword: "nova-senha-123",
	})
	if err != nil || resp == nil || !resp.Success {
		t.Fatalf("esperava sucesso, got resp=%v err=%v", resp, err)
	}
	if st.lastUser != "joao" {
		t.Fatalf("SetPassword chamado com %q", st.lastUser)
	}
}
```

```go
// internal/blocks/handler_test.go (trecho — conflito ask-first)
type fakeBlocker struct{ active *policy.Block }

func (f *fakeBlocker) Block(domain string, d time.Duration) (*policy.Block, error) { return nil, nil }
func (f *fakeBlocker) BlockDomains(domains []string, d time.Duration) ([]policy.Block, error) { return nil, nil }
func (f *fakeBlocker) ExtendBlock(domain string, d time.Duration) (*policy.Block, error) { return nil, nil }
func (f *fakeBlocker) ActiveBlock(domain string) *policy.Block { return f.active }

type fakeCatalog struct{}

func (fakeCatalog) Resolve(name string) (preset.Preset, error) { return preset.Preset{}, nil }

func TestBlock_ConflitoAskFirst(t *testing.T) {
	existing := &policy.Block{Domain: "youtube.com", ExpiresAt: time.Now().Add(time.Hour)}
	h := New(&fakeBlocker{active: existing}, fakeCatalog{})
	resp, err := h.Handle(context.Background(), &ipc.Request{
		Action: "block", Domain: "youtube.com", Duration: "4h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Success || !resp.Conflict || resp.ConflictBlock == nil {
		t.Fatalf("esperava conflito ask-first, got %+v", resp)
	}
}
```

### 8.8 Checklist de migração de uma ação (strangler — uma ação por commit)

1. **Fase 0:** escrever o teste de caracterização do `case` atual (congela o
   comportamento; suíte fica verde antes de mexer).
2. Criar `internal/<dominio>/handler.go` com o mesmo comportamento e
   mensagens + testes unitários (fakes das interfaces).
3. Registrar no `Registry` e trocar o `case` antigo por `registry.Get` (ou
   já usar o router novo, mantendo os demais `case` até migrarem).
4. Remover o `case`; adicionar a linha no `specs`; rodar a suíte completa
   (`go test ./...` + vet + frontend se tocar contrato).
5. Commit atômico: `refactor(ipc): migrate <ação> action to handler` — sem
   mudança de wire protocol nem de mensagens.

> Ordem sugerida de migração (menor risco primeiro): `user-*`, `dns-*`,
> `presets`, `apps`, `schedule`, `analytics`, `pomodoro` e por último `block`/
> `block-all`/`update` (lógica quente).

---

## 9. Progresso e próximo passo

### Fase 0 — Caracterização (✅ concluída em 2026-08-05)

- `focusguard-ui/vitest.config.ts` + devDeps de teste (vitest 2, jsdom, RTL).
- `focusguard-ui/src/api/client.test.ts` (13 testes) + `focusguard-ui/src/context/context.test.tsx` (6 testes) — 19 testes **verdes antes e depois** da Fase 1 (comportamento congelado).
- Falta: caracterização dos `case` do `ipc.Server` (backend — junto da Fase 3).

### Fase 1 — Frontend: responsabilidades (✅ concluída em 2026-08-05)

- `src/context.tsx` (god provider) → `src/context/{types.ts, auth-context.tsx, data-context.tsx, index.tsx}`: **AuthProvider** (auth/login/logout/sessão expirada) + **DataProvider** filho (daemonUp/status/presets/stats/refresh; ping 10s sempre, dados só autenticado, limpeza no logout).
- Toast → `src/lib/toast.ts` (módulo puro sonner).
- `src/hooks/use-action.ts`: `busy` + `execAction` + toast (usado em Pomodoro/Agenda/Apps/Presets).
- `App.tsx` + 12 telas migradas para `useAuth`/`useData`/`useToast`; `useApp` mantido só como compat no barrel.
- Validação: 19/19 testes, `tsc --noEmit`, `vite build` — verdes.
- Revisão (code-reviewer): checkpoints verificados — `refresh` estável (`useCallback` deps `[]`), ordem `authenticatedRef` → carga, limpeza síncrona no logout, carga imediata no login, cleanup de listeners/intervalos (StrictMode-safe).

### Fase 2 — Frontend: contrato assistido (✅ concluída em 2026-08-06)

- **Codegen Go → TS**: `scripts/gen-contract/main.go` (stdlib, `go/ast`; padrão `verifyicon` com `//go:build ignore`) gera `focusguard-ui/src/api/types.ts` a partir dos structs Go do `internal/ipc` + domínios (policy, preset, pomodoro, analytics, schedule, tamper). `make contract` regenera; `make contract-check` falha se houver drift (adicionado ao CI em `.github/workflows/release.yml` antes do GoReleaser).
  - Mapeia `time.Time`→string (RFC3339), `time.Duration`→number (nanosegundos), `omitempty`/ponteiros→opcional, tipos string nomeados com consts→literal union (`phase: "work" | "rest"`).
  - **Corrigiu drift real do espelho manual**: `Block.allowlist`, `ApiRequest.name`, `ApiResponse.user_is_admin` estavam ausentes no `types.ts` antigo (F2/C1 resolvidos — o Go é a fonte da verdade).
  - Geração determinística: ordem de campos = ordem do struct Go; preserva CRLF se o arquivo atual for CRLF.
- **Códigos de erro aditivos (B12)**: `internal/ipc/codes.go` (CodeDurationInvalid, CodeDomainRequired, CodeDomainConflict, CodeInvalid, CodeNotConfigured, CodeUnknownAction) + campo opcional `Response.Code` (`json:"code,omitempty"`). Populados nos principais caminhos de erro do `server.go` (bloqueio: duração/domínio/conflito; ações "não configurado"; validações de payload: user-verify, schedule-import, goal-set, dns-set-upstream, pomodoro). Mensagens inalteradas — comportamento preservado.
- **Frontend**: `ApiResponse.code` chega pelo codegen; `execAction` e `useAction` expõem `code` para a UI ramificar por código em vez de texto.
- Validação: `go build ./...`, `go vet ./...`, `go test ./internal/...` (exceto daemon elevado), `tsc --noEmit`, vitest 19/19 — verdes. Commits:
  - `feat(contract): generate api types from Go structs (make contract)`
  - `feat(ipc): add additive error codes to IPC responses`

### Fase 3 — Registry de ações (✅ concluída em 2026-08-06)

- **Infra**: `internal/ipc/spec.go` (`Permission`, `ActionSpec`, tabela `specs`
  com as 33 ações encaminháveis pelo web — `user-verify` deliberadamente
  ausente, vira allowlist por spec; `SpecFor`/`SpecActions`), `action.go`
  (`Handler`, `ActionError`, `Err`, `writeError`), `registry.go` (`Registry`
  com `Register`/`Get`/`Actions`/`ValidateSpecs`).
- **Roteador fino**: `ipc.Server.handleConnection` despacha por
  `registry.Get → Validate → Handle` (erros via `writeError`); as ações ainda
  não migradas caem no `dispatchLegacy` (strangler) — wire protocol e
  mensagens inalterados.
- **Migração inicial (10 ações de baixo risco)**: `ping`, `presets`,
  `preset-add/remove`, `apps-list/add/remove`, `tamper-log`, `goal-get/set`
  → handlers em `internal/ipc/handlers.go` (adaptador `funcHandler`),
  registrados no `NewServer` (o daemon segue com `NewServer(sched)` + `Set*`
  — composition root é a Fase 5). Switch: 34 → 24 cases.
- **B6/B7 (httpapi)**: o `switch` de permissão e o `actionTimeoutFor` somem —
  `handleAction` lê `ipc.SpecFor(req.Action)` (`Permission` + `Timeout`). Ação
  sem spec (user-verify ou desconhecida) → 403 (allowlist; teste
  `TestAction_UnknownAction_Forbidden`). Testes de timeout agora comparam com
  `ipc.SpecFor(...).Timeout`.
- **Fechamento**: `TestServer_RegisteredHandlersHaveSpecs` (todo handler
  registrado tem spec — direção registry→specs) e, a partir da Fase 4,
  `TestServer_SpecsAllHaveHandlers` (a inversa) + `ValidateRegistry` no boot.
- **Caracterização backend (pendência da Fase 0)**: coberta pela suíte
  existente do `server_test.go` (presets/apps/tamper/goal já tinham testes —
  os mesmos testes agora exercitam o roteador + handlers).
- Validação: `go vet` + `go test` de `internal/ipc` e `internal/httpapi`
  verdes; `contract-check` em dia (nenhum struct mudou). Commits:
  - `feat(ipc): add action registry with declarative specs (Handler, Registry, ActionSpec)`
  - `refactor(web): declarative action permissions and timeouts via ipc specs`
  - `refactor(ipc): route actions through the registry, migrate low-risk handlers`

### Fase 4 — Serviços de domínio (✅ concluída em 2026-08-06)

O switch legado `dispatchLegacy` foi eliminado — **toda ação conhecida agora é
um `Handler` no registry**; ação desconhecida vira `CodeUnknownAction`.

1. **Migração dos 12 cases restantes** → handlers em `internal/ipc/` com
   adaptador `funcHandler`, comportamento 1:1 (mensagens, códigos, ordem):
   - `analytics` (`stats`/`missions`/`sessions`) e `schedule` (`schedule-*`) →
     serviços de domínio `analytics.Service`/`schedule.Service` (ipc-free,
     erros via `*ipcerr.Error`).
   - `pomodoro` (`pomodoro`/`pomodoro-defaults`/`pomodoro-stop`) → novo
     `pomodoro.Service` (ipc-free; validação `maxPomodoroMinutes`/`maxPomodoroCycles`
     movida do switch).
   - `update`/`update-check` → novo `update.Service` (ipc-free; bridge
     `updateCheckerBridge` converte wire→domínio; latch `updateApplied` +
     `dispatchUpdateHook` preserva o restart pós-resposta).
   - `block`/`block-all`/`status`/`user-*`/`dns-*` → adapters inline no ipc
     (goal-style). Os pacotes `internal/users|blocks|dns|presets|apps`
     (esboços da seção 8) implementam `ipc.Handler` e são material do
     composition root da **Fase 5** — os adapters do ipc NÃO podem usá-los
     (ciclo de import ipc→domínio).
2. **Fechamento specs↔registry no boot**: `ipc.Server.ValidateRegistry()`
   (todo handler tem spec; `user-verify` web-only isento; todo spec tem
   handler) chamado em `cmd/focusguard-daemon` após o wiring de dependências.
   Testes: `TestServer_RegisteredHandlersHaveSpecs`,
   `TestServer_SpecsAllHaveHandlers`.
3. `handleConnection` virou roteador puro; comentários/AGENT.md atualizados.
4. Validação: `go build` + `go vet` + `go test ./internal/...` (30 pacotes)
   verdes; `contract-check` em dia (nenhum struct mudou).

### Fase 5 — Composition root (✅ concluída em 2026-08-06)

O `ipc.Server` virou **transport puro** para as ações de domínio — os
handlers vivem nos pacotes de domínio e o daemon os registra no boot:

1. **Transporte**: `Server.Register(h)` (registro pós-construção) e
   `Server.Dispatch(req)` (roteador Get → Validate → Handle, devolve o
   `Response`; `handleConnection` só encoda + dispara o hook de update
   pós-resposta). `writeError` virou `errorResponse` (ActionError/ipcerr →
   código estável; erro comum → mensagem).
2. **Handlers de domínio na produção**: os adapters `funcHandler` de
   `block`/`block-all`/`apps-*`/`goal-*`/`presets`/`preset-*`/`user-*`/`dns-*`
   saíram do pacote `ipc` (deletados `blocks_handler.go`/`dns_handler.go`/
   `users_handler.go`); os reais estão em `internal/blocks`, `internal/dns`,
   `internal/goal`, `internal/presets`, `internal/users`, `internal/apps`
   (interfaces estreitas — DIP, esboços da seção 8).
   - `NewServer` registra só os 15 de nível servidor (ping/status/tamper-log +
     adapters de `analytics`/`schedule`/`pomodoro`/`update` — serviços que não
     podem importar ipc).
   - Os testes internos do ipc (ciclo de import: domínio→ipc) usam os mesmos
     adapters movidos para `handlers_ref_test.go` (referência 1:1),
     registrados pelo `setupTestServer`/`startIntegrationServer`.
   - `internal/ipc/domain_wiring_test.go` (package `ipc_test`) compõe os
     handlers REAIS com o roteador via `Dispatch` + `ValidateRegistry` e
     dispara todas as 19 ações de domínio — rede contra drift
     referência↔domínio.
3. **Composition root (`cmd/focusguard-daemon`)**: os 19 handlers de domínio
   são construídos com as dependências reais e registrados via
   `server.Register` antes do `ValidateRegistry` (fechamento specs↔registry
   no boot, 34 ações). `SetApps`/`SetUsers`/`SetOnDNSStarted` saíram do boot
   (os handlers recebem as deps por construtor); `SetPresets`/`SetGoal`/
   `SetDNS` permanecem — o `status` e os adapters de pomodoro/schedule leem
   esses campos. `apps-*` é registrado incondicionalmente (com `pg` nil o
   `refreshGuard` é no-op; sem os handlers o boot falharia).
4. **Mudança de comportamento deliberada**: `user-set-password` ganhou
   fail-fast no `Validate` do handler de domínio (username vazio / senha < 8 →
   `CodeInvalid` antes do store — antes caía no store, sem código e com o
   prefixo "user: " na mensagem). As demais 18 ações reproduzem 1:1
   (mensagens, códigos, ordem) — preso pelos testes de domínio + wiring test.
5. Validação: `go build`/`vet` verdes, suíte Go completa verde (34+ pacotes),
   `contract-check` em dia, `tsc` + vitest 19/19. Commits:
   - `refactor(ipc): expose Register/Dispatch, extract errorResponse`
   - `refactor(ipc): move domain-backed handlers to test reference`
   - `feat(daemon): compose domain handlers at the composition root`
   - `test(ipc): wire real domain handlers with the router (external test)`
   - `fix(daemon): always register apps handlers so boot validation passes`
   - `test(ipc): dispatch all 19 domain actions through the real router`

### Próximo passo

1. **Lifecycle do daemon (restante da Fase 5 do plano — B4/B10/B8)**: extrair
   `internal/daemon` com `Run(ctx) error` e shutdown ordenado; mover a
   orquestração de update (`daemonUpdater`/flag/stop de guards) para
   `internal/update`. Com ele, `SetApps`/`SetUsers`/`SetOnDNSStarted` (hoje só
   usados pelos adapters de referência dos testes) podem ser removidos.
2. **Fase 6 — CLI por comando**: `cmd/focusguard/main.go` → `commands/` com um
   arquivo por comando + tabela (B5).
3. Cada fase vira uma issue/PR separada seguindo `docs/release.md` se for
   cortar release entre fases.
