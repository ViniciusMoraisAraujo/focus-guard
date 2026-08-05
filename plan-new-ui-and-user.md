# Plano — Nova UI do Server (DNS) + Sistema de Usuário

> **Status:** documento de planejamento — aguardando aprovação para implementar.
> Escopo: (1) nova tela **Servidor** (DNS sinkhole) na UI web existente; (2)
> sistema de **usuário + login** na entrada da UI, persistido em `user.json`,
> seguindo os padrões JSON do projeto (sem overengineering).
>
> **Decisões registradas (2026-08-05):**

| Decisão | Escolha | Motivo |
|---|---|---|
| Escopo da nova UI | **Nova tela na UI atual** (focusguard-ui) | Reusa todo o stack (sidebar, temas, cliente API, polling) — sem infra duplicada |
| Acesso à rede | **Só localhost** (`127.0.0.1:48902`), como hoje | Sem mudança de bind/Host guard; o login é portão de entrada local |
| Modelo de usuário | **Múltiplos usuários, sem roles** | Lista de usuários no user.json; apenas o `admin` gerencia a lista |
| Usuário inicial | **Admin sempre presente** (seed) | Criado no boot do daemon se o user.json não existir; os demais usuários são criados pelo admin |
| Persistência | **`user.json` separado** ao lado do `state.json` | Segue o padrão de `presets.json`/`apps.json`/`goal.json`; senhas fora do state.json (que é varrido por statewatch/tamper) |
| Hash de senha | **bcrypt** (`golang.org/x/crypto`) | Padrão correto mínimo; nunca senha em texto puro no disco |
| **Peso do auth (2026-08-05)** | **Manter o plano completo** | Validação: a API HTTP e as ações IPC **já existem** — o plano só adiciona pontas (3 endpoints + 5 actions + `internal/user` ~150 linhas espelhando `preset.Store`). Sem DB, JWT, roles ou camada nova; custo de runtime desprezível (bcrypt só no login) |
| Sessão web | **Token aleatório em memória + cookie HttpOnly** | Sem JWT, sem banco, sem tabela de sessões — processo user-space, restart desloga (aceitável) |
| Recursos da tela DNS | **Status + contadores ao vivo, ligar/desligar, upstream configurável** | Dados já expostos pelo IPC; upstream exige persistência nova |

---

## 1. Contexto

O `task.md` (DNS Sinkhole) **já está implementado** (v0.15.0):

- `internal/dnsserver` — sinkhole UDP+TCP porta 53, upstream Cloudflare Security
  `1.1.1.2:53`, TTL curto, panic recovery, contadores `queries`/`blocked`.
- CLI `focusguard dns start/stop/status` + ações IPC `dns-start`/`dns-stop`/
  `dns-status`; flag `dns_enabled` persistido no `state.json`.
- O `ipc.Response` **já carrega** `dns_enabled`, `dns_listening`, `dns_addr`,
  `dns_upstream`, `dns_queries`, `dns_blocked`, `dns_bind_error` — e o action
  `status` já faz merge desses dados (polling existente da UI basta).

**O que falta** para o pedido:

1. **UI**: não há tela de DNS na web (10 telas atuais, nenhuma do servidor).
2. **Upstream configurável**: `DefaultUpstream` é constante — não há persistência
   nem ação IPC para trocar.
3. **Login/usuário**: a web é aberta (sem autenticação) e não há `user.json`.

---

## 2. Arquitetura do login (visão geral)

```
Navegador (React UI)
  │  POST /api/login {username, password}
  ▼
focusguard-web (user-space) ── NUNCA toca o user.json ──┐
  ├─ valida credenciais → IPC user-verify (bcrypt no daemon)
  ├─ emite token aleatório (crypto/rand) → cookie HttpOnly
  │   SameSite=Strict, Path=/ ; sessão em mapa em memória (TTL 12h)
  ├─ middleware: /api/action exige sessão válida (401 sem cookie)
  └─ user-* (add/remove/set-password) só para sessão is_admin
                              │  IPC (localhost)
                              ▼
focusguard-daemon (admin) — dono do user.json (igual presets.json)
  ├─ seed: cria admin no boot se user.json não existir
  └─ user-verify / user-list / user-add / user-remove / user-set-password
```

