# e2e-clockguard-cleanup.ps1
# Limpeza pos-E2E (deve rodar ELEVADO):
#   O E2E adiantou o relogio +24h e o daemon instalado (0.16.4) criou o
#   bloqueio all-internet com expires_at gravado com o relogio adiantado
#   (12/08 18:20 = ~24h no relogio real). O CLI 0.16.4 nao tem unblock
#   manual. Limpeza segura:
#     1. Para watchdog + daemon.
#     2. Restaura state.json do backup do snapshot (estado pre-teste).
#     3. Remove a regra FocusGuard_AllInternet do firewall.
#     4. Religar watchdog + daemon (o Reconcile no boot reaplica o estado).
#     5. Verifica: state sem sentinela, firewall limpo, relogio ok.
#
# O tamper.jsonl NAO e restaurado: o evento clock/lockdown e historico
# legitimo do teste (append-only por design).
#
# Log: C:\ProgramData\FocusGuard\e2e-clockguard-cleanup.log

$ErrorActionPreference = 'Continue'
$log = 'C:\ProgramData\FocusGuard\e2e-clockguard-cleanup.log'
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

Set-Content -Path $log -Value "[ELEVADO] Limpeza pos-E2E ($(Get-Date -Format 's'))"

$watchdogRunning = (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status -eq 'Running'
$daemonRunning = (Get-Service FocusGuard -ErrorAction SilentlyContinue).Status -eq 'Running'

try {
    if ($watchdogRunning) { Stop-Service FocusGuardWatchdog -Force; Write-Log '[1] watchdog parado' }
    if ($daemonRunning) { Stop-Service FocusGuard -Force; Write-Log '[1] daemon parado' }
    Start-Sleep -Seconds 3

    if (Test-Path "$dataDir\state.json.e2e-bak") {
        Copy-Item "$dataDir\state.json.e2e-bak" "$dataDir\state.json" -Force
        Write-Log '[2] state.json restaurado do backup pre-teste'
    } else {
        Write-Log '[2] AVISO: backup state.json.e2e-bak ausente - limpando sentinela via JSON'
        $s = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
        if ($s.blocks.PSObject.Properties.Name -contains '*all-internet*') {
            $s.blocks.PSObject.Properties.Remove('*all-internet*')
        }
        $s | ConvertTo-Json -Depth 6 | Set-Content "$dataDir\state.json" -Encoding UTF8
    }

    netsh advfirewall firewall delete rule name=$allInternetRule 2>&1 | Out-Null
    Write-Log '[3] regra FocusGuard_AllInternet removida'

    Start-Service FocusGuard
    Start-Sleep -Seconds 5
    if ($watchdogRunning) { Start-Service FocusGuardWatchdog; Write-Log '[4] watchdog religado' }
    Start-Sleep -Seconds 8
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
$s = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
$hasSentinel = $s.blocks.PSObject.Properties.Name -contains '*all-internet*'
$fw = netsh advfirewall firewall show rule name=$allInternetRule 2>&1 | Out-String
$fwPresent = -not ($fw -match 'Nenhuma regra|No rules match')
Write-Log "[FIM] sentinela_no_state=$hasSentinel (esperado False) fw_AllInternet=$fwPresent (esperado False)"
Write-Log "[FIM] hora=$(Get-Date -Format 's')"
$svcD = (Get-Service FocusGuard).Status
$svcW = (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status
Write-Log "[FIM] daemon=$svcD watchdog=$svcW"
