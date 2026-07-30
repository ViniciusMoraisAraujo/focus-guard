param(
    [Parameter(Mandatory=$false)]
    [ValidateSet("install","uninstall","status")]
    [string]$Action = "status",

    [Parameter(Mandatory=$false)]
    [string]$ExePath = ""
)

$ServiceName = "FocusGuard"
$StateDir = "$env:PROGRAMDATA\FocusGuard"

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

    $existing = sc query $ServiceName 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[FocusGuard] Serviço '$ServiceName' já existe. Removendo..." -ForegroundColor Yellow
        sc stop $ServiceName 2>$null | Out-Null
        sc delete $ServiceName | Out-Null
        Start-Sleep -Seconds 2
    }

    sc create $ServiceName binPath=$exe start=auto displayname="FocusGuard Daemon"
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[FocusGuard] ✔ Serviço Windows '$ServiceName' instalado com sucesso!" -ForegroundColor Green
        Write-Host "[FocusGuard] O daemon iniciará automaticamente na inicialização do sistema." -ForegroundColor Cyan
        sc start $ServiceName | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[FocusGuard] ✔ Daemon iniciado!" -ForegroundColor Green
        } else {
            Write-Host "[FocusGuard] ⚠ Serviço instalado. Inicie manualmente com: sc start $ServiceName" -ForegroundColor Yellow
        }
    } else {
        Write-Host "[FocusGuard] ✘ Falha ao criar serviço Windows. Execute como Administrador." -ForegroundColor Red
        exit 1
    }
}

function Uninstall-Daemon {
    $existing = sc query $ServiceName 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[FocusGuard] Serviço '$ServiceName' não está instalado." -ForegroundColor Yellow
        return
    }

    Write-Host "[FocusGuard] Parando serviço '$ServiceName'..." -ForegroundColor Cyan
    sc stop $ServiceName 2>$null | Out-Null
    Start-Sleep -Seconds 2

    Write-Host "[FocusGuard] Removendo serviço '$ServiceName'..." -ForegroundColor Cyan
    sc delete $ServiceName | Out-Null
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
}

function Get-Status {
    $existing = sc query $ServiceName 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[FocusGuard] ✔ Daemon instalado como serviço Windows." -ForegroundColor Green
        sc query $ServiceName 2>$null | Select-String "STATE","START_TYPE" -SimpleMatch
    } else {
        Write-Host "[FocusGuard] ✘ Daemon não está instalado como serviço." -ForegroundColor Yellow
        Write-Host "[FocusGuard] Execute 'install-daemon.ps1 install' (como Administrador) para instalar." -ForegroundColor Cyan
    }
}

switch ($Action) {
    "install"   { Install-Daemon }
    "uninstall" { Uninstall-Daemon }
    "status"    { Get-Status }
}