**Por que a verificação passa pelo daemon (IPC) e não lê o arquivo direto:**
o web roda user-space e, no Linux, o `user.json` é criado com `chmod 0600`
pelo daemon (root) — o processo web **não conseguiria ler**. Além disso, a
credencial (hash) nunca sai do processo privilegiado. Mesmo padrão do
`presets.json` (daemon dono, IPC `preset-add/remove`).

**Por que sessão em memória:** localhost-only + processo on-demand. Restart do
`focusguard-web` desloga todos — comportamento aceitável e documentado.

---

## 3. Parte A — Sistema de usuários (`internal/user`)

### 3.1 Formato do `user.json` (padrão JSON do projeto)

Ao lado do `state.json` (`C:\ProgramData\FocusGuard\user.json` no Windows,
`/var/lib/focusguard/user.json` no Linux):

```json
{
  "version": 1,
  "users": [
    {
      "username": "admin",
      "password_hash": "$2a$10$...",
      "is_admin": true,
      "created_at": "2026-08-05T14:30:00-03:00"
    }
  ]
}
```

Regras:
- `snake_case` + `version`, igual ao `state.json`; campo novo é aditivo.
- **Nunca** senha em texto puro — só `password_hash` bcrypt.
- O `admin` (seed) tem `is_admin: true`; novos usuários criados pelo admin
  nascem com `is_admin: false` (sem roles — só esse flag binário).

### 3.2 Novo pacote `internal/user` (espelha o padrão do `preset.Store`)

```go
type User struct {
    Username     string `json:"username"`
    PasswordHash string `json:"password_hash"`
    IsAdmin      bool   `json:"is_admin"`
    CreatedAt    time.Time `json:"created_at,omitempty"`
}

type Store struct {
    mu   sync.Mutex
    path string
    users []User
}
```

- `NewStore(path)` — load best-effort (arquivo faltando/corrompido → estado
  mínimo com só o admin; nunca aborta o boot).
- `save()` — escrita atômica (temp + rename, `chmod 0600`), igual
  `preset.Store.save`.
- `Verify(username, password) (User, bool)` — `bcrypt.CompareHashAndPassword`.
- `Add(username, password) error` — valida (username não vazio/sem espaços,
  senha ≥ 8 chars), gera hash `bcrypt.GenerateFromPassword` (custo default),
  rejeita duplicado e rejeita criar outro `admin`.
- `Remove(username) error` — **nunca** remove o `admin` (o sistema sempre tem 1 admin).
- `SetPassword(username, newPassword) error` — troca o hash.
- `List() []string` — só nomes (nunca hashes).

### 3.3 Seed do admin (daemon)

No boot do `cmd/focusguard-daemon/main.go`, junto do `presetStore`:

```go
userStore := user.NewStore(filepath.Join(filepath.Dir(statePath), "user.json"))
userStore.EnsureAdmin() // cria o admin padrão se o arquivo não existir
```

- `EnsureAdmin()`: se não houver nenhum usuário `is_admin`, grava o admin com a
  senha padrão pré-definida.
- A senha padrão do admin **não aparece em texto puro neste documento** (por
  segurança); é definida na implementação e pode ser trocada após o login
  (`user-set-password`).
- Se o arquivo existir e tiver admin, nada é alterado (idempotente).

### 3.4 Ações IPC novas

Campos novos no `ipc.Request`: `UserName`, `UserPassword`.

| Ação | Payload | Resposta |
|---|---|---|
| `user-verify` | `{user_name, user_password}` | `success` + `user_is_admin` (login) |
| `user-list` | — | `users: [...]` (nomes) |
| `user-add` | `{user_name, user_password}` | `success`/`message` |
| `user-remove` | `{user_name}` | `success`/`message` |
| `user-set-password` | `{user_name, user_password}` | `success`/`message` |

