# GEMINI.md — scripts/

> Guidelines for AI agents and developers working in this directory. Also consult
> the root **[GEMINI.md](../GEMINI.md)** (specs, core conventions, architecture)
> before editing any code.

## Purpose

Scripts for **installation** (Windows and Linux), **MSI package building**, systemd
unit configuration, and validation utilities. Distributed alongside releases
(`install-daemon.ps1`, `install-linux.sh`, `focusguard.service`, `focusguard-tray.desktop`)
and executed within CI (`.github/workflows/release.yml`, `.github/workflows/test.yml`).

| File | Purpose |
|---|---|
| `build-msi.sh` | Generates `focusguard[-server]-<v>-<arch>.msi` via go-msi + WiX; supports `desktop` (default) and `server` profiles |
| `install-daemon.ps1` | Windows installer: copies to Program Files, creates SCM service, desktop shortcut, tray, and watchdog |
| `install-linux.sh` | Linux installer: `/opt/focusguard`, systemd service, XDG autostart, socket permissions |
| `focusguard.service` | Systemd service unit template (`ExecStart` configured by `install-linux.sh`) |
| `focusguard-tray.desktop` | Template for Linux XDG desktop autostart |
| `msi/` | WiX configuration (`wix.json`, `wix-server.json`, and `product.wxs`) |
| `../packaging/server.role` | Marker file for Server edition installations (enables headless sinkhole on first boot) |
| `verifyicon/` | Verification tool ensuring embedded executable icon matches `packaging/focusguard.ico` |
| `check-session-log.sh` | Validates `docs/session-log/` structure in CI and local checks (`make session-check`) |

---

## Specific conventions

1. **Line endings**: All `.sh` shell scripts MUST maintain LF endings (enforced via `.gitattributes`).
2. **PowerShell BOM**: `install-daemon.ps1` MUST be saved with a **UTF-8 BOM** (`EF BB BF`). Saving without a BOM breaks accented characters in PowerShell 5.1.
3. **Idempotency**: Installers must be completely idempotent (re-running on an existing installation updates services and restores configurations cleanly).
4. **Best-effort non-critical steps**: Failure to register shortcuts, tray autostart, or notifications should warn and proceed without failing the daemon installation.
5. **MSI build environment**: `build-msi.sh` executes exclusively in Windows environments where `go-msi` and `WiX 3.10+` are present.
6. **Unified UpgradeCode**: Desktop and Server editions share the same `UpgradeCode` with `AllowSameVersionUpgrades="yes"`. This allows seamless in-place switching between Desktop and Server editions.
7. **Session log**: Update `../docs/session-log/YYYY-MM-DD.md` at the end of each session.

---

## Known pitfalls & Gotchas

- **Windows process locks during MSI upgrades**: Running tray processes hold file locks on `focusguard-tray.exe`. WiX hooks issue `taskkill.exe /f /im focusguard-tray.exe` prior to `InstallValidate` to ensure clean file swaps.
- **Service recovery consistency**: SCM recovery actions in `install-daemon.ps1` should match definitions in `msi/wix.json` and `internal/infrastructure/autostart`.
- **Path relativity in WiX**: `go-msi` computes relative paths between `--out` and source files; keep temporary build artifacts on the same logical drive as the repository.

---

## Validation

- Syntax verification:
  ```bash
  bash -n scripts/build-msi.sh scripts/install-linux.sh scripts/check-session-log.sh
  ```
- PowerShell syntax & status check:
  ```bash
  powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install-daemon.ps1 status
  ```
- Icon verification:
  ```bash
  go run ./scripts/verifyicon/main.go
  ```
