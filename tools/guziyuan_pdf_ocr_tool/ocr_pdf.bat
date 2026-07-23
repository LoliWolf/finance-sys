@echo off
setlocal
set PYTHONIOENCODING=utf-8

if exist "%~dp0.venv\Scripts\python.exe" (
  set "PYTHON_EXE=%~dp0.venv\Scripts\python.exe"
  goto :python_found
)

if defined PYTHON (
  set "PYTHON_EXE=%PYTHON%"
  goto :python_found
)

where py >nul 2>&1
if not errorlevel 1 (
  set "USE_PY_LAUNCHER=1"
  goto :python_found
)

where python >nul 2>&1
if not errorlevel 1 (
  set "PYTHON_EXE=python"
  goto :python_found
)

echo [ERROR] Python 3 was not found. Set PYTHON or add py/python to PATH. 1>&2
exit /b 127

:python_found
if defined USE_PY_LAUNCHER (
  py -3 -c "from PIL import Image" >nul 2>&1
) else (
  "%PYTHON_EXE%" -c "from PIL import Image" >nul 2>&1
)
if errorlevel 1 (
  echo [ERROR] Pillow is missing from the selected Python interpreter. 1>&2
  echo         Create tools\guziyuan_pdf_ocr_tool\.venv and install requirements.txt, or set PYTHON. 1>&2
  exit /b 126
)

for %%C in (pdftoppm powershell.exe) do (
  where %%C >nul 2>&1
  if errorlevel 1 (
    echo [ERROR] PDF OCR dependency is missing from PATH: %%C 1>&2
    exit /b 127
  )
)

for %%C in (pdftotext pdftohtml pdfimages) do (
  where %%C >nul 2>&1
  if errorlevel 1 echo [WARN] Optional PDF extractor is unavailable; using render fallback: %%C 1>&2
)

if defined USE_PY_LAUNCHER (
  py -3 "%~dp0ocr_pdf_articles.py" %*
) else (
  "%PYTHON_EXE%" "%~dp0ocr_pdf_articles.py" %*
)
exit /b %ERRORLEVEL%
