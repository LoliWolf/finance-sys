param(
  [ValidateSet("start", "debug")]
  [string]$Mode = "start",

  [string]$EnvFile = ""
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$RootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -LiteralPath $RootDir

function Import-NacosAddress([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "Env file not found: $Path"
  }

  foreach ($rawLine in [System.IO.File]::ReadAllLines($Path)) {
    $line = $rawLine.Trim()
    if ($line.Length -eq 0 -or $line.StartsWith("#")) {
      continue
    }

    $separator = $line.IndexOf("=")
    if ($separator -lt 1) {
      continue
    }

    $key = $line.Substring(0, $separator).Trim()
    $value = $line.Substring($separator + 1).Trim()
    if ($key -ne "NACOS_SERVER_ADDR") {
      throw "Only NACOS_SERVER_ADDR is allowed in ${Path}; found: $key"
    }
    if ($value.Length -ge 2) {
      $first = $value.Substring(0, 1)
      $last = $value.Substring($value.Length - 1, 1)
      if (($first -eq '"' -and $last -eq '"') -or ($first -eq "'" -and $last -eq "'")) {
        $value = $value.Substring(1, $value.Length - 2)
      }
    }

    if ([string]::IsNullOrWhiteSpace($value)) {
      throw "NACOS_SERVER_ADDR is empty in $Path"
    }
    if (-not [string]::IsNullOrWhiteSpace($script:NacosAddressFromFile)) {
      throw "Duplicate NACOS_SERVER_ADDR in $Path"
    }
    $script:NacosAddressFromFile = $value
  }
  if ([string]::IsNullOrWhiteSpace($script:NacosAddressFromFile)) {
    throw "NACOS_SERVER_ADDR is missing from $Path"
  }
  $env:NACOS_SERVER_ADDR = $script:NacosAddressFromFile
}

function Set-DefaultEnv([string]$Name, [string]$Value) {
  $current = [Environment]::GetEnvironmentVariable($Name, "Process")
  if ([string]::IsNullOrWhiteSpace($current)) {
    [Environment]::SetEnvironmentVariable($Name, $Value, "Process")
  }
}

if ([string]::IsNullOrWhiteSpace($EnvFile)) {
  $localEnv = Join-Path $RootDir "bootstrap_go122.env"
  $exampleEnv = Join-Path $RootDir "bootstrap_go122.env.example"
  if (Test-Path -LiteralPath $localEnv -PathType Leaf) {
    $EnvFile = $localEnv
  } else {
    $EnvFile = $exampleEnv
  }
}
if (-not [System.IO.Path]::IsPathRooted($EnvFile)) {
  $EnvFile = Join-Path $RootDir $EnvFile
}
$script:NacosAddressFromFile = $null
Import-NacosAddress $EnvFile

$NacosNamespace = "public"
$NacosGroup = "DEFAULT_GROUP"
$NacosDataId = "expert_trade"
Set-DefaultEnv "OPEN_UPLOAD_PAGE" "1"
$env:GOTOOLCHAIN = "local"
foreach ($name in @("NACOS_NAMESPACE", "NACOS_GROUP", "NACOS_DATA_ID", "NACOS_USERNAME", "NACOS_PASSWORD", "NACOS_TIMEOUT_MS", "APP_PORT", "APP_BASE_URL")) {
  [Environment]::SetEnvironmentVariable($name, $null, "Process")
}

if ([string]::IsNullOrWhiteSpace($env:NACOS_SERVER_ADDR)) {
  throw "NACOS_SERVER_ADDR is required. Copy bootstrap_go122.env.example to bootstrap_go122.env and set a reachable host:port."
}
if (-not [string]::IsNullOrWhiteSpace($env:EXTRA_PATH)) {
  $env:PATH = $env:EXTRA_PATH + [System.IO.Path]::PathSeparator + $env:PATH
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  throw "Go was not found in PATH. Install Go 1.22.x or set EXTRA_PATH."
}

$query = "dataId={0}&group={1}&tenant={2}" -f [Uri]::EscapeDataString($NacosDataId), [Uri]::EscapeDataString($NacosGroup), [Uri]::EscapeDataString($NacosNamespace)
$configUrl = "http://$($env:NACOS_SERVER_ADDR)/nacos/v1/cs/configs?$query"
try {
  $nacosConfig = Invoke-RestMethod -UseBasicParsing -Method Get -Uri $configUrl -TimeoutSec 10
  if ($nacosConfig -is [string]) {
    $nacosConfig = $nacosConfig | ConvertFrom-Json
  }
  $ProdAppPort = [string]$nacosConfig.service.http.port
  $TestAppPort = [string]$nacosConfig.service.http.port_test
} catch {
  throw "Unable to read HTTP ports from Nacos at $($env:NACOS_SERVER_ADDR): $($_.Exception.Message)"
}
if ($ProdAppPort -notmatch '^\d+$') {
  throw "Nacos service.http.port must be numeric: $ProdAppPort"
}
if ($TestAppPort -notmatch '^\d+$') {
  throw "Nacos service.http.port_test must be numeric: $TestAppPort"
}
if ([int]$ProdAppPort -le 0 -or [int]$ProdAppPort -gt 65535) {
  throw "Nacos service.http.port must be in (0,65535]: $ProdAppPort"
}
if ([int]$TestAppPort -le 0 -or [int]$TestAppPort -gt 65535) {
  throw "Nacos service.http.port_test must be in (0,65535]: $TestAppPort"
}
if ([int]$ProdAppPort -eq [int]$TestAppPort) {
  throw "Nacos production and test HTTP ports must be different."
}
$DatabaseProfile = "test"
$AppPort = $TestAppPort
if ($env:FINANCE_SYS_ENV -ceq "PROD") {
  $DatabaseProfile = "production"
  $AppPort = $ProdAppPort
}
$AppBaseUrl = "http://127.0.0.1:$AppPort"

$TmpDir = Join-Path $RootDir "tmp"
$PidFile = Join-Path $TmpDir "api_nacos.pid"
$ApiExe = Join-Path $TmpDir "api_nacos.exe"
$StdoutLog = Join-Path $TmpDir "api_nacos.out.log"
$StderrLog = Join-Path $TmpDir "api_nacos.err.log"
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

if (Test-Path -LiteralPath $PidFile -PathType Leaf) {
  $oldPidText = (Get-Content -LiteralPath $PidFile -Raw).Trim()
  $oldPid = 0
  if ([int]::TryParse($oldPidText, [ref]$oldPid)) {
    $oldProcess = Get-Process -Id $oldPid -ErrorAction SilentlyContinue
    if ($null -ne $oldProcess) {
      $oldPath = $null
      try { $oldPath = $oldProcess.Path } catch { }
      if (-not [string]::IsNullOrWhiteSpace($oldPath) -and
          [string]::Equals($oldPath, $ApiExe, [StringComparison]::OrdinalIgnoreCase)) {
        Write-Host "[INFO] Stopping previously managed API PID $oldPid"
        Stop-Process -Id $oldPid -Force
        $oldProcess.WaitForExit()
      } else {
        Write-Warning "Ignoring stale PID file; PID $oldPid is not $ApiExe"
      }
    }
  }
  Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
}

$listeners = Get-NetTCPConnection -LocalPort ([int]$AppPort) -State Listen -ErrorAction SilentlyContinue
if ($listeners) {
  $owners = ($listeners | Select-Object -ExpandProperty OwningProcess -Unique) -join ", "
  throw "Port $AppPort is already in use by PID(s): $owners. Stop that process or update service.http.port in Nacos."
}

Write-Host "[INFO] Nacos: $($env:NACOS_SERVER_ADDR) dataId=$NacosDataId group=$NacosGroup namespace=$NacosNamespace"
Write-Host "[INFO] Database profile: $DatabaseProfile (FINANCE_SYS_ENV=$($env:FINANCE_SYS_ENV))"
Write-Host "[INFO] Effective HTTP port: $AppPort"

if ($Mode -eq "debug") {
  Write-Host "[DEBUG] Starting API in the foreground. Press Ctrl+C to stop."
  & go run ./cmd/api
  exit $LASTEXITCODE
}

Write-Host "[INFO] Building API executable"
& go build -o $ApiExe ./cmd/api
if ($LASTEXITCODE -ne 0) {
  exit $LASTEXITCODE
}

Remove-Item -LiteralPath $StdoutLog, $StderrLog -Force -ErrorAction SilentlyContinue
$process = Start-Process -FilePath $ApiExe -WorkingDirectory $RootDir -RedirectStandardOutput $StdoutLog -RedirectStandardError $StderrLog -PassThru
[System.IO.File]::WriteAllText($PidFile, $process.Id.ToString())
Write-Host "[INFO] API PID $($process.Id); logs: $StdoutLog and $StderrLog"

$healthUrl = $AppBaseUrl.TrimEnd("/") + "/healthz"
$uploadUrl = $AppBaseUrl.TrimEnd("/") + "/upload"
$deadline = [DateTime]::UtcNow.AddSeconds(60)
while ([DateTime]::UtcNow -lt $deadline) {
  $process.Refresh()
  if ($process.HasExited) {
    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    throw "API exited with code $($process.ExitCode). See $StderrLog"
  }
  try {
    $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUrl -TimeoutSec 2
    if ($response.StatusCode -eq 200) {
      Write-Host "[INFO] API is healthy: $healthUrl"
      if ($env:OPEN_UPLOAD_PAGE -eq "1") {
        Start-Process $uploadUrl
      }
      exit 0
    }
  } catch {
    Start-Sleep -Milliseconds 500
  }
}

Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
throw "API did not become healthy within 60 seconds. See $StderrLog"
