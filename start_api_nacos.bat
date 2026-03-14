@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT_DIR=%~dp0"
cd /d "%ROOT_DIR%"

set "ENV_FILE=%ROOT_DIR%bootstrap_go122.env.example"
if not exist "%ENV_FILE%" (
  echo [ERROR] Env file not found: %ENV_FILE%
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
if not defined APP_BASE_URL set "APP_BASE_URL=http://127.0.0.1:30005"
set "UPLOAD_URL=%APP_BASE_URL%/upload"
set "HEALTH_URL=%APP_BASE_URL%/healthz"
set "APP_PORT=30005"

for /f "tokens=4 delims=/: " %%P in ("%APP_BASE_URL%") do set "APP_PORT=%%P"

echo [INFO] Trying to stop old service on port %APP_PORT%
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$connections = Get-NetTCPConnection -LocalPort %APP_PORT% -State Listen -ErrorAction SilentlyContinue;" ^
  "if(-not $connections) { exit 0 }" ^
  "$procIds = $connections | Select-Object -Expand OwningProcess -Unique;" ^
  "foreach($procId in $procIds) { try { Stop-Process -Id $procId -Force -ErrorAction Stop; Write-Host ('[INFO] Stopped PID ' + $procId) } catch { Write-Host ('[WARN] Failed to stop PID ' + $procId + ': ' + $_.Exception.Message) } }"
timeout /t 1 /nobreak >nul

echo [INFO] Starting API with Nacos bootstrap from %ENV_FILE%
start "finance-sys-api" cmd /k "cd /d \"%ROOT_DIR%\" && go run ./cmd/api"

echo [INFO] Waiting for %HEALTH_URL%
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$deadline=(Get-Date).AddSeconds(60);" ^
  "while((Get-Date) -lt $deadline) {" ^
  "  try {" ^
  "    $resp=Invoke-WebRequest -UseBasicParsing -Uri '%HEALTH_URL%' -TimeoutSec 2;" ^
  "    if($resp.StatusCode -eq 200) { Start-Process '%UPLOAD_URL%'; exit 0 }" ^
  "  } catch {}" ^
  "  Start-Sleep -Milliseconds 500" ^
  "}" ^
  "Write-Host '[WARN] API did not become healthy within 60 seconds.'; exit 1"

exit /b %ERRORLEVEL%
