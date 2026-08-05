# AGENT.md — scripts/

> Guia para agentes de IA que trabalham neste diretório. Consulte também o
> **[AGENT.md](../AGENT.md)** na raiz (specs, convenções, armadilhas) — leia-o
> antes de editar qualquer código.

## Propósito

Scripts de **instalação** (Windows/Linux), **build do .msi**, unit systemd e
utilitários de verificação. Não fazem parte dos binários — são distribuídos
nas releases (`install-daemon.ps1`, `install-linux.sh`, `focusguard.service`,
`focusguard-tray.desktop`) e usados pelo CI (`.github/workflows/release.yml`).

| Arquivo | Papel |
|---|---|
| `build-msi.sh` | Gera `focusguard[-server]-<v>-<arch>.msi` via go-msi + WiX (CI job `windows-msi`); perfil `desktop` (padrão) ou `server` escolhe o manifesto |
| `install-daemon.ps1` | Instalação Windows: copia p/ Program Files, serviço SCM, atalho, tray, watchdog |
| `install-linux.sh` | Instalação Linux: `/opt/focusguard`, systemd, XDG autostart, atalho desktop |
| `focusguard.service` | Unit systemd (template; `ExecStart` é reescrito pelo install-linux.sh) |
| `focusguard-tray.desktop` | Template de autostart do tray (Linux) |
| `msi/` | `wix.json` (desktop) + `wix-server.json` (Server, headless) + `product.wxs` (template WiX) do go-msi |
| `../server.role` | Marcador vazio da edição Server — o MSI Server instala ao lado do daemon. Em instalação LIMPA (sem `state.json`) o DNS sinkhole nasce habilitado no 1º boot; em conversão de instalação existente, habilite na tela Rede ou com `focusguard dns start` (ver `isServerEdition` no daemon) |
| `verifyicon/` | Verifica se o ícone embutido no .exe == `focusguard.ico` |

## Regras específicas

1. **EOL LF nos `.sh`** (garantido pelo `.gitattributes`).
2. **`install-daemon.ps1` deve manter BOM UTF-8** (`EF BB BF`) — re-salvar sem
   BOM quebra acentos no PowerShell 5.1. Use `UTF8Encoding($true)` ao gravar.
3. Instaladores são **idempotentes** (serviço existente é removido/recriado;
   `sc failure`/recovery configurados para restart pós-update).
4. **Best-effort**: falha de atalho/tray/watchdog avisa e segue — nunca aborta
   a instalação do daemon.
5. `build-msi.sh` roda **apenas em ambiente Windows** (go-msi invoca WiX via
   `cmd.exe`); arquitetura `amd64` (padrão) ou `arm64`; perfil `desktop`
   (padrão) ou `server`.
6. **Edições desktop e Server compartilham o UpgradeCode** (sabores do mesmo
   produto): `product.wxs` usa `AllowSameVersionUpgrades="yes"` para que
   instalar uma sobre a outra na MESMA versão converta a máquina
   (RemoveExistingProducts troca a edição). Não criar um UpgradeCode novo por
   edição — isso criaria produtos independentes com serviços de mesmo nome.
   Atenção: o DNS liga sozinho no 1º boot apenas em instalação LIMPA (sem
   `state.json`); converter uma instalação existente mantém o estado e exige
   `focusguard dns start` (ou a tela Rede) para ativar o sinkhole.
7. Caminhos de build no Windows: use `ROOT_WIN` (`pwd -W`) e `cygpath -w` —
   ver histórico do fix do cross-drive `filepath.Rel` (commit `d3da75e` +
   `a890d92`): o go-msi resolve os caminhos do `wix.json` como absolutos e os
   torna relativos ao `--out`; se o repo e o `--out` ficarem em drives
   diferentes (repo em `D:`, temp do SO em `C:`), a geração aborta.

## Bugs e correções potenciais

- **`install-daemon.ps1` — `$WatchdogServiceName` não é definido** em lugar
  nenhum (linhas 66–74, 180–184 usam a variável, mas só `$ServiceName`/
  `$StateDir`/`$InstallDir` são declarados no topo). Em PowerShell, uma
  variável inexistente vira string vazia → `sc.exe create binPath=...` sem
  nome, e o watchdog não é instalado/removido corretamente (a remoção via MSI
  e via `focusguard uninstall` usa o nome correto `FocusGuardWatchdog`).
  **Correção:** declarar `$WatchdogServiceName = "FocusGuardWatchdog"` junto
  com `$ServiceName`, e alinhar o recovery às `actions=restart/5000/...`.

- **`install-daemon.ps1` — serviço watchdog e daemon usam o mesmo padrão de
  recovery mas com constantes divergentes do `wix.json`/`autostart`** — ao
  alterar nomes/políticas, mantenha um único ponto de verdade (o
  `FocusGuardWatchdog` do `scripts/msi/wix.json` e do `internal/autostart`).

- **`install-linux.sh` — atalho desktop ainda diz "a CLI sem argumentos abre a
  TUI (que exige um terminal)"** (linha ~202, `Terminal=true`). A TUI foi
  removida: a CLI sem argumentos agora **abre a interface web no navegador**.
  Revisar o comentário e avaliar se `Terminal=true` ainda faz sentido (abrir
  terminal para depois abrir o navegador é indesejável).- **`build-msi.sh` — versão e paths**: `MSI_NAME` é relativo (`--msi` sem
   caminho → grava na raiz); se um dia quiserem build fora da raiz, use caminho
   absoluto Windows. Também vale validar `$VERSION` com um regex de semver
   antes de chamar o go-msi (erro do WiX com versão inválida é obscuro).

- **Conversão desktop→server com o tray rodando**: o `tray.exe` é processo
  comum iniciado pelo hook da edição desktop; durante o `RemoveExistingProducts`
  ele pode estar com o arquivo travado → remoção falha ou agenda reboot
  (exposição já existente nos upgrades normais, só fica mais provável na troca
  de sabor). Se virar problema real, avaliar fechar o tray antes da troca
  (padrão do `stopForBinarySwap` do daemon).

## Testes

- `bash -n scripts/build-msi.sh scripts/install-linux.sh` — sintaxe.
- PowerShell: `powershell -NoProfile -ExecutionPolicy Bypass -File
  install-daemon.ps1 status` (o install/uninstall exigem admin).
- `go run ./scripts/verifyicon` após `make icon && make winres`.
