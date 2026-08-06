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
- Changing the `internal/ipc` payload without updating CLI + tray + web +
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
(kills denylisted processes), tamper detection (tamper log), and
multi-binary auto-update with rollback.

> 🚧 **Web interface (in progress):** `focusguard-web` (user-space, on
> demand) serves the React + TS UI (`focusguard-ui/`) and **proxies IPC
> actions to the daemon** at `http://127.0.0.1:48902` — **no changes to the
> daemon**. F1 (HTTP server) and F2 (10 screens) were implemented on
> 2026-08-03; see the full plan and roadmap in `docs/ui-plan.md` before
> writing related code.

### Platforms

| | Linux | Windows |
|---|---|---|
| Firewall | `iptables`/`ip6tables` | `netsh advfirewall` |
| Hosts | `/etc/hosts` | `C:\Windows\System32\drivers\etc\hosts` |
| IPC socket | `/run/focusguard.sock` | `%PROGRAMDATA%\FocusGuard\focusguard.sock` |
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
| `focusguard-daemon` | Background service | root `versioninfo.json` + `focusguard-daemon.exe.manifest` (**`requireAdministrator`**) |
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
implementation details belong in code comments, not here.

| Package | One-liner |
|---|---|
| `analytics` | Session history (JSONL), streaks, stats, export, report |
| `apps` | Process denylist for the process guard |
| `autostart` | Installs/removes the service + tray autostart + desktop shortcut |
| `daemon` | Daemon lifecycle: `Run(ctx) error` + ordered shutdown (B10) |
| `enforcer` | Applies blocks at the OS level (hosts + firewall), per platform |
| `filelog` | Shared file logging (append + rotation) next to the executable |
| `fsutil` | Filesystem helpers shared by the watchers |
| `goal` | Daily focus goal |
| `hostswatch` | Detects/reverts tampering of `hosts` |
| `httpapi` | Web UI HTTP server: IPC proxy + static assets + security guards |
| `icon` | Generates `.ico`/`.png` from the canonical artwork |
| `ipc` | Client-server protocol (Request/Response JSON) + action registry (`Handler`/`Registry`/`ActionSpec`) |
| `policy` | `Block` model and business rules (`IsActive`, `CanUnblock`, ...) |
| `pomodoro` | Work/rest/cycle sessions |
| `preset` | Catalog of block categories (builtin + custom) |
| `processguard` | Kills denylisted processes during an active session |
| `recovery` | Smart Recovery: detects and reverts a broken update |
| `schedule` | Recurring block scheduling |
| `scheduler` | Block lifecycle (source of truth in RAM) |
| `statewatch` | Detects/reverts tampering of `state.json` |
| `store` | Atomic JSON persistence + encrypted replicas |
| `tamper` | Append-only log of tampering attempts |
| `tray` | System tray icon controller |
| `update` | Atomic multi-binary auto-update, with daemon restart |
| `watchdog` | systemd health check (`NOTIFY_SOCKET`) |

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

---

## 5. Testing, validation, and Definition of Done

### Definition of Done — don't say "done" until every box is checked

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` succeeds
- [ ] `gofmt -l <changed files>` has no output (or run `gofmt -w`)
- [ ] `go test ./... -count=1 -timeout=60s` passes
- [ ] `git status` shows no build artifacts (`bin/`, `.exe`, etc.)
- [ ] New tests cover the change (section 4, rule 1)
- [ ] Commit message follows Conventional Commits (section 7)

```bash
go build ./...                        # compiles everything
go vet ./...                          # static analysis
gofmt -l <changed files>              # formatting (or gofmt -w)
go test ./... -count=1 -timeout=60s   # make test
```

- Package-specific tests: `go test ./internal/<pkg>/... -v`.
- After touching icon/versioninfo: `make icon && make winres` and verify
  the embedded icon with `go run ./scripts/verifyicon`.
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
├── docs/ui-plan.md             # web UI plan (F1+F2 implemented, roadmap)
├── docs/release.md             # release checklist and process
├── Makefile                    # build, icon, winres, ui, test, vet, fmt, tidy, clean, install, uninstall
├── cmd/focusguard-web/         # web interface server (user-space, embeds the UI)
├── internal/httpapi/           # HTTP: IPC proxy + static assets + localhost security
├── focusguard-ui/              # React + Vite + TS frontend (10 screens)
│   └── src/screens/              # Dashboard, Block, Panic, Settings, Pomodoro, Schedule, Apps, Presets, Stats, Security
├── .goreleaser.yaml            # release pipeline
├── .github/workflows/release.yml  # CI: tag v* → GoReleaser
├── .gitattributes              # *.sh → eol=lf
├── focusguard.ico / .png       # system icon (generated by cmd/focusguard-icon)
├── versioninfo.json            # daemon's Windows resources (icon + manifest + version)
├── cmd/
│   ├── focusguard/             # CLI (+ its own versioninfo.json)
│   ├── focusguard-daemon/      # service (+ rsrc_windows_*.syso)
│   ├── focusguard-icon/        # icon generator (build-time)
│   ├── focusguard-tray/        # systray (+ icon-only versioninfo.json)
│   └── focusguard-watchdog/    # health-check / Smart Recovery (+ versioninfo.json with icon)
├── internal/                   # 24 packages (see the map in section 3)
└── scripts/
    ├── install-daemon.ps1      # Windows install (copies to Program Files, service, shortcut, tray, watchdog)
    ├── install-linux.sh        # Linux install (/opt/focusguard, systemd, XDG autostart)
    ├── focusguard.service      # systemd unit
    ├── focusguard-tray.desktop # tray shortcut template (Linux)
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
- **GoReleaser hook has no shell** — the `go-winres` hook uses `sh -c`
  because GoReleaser runs hooks without a shell; keep that conditional when
  touching it.
- **IPC is the contract** — CLI/tray/daemon/**web** all speak the same
  protocol; changing the `internal/ipc` payload requires updating all three
  sides + `focusguard-ui/src/api/types.ts`.
- **Actions live in the registry, not the switch** — `ipc.Server` is a pure
  transport: `NewServer` registers only the server-level handlers
  (ping/status/tamper/service adapters), and the daemon (composition root)
  registers the domain-backed ones (`block`, `block-all`, `apps-*`, `goal-*`,
  `presets`, `preset-*`, `user-*`, `dns-*`) from their domain packages via
  `server.Register` before `ValidateRegistry`. The legacy switch
  (`dispatchLegacy`) is gone (Fase 4); an unregistered action returns
  `CodeUnknownAction`. A new action is a `Handler` + one line in `specs`
  (`internal/ipc/spec.go`) + `Register` in the composition root — never a new
  `case`. The in-package ipc tests use reference adapters
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
- **Port 48902** — `httpapi.DefaultAddr` is the single source of truth
  (server + CLI probe); don't scatter the port across literals.
- **`git status` before committing** — `.syso`, `focusguard.ico/.png`, and
  `versioninfo.json` are versioned and change when you run `make icon`/
  `make winres`; don't ignore them or accidentally commit them in code
  commits.

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
