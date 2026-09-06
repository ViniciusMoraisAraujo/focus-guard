# GEMINI.md — cmd/

> Guidelines for AI agents and developers working in this directory. Also consult
> the root **[GEMINI.md](../GEMINI.md)** (specs, core conventions, architecture)
> before editing any code.

## Purpose

Entry points for FocusGuard **binaries** (6 `package main` applications). Most
are thin adapters: they parse CLI flags/commands, assemble `ipc.Request`, call
the daemon over IPC, and render results. The core business logic lives in `internal/`.

| Binary | Role | Windows Resources |
|---|---|---|
| `focusguard/` | CLI (no args opens web UI in browser) | `cmd/focusguard/versioninfo.json` (icon only, no manifest) |
| `focusguard-daemon/` | Privileged background service | `packaging/versioninfo-daemon.json` + manifest (**`requireAdministrator`**) |
| `focusguard-tray/` | System tray companion | `cmd/focusguard-tray/versioninfo.json` (**icon only — NEVER manifest/admin**) |
| `focusguard-watchdog/` | Health-check & Smart Recovery | `cmd/focusguard-watchdog/versioninfo.json` (icon + version, no manifest) |
| `focusguard-web/` | Serves web UI & proxies IPC actions (user-space) | **No manifest** — never add admin |
| `focusguard-icon/` | Generates `focusguard.ico` and `.png` (pure stdlib build tool) | — |

---

## Specific conventions

1. **Strict TDD (Test-Driven Development)**: Write failing tests before modifying or adding CLI commands or binary lifecycle logic.
2. **Language split**: CLI output messages and prompts are in **PT-BR**; identifiers, code, and comments are in **English**.
3. **Stubbable process exits**: Use `var osExit = os.Exit` in `main.go` files that require exit codes so tests can stub it safely; `focusguard-web` uses standard `log.Fatalf`.
4. **Embedded web UI**: `focusguard-web` embeds the React build via `go:embed all:assets`. If `make ui` has not been run, the binary serves an informational "run make ui" fallback page (tested behavior).
5. **Dependency injection for OS interactions**: Browser spawning, process queries, network probes, and executable resolution are injectable (`probeWebServerFn`, `spawnWebServerFn`, `daemonResponds`, `killDaemon`, `osExecutable`). Always mock these in tests; never launch a real browser or SCM service during unit tests.
6. **Binary co-location**: `focusguard` discovers companion binaries (`focusguard-web`, `-daemon`, `-tray`, `-watchdog`) **alongside its own executable path** (`os.Executable()` + `filepath.Dir`). Do not break this co-location contract.
7. **File logging**: Each binary writes `<name>.log` next to the daemon binary via `internal/infrastructure/filelog` (append + 1 MiB rotation, best-effort: failures fall back to stderr without terminating). Follow the standard `logging.go` pattern.
8. **Session log**: Keep `../docs/session-log/YYYY-MM-DD.md` updated at the end of each session.

---

## Testing & Validation

- `go test ./cmd/focusguard/... ./cmd/focusguard-watchdog/... ./cmd/focusguard-web/...` (run unprivileged).
- ⚠️ `go test ./cmd/focusguard-daemon/...` on Windows requires an elevated prompt (`requireAdministrator` manifest). On Linux and CI, it runs hermetically unprivileged via `setupDaemonTestEnv`.
- `go build ./... && go vet ./...` before finishing any task.
