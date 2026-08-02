param(
    [Parameter(Mandatory=$false)]
    [ValidateSet("install","uninstall","status")]
    [string]$Action = "status",

    [Parameter(Mandatory=$false)]
    [string]$ExePath = ""
)

$ServiceName = "FocusGuard"
$StateDir = "$env:PROGRAMDATA\FocusGuard"

# Pasta protegida: os binários vivem em Program Files, cuja ACL padrão dá ao
# usuário comum apenas leitura+execução — não é possível excluir por acidente
# (o zip extraído vira um simples instalador descartável). ProgramW6432 evita
# o redirect 32-bit que apontaria para "Program Files (x86)".
$ProgramFilesDir = if ($env:ProgramW6432) { $env:ProgramW6432 } else { $env:ProgramFiles }
$InstallDir = Join-Path $ProgramFilesDir "FocusGuard"

# Get-CliExePath localiza o focusguard.exe (CLI) ao lado do daemon — é o alvo
# do atalho do Desktop e quem carrega o ícone embedado via go-winres.
# NOTA: NÃO reutiliza $ExePath de propósito — esse parâmetro é o caminho do
# daemon (Get-ExePath); o atalho deve sempre mirar a CLI, nunca o daemon.
function Get-CliExePath {
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    $parentDir = Split-Path -Parent $scriptDir
    $candidates = @(
        "$parentDir\focusguard.exe",
        "$parentDir\bin\focusguard.exe",
        "$parentDir\cmd\focusguard\focusguard.exe"
    )
    foreach ($c in $candidates) {
        if (Test-Path $c) {
            return (Resolve-Path $c).Path
        }
    }
    return ""
}

function Get-ExePath {
    if ($ExePath -and (Test-Path $ExePath)) {
        return $ExePath
    }
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    $parentDir = Split-Path -Parent $scriptDir
    $candidates = @(
        "$parentDir\focusguard-daemon.exe",
        "$parentDir\bin\focusguard-daemon.exe",
        "$parentDir\cmd\focusguard-daemon\focusguard-daemon.exe"
    )
    foreach ($c in $candidates) {
        if (Test-Path $c) {
            return (Resolve-Path $c).Path
        }
    }
    return ""
}

function Install-Daemon {
    $exe = Get-ExePath
    if (-not $exe) {
        Write-Host "[FocusGuard] ERRO: Não foi possível encontrar focusguard-daemon.exe" -ForegroundColor Red
        Write-Host "[FocusGuard] Especifique -ExePath ou execute o script do diretório do projeto." -ForegroundColor Yellow
        exit 1
    }
    Write-Host "[FocusGuard] Usando executável: $exe" -ForegroundColor Cyan

    if (-not (Test-Path $StateDir)) {
        New-Item -ItemType Directory -Path $StateDir -Force | Out-Null
        Write-Host "[FocusGuard] Diretório de estado criado: $StateDir" -ForegroundColor Green
    }

    # Copia a suíte inteira (daemon, CLI, watchdog, tray) para a pasta
    # protegida. O serviço, o atalho e os comandos install-tray/watchdog usam
    # caminhos relativos ao executável, então tudo precisa viver junto ali.
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    $srcDir = Split-Path -Parent $exe
    $copied = $false
    foreach ($name in @("focusguard.exe", "focusguard-daemon.exe", "focusguard-watchdog.exe", "focusguard-tray.exe")) {
        $src = Join-Path $srcDir $name
        if (Test-Path $src) {
            Copy-Item $src -Destination $InstallDir -Force
            $copied = $true
        }
    }
    if ($copied) {
        Write-Host "[FocusGuard] ✔ Binários copiados para a pasta protegida: $InstallDir" -ForegroundColor Green
        Write-Host "[FocusGuard]   (pode excluir a pasta do zip extraído com segurança)" -ForegroundColor Gray
    }
    $exe = Join-Path $InstallDir "focusguard-daemon.exe"

    $existing = sc.exe query $ServiceName 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[FocusGuard] Serviço '$ServiceName' já existe. Removendo..." -ForegroundColor Yellow
        sc.exe stop $ServiceName 2>$null | Out-Null
        sc.exe delete $ServiceName | Out-Null
        Start-Sleep -Seconds 2
    }

    sc.exe create $ServiceName binPath=$exe start=auto displayname="FocusGuard Daemon"
    if ($LASTEXITCODE -eq 0) {
        # Recovery automática: o daemon se auto-reinicia após aplicar um update
        # (exit code 1 — ver restartAfterUpdate). O SCM só sobe o serviço de
        # novo se a recovery estiver configurada: restart em 5s, 10s e 30s.
        sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/10000/restart/30000 | Out-Null
        Write-Host "[FocusGuard] ✔ Serviço Windows '$ServiceName' instalado com sucesso!" -ForegroundColor Green
        Write-Host "[FocusGuard] O daemon iniciará automaticamente na inicialização do sistema." -ForegroundColor Cyan
        Write-Host "[FocusGuard] Recovery configurada: o daemon reinicia sozinho após atualização." -ForegroundColor Cyan
        sc.exe start $ServiceName | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[FocusGuard] ✔ Daemon iniciado!" -ForegroundColor Green
        } else {
            Write-Host "[FocusGuard] ⚠ Serviço instalado. Inicie manualmente com: sc.exe start $ServiceName" -ForegroundColor Yellow
        }
    } else {
        Write-Host "[FocusGuard] ✘ Falha ao criar serviço Windows. Execute como Administrador." -ForegroundColor Red
        exit 1
    }
}

