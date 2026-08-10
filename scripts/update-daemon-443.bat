@echo off
title FocusGuard - Atualizar daemon (TLS 443)
echo ============================================================
echo  FocusGuard - Instalar daemon com suporte HTTPS :443
echo  Execute como Administrador!
echo ============================================================
echo.

net session >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERRO] Este script precisa ser executado como Administrador.
    echo        Clique com o botao direito no arquivo e escolha
    echo        "Executar como administrador".
    echo.
    pause
    exit /b 1
)

set SRV_NAME=FocusGuard
set INSTALL_DIR=C:\Program Files\FocusGuard

echo [1/4] Parando o servico %SRV_NAME%...
sc.exe stop %SRV_NAME% >nul 2>&1
timeout /t 3 /nobreak >nul

echo [2/4] Fazendo backup do daemon atual...
copy /Y "%INSTALL_DIR%\focusguard-daemon.exe" "%INSTALL_DIR%\focusguard-daemon.exe.bak-pre443" >nul
if %errorlevel% neq 0 (
    echo [ERRO] Falha no backup. Abortando.
    pause
    exit /b 1
)

echo [3/4] Instalando o daemon novo (com TLS 443)...
copy /Y "%~dp0focusguard-daemon-new.exe" "%INSTALL_DIR%\focusguard-daemon.exe" >nul
if %errorlevel% neq 0 (
    echo [ERRO] Falha ao copiar o binario. Restaurando backup...
    copy /Y "%INSTALL_DIR%\focusguard-daemon.exe.bak-pre443" "%INSTALL_DIR%\focusguard-daemon.exe" >nul
    sc.exe start %SRV_NAME% >nul 2>&1
    pause
    exit /b 1
)

echo [4/4] Iniciando o servico %SRV_NAME%...
sc.exe start %SRV_NAME% >nul 2>&1
timeout /t 2 /nobreak >nul

echo.
echo ============================================================
echo  Daemon atualizado com sucesso!
echo  Agora abra um site HTTPS bloqueado (ex.: youtube.com)
echo  o Firefox mostrara o aviso de certificado:
echo      "Avançado" -^> "Continuar" -^> veja a pagina do FocusGuard
echo.
echo  Se algo der errado, o backup esta em:
echo      %INSTALL_DIR%\focusguard-daemon.exe.bak-pre443
echo ============================================================
pause
