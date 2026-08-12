# AGENT.md — FocusGuard repository guide

> Reference document for **AI agents and developers** working in this
> repository. Read it before editing code. Keep it up to date whenever
> conventions, architecture, or rules change.

## Table of contents

0. [Never do without explicit confirmation](#0-never-do-without-explicit-confirmation)
1. [Specs — what this project is](#1-specs--what-this-project-is)
2. [Language and stack](#2-language-and-stack)
3. [Architecture](#3-architecture)
4. [Code conventions](#4-code-conventions-current-rules)
5. [Testing, validation, and Definition of Done](#5-testing-validation-and-definition-of-done)
6. [Relevant file structure](#6-relevant-file-structure)
7. [Commit conventions](#7-commit-conventions)
8. [Release](#8-release)
9. [Known pitfalls](#9-known-pitfalls)
10. [Glossary](#10-glossary)

---

## 0. Never do without explicit confirmation

If a task requires any of the following, **stop and ask** before proceeding —
don't assume it's implied by the request:

- Adding a manifest/admin elevation to the tray or to `focusguard-web` (only
  the daemon is privileged).
- Creating a "manual unblock" command, or any shortcut to undo a block
  before it expires — that's a product decision, not a bug.
- Reintroducing the pending-restart mechanism (`pendingRestart`/watcher) in
  the update flow — it was removed on purpose.
- Calling `sc.exe`/`iptables`/`systemctl`/any other real OS binary inside a
  unit test — always mock it (`execCommand`, `os.Stat`, etc.).
- Manually editing/generating `.syso`, `.ico`/`.png`, or `versioninfo.json` —
  only via `make icon` / `make winres`.
- Changing the `internal/transport/ipc` payload without updating CLI + tray + web +
  `focusguard-ui/src/api/types.ts` in the same commit.
- Changing the `state.json` schema without a migration/compatibility plan
  for state already persisted on disk.
- Saving `install-daemon.ps1` without preserving the UTF-8 BOM (`EF BB BF`).

In any of these cases: describe the plan and ask for confirmation before
editing code.

---

## 1. Specs — what this project is

**FocusGuard** is a Go tool to **block distracting websites** and help
maintain focus, operating at the system level (`hosts` file + firewall
rules). It's a client-server application:

- **CLI** (`focusguard`) — command-line interface; with no arguments it
  opens the web UI in the browser. `cmd/focusguard/` has **one file per
  command** + a `Command` table (`commands.go`): a new command is a new file
  + one entry in the table (and `usageOrder` for the help order) — the help
  is generated from the table (B5).
- **Daemon** (`focusguard-daemon`) — background service that applies and
  maintains blocks; talks to the CLI via IPC (Unix socket).
- **Tray** (`focusguard-tray`) — system tray icon with quick actions.
- **Watchdog** (`focusguard-watchdog`) — external health-check service and
  Smart Recovery (rollback after a broken update).
- **`focusguard-icon`** — **build-only** command (pure stdlib) that
  generates the multi-size icon `focusguard.ico` (Windows) and
  `focusguard.png` (Linux).

Main features: temporary blocks (no manual unblock), panic mode
(`block --internet`) with allowlist, category presets, pomodoro, recurring
scheduling, daily goals + streaks, analytics with export, process guard
(kills denylisted processes), tamper detection (tamper log), DNS sinkhole
(port 53, "Rei da Rede"), multi-binary auto-update with rollback, and a
**complete web UI**.

> ✅ **Web interface (complete):** `focusguard-web` (user-space, on demand)
> serves the React + TS UI (`focusguard-ui/`) and **proxies IPC actions to
> the daemon** at `http://127.0.0.1:48902` — **no changes to the daemon**.
> All 12 screens are implemented (Dashboard, Bloquear, Pânico, Pomodoro,
> Agenda, Apps, Presets, Estatísticas, Segurança, Configurações, Login,
> Rede) with login/sessions, SSE real-time events, and auth-gated actions.
> See the plan and API contract in `docs/ui-plan.md` before writing related
> code.

### Platforms

| | Linux | Windows |
|---|---|---|
| Firewall | `iptables`/`ip6tables` | `netsh advfirewall` |
| Hosts | `/etc/hosts` | `C:\Windows\System32\drivers\etc\hosts` |
| IPC socket | `/run/focusguard.sock` (`root:focusguard` 0660 — membros do grupo `focusguard` usam sem sudo; F5 do ui-plan) | `%PROGRAMDATA%\FocusGuard\focusguard.sock` |
| Service | systemd (unit + `NOTIFY_SOCKET` watchdog) | Native `svc` service (SCM), `sc.exe` |
| Install path | `/opt/focusguard` (root:root) | `C:\Program Files\FocusGuard` (System / All Users) |
| State | `/var/lib/focusguard/` | `C:\ProgramData\FocusGuard\` |
| Tray autostart | `~/.config/autostart` (XDG) | HKCU `...\CurrentVersion\Run` |

---

## 2. Language and stack

- **Go 1.26.5** (see `go.mod`, module `focusguard`). No CGO on Windows or
  Linux, except the tray (`tray-linux` requires CGO: appindicator/GTK).
- Main dependencies (all already in `go.mod` — **avoid adding new ones
  unless necessary; stdlib first**):
  - `fsnotify` — file watchers (hosts, state).
  - `getlantern/systray` — system tray.
  - `creativeprojects/go-selfupdate` — auto-update (beta/stable channels).
  - `golang.org/x/sys` — Windows service (`svc`), etc.
  - `golang.org/x/mod/semver` — version comparison for updates.
- **`go-winres`** (build tool, not a Go dependency) — generates Windows
  `.exe` metadata (`rsrc_windows_*.syso`) from `versioninfo.json`. Install
  with: `go install github.com/tc-hib/go-winres@latest`.

### Languages used in the repo

- **Code, identifiers, and code comments**: English.
- **UI/CLI-facing strings**: currently Brazilian Portuguese (PT-BR) —
  match the existing convention in the file you're editing rather than
  mixing languages within the same package.
- **README, CHANGELOG, and other docs**: PT-BR (this file is the
  exception, kept in English).
- **Commit messages**: English (Conventional Commits — section 7).
- **This AGENT.md**: English. Agent-facing instructions live here in
  English regardless of the language used elsewhere in the repo.

---

## 3. Architecture

### Data flow

```
CLI (focusguard) ────────┐
Tray (focusguard-tray) ──┼── IPC (Unix socket, JSON) ──→ Daemon (focusguard-daemon)
Web (focusguard-web) ────┘                                        │
                                                             [Scheduler]  ← source of truth, in RAM
                                                          ┌────────┴────────┐
                                                     [Store]          [Enforcer]
                                               (atomic state.json)   ┌────┴────┐
                                                               /etc/hosts  Firewall
                                                               (hosts)  (iptables/netsh)
                                                          ┌────────┴────────┐
                                                   [HostsWatcher]    [StateWatcher]
                                                  (fsnotify + SHA-256, restore disk from RAM)
```

### Binaries (`cmd/`)

| Binary | Role | Windows resources |
|---|---|---|
| `focusguard` | CLI (commands; no args opens the web UI) | `cmd/focusguard/versioninfo.json` (icon only, no manifest) |
| `focusguard-daemon` | Background service | `packaging/versioninfo-daemon.json` + `packaging/focusguard-daemon.exe.manifest` (**`requireAdministrator`**) |
| `focusguard-tray` | System tray | `cmd/focusguard-tray/versioninfo.json` (**icon only — NEVER manifest/admin**) |
| `focusguard-watchdog` | Health-check / Smart Recovery | `cmd/focusguard-watchdog/versioninfo.json` (icon + version, no manifest) |
| `focusguard-web` | Serves the web UI + proxies IPC actions (user-space, on demand) | No manifest — **never** add admin |
| `focusguard-icon` | Generates icons (build-time) | — |

> ⚠️ The daemon embeds a `requireAdministrator` manifest; that's why
> **running `go test ./cmd/focusguard-daemon/...` in a non-elevated shell
> fails** (the test binary won't start) — this is an environment
> limitation, not a bug.

### Internal packages (`internal/`) — quick map

Every package has a dedicated test (`*_test.go` in the same directory);
implementation details belong in code comments, not here.Packages are grouped in four layers — `domain` (business logic), `infrastructure` (OS I/O), `transport` (IPC/HTTP + observability), `system` (daemon/tray/watchdog lifecycle); see `docs/reorg-plan.md` Fase C.

| Package | One-liner |
|---|---|
| `domain/analytics` | Session history (JSONL), streaks, stats, export, report |
| `domain/apps` | Process denylist for the process guard |
| `infrastructure/autostart` | Installs/removes the service + tray autostart + desktop shortcut |
| `domain/blocks` | Domain handlers for the `block`/`block-all` actions (`Blocker`/`Catalog`) |
| `system/daemon` | Daemon lifecycle: `Run(ctx) error` + ordered shutdown (B10) |
| `infrastructure/dns` | Domain handlers for the DNS sinkhole (`start`/`stop`/`status`/`set-upstream`) |
| `infrastructure/dnsserver` | Embedded DNS sinkhole (port 53, miekg/dns) + upstream forwarding |
| `infrastructure/enforcer` | Applies blocks at the OS level (hosts + firewall), per platform |
| `transport/eventhub` | In-process event pub/sub (ring buffer + long-poll `Wait`) — daemon state changes |
| `infrastructure/filelog` | Shared file logging (append + rotation) next to the executable |
| `infrastructure/fsutil` | Filesystem helpers shared by the watchers |
| `domain/goal` | Daily focus goal |
| `infrastructure/hostswatch` | Detects/reverts tampering of `hosts` |
| `transport/httpapi` | Web UI HTTP server: IPC proxy + static assets + security guards |
| `infrastructure/icon` | Generates `.ico`/`.png` from the canonical artwork |
| `transport/ipc` | Client-server protocol (Request/Response JSON) + action registry (`Handler`/`Registry`/`ActionSpec`) |
| `transport/ipcerr` | Stable IPC error codes (`Error`) — mirror of `internal/transport/ipc/codes.go`, additive-only |
| `transport/metrics` | Per-action latency registry (ring + percentiles) — daemon IPC and web proxy |
| `domain/policy` | `Block` model and business rules (`IsActive`, `CanUnblock`, ...) |
| `domain/pomodoro` | Work/rest/cycle sessions |
| `domain/preset` | Catalog of block categories (builtin + custom) |
| `domain/presets` | Domain handlers for the preset catalog actions (list/add/remove) |
| `infrastructure/processguard` | Kills denylisted processes during an active session |
| `domain/recovery` | Smart Recovery: detects and reverts a broken update |
| `domain/schedule` | Recurring block scheduling |
| `domain/scheduler` | Block lifecycle (source of truth in RAM) |
| `infrastructure/statewatch` | Detects/reverts tampering of `state.json` |
| `infrastructure/store` | Atomic JSON persistence + encrypted replicas |
| `infrastructure/tamper` | Append-only log of tampering attempts |
| `system/tray` | System tray icon controller |
| `infrastructure/update` | Atomic multi-binary auto-update, with daemon restart |
| `domain/user` | User accounts/password store (admin user, hashing) |
| `domain/users` | Domain handlers for user management (add/remove/verify/set-password) |
| `system/watchdog` | systemd health check (`NOTIFY_SOCKET`) |

---

## 4. Code conventions (current rules)

1. **TDD** — write tests alongside the feature, in the same commit. Packages
   have extensive coverage (see the test table in the README). Don't break
   existing tests.
2. **Per-platform files** — use `_windows.go`, `_linux.go`, `_other.go`
   suffixes (implicit build tags) for OS-specific code, with a shared
   interface in the base file (e.g., `enforcer.go`, `systray.go`).
3. **Source of truth in RAM** — the scheduler keeps state in memory; disk
   (`state.json`) is a reflection. Watchers restore disk from RAM when they
   detect tampering.
4. **Atomic writes** — `store` writes to a temp file then renames; writes
   made by the daemon itself are marked via **SHA-256** so watchers don't
   react to their own self-writes (no loop).
5. **Best-effort for the OS** — a firewall/notification/autostart failure
   **never aborts the daemon**: it logs, warns, and moves on. Convention:
   auxiliary operations return an error without breaking the main flow.
6. **IPC never blocks the tray** — every tray→daemon call uses
   `SendWithTimeout` (5s); systray handlers are non-blocking.
7. **Defensive coding** — sanitize domains (`sanitizeDomain`: strips
   scheme/CRLF/whitespace, collapses `www.`), validate inputs, enforce caps
   (e.g., pomodoro `--work` max 7 days), atomic rollback on update, sweep
   orphaned firewall rules.
8. **Append-only JSONL persistence** for logs (analytics, tamper) — corrupt
   lines are skipped on read.
9. **No new dependencies unless necessary** — stdlib first; check `go.mod`
   before pulling in a library.
10. **Windows resources via go-winres** — changed `versioninfo.json` or the
    icon? Run `make winres` (and `make icon` to regenerate
    `focusguard.ico`/`.png`). The `.syso` files **are committed** (CI needs
    them).
11. **Web UI: run `make ui` before building `focusguard-web`** — the React
    dist is copied into `cmd/focusguard-web/assets` (gitignored, only
    `.gitkeep` is versioned) and embedded via `go:embed`. Without `make ui`,
    the binary serves a "run make ui" page. Dev: `go run ./cmd/focusguard-web
    -assets focusguard-ui/dist` or `cd focusguard-ui && npm run dev` (Vite
    on :5173 with `/api` proxy).
12. **Never add a manifest to the tray** — the tray runs as a regular user,
    unelevated. The daemon is the only binary with `requireAdministrator`.
13. **Script files** — `*.sh` use LF line endings (`.gitattributes`);
    `install-daemon.ps1` must keep its **UTF-8 BOM** (write with
    `UTF8Encoding($true)` when re-saving).
14. **Tests mock the OS** — tests for packages that shell out (autostart,
    enforcer, etc.) mock `execCommand` and `os.Stat` (e.g., the
    `fakeFileInfo` helper) so they don't depend on real system binaries or
    elevation. Follow this pattern in new tests — never call real
    `sc.exe`/`iptables`/`systemctl` inside a unit test.
15. **Session log (handoff between sessions/days)** — at the end of every
    working session, write or update `docs/session-log/YYYY-MM-DD.md` (the
    file for the current date; update it if it already exists — don't create
    a duplicate). Keep it **brief and factual** — it's the handoff that
    guides the next agent: what was done (features/fixes/refactors, key
    files, commit hashes), important decisions and the why, what's still in
    flight (uncommitted work, open items), and the validation status. Follow
    the template and rules in `docs/session-log/README.md`. A session
    without its summary is not finished. **Enforced automatically:**
    `make session-check` fails if today's entry is missing or off-template
    (run it before committing), and CI validates the structure of every
    existing entry (`scripts/check-session-log.sh` in `.github/workflows/`
    `test.yml` — the CI check does NOT require today's file, since a push
    can happen on a different day than the work).

---

## 5. Testing, validation, and Definition of Done

### Definition of Done — don't say "done" until every box is checked

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` succeeds
- [ ] `gofmt -l <changed files>` has no output (or run `gofmt -w`)
- [ ] `go test ./... -count=1 -timeout=60s` passes
- [ ] `git status` shows no build artifacts (`bin/`, `.exe`, etc.)
- [ ] New tests cover the change (section 4, rule 1)
- [ ] Session summary written/updated in `docs/session-log/` (section 4, rule 15) — `make session-check` must pass
- [ ] Commit message follows Conventional Commits (section 7)

```bash
go build ./...                        # compiles everything
go vet ./...                          # static analysis
gofmt -l <changed files>              # formatting (or gofmt -w)
go test ./... -count=1 -timeout=60s   # make test
```

- Package-specific tests: `go test ./internal/<pkg>/... -v`.
- After touching icon/versioninfo: `make icon && make winres` and verify
  the embedded icon with `go run ./scripts/verifyicon/main.go` (the script
  carries a `//go:build ignore` tag, so pass the file explicitly).
- After touching `install-daemon.ps1`: validate the syntax with the
  PowerShell parser and **confirm the BOM (EF BB BF) was preserved**.
- ⚠️ Windows: `go test ./cmd/focusguard-daemon/...` requires an **elevated**
  shell (manifest `requireAdministrator`). In a non-admin shell, only run
  the non-elevated packages.
- Command-line cross-checks on Windows: use POSIX bash (never `dir`/`copy`/
  `findstr`); use `/` in paths.

---

## 6. Relevant file structure

```
├── AGENT.md                    # this guide
├── docs/
│   ├── ui-plan.md              # web UI plan + API contract (12 screens)
│   ├── bug-hunt-plan.md        # completed bug-hunt (Etapas 0–8, 4 real bugs fixed)
│   ├── reorg-plan.md           # internal/ layering + packaging reorg
│   ├── release.md              # release checklist and process
│   ├── session-log/            # daily handoff summaries (README has the template)
│   └── perf-2026-08-05.md / dns-sinkhole-spec.md
├── Makefile                    # build, icon, winres, ui, contract(-check), msi, test, vet, fmt, tidy, clean, install, uninstall, session-check
├── internal/transport/httpapi/  # HTTP: IPC proxy + static assets + localhost security
├── focusguard-ui/              # React + Vite + TS frontend (12 screens)
│   └── src/screens/              # Dashboard, Block, Panic, Settings, Pomodoro, Schedule, Apps, Presets, Stats, Security, Login, Rede
├── .goreleaser.yaml            # release pipeline
├── .github/workflows/
│   ├── release.yml             # CI: tag v* → GoReleaser + MSI (desktop/server)
│   └── test.yml                # CI: build+vet, -race (Linux), socket chown as root
├── .gitattributes              # *.sh → eol=lf
├── packaging/                  # build-time assets
│   ├── artwork/focusguard.png  # canonical artwork (1024px)
│   ├── focusguard.ico / .png   # system icon (generated by cmd/focusguard-icon)
│   ├── versioninfo-daemon.json # daemon's Windows resources (icon + manifest + version)
│   ├── focusguard-daemon.exe.manifest
│   └── server.role / install.txt
├── cmd/
│   ├── focusguard/             # CLI (+ its own versioninfo.json)
│   ├── focusguard-daemon/      # service (+ rsrc_windows_*.syso)
│   ├── focusguard-icon/        # icon generator (build-time)
│   ├── focusguard-tray/        # systray (+ icon-only versioninfo.json)
│   ├── focusguard-watchdog/    # health-check / Smart Recovery (+ versioninfo.json with icon)
│   └── focusguard-web/         # web server (user-space, no manifest)
├── internal/                   # 34 packages in 4 layers (see the map in section 3)
│   ├── domain/                 # business logic (13 packages)
│   ├── infrastructure/         # OS I/O (13 packages)
│   ├── transport/              # IPC/HTTP + observability (5 packages)
│   └── system/                 # daemon/tray/watchdog lifecycle (3 packages)
└── scripts/
    ├── install-daemon.ps1      # Windows install (copies to Program Files, service, shortcut, tray, watchdog)
    ├── install-linux.sh        # Linux install (/opt/focusguard, systemd, XDG autostart, socket group)
    ├── focusguard.service      # systemd unit
    ├── focusguard-tray.desktop # tray shortcut template (Linux)
    ├── build-msi.sh            # .msi build via go-msi + WiX
    ├── check-session-log.sh    # validates docs/session-log (CI structure + make session-check --today)
    ├── msi/                    # wix.json / wix-server.json / product.wxs
    └── verifyicon/             # verifies the embedded icon matches focusguard.ico
```

---

## 7. Commit conventions

**Conventional Commits, in English, with scope.** Format:

```
<type>(<scope>): <short imperative description>
```

Types used in history: `feat`, `fix`, `perf`, `docs`, `test`, `ci`, `chore`.
Typical scopes: `ui`, `install`, `icon`, `tray`, `update`, `store`,
`scheduler`, `enforcer`, `watchers`, `daemon`, `ipc`, `autostart`, `focus`,
`changelog`, `readme`, `release`.

Real examples from the repository:

```
refactor(cli): remove interactive TUI, open web UI by default
feat(install): install to Program Files with desktop shortcut, tray and watchdog
feat(icon): add focusguard.ico embedded in executables, tray and shortcut
fix(enforcer): roll back partial firewall rules on block failure
fix(update): atomic multi-binary update, semver compare and auto-restart
feat(update): restart daemon immediately on version update, keep blocks
perf(store): drop fsync from state.json saves
docs(changelog): add v0.6.0 release section
ci(release): allow different binary counts in linux archive
test(daemon): cover real store-statewatch-scheduler chain
```

Rules:

- **One commit per coherent change.** Don't mix feature + docs + fix in the
  same commit.
- Docs updates (CHANGELOG/README) get their own commit
  (`docs(changelog): ...` / `docs(readme): ...`).
- Short description (≤ ~72 chars), lowercase, imperative mood.
- Don't commit build artifacts (`bin/`, compiled executables) — check
  `git status` first. **However**, `focusguard-daemon.exe.manifest`, the
  `rsrc_windows_*.syso` files, and `versioninfo.json` **are intentionally
  versioned** (`go build` and CI need them): commit them normally alongside
  code.
- Commit code **with passing tests** (section 5).

---

## 8. Release

This is a rare process, done manually by the maintainer — **not a standard
agent task**. If asked to cut a release, follow `docs/release.md` (full
checklist for CHANGELOG, tagging, and what CI/GoReleaser produces) and
confirm the version/tag with the person before pushing the tag
(`git push origin vX.Y.Z` triggers publication — not silently reversible).

---

## 9. Known pitfalls

- **Daemon requires admin** — `requireAdministrator` manifest; daemon tests
  don't run in a non-elevated shell (Windows). Don't "fix" this: it's
  intentional.
- **Tray without admin** — the tray **cannot** get a manifest; it runs as a
  regular user and must not require elevation.
- **ps1 BOM** — editing `install-daemon.ps1` and re-saving without a UTF-8
  BOM breaks accented characters in PowerShell 5.1. Preserve `EF BB BF`.
- **Don't regress the icon fallback** — the tray uses **exclusively** the
  embedded icon (`RT_GROUP_ICON` + `RT_ICON`); don't reintroduce runtime
  rendering or depend on external assets at runtime.
- **Update restarts immediately** — applying an update ends the pomodoro
  session (best-effort) and restarts the daemon right away (exit 1 →
  supervisor brings up the new version). Blocks are **not** touched: they
  stay in `state.json` and boot restores them. Don't reintroduce the
  pending-restart mechanism (`pendingRestart`/watcher) — it was removed on
  purpose.
- **Windows update stops watchdog + tray before the swap** — before
  `UpdateToAll`, the daemon stops the `FocusGuardWatchdog` service and
  `taskkill`s the tray (running GUI exes are locked against rename — the
  "Acesso negado" bug). The tray only returns at the next login (HKCU Run).
  If the daemon's own exe still refuses to be renamed (it can't stop
  itself), `UpdateToAll` schedules the whole suite via
  `MoveFileEx(MOVEFILE_DELAY_UNTIL_REBOOT)` and returns
  `ErrScheduledOnReboot`: the daemon keeps running the old version, clears
  `update.inprogress` and reports `PendingReboot` (no restart). The staged
  files use `.<name>.new` (never `focusguard-daemon-new*`, which
  `CleanupStale` would sweep before reboot).
- **GoReleaser hook has no shell** — the `go-winres` hook uses `sh -c`
  because GoReleaser runs hooks without a shell; keep that conditional when
  touching it.
- **IPC is the contract** — CLI/tray/daemon/**web** all speak the same
  protocol; changing the `internal/transport/ipc` payload requires updating all three
  sides + `focusguard-ui/src/api/types.ts` in the same commit. The TS mirror
  is **generated** — after changing an IPC struct run `make contract` (and
  `make contract-check` to verify no drift; the CI checks it before a
  release).
- **Actions live in the registry, not the switch** — `ipc.Server` is a pure
  transport: `NewServer` registers only the server-level handlers
  (ping/status/tamper/service adapters), and the daemon (composition root)
  registers the domain-backed ones (`block`, `block-all`, `apps-*`, `goal-*`,
  `presets`, `preset-*`, `user-*`, `dns-*`, `stats`/`missions`/`sessions`,
  `schedule-*`, `pomodoro-*`, `update`/`update-check`) from their domain
  packages via
  `server.Register` before `ValidateRegistry`. The legacy switch
  (`dispatchLegacy`) is gone (Fase 4); an unregistered action returns
  `CodeUnknownAction`. A new action is a `Handler` + one line in `specs`
  (`internal/transport/ipc/spec.go`) + `Register` in the composition root — never a new
  `case`. Domain-backed actions mount via `ipc.DomainAction[In, Out]` (the
  domain package defines its own input/output types and never imports ipc —
  DIP; the composition root translates the wire with Decode/Encode) and the
  composition root registers `...{...}.Handler()`. The in-package ipc tests use reference adapters
  (`handlers_ref_test.go`) because ipc cannot import the domain packages
  (cycle); `domain_wiring_test.go` (package `ipc_test`) wires the REAL domain
  handlers through the router. `ValidateRegistry()` closes specs↔registry at
  boot (every handler has a spec, every spec has a handler; `user-verify` is
  web-only and exempt). The web proxy (`httpapi`) reads permission + timeout
  from `ipc.SpecFor`; an action **without** a spec (`user-verify`, unknown) is
  not forwarded (403 allowlist).
- **`focusguard-web` is user-space** — never add a manifest/admin to it;
  the daemon is the only privileged process. Web only proxies via
  `ipc.Client`.
- **Real-time events** — the daemon publishes coarse state changes on
  `internal/transport/eventhub` (`scheduler`/`pomodoro`/`schedule`/`watchPomodoroCompletions`
  hooks); the web relays the `event-subscribe` long-poll over SSE
  (`GET /api/events`) and the UI refreshes the affected data. New event type =
  `ipc.Event*` constant + publish point + frontend listener (regenerate
  `types.ts` via `make contract` if the wire type changes).
- **Observability** — the daemon measures every IPC dispatch into
  `internal/transport/metrics` and logs structured slow-action lines (> 1s;
  `event-subscribe` excluded — it is a 20s long-poll by design); the web
  proxy measures its own per-action latency and exposes both via
  `GET /api/metrics`; read the daemon side with `focusguard metrics
  [--reset]`. New actions are measured automatically.
- **Port 48902** — `httpapi.DefaultAddr` is the single source of truth
  (server + CLI probe); don't scatter the port across literals.
- **`git status` before committing** — `.syso`, `focusguard.ico/.png`, and
  `versioninfo.json` are versioned and change when you run `make icon`/
  `make winres`; don't ignore them or accidentally commit them in code
  commits.
- **Bug-hunt is done, don't regress the fixes** — `docs/bug-hunt-plan.md`
  records Etapas 0–8 (2026-08-10) and the 4 real bugs fixed with TDD tests:
  orphan firewall rule left when the last block expires (`scheduler`),
  `BlockDomains` batch dropping pre-existing protection, refresh goroutine
  leaking on shutdown, and the ICS `+1h` fallback emitting a `"24:xx"`
  window past midnight. The scheduler package has `fuzz_test.go` (3 targets)
  and the repo runs `-race` + the socket-chown test as root in CI
  (`.github/workflows/test.yml`).

---

## 10. Glossary

- **Self-write** — a write to `hosts`/`state.json` made by the daemon
  itself; marked via hash so watchers don't treat it as external tampering.
- **Reconcile** — the scheduler re-reads state from RAM and reapplies
  active blocks at the OS level (hosts + firewall), fixing any drift.
- **Source of truth in RAM** — `state.json` reflects what the scheduler
  holds in memory, not the other way around; on conflict, RAM wins and disk
  is rewritten.
- **Tamper** — any external change (outside the daemon) to `hosts` or
  `state.json` that removes or weakens an active block.
- **Best-effort** — an operation whose failure is logged but never aborts
  the daemon (e.g., a failed notification, a failed autostart registration).
- **Smart Recovery** — the watchdog's automatic rollback when an update
  leaves the daemon broken (not to be confused with "manual unblock",
  which doesn't exist).
