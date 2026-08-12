# e2e-clockguard-real.ps1
# E2E real do Clock Guard (deve rodar ELEVADO):
#   1. Snapshot: hora, state.json, tamper.jsonl, firewall, status CLI.
#   2. Para o watchdog + daemon; adianta o relogio +24h.
#   3. Bloqueia UDP/123 (simula "rede bloqueada" so para o NTP - cirurgico,
#      nao derruba a internet do usuario).
#   4. Sobe o daemon -> boot check: suspeita + NTP offline -> lockdown
#      preventivo NA SUSPEITA (sentinela source=clock-guard no state.json,
#      regra FocusGuard_AllInternet no firewall, SEM evento de tamper).
#   5. Desbloqueia UDP/123 -> restart do daemon -> NTP confirma a burla ->
#      tamper-log grava clock/lockdown "confirmado por NTP", bloqueio mantido.
#   6. Corrige o relogio -> restart -> relogio consistente -> LIBERACAO
#      automatica (sentinela sai do state, regra AllInternet removida).
#   7. Restaura watchdog/daemon e garante o relogio correto (finally).
#
# Log de saida: C:\ProgramData\FocusGuard\e2e-clockguard-real.log
# (linhas marcadas [SNAP]/[CHECK*]/[FIM] para parsing externo).
# ATENCAO: apenas ASCII neste arquivo (Windows PowerShell 5.1 le .ps1 sem
# BOM como ANSI - acentos/em-dash em UTF-8 quebram o parse).

$ErrorActionPreference = 'Continue'
$log = 'C:\ProgramData\FocusGuard\e2e-clockguard-real.log'
$dataDir = 'C:\ProgramData\FocusGuard'
$cli = 'C:\Program Files\FocusGuard\focusguard.exe'
$ntpRule = 'FG_E2E_BlockNTP'
$sentinel = '*all-internet*'
$allInternetRule = 'FocusGuard_AllInternet'

function Write-Log([string]$m) {
    $line = "[$(Get-Date -Format 'HH:mm:ss')] $m"
    Add-Content -Path $log -Value $line
    Write-Host $line
}

# --- Elevacao -----------------------------------------------------------
$id = [Security.Principal.WindowsIdentity]::GetCurrent()
$pr = New-Object Security.Principal.WindowsPrincipal($id)
if (-not $pr.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Add-Content -Path $log -Value '[ERRO] Este script exige shell ELEVADO. Aborte.'
    exit 1
}

Set-Content -Path $log -Value "[ELEVADO] Inicio do E2E real do Clock Guard ($(Get-Date -Format 's'))"
Write-Log "PID: $($id.Name)"

# --- Helpers ------------------------------------------------------------
function Get-StateBlocks {
    $s = Get-Content "$dataDir\state.json" -Raw | ConvertFrom-Json
    return $s.blocks
}
function Get-TamperLines { return (Get-Content "$dataDir\tamper.jsonl" -ErrorAction SilentlyContinue | Measure-Object -Line).Lines }
function Test-FwRule([string]$name) {
    $out = netsh advfirewall firewall show rule name=$name 2>&1 | Out-String
    if ($out -match 'Nenhuma regra|No rules match') { return $false }
    return $out -match [regex]::Escape($name)
}
function Wait-ServiceRunning([string]$svc, [int]$timeoutSec) {
    for ($i = 0; $i -lt $timeoutSec; $i++) {
        $st = (Get-Service $svc -ErrorAction SilentlyContinue).Status
        if ($st -eq 'Running') { return $true }
        Start-Sleep -Seconds 1
    }
    return $false
}

# --- 1. Snapshot --------------------------------------------------------
$snapLocal = Get-Date
$snapTamper = Get-TamperLines
Write-Log "[SNAP] hora_local=$($snapLocal.ToString('s'))"
Write-Log "[SNAP] state_blocks_keys=$((Get-StateBlocks).PSObject.Properties.Name -join ',')"
Write-Log "[SNAP] tamper_lines=$snapTamper"
Write-Log "[SNAP] fw_AllInternet=$(Test-FwRule $allInternetRule)"
Copy-Item "$dataDir\state.json" "$dataDir\state.json.e2e-bak" -Force
Copy-Item "$dataDir\tamper.jsonl" "$dataDir\tamper.jsonl.e2e-bak" -Force -ErrorAction SilentlyContinue
Write-Log "[SNAP] backups: state.json.e2e-bak / tamper.jsonl.e2e-bak"

