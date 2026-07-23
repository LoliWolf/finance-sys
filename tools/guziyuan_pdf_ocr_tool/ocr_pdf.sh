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

missing_commands=
for required_command in pdftoppm; do
  if ! command -v "$required_command" >/dev/null 2>&1; then
    missing_commands="$missing_commands $required_command"
  fi
done
if [ "$(uname -s)" = Darwin ] && ! command -v swift >/dev/null 2>&1; then
  missing_commands="$missing_commands swift"
fi
if [ -n "$missing_commands" ]; then
  echo "[ERROR] PDF OCR dependencies are missing from PATH:$missing_commands" >&2
  echo "        macOS: install Poppler and the Xcode Command Line Tools; do not hard-code a Homebrew prefix." >&2
  exit 127
fi

for optional_command in pdftotext pdftohtml pdfimages; do
  if ! command -v "$optional_command" >/dev/null 2>&1; then
    echo "[WARN] Optional PDF extractor is unavailable; OCR will fall back to page rendering: $optional_command" >&2
  fi
done

exec "$python_cmd" "$script_dir/ocr_pdf_articles.py" "$@"
