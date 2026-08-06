# Release — FocusGuard

**SemVer** versioning (current: **v0.16.0**). The changelog follows **Keep a
Changelog**, with dated sections and emoji categories (e.g. `### 🛡 ...`).

> F5 do ui-plan (2026-08-06): a release é **conjunta** — todos os arquivos
> Linux já carregam o `focusguard-web` (build no hook + tar.gz) e o
> `install-linux.sh` cria o grupo `focusguard` para o acesso ao socket sem
> sudo.

## Release checklist

1. **CHANGELOG** — move the contents of `## [Unreleased]` into a new
   `## [x.y.z] - YYYY-MM-DD` section, summarized by theme (emoji + bullets),
   and leave an empty `## [Unreleased]` at the top.
2. **Commits** — make the conventional commits (incl. `docs(changelog)`).
3. **Tag** — create an annotated tag and push the branch + tag:
   ```bash
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin main
   git push origin vX.Y.Z
   ```
4. **CI produces the release** — pushing a `v*` tag triggers
   `.github/workflows/release.yml`, which runs GoReleaser (hooks: `go mod
   tidy`, regenerates the icon via `go run ./cmd/focusguard-icon`, runs
   `go-winres make` for daemon/CLI/tray/watchdog, and builds the **web UI** with
   `npm ci && npm run build` — requires Node.js on the runner). The release
   is published **automatically** on GitHub with the per-platform archives.
   The `windows-msi` job then builds **both installers** (desktop + Server)
   and attaches them to the release.

## What the release contains

- **Windows** (`focusguard_<v>_windows_<arch>.zip`): `focusguard.exe`,
  `focusguard-daemon.exe`, `focusguard-watchdog.exe`, `focusguard-tray.exe`,
  `focusguard-web.exe` + `install-daemon.ps1` + `install.txt`.
- **Instaladores MSI** (anexados à release pelo job `windows-msi`):
  `focusguard-<v>-amd64.msi` (edição desktop) e
  `focusguard-server-<v>-amd64.msi` (edição Server, headless). Gerados via
  `make msi VERSION=<v> && make msi-server VERSION=<v>` — ver
  `scripts/build-msi.sh`.
- **Linux** (`focusguard_<v>_linux_<arch>.tar.gz`): binaries (incl.
  `focusguard-web`) + `focusguard.service` + `install-linux.sh` +
  `focusguard-tray.desktop` + README/CHANGELOG + `focusguard.ico`/`.png` +
  `install.txt`.

## Details

- The **daemon version is injected via ldflags** in GoReleaser:
  `-X main.daemonVersion={{ .Version }}`. Dev builds without ldflags end up
  with `0.0.0-dev` → auto-update disabled (expected behavior).
- The `versioninfo.json` files have fixed `file_version`/`product_version`
  fields (currently stale) — they're **informational** in the `.exe`
  metadata; the functional version comes from the tag/ldflags. Update them
  alongside a release if you want accurate metadata.
- GoReleaser's changelog excludes `docs:`, `test:`, and `chore:` commits.
- **Local requirement for `make winres` / hooks**: `go-winres` installed
  (`go install github.com/tc-hib/go-winres@latest`).
