#!/bin/zsh

if [ -z "${ZSH_VERSION:-}" ]; then
  exec /bin/zsh "$0" "$@"
fi

set -euo pipefail

ROOT_DIR="${0:A:h}"
cd "$ROOT_DIR"
mkdir -p tmp

BOOTSTRAP_FILE="$ROOT_DIR/bootstrap_go122.env"
[[ -f "$BOOTSTRAP_FILE" ]] || BOOTSTRAP_FILE="$ROOT_DIR/bootstrap_go122.env.example"

export NACOS_SERVER_ADDR="$(sed -n 's/^NACOS_SERVER_ADDR=//p' "$BOOTSTRAP_FILE" | tail -n 1)"
export FINANCE_SYS_ENV=PROD
export OPEN_UPLOAD_PAGE=0
export GOTOOLCHAIN=local

for COMMAND_NAME in curl go launchctl lsof nc pgrep screen; do
  if ! command -v "$COMMAND_NAME" >/dev/null 2>&1; then
    echo "[ERROR] Required command not found: $COMMAND_NAME" >&2
    exit 127
  fi
done

NACOS_CONFIG="$(curl -fsS --max-time 10 --get "http://$NACOS_SERVER_ADDR/nacos/v1/cs/configs" \
  --data-urlencode "dataId=expert_trade" \
  --data-urlencode "group=DEFAULT_GROUP" \
  --data-urlencode "tenant=public")"

NACOS_PRIMARY_ADDR="${NACOS_SERVER_ADDR%%,*}"
NACOS_HOST="${NACOS_PRIMARY_ADDR%:*}"
NACOS_HTTP_PORT="${NACOS_PRIMARY_ADDR##*:}"
if [[ "$NACOS_HTTP_PORT" != <-> ]]; then
  echo "[ERROR] Invalid NACOS_SERVER_ADDR: $NACOS_SERVER_ADDR" >&2
  exit 1
fi
NACOS_GRPC_PORT=$((NACOS_HTTP_PORT + 1000))
if ! nc -z -G 3 "$NACOS_HOST" "$NACOS_GRPC_PORT" >/dev/null 2>&1; then
  echo "[ERROR] Nacos SDK gRPC endpoint is unreachable: $NACOS_HOST:$NACOS_GRPC_PORT" >&2
  exit 1
fi
echo "[INFO] Nacos HTTP and SDK gRPC endpoints are reachable: $NACOS_PRIMARY_ADDR, $NACOS_HOST:$NACOS_GRPC_PORT"

if command -v jq >/dev/null 2>&1; then
  AGENT_ENDPOINT="$(printf '%s' "$NACOS_CONFIG" | jq -er '.agent.endpoint')"
  API_PORT="$(printf '%s' "$NACOS_CONFIG" | jq -er '.service.http.port')"
elif command -v python3 >/dev/null 2>&1; then
  RUNTIME_ENDPOINTS="$(printf '%s' "$NACOS_CONFIG" | python3 -c 'import json,sys; c=json.load(sys.stdin); print(c["agent"]["endpoint"], c["service"]["http"]["port"])')"
  AGENT_ENDPOINT="${RUNTIME_ENDPOINTS% *}"
  API_PORT="${RUNTIME_ENDPOINTS##* }"
else
  echo "[ERROR] jq or python3 is required to read runtime endpoints from Nacos." >&2
  exit 127
fi

AGENT_PORT="$(printf '%s' "$AGENT_ENDPOINT" | sed -E 's#^[a-zA-Z]+://[^:/]+:([0-9]+).*$#\1#')"
if [[ "$AGENT_PORT" != <-> ]] || (( AGENT_PORT < 1 || AGENT_PORT > 65535 )); then
  echo "[ERROR] Invalid agent.endpoint in Nacos: $AGENT_ENDPOINT" >&2
  exit 1
fi
if [[ "$API_PORT" != <-> ]] || (( API_PORT < 1 || API_PORT > 65535 )); then
  echo "[ERROR] Invalid service.http.port in Nacos: $API_PORT" >&2
  exit 1
fi

API_SESSION="finance-sys-api-prod"
AGENT_LABEL="com.loliwolf.finance-sys.agent"
AGENT_LOG="$ROOT_DIR/tmp/agent.log"
API_LOG="$ROOT_DIR/tmp/api_nacos.log"
AGENT_PID_FILE="$ROOT_DIR/tmp/agent.pid"
API_PID_FILE="$ROOT_DIR/tmp/api_nacos.pid"
AGENT_HEALTH_URL="${AGENT_ENDPOINT%/v1/resolve-document}/healthz"
API_HEALTH_URL="http://127.0.0.1:$API_PORT/healthz"

stop_listener() {
  local PORT="$1"
  local EXPECTED_COMMAND="$2"
  local LISTENERS PID PROCESS_COMMAND

  LISTENERS="$(lsof -nP -tiTCP:"$PORT" -sTCP:LISTEN 2>/dev/null || true)"
  [[ -n "$LISTENERS" ]] || return 0
  for PID in ${(f)LISTENERS}; do
    PROCESS_COMMAND="$(ps -p "$PID" -o command= 2>/dev/null || true)"
    if [[ "$PROCESS_COMMAND" != *"$EXPECTED_COMMAND"* ]]; then
      echo "[ERROR] Port $PORT is occupied by an unmanaged process: PID=$PID COMMAND=$PROCESS_COMMAND" >&2
      return 1
    fi
    echo "[INFO] Stopping stale process PID $PID on port $PORT"
    kill "$PID"
  done

  for _ in {1..50}; do
    lsof -tiTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1 || return 0
    sleep 0.1
  done
  echo "[ERROR] Port $PORT was not released." >&2
  return 1
}

