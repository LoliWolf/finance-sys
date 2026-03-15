@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT_DIR=%~dp0"
cd /d "%ROOT_DIR%"

set "ENV_FILE=%ROOT_DIR%bootstrap_go122.env.example"
if not exist "%ENV_FILE%" (
  echo [ERROR] Env file not found: %ENV_FILE%
  pause
  exit /b 1
)

for /f "usebackq tokens=* delims=" %%A in ("%ENV_FILE%") do (
  set "LINE=%%A"
  if not "!LINE!"=="" (
    if not "!LINE:~0,1!"=="#" (
      for /f "tokens=1,* delims==" %%B in ("!LINE!") do (
        if not "%%B"=="" set "%%B=%%C"
      )
    )
  )
)

set "GOTOOLCHAIN=local"
set "GOCACHE=%ROOT_DIR%.gocache"
if defined EXTRA_PATH set "PATH=%EXTRA_PATH%;%PATH%"
if not defined APP_BASE_URL set "APP_BASE_URL=http://127.0.0.1:30005"
set "UPLOAD_URL=%APP_BASE_URL%/upload"
set "HEALTH_URL=%APP_BASE_URL%/healthz"
set "APP_PORT=30005"

for /f "tokens=4 delims=/: " %%P in ("%APP_BASE_URL%") do set "APP_PORT=%%P"

echo [DEBUG] Trying to stop old service on port %APP_PORT%
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$connections = Get-NetTCPConnection -LocalPort %APP_PORT% -State Listen -ErrorAction SilentlyContinue;" ^
  "if(-not $connections) { exit 0 }" ^
  "$procIds = $connections | Select-Object -Expand OwningProcess -Unique;" ^
  "foreach($procId in $procIds) { try { Stop-Process -Id $procId -Force -ErrorAction Stop; Write-Host ('[DEBUG] Stopped PID ' + $procId) } catch { Write-Host ('[WARN] Failed to stop PID ' + $procId + ': ' + $_.Exception.Message) } }"
timeout /t 1 /nobreak >nul

echo [DEBUG] ROOT_DIR=%ROOT_DIR%
echo [DEBUG] ENV_FILE=%ENV_FILE%
echo [DEBUG] NACOS_SERVER_ADDR=%NACOS_SERVER_ADDR%
echo [DEBUG] NACOS_NAMESPACE=%NACOS_NAMESPACE%
echo [DEBUG] NACOS_GROUP=%NACOS_GROUP%
echo [DEBUG] NACOS_DATA_ID=%NACOS_DATA_ID%
echo [DEBUG] NACOS_USERNAME=%NACOS_USERNAME%
echo [DEBUG] GOTOOLCHAIN=%GOTOOLCHAIN%
echo [DEBUG] GOCACHE=%GOCACHE%
echo [DEBUG] EXTRA_PATH=%EXTRA_PATH%
echo [DEBUG] APP_BASE_URL=%APP_BASE_URL%
echo.
echo [DEBUG] Waiting for %HEALTH_URL% and will open %UPLOAD_URL%

start "" powershell -WindowStyle Hidden -NoProfile -ExecutionPolicy Bypass -Command ^
  "$deadline=(Get-Date).AddSeconds(60);" ^
  "while((Get-Date) -lt $deadline) {" ^
  "  try {" ^
  "    $resp=Invoke-WebRequest -UseBasicParsing -Uri '%HEALTH_URL%' -TimeoutSec 2;" ^
  "    if($resp.StatusCode -eq 200) { Start-Process '%UPLOAD_URL%'; exit 0 }" ^
  "  } catch {}" ^
  "  Start-Sleep -Milliseconds 500" ^
  "}" ^
  "exit 1"

echo.
echo [DEBUG] Starting API. Press Ctrl+C to stop.
echo.
go run ./cmd/api
set "EXIT_CODE=%ERRORLEVEL%"
echo.
echo [DEBUG] Process exited with code %EXIT_CODE%
pause
exit /b %EXIT_CODE%
