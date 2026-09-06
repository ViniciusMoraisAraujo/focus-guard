# GEMINI.md — internal/

> Guidelines for AI agents and developers working in this directory. Also consult
> the root **[GEMINI.md](../GEMINI.md)** (specs, core conventions, architecture)
> before editing any code.

## Purpose

The core of FocusGuard: **34 packages** consumed by the binaries in `cmd/`.
Authoritative state is held in RAM (`scheduler`), with disk (`state.json`) as a
persisted mirror. File watchers revert unauthorized tampering, and IPC serves
as the strict protocol between clients (CLI, tray, web UI) and the daemon.

---

## Package map

Packages are divided into 4 architectural layers (`docs/reorg-plan.md`):

### 1. Domain (`domain/` — Business logic)
| Package | Responsibility |
|---|---|
| `analytics` | Session history (JSONL), focus streaks, stats, CSV/JSON/HTML export |
| `apps` | Process denylist (`apps.json`) for process guard protection |
| `blocks` | Domain handlers for `block` and `block-all` actions (`Blocker`/`Catalog`) |
| `goal` | Daily focus goal tracking (`goal.json`) |
| `interceptor` | Focus Interceptor Page domain handlers (`interceptor-set`/`interceptor-status`) |
| `policy` | `Block` domain model (`IsActive`, `CanUnblock`, `RemainingTime`) |
| `pomodoro` | Work/rest/cycle state machine, persisted preferences, session summaries |
| `preset` | Builtin categories (social, video, news, games) and custom category catalogs |
| `presets` | Domain handlers for preset management (list/add/remove) |
| `recovery` | Smart Recovery: `FindRecentBackup`, `ShouldRollBack`, `RestoreFromBackup` |
| `schedule` | Recurring schedule rules (days/hours, overnight windows, iCal parsing) |
| `scheduler` | Authoritative block lifecycle: `Block`, `Reconcile`, expiration, IP refresh (15m) |
| `user` | Admin user account and password hash store |
| `users` | Domain handlers for authentication & user management |

### 2. Infrastructure (`infrastructure/` — OS I/O)
| Package | Responsibility |
|---|---|
| `autostart` | Service installation (SCM / systemd), tray autostart, desktop shortcuts |
| `dns` | Domain handlers for DNS sinkhole management |
| `dnsserver` | Embedded DNS sinkhole (port 53, `miekg/dns`) with upstream forwarding |
| `enforcer` | OS block enforcer: hosts file + firewall (`iptables`/`ip6tables`/`nft`/`netsh`) |
| `filelog` | Shared append & rotating file logging next to binary executables |
| `fsutil` | Cryptographic SHA-256 file hashing for anti-tamper loop prevention |
| `hostswatch` | Hosts file monitor: fsnotify + hash anti-loop; detects and reverts changes |
| `icon` | Dynamic icon rasterizer for `focusguard.ico` and `.png` |
| `processguard` | Background process killer for denylisted applications during sessions |
| `statewatch` | `state.json` watcher: restores disk state directly from scheduler RAM |
| `store` | Atomic JSON persistence with hardware-bound AES-256-GCM replica |
| `tamper` | Append-only JSONL log of detected and reverted tampering attempts |
| `update` | Atomic multi-binary auto-update (`UpdateToAll`) with rollback |

### 3. Transport (`transport/` — IPC, HTTP, Observability)
| Package | Responsibility |
|---|---|
| `eventhub` | In-process pub/sub event bus (ring buffer + long-polling) |
| `httpapi` | Web UI HTTP server: IPC proxy + static assets + localhost security guards |
| `ipc` | Unix socket protocol, client `SendWithTimeout`, server action registry |
| `ipcerr` | Canonical IPC error codes (`Error`) matching `ipc/codes.go` |
| `metrics` | Per-action latency registry (ring buffer + percentiles) |

### 4. System (`system/` — Process lifecycle)
| Package | Responsibility |
|---|---|
| `daemon` | Daemon lifecycle: `Run(ctx) error` and graceful ordered shutdown |
| `tray` | Systray icon controller: menus, dynamic tooltips, non-blocking IPC |
| `watchdog` | systemd `NOTIFY_SOCKET` health-check watchdog |

---

## Specific conventions

1. **Strict TDD (Test-Driven Development) — MANDATORY**:
   Every domain rule, enforcer logic, or bug fix MUST be developed test-first (Red → Green → Refactor). All packages have extensive test suites. Never push code without tests.
2. **Authoritative RAM**:
   Never treat `state.json` as the authoritative source of truth. `Reconcile` re-applies active blocks from RAM to disk and OS rules.
3. **Atomic writes & anti-loop hashes**:
   `store` writes via temp files and renames. Self-writes are registered by SHA-256 hash (`MarkSelfWrite`) so watchers do not treat daemon writes as external tampering.
4. **Best-effort OS operations**:
   OS-level auxiliary failures (firewall cleanup, notification, autostart) log a warning but **never crash the daemon**.
5. **Non-blocking IPC**:
   Tray and web calls to the daemon use `SendWithTimeout` (5s max).
6. **Platform separation & OS mocking**:
   Keep OS-specific code in `_windows.go` and `_linux.go`. Mock external system calls (`execCommand`, `os.Stat`, `osRename`, `goos`). Never invoke real system binaries in unit tests.
7. **Input sanitization & defense-in-depth**:
   Sanitize all domains (`sanitizeDomain`), enforce reasonable bounds (pomodoro work ≤ 7 days, goal ≤ 1440m), sweep orphan firewall rules on sync.
8. **Session log**:
   Update `../docs/session-log/YYYY-MM-DD.md` before concluding any session.

---

## Hardening notes & Regression prevention

- **Bug-hunt fixes (2026-08-10)**: See `docs/bug-hunt-plan.md` for full details. Do not regress:
  - Orphan firewall rule cleanup on last block expiry (`scheduler`).
  - Batch domain blocking retaining pre-existing rules (`BlockDomains`).
  - Refresh goroutine shutdown leakage (`Stop()` cleanly cancels).
  - ICS +1h overnight window wrap past midnight (`icsWindow`).
  - Reversible rollback on `store.Save` failure in `Block`, `ExtendBlock`, and DNS settings.
- **Update timeouts**: Long downloads in `ipc/server.go` should account for network latency during archive fetches.
- **Lock discipline**: Avoid placing disk I/O under read/write mutexes on hot paths.
- **Ticker disposal**: Ensure long-lived tickers in controllers or background workers are properly stopped if components gain teardown paths.

---

## Testing

- `go test ./internal/... -count=1 -timeout=60s` (hermetic, mock OS).
- Run with `-race` when modifying `scheduler`, `pomodoro`, `eventhub`, or `processguard`.
- Fuzz testing: `internal/domain/schedule/fuzz_test.go` (`FuzzParseICS`, `FuzzWindowsPairs`, `FuzzParseClock`).
