@echo off
setlocal
cd /d D:\FinanceSys\eastmoney_bridge
if not exist logs mkdir logs
set "PYTHON=py"

:run
echo [%date% %time%] starting gm_runner >> logs\runner-supervisor.log
"%PYTHON%" -3.10 -m gm_runner.main >> logs\runner.log 2>&1
set "EXIT_CODE=%errorlevel%"
echo [%date% %time%] gm_runner exited with code %EXIT_CODE%; restart in 5 seconds >> logs\runner-supervisor.log
timeout /t 5 /nobreak >nul
goto run