wait_for_health() {
  local SERVICE_NAME="$1"
  local HEALTH_URL="$2"
  local LOG_FILE="$3"

  for _ in {1..120}; do
    if curl -fsS --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
      echo "[INFO] Service is healthy: $HEALTH_URL"
      return 0
    fi
    sleep 0.5
  done

  echo "[ERROR] $SERVICE_NAME did not become healthy: $HEALTH_URL" >&2
  tail -n 50 "$LOG_FILE" >&2 || true
  return 1
}

wait_for_launchd_health() {
  local LABEL="$1"
  local HEALTH_URL="$2"
  local LOG_FILE="$3"

  for _ in {1..120}; do
    if curl -fsS --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
      echo "[INFO] Service is healthy: $HEALTH_URL"
      return 0
    fi
    if ! launchctl list "$LABEL" >/dev/null 2>&1; then
      echo "[ERROR] launchd job exited before becoming healthy: $LABEL" >&2
      tail -n 50 "$LOG_FILE" >&2 || true
      return 1
    fi
    sleep 0.5
  done

  echo "[ERROR] Service did not become healthy: $HEALTH_URL" >&2
  tail -n 50 "$LOG_FILE" >&2 || true
  return 1
}

echo "[INFO] Building production API executable"
go build -o "$ROOT_DIR/tmp/api_nacos" ./cmd/api

launchctl remove com.loliwolf.finance-sys.api >/dev/null 2>&1 || true
launchctl remove "$AGENT_LABEL" >/dev/null 2>&1 || true
screen -S finance-sys-api-check -X quit >/dev/null 2>&1 || true
screen -S "$API_SESSION" -X quit >/dev/null 2>&1 || true
screen -wipe >/dev/null 2>&1 || true

SUPERVISOR_PIDS="$(pgrep -f "fast_start_prod.sh.*__(agent|api)" 2>/dev/null || true)"
if [[ -n "$SUPERVISOR_PIDS" ]]; then
  for PID in ${(f)SUPERVISOR_PIDS}; do
    PROCESS_COMMAND="$(ps -p "$PID" -o command= 2>/dev/null || true)"
    if [[ "$PROCESS_COMMAND" == *"$ROOT_DIR/fast_start_prod.sh __"* ]]; then
      echo "[INFO] Stopping stale supervisor PID $PID"
      kill "$PID" 2>/dev/null || true
    fi
  done
fi
sleep 0.5

stop_listener "$API_PORT" "api_nacos"
stop_listener "$AGENT_PORT" "app.runner"
rm -f "$AGENT_PID_FILE" "$API_PID_FILE"
: > "$AGENT_LOG"
rm -f "$ROOT_DIR/tmp/screenlog.0" "$API_LOG"
ln -s "$ROOT_DIR/tmp/screenlog.0" "$API_LOG"

launchctl submit -l "$AGENT_LABEL" -o "$AGENT_LOG" -e "$AGENT_LOG" -- \
  /usr/bin/env \
  "NACOS_SERVER_ADDR=$NACOS_SERVER_ADDR" \
  "FINANCE_SYS_ENV=$FINANCE_SYS_ENV" \
  "PYTHONPATH=$ROOT_DIR/agent" \
  "$ROOT_DIR/agent/.venv/bin/python" -m app.runner
if ! wait_for_launchd_health "$AGENT_LABEL" "$AGENT_HEALTH_URL" "$AGENT_LOG"; then
  launchctl remove "$AGENT_LABEL" >/dev/null 2>&1 || true
  exit 1
fi

AGENT_PID="$(lsof -nP -tiTCP:"$AGENT_PORT" -sTCP:LISTEN | head -n 1)"
printf '%s\n' "$AGENT_PID" > "$AGENT_PID_FILE"
echo "[INFO] Agent PID $AGENT_PID; endpoint: $AGENT_ENDPOINT"

(
  cd "$ROOT_DIR/tmp"
  screen -L -dmS "$API_SESSION" /usr/bin/env \
    "NACOS_SERVER_ADDR=$NACOS_SERVER_ADDR" \
    "FINANCE_SYS_ENV=$FINANCE_SYS_ENV" \
    "OPEN_UPLOAD_PAGE=$OPEN_UPLOAD_PAGE" \
    "GOTOOLCHAIN=$GOTOOLCHAIN" \
    "$ROOT_DIR/tmp/api_nacos"
)
if ! wait_for_health "API" "$API_HEALTH_URL" "$API_LOG"; then
  screen -S "$API_SESSION" -X quit >/dev/null 2>&1 || true
  launchctl remove "$AGENT_LABEL" >/dev/null 2>&1 || true
  exit 1
fi

API_PID="$(lsof -nP -tiTCP:"$API_PORT" -sTCP:LISTEN | head -n 1)"
printf '%s\n' "$API_PID" > "$API_PID_FILE"
echo "[INFO] API PID $API_PID; endpoint: http://127.0.0.1:$API_PORT"
echo "[INFO] FinanceSys production Agent is managed by launchd; API is running in a detached user session."