function Uninstall-Daemon {
    $existing = sc.exe query $ServiceName 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[FocusGuard] Serviço '$ServiceName' não está instalado." -ForegroundColor Yellow
        return
    }

    Write-Host "[FocusGuard] Parando serviço '$ServiceName'..." -ForegroundColor Cyan
    sc.exe stop $ServiceName 2>$null | Out-Null
    Start-Sleep -Seconds 2

    Write-Host "[FocusGuard] Removendo serviço '$ServiceName'..." -ForegroundColor Cyan
    sc.exe delete $ServiceName | Out-Null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[FocusGuard] ✔ Serviço removido com sucesso!" -ForegroundColor Green
    } else {
        Write-Host "[FocusGuard] ✘ Falha ao remover serviço." -ForegroundColor Red
        exit 1
    }

    if (Test-Path $StateDir) {
        Write-Host "[FocusGuard] Diretório de estado preservado: $StateDir" -ForegroundColor Cyan
        Write-Host "[FocusGuard] Para remover completamente, exclua manualmente o diretório." -ForegroundColor Gray
    }

    Remove-DesktopShortcut

    if (Test-Path $InstallDir) {
        Remove-Item $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
        Write-Host "[FocusGuard] ✔ Pasta de instalação removida: $InstallDir" -ForegroundColor Green
    }
}

# Install-DesktopShortcut cria um atalho .lnk no Desktop apontando para o
# focusguard.exe (CLI), com o ícone embedado no próprio executável via
# go-winres (IconLocation=focusguard.exe,0). Best-effort: falha não aborta.
function Install-DesktopShortcut {
    # Prefere o CLI da pasta protegida (Program Files); o do zip extraído é o
    # fallback para quem rodou o script sem copiar os binários.
    $cli = Join-Path $InstallDir "focusguard.exe"
    if (-not (Test-Path $cli)) {
        $cli = Get-CliExePath
    }
    if (-not $cli) {
        Write-Host "[FocusGuard] Aviso: focusguard.exe não encontrado. Atalho do Desktop não criado." -ForegroundColor Yellow
        return
    }

    $desktop = [Environment]::GetFolderPath("Desktop")
    if (-not $desktop) {
        Write-Host "[FocusGuard] Aviso: sem pasta Desktop. Atalho não criado." -ForegroundColor Yellow
        return
    }

    try {
        $lnk = Join-Path $desktop "FocusGuard.lnk"
        $ws = New-Object -ComObject WScript.Shell
        $sc = $ws.CreateShortcut($lnk)
        $sc.TargetPath = $cli
        $sc.WorkingDirectory = Split-Path -Parent $cli
        $sc.IconLocation = "$cli,0"
        $sc.Description = "FocusGuard - bloqueio focado de distrações"
        $sc.Save()
        Write-Host "[FocusGuard] ✔ Atalho do FocusGuard criado no Desktop ($lnk)." -ForegroundColor Green
    } catch {
        Write-Host "[FocusGuard] Aviso: não foi possível criar o atalho do Desktop: $($_.Exception.Message)" -ForegroundColor Yellow
    }
}

# Remove-DesktopShortcut apaga o atalho .lnk do Desktop.
function Remove-DesktopShortcut {
    $desktop = [Environment]::GetFolderPath("Desktop")
    if (-not $desktop) { return }
    $lnk = Join-Path $desktop "FocusGuard.lnk"
    if (Test-Path $lnk) {
        Remove-Item $lnk -Force
        Write-Host "[FocusGuard] Atalho do FocusGuard removido do Desktop." -ForegroundColor Cyan
    }
}

function Get-Status {
    $existing = sc.exe query $ServiceName 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[FocusGuard] ✔ Daemon instalado como serviço Windows." -ForegroundColor Green
        sc.exe query $ServiceName 2>$null | Select-String "STATE","START_TYPE" -SimpleMatch
    } else {
        Write-Host "[FocusGuard] ✘ Daemon não está instalado como serviço." -ForegroundColor Yellow
        Write-Host "[FocusGuard] Execute 'install-daemon.ps1 install' (como Administrador) para instalar." -ForegroundColor Cyan
    }
}

switch ($Action) {
    "install"   { Install-Daemon; Install-DesktopShortcut }
    "uninstall" { Uninstall-Daemon }
    "status"    { Get-Status }
}
