# e2e-clockguard-cleanup2.ps1
# Limpeza pos-E2E v2 (deve rodar ELEVADO).
#
# Por que v2: o cleanup v1 restaurou o state.json do backup pre-teste, mas o
# backup tem last_known_time defasado (~13 min atras). Ao religar, o clock
# guard do daemon instalado ve gap > tolerancia (5 min) -> suspeita -> NTP
# confirma -> RE-BLOQUEIA o all-internet (observado no tamper-log:
# clock/lockdown + hosts/restore "*all-internet*" a cada ~30s).
#
# v2: restaura o backup E re-ancora last_known_time para AGORA, de modo que o
# boot do daemon nao veja gap. Tambem remove a regra FocusGuard_AllInternet.
#
# Log: C:\ProgramData\FocusGuard\e2e-clockguard-cleanup2.log

$ErrorActionPreference = 'Continue'
$log = 'C:\ProgramData\FocusGuard\e2e-clockguard-cleanup2.log'
$dataDir = 'C:\ProgramData\FocusGuard'
$allInternetRule = 'FocusGuard_AllInternet'

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

Set-Content -Path $log -Value "[ELEVADO] Limpeza pos-E2E v2 ($(Get-Date -Format 's'))"

$watchdogRunning = (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status -eq 'Running'
$daemonRunning = (Get-Service FocusGuard -ErrorAction SilentlyContinue).Status -eq 'Running'

try {
    if ($watchdogRunning) { Stop-Service FocusGuardWatchdog -Force; Write-Log '[1] watchdog parado' }
    if ($daemonRunning) { Stop-Service FocusGuard -Force; Write-Log '[1] daemon parado' }
    Start-Sleep -Seconds 3

    # Restaurar do backup pre-teste (blocks={}) e re-ancorar last_known_time.
    Copy-Item "$dataDir\state.json.e2e-bak" "$dataDir\state.json" -Force
    Write-Log '[2] state.json restaurado do backup pre-teste'

    $s = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
    $nowLocal = Get-Date
    $s.last_known_time = $nowLocal.ToString('o')
    if ($s.blocks.PSObject.Properties.Name -contains '*all-internet*') {
        $s.blocks.PSObject.Properties.Remove('*all-internet*')
    }
    $s | ConvertTo-Json -Depth 6 | Set-Content "$dataDir\state.json" -Encoding UTF8
    Write-Log "[2] last_known_time re-ancorado para $($s.last_known_time) (evita re-bloqueio no boot)"

    netsh advfirewall firewall delete rule name=$allInternetRule 2>&1 | Out-Null
    Write-Log '[3] regra FocusGuard_AllInternet removida'

    Start-Service FocusGuard
    Start-Sleep -Seconds 5
    if ($watchdogRunning) { Start-Service FocusGuardWatchdog; Write-Log '[4] watchdog religado' }
    Start-Sleep -Seconds 12
}
finally {
    if ((Get-Service FocusGuard -ErrorAction SilentlyContinue).Status -ne 'Running') {
        Start-Service FocusGuard; Write-Log '[finally] daemon religado'
    }
    if ($watchdogRunning -and (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status -ne 'Running') {
        Start-Service FocusGuardWatchdog; Write-Log '[finally] watchdog religado'
    }
}

# --- Verificacao final -------------------------------------------------
Start-Sleep -Seconds 3
$s = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
$hasSentinel = $s.blocks.PSObject.Properties.Name -contains '*all-internet*'
$fw = netsh advfirewall firewall show rule name=$allInternetRule 2>&1 | Out-String
$fwPresent = -not ($fw -match 'Nenhuma regra|No rules match')
Write-Log "[FIM] sentinela_no_state=$hasSentinel (esperado False) fw_AllInternet=$fwPresent (esperado False)"
Write-Log "[FIM] last_known_time=$($s.last_known_time) hora=$(Get-Date -Format 's')"
$svcD = (Get-Service FocusGuard).Status
$svcW = (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status
Write-Log "[FIM] daemon=$svcD watchdog=$svcW"
