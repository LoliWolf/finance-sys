@echo off
setlocal
set PYTHONIOENCODING=utf-8
python "%~dp0ocr_pdf_articles.py" %*
exit /b %ERRORLEVEL%
