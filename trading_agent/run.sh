#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

if [ -f "$ROOT_DIR/bootstrap_go122.env" ]; then
  set -a
  . "$ROOT_DIR/bootstrap_go122.env"
  set +a
elif [ -f "$ROOT_DIR/bootstrap_go122.env.example" ]; then
  set -a
  . "$ROOT_DIR/bootstrap_go122.env.example"
  set +a
fi

exec "$ROOT_DIR/trading_agent/.venv/bin/python" -m trading_agent.app.main
