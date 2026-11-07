@echo off
chcp 936 >nul
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0verify.ps1" %*
if defined EPO_NOPAUSE exit /b 0
pause