Campos novos no `ipc.Response`: `Users []string` (`users,omitempty`),
`UserIsAdmin bool` (`user_is_admin,omitempty`).

> **Autorização:** o daemon continua confiando no IPC local (como hoje — CLI e
> tray têm controle total). A restrição "só admin gerencia usuários" é imposta
> na camada web (middleware do httpapi, que conhece a sessão). O daemon **não**
> repete a checagem — sem overengineering.

---

## 4. Parte B — Autenticação no `focusguard-web` (`internal/httpapi`)

Novo arquivo `internal/httpapi/auth.go`:

### 4.1 Endpoints

| Endpoint | Método | Público | Descrição |
|---|---|---|---|
| `/api/login` | POST | ✅ | `{username, password}` → valida via IPC `user-verify`; emite cookie `fg_session` |
| `/api/logout` | POST | ❌ | Invalida a sessão e limpa o cookie |
| `/api/auth/status` | GET | ✅ | `{authenticated, username, is_admin}` — a SPA decide se mostra login |

`/api/action` passa a exigir sessão válida (**401** sem cookie). `/api/health`,
`/api/ping` e os assets estáticos continuam públicos (a SPA faz o gate de
login; servidor nunca esconde index.html).

### 4.2 Gerenciador de sessões (em memória)

```go
type session struct {
    username string
    isAdmin  bool
    expires  time.Time
}
```

- Token: `crypto/rand` 32 bytes → hex (32 chars), valor do cookie.
- Mapa `map[string]session` com mutex; TTL fixo de **12h**; limpeza lazy
  (varredura a cada login) — sem goroutine de cleanup (não precisa).
- Cookie: `HttpOnly`, `SameSite=Strict`, `Path=/`, sem `Secure` (localhost
  http). CSRF já coberto pela política atual (Content-Type obrigatório +
  Host guard + SameSite).

### 4.3 Middleware

- Envolve `/api/action`: sem cookie válido → `401 {"success":false,...}`.
- Para ações `user-*`: exige `session.isAdmin` → senão `403`.
- Rate limit de login (brute force do admin padrão): contador em memória por IP
  (localhost), **5 falhas → trava 30s**; resposta 429.

---

## 5. Parte C — Upstream DNS configurável

### 5.1 Persistência (padrão `dns_enabled`)

- `internal/store/json.go`: `State.DNSUpstream string \`json:"dns_upstream,omitempty"\``
  (aditivo; state antigo carrega vazio → fallback `DefaultUpstream`).
- `internal/scheduler`: campo `dnsUpstream`, getter `DNSUpstream()`,
  `SetDNSUpstream(u string) error` (persiste + no-op se igual), incluído em
  `ramState()`/bootstrap/`statesEqual` (senão o tamper restaura valor velho).

### 5.2 Aplicação no servidor

- `internal/dnsserver/controller.go`: novo `SetUpstream(u string) error` —
  atualiza `c.upstream`; se estiver escutando, faz `Stop()` + `Start()`
  (DNS é stateless, reinício é instantâneo e o uptime/counters reiniciam —
  aceitável e documentado).
- `cmd/focusguard-daemon/main.go`: na construção, `dnsserver.NewController(sched,
  DefaultBindAddr, sched.DNSUpstream())` (usa o persistido se houver).

### 5.3 Ação IPC + validação

| Ação | Payload | Comportamento |
|---|---|---|
| `dns-set-upstream` | `{upstream}` | Valida `host[:port]` (`net.SplitHostPort`; sem porta → `:53`); rejeita vazio/inválido; persiste via scheduler e aplica via controller |

---

## 6. Parte D — UI (React)

### 6.1 Gate de login (`App.tsx` + novo `login-screen.tsx`)

- No mount, `GET /api/auth/status`:
  - `authenticated: false` → renderiza `<LoginScreen />` no lugar do Shell.
  - `authenticated: true` → app normal; o contexto guarda `username`/`isAdmin`.
