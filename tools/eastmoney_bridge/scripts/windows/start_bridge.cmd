@echo off
setlocal
cd /d D:\FinanceSys\eastmoney_bridge
if not exist logs mkdir logs
if not exist .venv\Scripts\python.exe exit /b 2
.venv\Scripts\python.exe -m bridge_api.main >> logs\bridge.log 2>&1
exit /b %errorlevel%

