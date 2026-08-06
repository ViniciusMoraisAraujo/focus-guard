# Plano — FocusGuard UI (Web Amigável)

> **Status:** documento vivo. **F1 e F2 implementadas em 2026-08-03** — a
> interface web já roda (ver seção 10). **F4 em andamento (2026-08-04):**
> todas as 10 telas navegáveis; grade semanal da Agenda e anel visual do
> Pomodoro adicionados.
>
> **Decisões registradas:**
>
> | Decisão | Escolha | Motivo |
> |---|---|---|
> | Tipo de UI | Web app local (navegador) | Sem instalação de app; abre em qualquer navegador |
> | Stack | React + Vite + TypeScript | Ecossistema grande, DX excelente, tipagem forte |
> | Execução | Só navegador (`http://127.0.0.1:48902`) | Simplifica: sem Electron, sem CORS, sem processo extra |
> | Local do projeto | Monorepo — `focusguard-ui/` + `cmd/focusguard-web/` | Releases acopladas, versionamento unificado |
> | Plataforma | Windows primeiro (Linux depois) | IPC TCP já disponível no Windows |
> | Integração | **`focusguard-web` (binário separado)** faz proxy das ações IPC | Navegador não fala TCP; **zero mudanças no daemon** |
> | Escopo MVP | 4 telas (Dashboard, Bloquear, Modo pânico, Config) | Entrega rápida de valor, base para o resto |

> 🏗️ **Decisão de arquitetura (atualizada em 2026-08-03):** o plano original
> previa servir a UI **dentro do daemon** (API HTTP + `go:embed` nele). A
> análise comparativa (daemon × tray × binário separado) levou à **Opção C**:
> um processo **user-space** `focusguard-web`, iniciado por demanda pelo
> `focusguard web`, que serve a UI e **faz proxy das ações IPC para o daemon**
> (mesmo `ipc.Client` que CLI/tray usam). Vantagens: o daemon admin não ganha
> superfície HTTP, UI e daemon têm ciclos de vida independentes, e **nenhuma
> mudança no daemon foi necessária** (o `HandleRequest` do plano não precisou
> ser criado).

---

## 1. Visão geral

**FocusGuard UI** é a interface gráfica amigável do FocusGuard: um web app local
servido pelo `focusguard-web` em `http://127.0.0.1:48902`. O usuário roda
`focusguard web`, o CLI inicia o servidor (se necessário) e abre o navegador —
bloqueios, pomodoro, agenda e estatísticas ficam a um clique de distância.

**Princípios de UX (o "amigável"):**
- **Zero configuração** — `focusguard web` abre o navegador direto no painel.
- **Ações em 1 clique** — bloquear um site é selecionar + confirmar, nunca digitar comando.
- **Feedback imediato** — toasts de sucesso/erro, countdowns ao vivo, estados vazios explicativos.
- **Consistente com a identidade** — escudo azul-marinho + verde do ícone do sistema.

---

## 2. Arquitetura

```
┌───────────── Navegador (Chrome/Edge) ─────────────┐
│                                                   │
│   FocusGuard UI (React + Vite + TS)               │
│     │  fetch() / (futuro) WebSocket               │
│     ▼                                             │
│   Cliente API tipado (src/api/)                   │
└───────────────┬───────────────────────────────────┘
                │  http://127.0.0.1:48902 (localhost apenas)
                ▼
┌───────────── focusguard-web (Go, user-space, por demanda) ─┐
│  cmd/focusguard-web + internal/httpapi                     │
│    ├─ GET  /api/health → o próprio servidor (spawn probe)  │
│    ├─ GET  /api/ping   → proxy do ping do daemon           │
│    ├─ POST /api/action → proxy via ipc.Client (5s timeout) │
│    └─ GET  /           → UI estática (go:embed do dist)    │
└───────────────┬────────────────────────────────────────────┘
                │  IPC (TCP 127.0.0.1:48901 no Windows)
                ▼
   Daemon focusguard-daemon (serviço) — INALTERADO
```

**Por que um binário separado (Opção C):**
- **Daemon admin sem superfície HTTP** — um bug no servidor web não vira
  comprometimento de um processo com privilégios.
