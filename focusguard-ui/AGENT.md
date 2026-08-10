# AGENT.md — focusguard-ui/

> Guia para agentes de IA que trabalham neste diretório. Consulte também o
> **[AGENT.md](../AGENT.md)** na raiz (specs, convenções, armadilhas) e o
> **[docs/ui-plan.md](../docs/ui-plan.md)** (plano + contrato da API) — leia-os
> antes de editar qualquer código.

## Propósito

Frontend **React 18 + Vite + TypeScript** da interface web do FocusGuard.
Roda no navegador contra `http://127.0.0.1:48902` (`focusguard-web` faz proxy
das ações IPC para o daemon). O `dist` compilado é copiado para
`cmd/focusguard-web/assets` pelo `make ui` e embutido via `go:embed`.

## Estrutura

| Caminho | Papel |
|---|---|
| `src/api/types.ts` | **Espelha o contrato IPC Go** (`ipc.Request/Response`, `policy.Block`, `preset.Preset`, `pomodoro.State`, `analytics.Stats`, `schedule.Rule`, `tamper.Event`) — manter em sincronia com `internal/transport/ipc` (gerado via `make contract`) |
| `src/api/client.ts` | `action()` (fetch POST `/api/action`), `pingDaemon()`, `execAction()` + `api.*` helpers (+ `client.test.ts`) |
| `src/context/` | Providers de estado: `auth-context.tsx` (login/sessão), `data-context.tsx` (status 10s, stats 60s, `daemonUp`), `index.tsx`, `types.ts` (+ `context.test.tsx`) |
| `src/App.tsx` | Shell: sidebar desktop + Sheet mobile, navegação entre as 12 telas |
| `src/screens/` | 12 telas: Dashboard, Bloquear, Panico, Pomodoro, Agenda, Apps, Presets, Estatisticas, Seguranca, Configuracoes, Login, Rede |
| `src/components/` | `circular-timer.tsx`, `weekly-grid.tsx` (+ `weekly-grid.test.tsx`), `screen.tsx`, `theme-provider.tsx`, `theme-toggle.tsx` |
| `src/components/ui/` | Componentes shadcn-style (button, card, dialog, sheet, tabs, tooltip, sonner, etc.) |
| `src/hooks/useCountdown.ts` | Countdown client-side |
| `src/lib/utils.ts` | `cn()` (clsx + tailwind-merge) |

## Regras específicas

1. **A UI nunca adivinha estado** — toda mutação (block, goal, pomodoro) confia
   na resposta do daemon (`success`/`message`); sempre trate `success:false` e
   exiba `message`.
2. **Serialização Go → JS**: `goal` e durações vêm em **nanossegundos**
   (converter `ns/1e9/60`); `ExpiresAt`/`StartedAt`/`at` são **RFC3339**
   (`new Date(rfc3339)`); `duration` (input) é string Go (`"30m"`, `"4h"`).
3. **Modo pânico** = domínio sentinela `*all-internet*` no status.
4. **Daemon offline** = HTTP 503 → a UI mostra o banner "daemon desligado".
5. **Sem `dangerouslySetInnerHTML`** — React escapa por padrão; CSP restritiva
   servida pelo backend.
6. Build: `npm ci && npm run build` (hooks do goreleaser/`make ui`); `tsc`
   deve passar (TS estrito).
7. Dev: `cd focusguard-ui && npm run dev` (Vite :5173 com proxy `/api`).
8. Testes (vitest): `npm test` — cobre o `weekly-grid` (janelas overnight,
   lanes, marcador "agora"), o `context/` (fallback SSE→polling) e o
   `client.ts`; rode junto com o `tsc` ao mexer na UI.

## Bugs e correções potenciais

- **`src/api/client.ts` — helpers `api.*` redundantes com `execAction`**
  (`api.update`, `api.pomodoro`, `api.missions`, etc.). A tela **Configurações
  aplica update de verdade** (`Configuracoes.tsx`, `execAction({ action:
  "update", channel })` com dialog de confirmação) — a seção 7.4 do ui-plan
  está desatualizada nesse ponto. Prefira `execAction` para ações novas;
  remova helpers não usados quando puder.

- ~~`api.pomodoro` enviava `work`/`rest`~~ — **corrigido**: agora envia
  `work_min`/`rest_min`, alinhado ao contrato Go
  (`internal/transport/ipc`, `ipc.Request.WorkMin/RestMin`).

- **`src/context.tsx` — `refresh()` só limpa `status` quando o daemon cai;
  `presets` e `stats` ficam com dados velhos** até o próximo ciclo bem-sucedido
  (presets são refetchados a cada 10s, stats a cada 60s). A UI pode exibir
  presets obsoletos por até ~10s após uma queda — considerar limpar `presets`
  junto com `status` quando `daemonUp` for `false`.

- **`src/api/types.ts` — `ApiResponse` não espelha todos os campos do
  `ipc.Response` Go** (ex.: `protection_error` existe, mas a UI trata via
  `status.doh_active`); ao adicionar ação nova, verifique **os dois lados**
  (Go `internal/transport/ipc` e `types.ts`) e o `ui-plan.md` seção 4.

- **`src/context/` — os providers fazem 2 fetchs simultâneos no load**
  (status + stats) e mais 2 por ciclo (10s/60s); ok para o volume, mas evite
  adicionar mais polling por tela (o `useCountdown` já é client-side).

## Validação

- `cd focusguard-ui && npm run build` (ou `npx tsc --noEmit`) — deve passar.
- Após mudanças na UI: rodar `make ui` para copiar o dist para
  `cmd/focusguard-web/assets` antes de compilar o binário.