- `LoginScreen`: card centralizado com a identidade do sistema (escudo), campos
  usuário/senha, submit com loading, erro inline (senha inválida / daemon
  indisponível — o `user-verify` vira 503 se o daemon estiver fora), botão
  desabilitado durante a chamada. Visual consistente com o design system atual
  (dark, `font-heading`, hover/transições).
- Toast de sessão expirada: se uma chamada de `/api/action` devolver 401,
  desloga na hora e volta ao login.

### 6.2 Tela **Servidor** (nova, `screens/Servidor.tsx`)

Item novo na navegação (ícone `Globe`/`RadioTower` da lucide), entre
"Segurança" e "Configurações":

- **Card Status**: toggle Ligar/Desligar (IPC `dns-start`/`dns-stop`),
  badges de `dns_listening`, endereço (`dns_addr`), upstream (`dns_upstream`),
  uptime (from `started_at` — precisa ser exposto no status; ver nota 6.4),
  e **contadores ao vivo** `dns_queries` / `dns_blocked` (o `status` já
  carrega esses dados — o polling de 10s da UI atualiza sem infra nova).
- **Erro de bind** (`dns_bind_error`): alerta com a dica do Windows (ICS /
  dnscache ocupando a porta 53).
- **Card Upstream**: chips (Cloudflare Security `1.1.1.2`, Quad9 `9.9.9.9`,
  Google `8.8.8.8`, Cloudflare `1.1.1.1`) + campo custom (`host[:port]`) →
  salva via `dns-set-upstream`; ao trocar com o servidor ligado, avisa que
  ele reinicia na hora.
- **Card Ajuda** (estático): passos do task.md — IP estático no roteador +
  DNS primário do DHCP apontando para este PC + DNS secundário público
  (failover), e a nota do "DNS secundário furador" (por isso o sinkhole
  responde `0.0.0.0` com Status OK).

### 6.3 Gestão de usuários (`Configuracoes.tsx`)

Card **Usuários** visível **só para `is_admin`**:

- Lista de usuários (`user-list`) com badge "admin".
- Adicionar usuário (username + senha ≥ 8) → `user-add`.
- Remover usuário → `user-remove` (admin nunca removível; botão desabilitado).
- Redefinir senha → `user-set-password`.
- Usuário comum vê apenas "trocar minha senha" (mesma ação com o próprio nome).

### 6.4 Ajustes no cliente API e tipos

- `types.ts`: tipos de auth (`AuthStatus`), campos DNS no `ApiResponse`
  (mapear os `dns_*` do Go) e campos de usuário; conversões na borda
  (RFC3339 → `Date` para `started_at`).
- `client.ts`: `login`, `logout`, `authStatus`; `dnsStart`, `dnsStop`,
  `dnsSetUpstream`; `userList`, `userAdd`, `userRemove`, `userSetPassword`.
- `fetch` ganha `credentials: "same-origin"` (cookie da sessão).

---

## 7. Parte E — CLI de recuperação (opcional, barato)

`focusguard user ...` falando direto via IPC (o CLI já tem confiança total do
daemon — mesma superfície de hoje):

- `focusguard user list`
- `focusguard user add <nome>` (senha via prompt, não em argumento)
- `focusguard user remove <nome>`
- `focusguard user set-password <nome>`

Valor: recuperação se esquecer a senha do admin (sem depender da UI).
Rejeitar `admin remove` no CLI também.

---

## 8. Arquivos tocados

