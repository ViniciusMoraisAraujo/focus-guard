# GEMINI.md — FocusGuard repository guide

> Primary reference document for **AI agents and developers** working in this
> repository. Read it before editing code. Keep it up to date whenever
> conventions, architecture, or rules change.

## Table of contents

0. [Never do without explicit confirmation](#0-never-do-without-explicit-confirmation)
1. [Specs — what this project is](#1-specs--what-this-project-is)
2. [Language and stack](#2-language-and-stack)
3. [Architecture](#3-architecture)
4. [Code conventions](#4-code-conventions)
5. [Testing, validation, and Definition of Done](#5-testing-validation-and-definition-of-done)
6. [Relevant file structure](#6-relevant-file-structure)
7. [Commit conventions](#7-commit-conventions)
8. [Release](#8-release)
9. [Known pitfalls & Gotchas](#9-known-pitfalls--gotchas)
10. [Glossary](#10-glossary)

---

## 0. Never do without explicit confirmation

If a task requires any of the following, **stop and ask** before proceeding —
do not assume it is implied by the request:

- **Elevation**: Adding an admin/elevation manifest to the tray or to `focusguard-web` (only the daemon is privileged).
- **Manual unblock / Bypass**: Creating any "manual unblock" command, shortcut, or backdoor to undo an active block before expiration — that is a core product design decision, not a bug.
- **Pending restart**: Reintroducing the pending-restart mechanism (`pendingRestart`/watcher) in the update flow — it was deliberately removed.
- **Real OS binaries in tests**: Invoking real `sc.exe`, `iptables`, `systemctl`, `netsh`, or other OS binaries inside unit tests — always mock them (`execCommand`, `os.Stat`, etc.).
- **Manual asset generation**: Manually editing or hand-crafting `.syso`, `.ico`/`.png`, or `versioninfo.json` — only use `make icon` and `make winres`.
- **Breaking IPC contract**: Changing `internal/transport/ipc` payloads without updating CLI, tray, web, and `focusguard-ui/src/api/types.ts` (`make contract`) in the exact same commit.
- **Unmigrated state changes**: Modifying the `state.json` schema without a backward-compatible migration plan for disk state.
- **PowerShell BOM**: Saving `install-daemon.ps1` without preserving its UTF-8 BOM (`EF BB BF`).

In any of these cases: explain the plan and obtain explicit confirmation before modifying code.

---

## 1. Specs — what this project is

**FocusGuard** is an integrity-focused tool to **block distracting websites and applications** and preserve productivity, operating at the system level (`hosts` file, DNS sinkhole, and firewall rules).

- **CLI (`focusguard`)** — Command-line interface. When invoked without arguments, it opens the web UI in the default browser. One file per command in `cmd/focusguard/` + command table in `commands.go`.
- **Daemon (`focusguard-daemon`)** — Background service that applies and maintains blocks; communicates with clients via IPC (Unix socket / named pipe).
- **Tray (`focusguard-tray`)** — System tray icon with quick controls and status updates. Runs unelevated.
- **Watchdog (`focusguard-watchdog`)** — External health-check monitor and Smart Recovery engine (rolls back broken updates).
- **Web UI & Server (`focusguard-web`)** — Serves the embedded React 18 + TS UI (`focusguard-ui/`) and proxies IPC actions to the daemon at `http://127.0.0.1:48902`. All 12 screens are implemented: Dashboard, Bloquear, Pomodoro, Agenda, Apps, Presets, Estatísticas, Segurança, Configurações, Login, Rede, Guia.
- **Mobile (`android/`)** — Native Android app (Kotlin + Jetpack Compose) using local `VpnService` DNS sinkhole and `AccessibilityService`.
- **Build tool (`focusguard-icon`)** — Build-time generator for multi-size `.ico` and `.png` icons from canonical artwork.

### Platforms

| Component | Linux | Windows |
|---|---|---|
| Firewall | `iptables` / `ip6tables` / `nftables` | `netsh advfirewall` |
| Hosts | `/etc/hosts` | `C:\Windows\System32\drivers\etc\hosts` |
| IPC Socket | `/run/focusguard/focusguard.sock` (0660 `focusguard:focusguard`) | `%PROGRAMDATA%\FocusGuard\focusguard.sock` |
| Service | systemd (`User=focusguard` + capabilities + watchdog) | Native SCM Windows Service (`svc`), `sc.exe` |
| Install Path | `/opt/focusguard` (root:root) | `C:\Program Files\FocusGuard` (System / All Users) |
| State Dir | `/var/lib/focusguard/` | `C:\ProgramData\FocusGuard\` |
| Tray Autostart | `~/.config/autostart` (XDG desktop entry) | HKCU `...\CurrentVersion\Run` |

---

## 2. Language and stack

- **Go 1.26.5** (module `focusguard`). No CGO on Windows or Linux, except `focusguard-tray` on Linux (requires GTK3 / appindicator).
- Key dependencies: `fsnotify` (filesystem watchers), `getlantern/systray` (tray), `creativeprojects/go-selfupdate` (update engine), `golang.org/x/sys` (Windows services), `golang.org/x/mod/semver` (version comparison).
- **Stdlib first**: Avoid adding new dependencies unless strictly necessary.

### Languages used in the repository

- **Agent and developer guidelines (`GEMINI.md`, `AGENT.md`)**: English across root and all subdirectories.
- **Code, identifiers, and code comments**: English.
- **UI/CLI user-facing strings**: Brazilian Portuguese (PT-BR).
- **End-user documentation (README, CHANGELOG, manuals)**: Brazilian Portuguese (PT-BR).
- **Commit messages**: English (Conventional Commits).

---

## 3. Architecture

### Data flow

```
CLI (focusguard) ────────┐
Tray (focusguard-tray) ──┼── IPC (Unix socket / JSON) ──→ Daemon (focusguard-daemon)
Web (focusguard-web) ────┘                                        │
                                                             [Scheduler]  ← Source of truth in RAM
                                                          ┌────────┴────────┐
                                                     [Store]          [Enforcer]
                                               (atomic state.json)   ┌────┴────┐
                                                               /etc/hosts  Firewall / DNS Sinkhole
                                                          ┌────────┴────────┐
                                                   [HostsWatcher]    [StateWatcher]
                                                  (fsnotify + SHA-256 anti-tamper)
```

### Binaries (`cmd/`)

| Binary | Role | Windows Resources |
|---|---|---|
| `focusguard` | CLI (no args opens web UI) | `cmd/focusguard/versioninfo.json` (icon only, no manifest) |
| `focusguard-daemon` | Privileged background service | `packaging/versioninfo-daemon.json` + manifest (**`requireAdministrator`**) |
| `focusguard-tray` | Unelevated user tray app | `cmd/focusguard-tray/versioninfo.json` (**icon only — NEVER manifest/admin**) |
| `focusguard-watchdog` | Health-check & Smart Recovery | `cmd/focusguard-watchdog/versioninfo.json` (icon + version, no manifest) |
| `focusguard-web` | User-space UI server & IPC proxy | Unelevated — **never** add admin |
| `focusguard-icon` | Build-time icon generator | — |

### Internal layers (`internal/` — 34 packages)

Packages are organized in 4 layers (`docs/reorg-plan.md`):
1. **`domain/`**: Business rules — `analytics`, `apps`, `blocks`, `goal`, `interceptor`, `policy`, `pomodoro`, `preset`, `presets`, `recovery`, `schedule`, `scheduler`, `user`, `users`.
2. **`infrastructure/`**: OS I/O — `autostart`, `dns`, `dnsserver`, `enforcer`, `filelog`, `fsutil`, `hostswatch`, `icon`, `processguard`, `statewatch`, `store`, `tamper`, `update`.
3. **`transport/`**: IPC / HTTP & metrics — `eventhub`, `httpapi`, `ipc`, `ipcerr`, `metrics`.
4. **`system/`**: Process lifecycle — `daemon`, `tray`, `watchdog`.

---

## 4. Code conventions

1. **Strict TDD (Test-Driven Development) — MANDATORY (Tests FIRST)**:
   Every feature, business rule, or bug fix MUST strictly follow Red → Green → Refactor. Write the failing unit test before implementing production code, within the same commit. Never push code without automated test coverage.
2. **Per-platform separation**: Use `_windows.go`, `_linux.go`, and `_other.go` suffixes with a common interface in the base file.
3. **Source of truth in RAM**: The scheduler holds authoritative state in memory; `state.json` is a persisted mirror. On discrepancy, RAM wins and disk is repaired.
4. **Atomic writes & SHA-256 loop prevention**: `store` writes to temp files before rename. Daemon self-writes are hashed (`MarkSelfWrite`) so watchers never trigger recursive loops.
5. **Best-effort OS operations**: Non-critical OS failures (firewall warning, notification error, autostart) log and proceed; they **never crash the daemon**.
6. **Non-blocking IPC**: Tray and web interactions use `SendWithTimeout` (5s max). Handlers must never block GUI threads.
7. **Defensive validation**: Sanitize domains (`sanitizeDomain`), enforce safe bounds (pomodoro max 7 days, daily goal max 1440m), atomic rollbacks on update, sweep orphan firewall rules.
8. **Append-only JSONL logging**: Analytics and tamper event logs append safely; corrupt lines are skipped on read.
9. **Windows resources via `go-winres`**: Run `make icon && make winres` whenever icons or `versioninfo.json` change. Committed `.syso` files are versioned for CI.
10. **Web UI compilation**: Run `make ui` before building `focusguard-web`. Assets are embedded via `go:embed`.
11. **Unelevated user applications**: The tray and web binaries run in user space and must never require administrative elevation.
12. **Shell scripts & BOM**: Shell scripts (`*.sh`) must have LF endings. `install-daemon.ps1` MUST retain its UTF-8 BOM (`EF BB BF`).
13. **Tests mock the OS**: Never invoke real system binaries (`sc.exe`, `iptables`, `systemctl`) in unit tests. Always mock through `execCommand`, `os.Stat`, etc.
14. **IPC registry pattern**: Actions register via `server.Register(action, handler)` and `specs` (`internal/transport/ipc/spec.go`). No monolithic switch statements.
15. **Daily session log**: Update `docs/session-log/YYYY-MM-DD.md` at the end of every working session. `make session-check` validates today's handoff.
16. **Rule synchronization**: Keep `GEMINI.md` and legacy `AGENT.md` synchronized via `make sync-rules`. CI validates parity with `make sync-rules-check`.

---

## 5. Testing, validation, and Definition of Done

### Definition of Done — all items must pass before completion:

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` succeeds
- [ ] `gofmt -l .` reports no unformatted files (or run `gofmt -w .`)
- [ ] `go test ./... -count=1 -timeout=60s` passes
- [ ] `make sync-rules-check` passes (zero drift between `GEMINI.md` and `AGENT.md`)
- [ ] `make session-check` passes (today's handoff log exists in `docs/session-log/`)
- [ ] `git status` shows no stray compiled artifacts
- [ ] New code is fully covered by automated tests written first (TDD)
- [ ] Commit message follows Conventional Commits format

```bash
go build ./...
go vet ./...
go test ./... -count=1 -timeout=60s
make sync-rules-check
make session-check
```

---

## 6. Relevant file structure

```
├── GEMINI.md                   # Primary repo guide (AGENT.md is an exact mirror)
├── docs/                       # Specifications, plans, and daily session logs
│   ├── ui-plan.md              # Web UI plan & API contract (12 screens)
│   ├── bug-hunt-plan.md        # Bug-hunt history and regression tests
│   ├── linux-validation-plan.md# Linux validation suite (Etapas 0–7)
│   ├── release.md              # Release checklist and procedures
│   └── session-log/            # Daily handoff summaries (YYYY-MM-DD.md)
├── Makefile                    # build, ui, test, sync-rules, sync-rules-check, session-check
├── cmd/                        # 6 binary entry points (CLI, daemon, tray, watchdog, web, icon)
├── internal/                   # 34 internal packages across domain, infra, transport, system
├── focusguard-ui/              # React 18 + Vite + TS frontend (12 screens)
├── android/                    # Native Android app (Kotlin + Jetpack Compose)
├── packaging/                  # Icons, manifests, Windows versioninfo
└── scripts/                    # Installer scripts, WiX templates, validation utilities
```

---

## 7. Commit conventions

**Conventional Commits, in English, with scope:**

```
<type>(<scope>): <short imperative description>
```

- **Types**: `feat`, `fix`, `perf`, `docs`, `test`, `ci`, `chore`.
- **Common scopes**: `ui`, `install`, `icon`, `tray`, `update`, `store`, `scheduler`, `enforcer`, `watchers`, `daemon`, `ipc`, `autostart`, `dns`, `android`, `session`.
- **Rules**:
  - One commit per coherent change. Keep descriptions ≤ 72 chars in lowercase imperative mood.
  - Never include automated agent or tool footers ("Generated with...", "Co-Authored-By:...").

---

## 8. Release

Releases are executed manually following `docs/release.md`. Publishing a Git tag `vX.Y.Z` triggers the CI GoReleaser pipeline. Always confirm version numbers and changelog entries before creating release tags.

---

## 9. Known pitfalls & Gotchas

- **Daemon privileges on Windows**: `focusguard-daemon` requires administrative elevation. Running `go test ./cmd/focusguard-daemon/...` on Windows requires an elevated prompt. On Linux, daemon tests run hermetically unprivileged via `setupDaemonTestEnv`.
- **Tray is unprivileged**: Never add an elevation manifest to the tray.
- **Immediate daemon restart on update**: Applying an update terminates any active pomodoro session and restarts the daemon immediately (exit code 1 → supervisor restarts with new binary). Persisted blocks in `state.json` are retained and restored.
- **Windows binary swap locks**: Before updating executables on Windows, the daemon terminates the tray and stops the watchdog service to unlock `.exe` files. If replacement is locked, `MoveFileEx(MOVEFILE_DELAY_UNTIL_REBOOT)` schedules swap on reboot.
- **IPC contract synchronization**: The IPC wire format is shared between CLI, tray, daemon, and web UI. Run `make contract` to update `focusguard-ui/src/api/types.ts` whenever IPC types change, and verify with `make contract-check`.
- **Default web port**: `48902` is defined canonically in `httpapi.DefaultAddr`. Never hardcode literal port numbers across packages.
- **GoReleaser hooks without shell**: The `go-winres` hook in `.goreleaser.yaml` invokes `sh -c` because GoReleaser does not execute hooks inside a subshell.

---

## 10. Glossary

- **Self-write**: A write to `hosts` or `state.json` initiated by the daemon itself, marked with a cryptographic hash to avoid triggering tamper alerts.
- **Reconcile**: The scheduler re-evaluates in-memory state and re-enforces active rules at the OS level (hosts, DNS, firewall) to correct external drift.
- **Source of truth in RAM**: `state.json` reflects scheduler memory; on divergence, memory prevails and disk is repaired.
- **Tamper**: Any external unauthorized modification to `hosts` or `state.json` that attempts to weaken or bypass active blocks.
- **Best-effort**: Operations whose failures are logged as warnings but never abort daemon execution.
- **Smart Recovery**: The watchdog service's automatic rollback mechanism to restore previous working binaries when an update fails health checks.