$daemonRunning = (Get-Service FocusGuard -ErrorAction SilentlyContinue).Status -eq 'Running'
$watchdogRunning = (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status -eq 'Running'
Write-Log "[SNAP] daemon_running=$daemonRunning watchdog_running=$watchdogRunning"

try {
    # --- 2. Parar servicos + adiantar relogio --------------------------
    if ($watchdogRunning) { Stop-Service FocusGuardWatchdog -Force; Write-Log '[SERVICES] watchdog parado' }
    if ($daemonRunning) { Stop-Service FocusGuard -Force; Write-Log '[SERVICES] daemon parado' }
    Start-Sleep -Seconds 3

    $fake = $snapLocal.AddHours(24)
    Set-Date -Date $fake | Out-Null
    Write-Log "[CLOCK] relogio adiantado +24h -> $(Get-Date -Format 's')"

    # --- 3. Bloquear NTP (UDP 123 out) ---------------------------------
    netsh advfirewall firewall add rule name=$ntpRule dir=out protocol=UDP remoteport=123 action=block | Out-Null
    Write-Log "[NTPBLOCK] regra $ntpRule criada (UDP 123 out) - NTP do daemon ficara offline"

    # --- 4. Subir daemon -> lockdown NA SUSPEITA (NTP offline) ----------
    Start-Service FocusGuard
    if (-not (Wait-ServiceRunning FocusGuard 30)) { Write-Log '[CHECK1] ERRO: daemon nao ficou Running'; exit 2 }
    Start-Sleep -Seconds 15   # boot: init + interceptor + boot check (NTP timeout 3s)

    $b1 = Get-StateBlocks
    $sent1 = $b1.$sentinel
    Write-Log "[CHECK1] sentinela_present=$($null -ne $sent1) source=$($sent1.source)"
    Write-Log "[CHECK1] fw_AllInternet=$(Test-FwRule $allInternetRule)"
    $t1 = Get-TamperLines
    Write-Log "[CHECK1] tamper_lines=$t1 (antes=$snapTamper)"
    $cli1 = & $cli status 2>&1 | Out-String
    Write-Log "[CHECK1] cli_has_block=$($cli1 -match '\*all-internet\*')"

    # --- 5. Rede volta -> NTP confirma a burla --------------------------
    netsh advfirewall firewall delete rule name=$ntpRule | Out-Null
    Write-Log '[NTPUNBLOCK] regra UDP 123 removida - NTP acessivel de novo'
    Restart-Service FocusGuard -Force
    if (-not (Wait-ServiceRunning FocusGuard 30)) { Write-Log '[CHECK2] ERRO: daemon nao ficou Running'; exit 2 }
    Start-Sleep -Seconds 15

    $b2 = Get-StateBlocks
    $sent2 = $b2.$sentinel
    $t2 = Get-TamperLines
    $tamperDelta = $t2 - $t1
    $tail = Get-Content "$dataDir\tamper.jsonl" -Tail 3 | Out-String
    Write-Log "[CHECK2] sentinela_present=$($null -ne $sent2) source=$($sent2.source) tamper_delta=$tamperDelta"
    Write-Log "[CHECK2] tamper_tail=$tail"

    # --- 6. Corrigir relogio -> liberacao automatica -------------------
    Set-Date -Date $snapLocal | Out-Null
    Write-Log "[CLOCKFIX] relogio restaurado -> $(Get-Date -Format 's')"
    Restart-Service FocusGuard -Force
    if (-not (Wait-ServiceRunning FocusGuard 30)) { Write-Log '[CHECK3] ERRO: daemon nao ficou Running'; exit 2 }
    Start-Sleep -Seconds 15

    $b3 = Get-StateBlocks
    $sent3 = $b3.$sentinel
    $t3 = Get-TamperLines
    $cli3 = & $cli status 2>&1 | Out-String
    Write-Log "[CHECK3] sentinela_present=$($null -ne $sent3) (esperado False)"
    Write-Log "[CHECK3] fw_AllInternet=$(Test-FwRule $allInternetRule) (esperado False)"
    Write-Log "[CHECK3] tamper_lines=$t3 cli_has_block=$($cli3 -match '\*all-internet\*')"

    # --- Verdict --------------------------------------------------------
    $ok1 = ($null -ne $sent1 -and $sent1.source -eq 'clock-guard' -and (Test-FwRule $allInternetRule))
    $ok2 = ($null -ne $sent2 -and $tamperDelta -ge 1 -and $tail -match 'clock')
    $ok3 = ($null -eq $sent3 -and -not (Test-FwRule $allInternetRule))
    Write-Log "[FIM] CHECK1(lockdown_suspeita)=$ok1 CHECK2(confirmacao_tamper)=$ok2 CHECK3(liberacao)=$ok3"
    Write-Log "[FIM] VEREDICTO=$($ok1 -and $ok2 -and $ok3)"
}
finally {
    # --- 7. Restauracao garantida --------------------------------------
    netsh advfirewall firewall delete rule name=$ntpRule 2>&1 | Out-Null
    Write-Log '[RESTORE] regra UDP 123 removida (se existia)'
    if ((Get-Date) -gt $snapLocal.AddMinutes(1) -or (Get-Date) -lt $snapLocal.AddMinutes(-1)) {
        Set-Date -Date $snapLocal | Out-Null
        Write-Log "[RESTORE] relogio corrigido -> $(Get-Date -Format 's')"
    } else {
        Write-Log "[RESTORE] relogio ok ($(Get-Date -Format 's'))"
    }
    if ($watchdogRunning -and (Get-Service FocusGuardWatchdog -ErrorAction SilentlyContinue).Status -ne 'Running') {
        Start-Service FocusGuardWatchdog; Write-Log '[RESTORE] watchdog religado'
    }
    if ((Get-Service FocusGuard -ErrorAction SilentlyContinue).Status -ne 'Running') {
        Start-Service FocusGuard; Write-Log '[RESTORE] daemon religado'
    }
    Write-Log '[RESTORE] estado restaurado. Backups: state.json.e2e-bak / tamper.jsonl.e2e-bak'
}
