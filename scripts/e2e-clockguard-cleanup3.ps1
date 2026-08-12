# e2e-clockguard-cleanup3.ps1
# Limpeza pos-E2E v3 (deve rodar ELEVADO).
#
# v1/v2 falharam porque o statewatch do daemon instalado (0.16.4) reverte
# edicoes externas ao state.json (anti-tamper) e o hostswatch restaura o
# sentinela all-internet a cada 30s (eventos hosts/restore "*all-internet*").
#
# v3: para os servicos, reescreve o state.json do backup pre-teste (sem
# sentinela) com last_known_time re-ancorado, remove a regra de firewall,
# limpa entradas FocusGuard do hosts file, religa e verifica 2x (15s e 30s)
# para confirmar que o sentinela NAO volta.
#
# Log: C:\ProgramData\FocusGuard\e2e-clockguard-cleanup3.log

$ErrorActionPreference = 'Continue'
$log = 'C:\ProgramData\FocusGuard\e2e-clockguard-cleanup3.log'
$dataDir = 'C:\ProgramData\FocusGuard'
$allInternetRule = 'FocusGuard_AllInternet'
$hostsPath = "$env:SystemRoot\System32\drivers\etc\hosts"

function Write-Log([string]$m) {
    $line = "[$(Get-Date -Format 'HH:mm:ss')] $m"
    Add-Content -Path $log -Value $line
    Write-Host $line
}

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
$pr = New-Object Security.Principal.WindowsPrincipal($id)
if (-not $pr.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Add-Content -Path $log -Value '[ERRO] Requer shell ELEVADO.'
    exit 1
}

Set-Content -Path $log -Value "[ELEVADO] Limpeza pos-E2E v3 ($(Get-Date -Format 's'))"

$watchdogRunning = (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status -eq 'Running'
$daemonRunning = (Get-Service FocusGuard -ErrorAction SilentlyContinue).Status -eq 'Running'

try {
    if ($watchdogRunning) { Stop-Service FocusGuardWatchdog -Force; Write-Log '[1] watchdog parado' }
    if ($daemonRunning) { Stop-Service FocusGuard -Force; Write-Log '[1] daemon parado' }
    Start-Sleep -Seconds 3

    # Zerar o state.json a partir do backup pre-teste.
    if (Test-Path "$dataDir\state.json.e2e-bak") {
        Copy-Item "$dataDir\state.json.e2e-bak" "$dataDir\state.json" -Force
        Write-Log '[2] state.json restaurado do backup pre-teste'
    }
    $s = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
    $s.last_known_time = (Get-Date).ToString('o')
    if ($s.blocks.PSObject.Properties.Name -contains '*all-internet*') {
        $s.blocks.PSObject.Properties.Remove('*all-internet*')
    }
    $s | ConvertTo-Json -Depth 6 | Set-Content "$dataDir\state.json" -Encoding UTF8
    Write-Log "[2] sentinela removido + last_known_time=$( $s.last_known_time)"

    netsh advfirewall firewall delete rule name=$allInternetRule 2>&1 | Out-Null
    Write-Log '[3] regra FocusGuard_AllInternet removida'

    # Limpar entradas FocusGuard do hosts file (hostswatch restaura a cada 30s).
    if (Test-Path $hostsPath) {
        $lines = Get-Content $hostsPath
        $kept = $lines | Where-Object { $_ -notmatch 'FocusGuard' -and $_ -notmatch '\*all-internet\*' }
        if ($kept.Count -ne $lines.Count) {
            Set-Content -Path $hostsPath -Value $kept -Encoding ASCII
            Write-Log "[3] hosts file limpo de entradas FocusGuard ($($lines.Count) -> $($kept.Count) linhas)"
        } else {
            Write-Log '[3] hosts file ja estava limpo'
        }
    }

    Start-Service FocusGuard
    Start-Sleep -Seconds 5
    if ($watchdogRunning) { Start-Service FocusGuardWatchdog; Write-Log '[4] watchdog religado' }
}
finally {
    if ((Get-Service FocusGuard -ErrorAction SilentlyContinue).Status -ne 'Running') {
        Start-Service FocusGuard; Write-Log '[finally] daemon religado'
    }
    if ($watchdogRunning -and (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status -ne 'Running') {
        Start-Service FocusGuardWatchdog; Write-Log '[finally] watchdog religado'
    }
}

# --- Verificacao 1 (apos ~15s) -----------------------------------------
Start-Sleep -Seconds 12
$s1 = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
$has1 = $s1.blocks.PSObject.Properties.Name -contains '*all-internet*'
$fw1 = netsh advfirewall firewall show rule name=$allInternetRule 2>&1 | Out-String
$fw1p = -not ($fw1 -match 'Nenhuma regra|No rules match')
Write-Log "[V1] sentinela=$has1 (esperado False) fw=$fw1p (esperado False)"

# --- Verificacao 2 (apos ~30s - janela do hostswatch) ------------------
Start-Sleep -Seconds 20
$s2 = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
$has2 = $s2.blocks.PSObject.Properties.Name -contains '*all-internet*'
$fw2 = netsh advfirewall firewall show rule name=$allInternetRule 2>&1 | Out-String
$fw2p = -not ($fw2 -match 'Nenhuma regra|No rules match')
Write-Log "[V2] sentinela=$has2 (esperado False) fw=$fw2p (esperado False) hora=$(Get-Date -Format 's')"

$svcD = (Get-Service FocusGuard).Status
$svcW = (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status
Write-Log "[FIM] daemon=$svcD watchdog=$svcW sentinela_final=$has2 fw_final=$fw2p"
