@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0run_api_nacos.ps1" -Mode start %*
exit /b %ERRORLEVEL%