| Tipo | Arquivo | Mudança |
|---|---|---|
| **novo** | `internal/user/user.go` + `user_test.go` | Store, seed, bcrypt |
| **novo** | `internal/httpapi/auth.go` | sessões, login/logout/status, middleware, rate limit |
| **novo** | `focusguard-ui/src/components/login-screen.tsx` | tela de login |
| **novo** | `focusguard-ui/src/screens/Servidor.tsx` | tela DNS |
| edit | `internal/store/json.go` | `DNSUpstream` |
| edit | `internal/scheduler/scheduler.go` | `dnsUpstream` + persistência |
| edit | `internal/dnsserver/controller.go` | `SetUpstream` (restart) |
| edit | `internal/ipc/ipc.go` | campos `user_name/user_password/upstream` + `users/user_is_admin` |
| edit | `internal/ipc/server.go` | actions `user-*` + `dns-set-upstream` |
| edit | `cmd/focusguard-daemon/main.go` | seed `userStore` + upstream persistido no controller |
| edit | `internal/httpapi/httpapi.go` | rotas de auth + middleware no `/api/action` |
| edit | `cmd/focusguard/main.go` (+test) | CLI `user ...` (opcional) |
| edit | `focusguard-ui/src/api/types.ts`, `client.ts` | tipos/endpoints novos |
| edit | `focusguard-ui/src/App.tsx`, `screens/Configuracoes.tsx` | gate de login + card Usuários |
| edit | `go.mod` | `golang.org/x/crypto` (bcrypt) |
| edit | `docs/ui-plan.md`, `AGENT.md` | documentação |

---

## 9. Testes

| Suíte | Cobertura |
|---|---|
| `internal/user` | seed idempotente; add/remove/list/verify; bcrypt round-trip; validações (duplicado, senha curta, remover admin); arquivo corrompido best-effort; **nenhum texto puro no arquivo** |
| `internal/scheduler` + `store` | `dns_upstream` round-trip, bootstrap, `statesEqual` com upstream |
| `internal/dnsserver` | `SetUpstream` para/reinicia; upstream aplicado no `Status` |
| `internal/ipc` | actions `user-*` (via fake do daemon) e `dns-set-upstream` (validação) |
| `internal/httpapi` | login ok/falha/usuário desconhecido; cookie; 401 sem sessão; 403 `user-*` sem admin; 429 no rate limit; logout invalida; assets públicos |
| `cmd/focusguard-daemon` (suíte elevada) | seed do admin no boot; upstream persistido no boot |
| `focusguard-ui` | `npx tsc --noEmit` |
| Global | `go build ./...` + `go vet ./...` |

---

## 10. Ordem de implementação (fases)

1. **F1 — Backend de usuário**: `internal/user` + seed no daemon + ações IPC.
2. **F2 — Auth HTTP**: `auth.go` (sessões, login/logout/status, middleware,
   rate limit) + rotas no `httpapi`.
3. **F3 — Upstream configurável**: store → scheduler → controller → IPC.
4. **F4 — UI**: login gate + LoginScreen + card Usuários.
5. **F5 — Tela Servidor**: Servidor.tsx + client API + tipos.
6. **F6 — CLI + docs + validação**: `focusguard user`, ui-plan/AGENT.md,
   testes completos + code review + suíte do daemon elevada.

---

## 11. Riscos e notas

| Item | Nota |
|---|---|
| **Senha padrão do admin** | Não versionada em texto puro (por segurança); definida na implementação; trocável via `user-set-password` |
| **Sessões em memória** | Restart do `focusguard-web` desloga todos — aceitável (processo on-demand, localhost) |
| **`is_admin` só na camada web** | O daemon confia no IPC local (como CLI/tray hoje); se um dia o IPC for exposto, rever |
| **`user.json` fora do statewatch** | O watcher/tamper cobre só o `state.json`; o `user.json` é arquivo separado — sem conflito |
| **Troca de upstream com servidor ligado** | Reinício instantâneo; contadores reiniciam (uptime/queries/blocked zeram) |
| **Login com daemon fora** | `user-verify` devolve 503 → a tela de login mostra "daemon indisponível" em vez de erro de senha |
| **Hash bcrypt** | Custo default (10); sem salt manual (bcrypt embute) |
| **403 vs 401** | 401 = não autenticado (vai pro login); 403 = autenticado mas sem permissão (só admin) |