- **Ciclos de vida independentes** — tweak de UI não reinicia o serviço; o
  `focusguard-web` pode ser derrubado e re-subido à vontade.
- **Zero mudanças no daemon** — o proxy reusa o `ipc.Client` (mesmo caminho da
  CLI e do tray).

### Fluxos

| Modo | Como roda |
|---|---|
| **Dev (UI)** | `cd focusguard-ui && npm run dev` → Vite em `:5173` com proxy `/api` → `focusguard-web` `:48902` (hot reload) |
| **Dev (servidor)** | `go run ./cmd/focusguard-web -assets focusguard-ui/dist` (serve do disco) |
| **Prod** | `make ui` compila o React e copia o `dist` para `cmd/focusguard-web/assets` (embutido via `go:embed`) |
| **Usuário** | `focusguard web` → spawn on-demand (singleton via probe de porta) + abre o navegador |

---

## 3. Componentes implementados (repo atual)

### 3.1 `internal/httpapi` (novo)
Pacote HTTP com as guardas de segurança locais:
- `POST /api/action` — mesmo JSON do `ipc.Request`; resposta = `ipc.Response`.
- `GET /api/ping` / `GET /api/health` — health do daemon / do próprio servidor.
- `GET /` + assets — UI estática com **SPA fallback**; sem UI compilada, serve
  página "rode `make ui`".
- **Segurança (implementada):** bind loopback apenas; validação de `Host`
  (mata DNS rebinding); `Content-Type: application/json` obrigatório em
  `/api/action` (mata CSRF por simple-request); `MaxBytesReader` (1 MiB);
  headers `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`,
  `Content-Security-Policy`.
- **Token anti-CSRF por instalação:** não implementado (ver seção 9).

### 3.2 `cmd/focusguard-web` (novo)
Binário user-space (sem manifest, sem admin). Flags `-addr` (padrão
`127.0.0.1:48902`) e `-assets` (dev). Embuta `all:assets` via `go:embed`.

### 3.3 CLI `focusguard web`
Probe `/api/health` → se já no ar, só abre o navegador; senão, spawna o
`focusguard-web` ao lado do CLI (Windows: `HideWindow`; Linux: `setsid`) e
abre o navegador. Nunca sobe segunda instância.

### 3.4 Build/release
- `Makefile`: `make ui` (npm ci + build + copia para assets), `make build-web`.
- `.goreleaser.yaml`: builds `web-linux`/`web-windows` (amd64+arm64) nos
  arquivos das duas plataformas; before-hook `npm ci && npm run build` (exige
  Node no runner — ver AGENT.md).
- Installers e updater: `focusguard-web` entra em `binaryNames` (Windows
  install), `install-daemon.ps1`, `install-linux.sh` (opcional) e
  `siblingBinaries` do daemon (update multi-binário).

---

## 4. Contrato da API

Transporte: JSON sobre HTTP, `Content-Type: application/json`, sempre
`127.0.0.1:48902`.

### Endpoints

| Endpoint | Método | Descrição |
|---|---|---|
| `/api/health` | GET | Health do **focusguard-web** (sempre 200 se o servidor roda) |
| `/api/ping` | GET | Proxy do ping do daemon (200/503) |
| `/api/action` | POST | Todas as ações (mesmo `action` do IPC); 503 quando o daemon cai |
| `/` | GET | UI (index.html) com SPA fallback |
| `/ws` | GET (futuro) | Push de eventos em tempo real |

### `POST /api/action` — exemplos

```jsonc
// status (o principal do Dashboard)
{"action": "status"}
// → {"success": true, "blocks": [...], "goal": 14400000000000,
//    "current_version": "0.6.0", "pomodoro": {...}, "doh_active": true, ...}

// bloquear site
{"action": "block", "domain": "youtube.com", "duration": "4h"}

// modo pânico com allowlist
{"action": "block-all", "duration": "30m", "allowlist": ["docs.google.com", "github.com"]}

// meta diária (minutos)
{"action": "goal-set", "goal_minutes": 240}

// presets
{"action": "presets"}
```

### ⚠️ Peculiaridades de serialização (armadilhas para a UI)

