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

echo [3/4] Removing local data (credentials, state)...
rmdir /S /Q "%LOCALAPPDATA%\epo" >nul 2>&1

echo [4/4] Cleaning program files (exe, log folder)...
del /f /q "%~dp0epo.exe" >nul 2>&1
del /f /q "%~dp0epo-daemon.exe" >nul 2>&1
del /f /q "%~dp0epo-gui.exe" >nul 2>&1
del /f /q "%~dp0README.md" >nul 2>&1
del /f /q "%~dp0checksums.txt" >nul 2>&1
rd /s /q "%~dp0log" >nul 2>&1

echo.
echo Done. If this folder is empty, delete it manually:
echo   %~dp0
echo.
pause
