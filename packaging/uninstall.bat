@echo off
setlocal
cd /d "%~dp0"

echo ========================================
echo   epo portable uninstaller
echo ========================================

echo [1/3] Removing autostart entry...
reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v epo-daemon /f >nul 2>&1

echo [2/3] Stopping daemon and GUI...
if exist "%LOCALAPPDATA%\epo" (
    > "%LOCALAPPDATA%\epo\stop.flag" echo stop
)
timeout /t 6 /nobreak >nul
taskkill /IM epo-daemon.exe /F >nul 2>&1
taskkill /IM epo-gui.exe /F >nul 2>&1

echo [3/3] Removing local data (credentials, logs, state)...
rmdir /S /Q "%LOCALAPPDATA%\epo" >nul 2>&1

echo.
echo Done.
echo Please delete this folder manually:
echo   %~dp0
echo.
pause