| Campo | Formato | Detalhe |
|---|---|---|
| `goal` | int64 **nanossegundos** | `time.Duration` do Go serializa em ns — converter `ns / 1e9 / 60` para minutos |
| `ExpiresAt` / `StartedAt` | RFC3339 | `time.Time` do Go — `new Date(rfc3339)` no JS |
| `duration` (input) | string Go | `"30m"`, `"4h"` — o parser é `time.ParseDuration` |
| `total_focus` / `per_day[].duration` | ns | Stats do analytics (só sessões **concluídas** de pomodoro) |
| modo pânico | domínio sentinela | `domain === "*all-internet*"` identifica o block-all |
| erros | `success: false` + `message` | A UI deve SEMPRE tratar `success:false` e exibir `message` |
| daemon offline | HTTP 503 | A UI trata como estado "daemon desligado" (banner) |

> **Regra:** a UI nunca adivinha estado — qualquer mutação (block, goal,
> pomodoro) confia na resposta do daemon (`success`/`message`), igual ao padrão
> do tray.

---

## 5. Estrutura do projeto (monorepo)

```
cmd/focusguard-web/            # servidor web (Go, user-space)
  ├── main.go                  # embed all:assets + flags + wiring
  └── assets/                  # dist do React copiado pelo "make ui" (.gitkeep versionado)
internal/httpapi/              # servidor HTTP: proxy IPC + estático + segurança
  ├── httpapi.go
  └── httpapi_test.go
focusguard-ui/                 # frontend React + Vite + TS
  ├── package.json
  ├── vite.config.ts           # proxy /api → 127.0.0.1:48902 (dev)
  ├── index.html
  ├── public/favicon.svg       # escudo (identidade do sistema)
  └── src/
      ├── api/types.ts         # espelha ipc.Request/Response + policy/preset/pomodoro/analytics
      ├── api/client.ts        # action() + pingDaemon() tipados
      ├── context.tsx          # polling 10s (status) / 60s (stats) + toasts
      ├── hooks/useCountdown.ts
      ├── components/ui.tsx    # Button, Chip, Card, Modal, Spinner, Field
      └── screens/             # Dashboard, Bloquear, Panico, Configuracoes
```

---

## 6. UX / Design System (implementado)

- **Paleta dark:** fundo `#0a0f1e` (gradiente radial), cards `#121a33`, bordas
  `#1f2a4a`, foco verde `#22c55e`, pânico vermelho `#ef4444`, acento
  azul-marinho `#1d4ed8` (herança do ícone).
- **Micro-interações:** countdown tabular-nums ao vivo, botão de pânico grande
  com glow + pulso, chips selecionáveis, toasts com slide-in, transições
  suaves, estados de hover/active em tudo.
- **Acessibilidade:** contraste AA, foco visível, `aria-live` nos toasts,
  `role="dialog"` no modal, labels em todos os campos.
- **Segurança no front:** React escapa tudo por padrão (sem
  `dangerouslySetInnerHTML`) + CSP restritiva servida pelo backend.

---

## 7. Telas do MVP (4 — implementadas)

### 7.1 Dashboard
- Hero de status: "Modo pânico" / "Pomodoro ativo (ciclo x/y)" / "Foco ativo —
  N bloqueios" / "Sem bloqueios", com countdown do próximo fim (client-side).
- **Meta do dia:** barra de progresso (foco de hoje = stats `per_day` +
  sessão pomodoro ativa, vs `goal`).
- Cards de bloqueios ativos com countdown e horários.
- Ações rápidas: "Bloquear site" e "Modo pânico".

### 7.2 Bloquear
- Modo categoria (chips de presets com contagem de domínios) ou domínio livre.
- Chips de duração (30m/1h/2h/4h/8h) + custom em minutos.
- Toasts com a mensagem real do daemon; botão desabilitado durante a chamada.

### 7.3 Modo pânico
- Botão vermelho grande com confirmação explícita (modal).
- Duração + allowlist opcional (textarea separado por vírgula).
- Detecta pânico ativo (`*all-internet*`) e avisa.

### 7.4 Configurações
- Meta diária (chips 2h/4h/6h/8h + custom) → `goal-set`.
- Atualizações: exibe `update_available`/`update_version` do cache do `status`
  (nunca aplica pela UI — via `focusguard update` no terminal).
