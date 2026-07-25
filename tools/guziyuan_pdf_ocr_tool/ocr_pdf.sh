#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
export PYTHONIOENCODING=utf-8

python_cmd=
if [ -x "$script_dir/.venv/bin/python3" ]; then
  python_cmd=$script_dir/.venv/bin/python3
elif [ -x "$script_dir/.venv/bin/python" ]; then
  python_cmd=$script_dir/.venv/bin/python
elif [ -n "${PYTHON:-}" ]; then
  python_cmd=$PYTHON
else
  if command -v python3 >/dev/null 2>&1; then
    python_cmd=python3
  elif command -v python >/dev/null 2>&1; then
    python_cmd=python
  fi
fi
if [ -z "$python_cmd" ]; then
  echo "[ERROR] Python 3 was not found. Create the tool .venv, set PYTHON, or add python3 to PATH." >&2
  exit 127
fi
if ! "$python_cmd" -c 'from PIL import Image' >/dev/null 2>&1; then
  echo "[ERROR] Pillow is missing from the selected interpreter: $python_cmd" >&2
  echo "        Install requirements.txt into tools/guziyuan_pdf_ocr_tool/.venv." >&2
  exit 126
fi

system_name=$(uname -s)
if [ "$system_name" = Darwin ]; then
  if ! command -v swift >/dev/null 2>&1; then
    echo "[ERROR] Swift is required for macOS PDFKit and Vision OCR." >&2
    exit 127
  fi
elif ! command -v pdftoppm >/dev/null 2>&1; then
  echo "[ERROR] PDF OCR dependency is missing from PATH: pdftoppm" >&2
  exit 127
fi

for optional_command in pdftotext pdftohtml pdfimages; do
  if ! command -v "$optional_command" >/dev/null 2>&1; then
    echo "[WARN] Optional PDF extractor is unavailable: $optional_command" >&2
  fi
done

exec "$python_cmd" "$script_dir/ocr_pdf_articles.py" "$@"
