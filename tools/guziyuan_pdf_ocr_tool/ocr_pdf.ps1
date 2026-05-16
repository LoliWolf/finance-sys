$ErrorActionPreference = "Stop"
$env:PYTHONIOENCODING = "utf-8"
$script = Join-Path $PSScriptRoot "ocr_pdf_articles.py"
python $script @args
exit $LASTEXITCODE