- Proteção: regras de firewall, DoH/DoT; versão e estado do daemon.

---

## 8. Roadmap

| Fase | Estado | Entrega |
|---|---|---|
| **F1 — Fundação** | ✅ 2026-08-03 | `internal/httpapi` + `cmd/focusguard-web` + `focusguard web` + `make ui` + goreleaser/installers |
| **F2 — UI MVP** | ✅ 2026-08-03 | Scaffold React + 4 telas + cliente API tipado + build validado |
| **F3 — Real-time** | ✅ 2026-08-06 | **SSE** (decisão: em vez de `/ws`) — event hub no daemon + long-poll `event-subscribe` + `GET /api/events` + EventSource no frontend com fallback de polling; expiração de block/pomodoro/schedule viram eventos (refactor-plan Fase 7) |
| **F4 — Telas restantes** | 🚧 (telas ✔, visuais em andamento) | Pomodoro visual (anel), Agenda (grade semanal), Stats (gráficos), Presets, Apps, Tamper-log — todos os dados já expostos |
| **F5 — Linux + polimento** | ⬜ | Acesso ao socket no Linux, docs finais, release conjunta |

---

## 9. Riscos e mitigações

| Risco | Mitigação |
|---|---|
| Node necessário no CI de release (hook do goreleaser) | Documentado no AGENT.md; hook com `npm ci` (lockfile) p/ build reprodutível |
| `go:embed` não alcança `focusguard-ui/dist` (fora do pacote) | Copy-step no `make ui`/hook para `cmd/focusguard-web/assets` (com `.gitkeep` versionado) |
| Superfície de ataque | Processo **user-space** (não admin), bind loopback, Host check, Content-Type obrigatório, CSP, `MaxBytesReader` |
| Porta 48902 ocupada | Constante `httpapi.DefaultAddr` compartilhada; probe de health decide spawn vs. reuso |
| Serialização Go → JS (nanos/RFC3339) | Tabela na seção 4; `types.ts` converte na borda |
| UI desatualizada (polling 10s/60s) | Fase 3 resolve com WS; countdown é client-side, então polling leve basta |
| **Token anti-CSRF por instalação** | **Aberto:** Host + Content-Type cobrem os vetores conhecidos; token injetado no HTML servido (header custom → preflight) fica como hardening futuro |

---

## 10. Status de implementação (2026-08-03)

- ✅ `internal/httpapi` com testes (httptest + cliente stub): health, ping,
  action (forward/timeout/503), static + SPA fallback + stub "make ui",
  guardas de Host/Content-Type/headers.
- ✅ `cmd/focusguard-web` com `go:embed` dos assets.
- ✅ `focusguard web` com spawn por plataforma (Windows `HideWindow` / Linux
  `setsid`) e testes com injeção de dependência.
- ✅ `focusguard-ui` (React 18 + Vite 5 + TS estrito): `tsc` limpo, build
  160 kB JS (51 kB gzip) + 11 kB CSS.
- ✅ Wiring: `make ui`, `make build-web`, goreleaser (web amd64+arm64 nas duas
  plataformas + hook npm), installers (ps1/sh), `binaryNames`,
  `siblingBinaries`, `.gitignore`.
- ✅ Smoke test real: servidor servindo a UI, `/api/action` devolvendo status
  do daemon ao vivo, 415/403/headers de segurança confirmados.
- ✅ F3 — **SSE real-time** (2026-08-06): `internal/eventhub` + ação IPC
  `event-subscribe` + `GET /api/events` (SSE com keepalive e Last-Event-ID) +
  EventSource no `DataProvider` com fallback de polling (30s) — ver Fase 7 do
  refactor-plan.
- ⬜ F4–F5 (roadmap acima).

---

## 11. Próximos passos

1. ~~F3~~ ✅ (SSE — refactor-plan Fase 7): expiração de block, pomodoro e
   schedule chegam por evento; o polling virou fallback.
2. **F4:** telas de Pomodoro, Agenda, Stats, Presets, Apps, Tamper-log.
3. **F5:** acesso ao socket no Linux (grupo/sudo), revisão final de docs,
   release conjunta com o `focusguard-web`.
