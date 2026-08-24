param(
    [Parameter(Mandatory=$true)]
    [string]$CertificatePath,

    [Parameter(Mandatory=$true)]
    [string]$CertificatePassword,

    [Parameter(Mandatory=$false)]
    [string]$SearchPath = ".",

    [Parameter(Mandatory=$false)]
    [string]$TimestampServer = "http://timestamp.digicert.com",

    [Parameter(Mandatory=$false)]
    [string]$Description = "FocusGuard — bloqueio focado de distrações",

    [Parameter(Mandatory=$false)]
    [switch]$VerifyOnly
)

# sign-windows.ps1 — Assina binários Windows (.exe, .msi) com Authenticode.
#
# Uso local:
#   .\scripts\sign-windows.ps1 -CertificatePath cert.pfx -CertificatePassword "senha"
#
# CI (GitHub Actions):
#   O certificado é salvo como GitHub Secret (base64 do PFX) e decodificado
#   antes de chamar este script. Ver .github/workflows/release.yml → sign-windows.
#
# O script é idempotente: arquivos já assinados são ignorados (verificação
# automática via Get-AuthenticodeSignature).

$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------- Validações
if (-not (Test-Path $CertificatePath)) {
    Write-Error "Certificado não encontrado: $CertificatePath"
    exit 1
}

if (-not (Test-Path $SearchPath)) {
    Write-Error "Diretório não encontrado: $SearchPath"
    exit 1
}

# Encontrar signtool.exe — tenta o path comum do Windows SDK
$signtool = $null
$sdks = @(
    "${env:ProgramFiles(x86)}\Windows Kits\10\bin\x64\signtool.exe",
    "${env:ProgramFiles(x86)}\Windows Kits\10\bin\x86\signtool.exe",
    "${env:ProgramFiles}\Windows Kits\10\bin\x64\signtool.exe"
)
foreach ($sdk in $sdks) {
    if (Test-Path $sdk) {
        $signtool = $sdk
        break
    }
}
if (-not $signtool) {
    Write-Error "signtool.exe não encontrado. Instale o Windows SDK ou adicione ao PATH."
    exit 1
}
Write-Host "==> Usando signtool: $signtool" -ForegroundColor Cyan

# ---------------------------------------------------------------- Converter senha
$securePassword = ConvertTo-SecureString $CertificatePassword -AsPlainText -Force

# ---------------------------------------------------------------- Encontrar arquivos
$files = Get-ChildItem -Path $SearchPath -Recurse -Include *.exe,*.msi -File
if ($files.Count -eq 0) {
    Write-Host "Nenhum arquivo .exe ou .msi encontrado em $SearchPath" -ForegroundColor Yellow
    exit 0
}

Write-Host "==> Encontrados $($files.Count) arquivo(s) para assinatura:" -ForegroundColor Cyan
foreach ($f in $files) {
    $rel = $f.FullName.Replace("$((Resolve-Path $SearchPath).Path)\", "")
    Write-Host "  - $rel" -ForegroundColor Gray
}

# ---------------------------------------------------------------- Verificar se já assinados
$toSign = @()
$alreadySigned = @()
foreach ($f in $files) {
    $sig = Get-AuthenticodeSignature -FilePath $f.FullName
    if ($sig.Status -eq "Valid") {
        $alreadySigned += $f
        $rel = $f.FullName.Replace("$((Resolve-Path $SearchPath).Path)\", "")
        Write-Host "  [SKIP] $rel — já assinado" -ForegroundColor DarkGray
    } else {
        $toSign += $f
    }
}

if ($alreadySigned.Count -gt 0) {
    Write-Host "" -ForegroundColor Gray
    Write-Host "  $($alreadySigned.Count) arquivo(s) já assinado(s) — ignorando." -ForegroundColor Yellow
}

if ($toSign.Count -eq 0) {
    Write-Host "" -ForegroundColor Green
    Write-Host "==> Todos os arquivos já estão assinados." -ForegroundColor Green
    exit 0
}

if ($VerifyOnly) {
    Write-Host "" -ForegroundColor Yellow
    Write-Host "==> Modo verificação: $($toSign.Count) arquivo(s) NÃO assinado(s)." -ForegroundColor Yellow
    foreach ($f in $toSign) {
        $rel = $f.FullName.Replace("$((Resolve-Path $SearchPath).Path)\", "")
        Write-Host "  [FAIL] $rel" -ForegroundColor Red
    }
    exit 1
}

# ---------------------------------------------------------------- Assinar
Write-Host "" -ForegroundColor Gray
Write-Host "==> Assinando $($toSign.Count) arquivo(s)..." -ForegroundColor Green

$signed = 0
$failed = 0
foreach ($f in $toSign) {
    $rel = $f.FullName.Replace("$((Resolve-Path $SearchPath).Path)\", "")
    Write-Host "  Assinando: $rel" -ForegroundColor Cyan -NoNewline

    try {
        $result = & $signtool sign `
            /f $CertificatePath `
            /p $CertificatePassword `
            /fd SHA256 `
            /tr $TimestampServer `
            /td SHA256 `
            /d $Description `
            /d 0x10000 `
            $f.FullName 2>&1

        if ($LASTEXITCODE -eq 0) {
            # Verificar assinatura
            $sig = Get-AuthenticodeSignature -FilePath $f.FullName
            if ($sig.Status -eq "Valid") {
                Write-Host " [OK]" -ForegroundColor Green
                $signed++
            } else {
                Write-Host " [WARN] Status=$($sig.Status)" -ForegroundColor Yellow
                $signed++ # Continuar mesmo com warning
            }
        } else {
            Write-Host " [ERRO]" -ForegroundColor Red
            Write-Host "    $result" -ForegroundColor Red
            $failed++
        }
    } catch {
        Write-Host " [EXCEÇÃO]" -ForegroundColor Red
        Write-Host "    $($_.Exception.Message)" -ForegroundColor Red
        $failed++
    }
}

# ---------------------------------------------------------------- Resumo
Write-Host "" -ForegroundColor Gray
Write-Host "==> Resumo da assinatura:" -ForegroundColor Cyan
Write-Host "  Assinados: $signed" -ForegroundColor Green
if ($failed -gt 0) {
    Write-Host "  Falhas:    $failed" -ForegroundColor Red
}
if ($alreadySigned.Count -gt 0) {
    Write-Host "  Ignorados: $($alreadySigned.Count) (já assinados)" -ForegroundColor Yellow
}

# ---------------------------------------------------------------- Verificação final
Write-Host "" -ForegroundColor Gray
Write-Host "==> Verificando assinaturas..." -ForegroundColor Cyan
$allFiles = Get-ChildItem -Path $SearchPath -Recurse -Include *.exe,*.msi -File
$valid = 0
$invalid = 0
foreach ($f in $allFiles) {
    $sig = Get-AuthenticodeSignature -FilePath $f.FullName
    $rel = $f.FullName.Replace("$((Resolve-Path $SearchPath).Path)\", "")
    if ($sig.Status -eq "Valid") {
        Write-Host "  [OK] $rel" -ForegroundColor Green
        $valid++
    } else {
        Write-Host "  [FAIL] $rel — $($sig.Status)" -ForegroundColor Red
        $invalid++
    }
}

Write-Host "" -ForegroundColor Gray
if ($invalid -gt 0) {
    Write-Host "==> $invalid arquivo(s) com assinatura inválida!" -ForegroundColor Red
    exit 1
} else {
    Write-Host "==> Todos os $valid arquivo(s) estão assinados corretamente." -ForegroundColor Green
    exit 0
}
