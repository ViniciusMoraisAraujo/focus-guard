# GEMINI.md — focusguard-ui/

> Guidelines for AI agents and developers working in this directory. Also consult
> the root **[GEMINI.md](../GEMINI.md)** (specs, core conventions, architecture)
> and **[docs/ui-plan.md](../docs/ui-plan.md)** (UI architecture & API contract)
> before editing any code.

## Purpose

The **React 18 + Vite + TypeScript** web interface for FocusGuard. Runs in
the browser against `http://127.0.0.1:48902` (`focusguard-web` acts as the user-space
HTTP-to-IPC proxy to the daemon). The compiled `dist/` is bundled into
`cmd/focusguard-web/assets/` via `make ui` and embedded via `go:embed`.

---

## Directory structure

| Path | Purpose |
|---|---|
| `src/api/types.ts` | **Mirrors the Go IPC contract** (`ipc.Request/Response`, `policy.Block`, `preset.Preset`, `pomodoro.State`, `analytics.Stats`, `schedule.Rule`, `tamper.Event`) — updated via `make contract` |
| `src/api/client.ts` | IPC client: `action()` (POST `/api/action`), `pingDaemon()`, `execAction()` |
| `src/context/` | State providers: `auth-context.tsx` (session/login), `data-context.tsx` (polling/SSE status, stats, daemon health) |
| `src/App.tsx` | App shell: desktop sidebar + mobile Sheet navigation for all 12 screens |
| `src/screens/` | **12 screens**: Dashboard, Bloquear, Pomodoro, Agenda, Apps, Presets, Estatísticas, Segurança, Configurações, Login, Rede, Guia |
| `src/components/` | Custom components (`circular-timer.tsx`, `weekly-grid.tsx`, `theme-provider.tsx`, etc.) |
| `src/components/ui/` | shadcn-style UI primitives (button, card, dialog, sheet, tabs, sonner, etc.) |
| `src/hooks/` | Custom client hooks (`useCountdown.ts`, etc.) |
| `src/lib/utils.ts` | Utility functions (`cn` for Tailwind class merging) |

---

## Specific conventions

1. **Strict TDD (Test-Driven Development) — MANDATORY**:
   Always write tests first using Vitest (`npm test`) for new components, screens, hooks, and API client routines. Run `npm test` and `npx tsc --noEmit` before considering any task complete.
2. **Never assume state client-side**:
   Mutations (blocking, pomodoro, goals, schedules) must trust the daemon's response (`success`/`message`). Always handle `success: false` and surface `message` in the UI.
3. **Go ↔ JS serialization rules**:
   - Durations from Go (`goal`, elapsed time) arrive in **nanoseconds** (convert: `ns / 1e9 / 60` for minutes).
   - Timestamps (`ExpiresAt`, `StartedAt`, `at`) arrive in **RFC3339** format (`new Date(rfc3339)`).
   - Duration inputs to Go must be valid Go duration strings (e.g., `"30m"`, `"2h"`, `"45m"`).
4. **Domain-level blocking**:
   Blocks apply exclusively to domains, hostnames, or category presets (no path-based blocking).
5. **Daemon offline handling**:
   When `focusguard-web` returns HTTP 503, the UI must gracefully display the "daemon offline" status banner and suppress mutative actions.
6. **Security & Content Protection**:
   Never use `dangerouslySetInnerHTML`. Rely on standard React escaping and strict backend CSP headers.
7. **Session log**:
   Update `../docs/session-log/YYYY-MM-DD.md` at the end of each session.

---

## Known gotchas & Integration notes

- **API client consolidation**: Prefer `execAction` for newer daemon actions rather than ad-hoc helpers in `client.ts`.
- **Context stale state**: On daemon disconnection, ensure cached stats/presets do not linger misleadingly.
- **Contract parity**: When modifying Go IPC structs in `internal/transport/ipc`, run `make contract` to re-generate `src/api/types.ts` and commit both in the same commit.

---

## Validation

- Compile check: `npm run build` or `npx tsc --noEmit` must pass with zero errors.
- Unit tests: `npm test` must pass all test suites.
- Embed update: Run `make ui` from the repository root to compile and copy assets into `cmd/focusguard-web/assets`.
