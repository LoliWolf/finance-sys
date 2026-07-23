$ErrorActionPreference = "Stop"
$env:PYTHONIOENCODING = "utf-8"
$script = Join-Path $PSScriptRoot "ocr_pdf_articles.py"
$pythonArgs = @()
if (Test-Path -LiteralPath (Join-Path $PSScriptRoot ".venv\Scripts\python.exe") -PathType Leaf) {
  $python = Join-Path $PSScriptRoot ".venv\Scripts\python.exe"
} elseif (Test-Path -LiteralPath (Join-Path $PSScriptRoot ".venv\bin\python3") -PathType Leaf) {
  $python = Join-Path $PSScriptRoot ".venv\bin\python3"
} elseif (-not [string]::IsNullOrWhiteSpace($env:PYTHON)) {
  $python = $env:PYTHON
} else {
  foreach ($name in @("python3", "python", "py")) {
    $command = Get-Command $name -ErrorAction SilentlyContinue
    if ($null -ne $command) {
      $python = $command.Source
      if ($name -eq "py") {
        $pythonArgs = @("-3")
      }
      break
    }
  }
}
if ([string]::IsNullOrWhiteSpace($python)) {
  throw "Python 3 was not found. Set PYTHON or create the OCR/Agent virtual environment."
}

& $python @pythonArgs -c "from PIL import Image"
if ($LASTEXITCODE -ne 0) {
  throw "Pillow is missing from the selected Python interpreter: $python"
}
foreach ($name in @("pdftoppm")) {
  if (-not (Get-Command $name -ErrorAction SilentlyContinue)) {
    throw "PDF OCR dependency is missing from PATH: $name"
  }
}
foreach ($name in @("pdftotext", "pdftohtml", "pdfimages")) {
  if (-not (Get-Command $name -ErrorAction SilentlyContinue)) {
    Write-Warning "Optional PDF extractor is unavailable; OCR will use page rendering: $name"
  }
}

& $python @pythonArgs $script @args
exit $LASTEXITCODE
