#!/bin/sh
set -eu

mode=start
env_file=

usage() {
  echo "Usage: $0 [start|debug] [--env-file PATH]" >&2
}

if [ "${1:-}" = "start" ] || [ "${1:-}" = "debug" ]; then
  mode=$1
  shift
fi
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      env_file=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$root_dir"

load_nacos_address() {
  file=$1
  [ -f "$file" ] || { echo "[ERROR] Env file not found: $file" >&2; exit 1; }
  cr=$(printf '\r')
  found=0
  while IFS= read -r line || [ -n "$line" ]; do
    line=${line%"$cr"}
    case "$line" in
      ''|'#'*) continue ;;
    esac
    key=${line%%=*}
    [ "$key" != "$line" ] || continue
    value=${line#*=}
    if [ "$key" != NACOS_SERVER_ADDR ]; then
      echo "[ERROR] Only NACOS_SERVER_ADDR is allowed in $file; found: $key" >&2
      exit 1
    fi
    case "$value" in
      \"*\") value=${value#\"}; value=${value%\"} ;;
      \'*\') value=${value#\'}; value=${value%\'} ;;
    esac
    [ -n "$value" ] || { echo "[ERROR] NACOS_SERVER_ADDR is empty in $file" >&2; exit 1; }
    [ "$found" -eq 0 ] || { echo "[ERROR] Duplicate NACOS_SERVER_ADDR in $file" >&2; exit 1; }
    NACOS_SERVER_ADDR=$value
    found=1
  done < "$file"
  [ "$found" -eq 1 ] || { echo "[ERROR] NACOS_SERVER_ADDR is missing from $file" >&2; exit 1; }
  export NACOS_SERVER_ADDR
}

if [ -z "$env_file" ]; then
  if [ -f "$root_dir/bootstrap_go122.env" ]; then
    env_file=$root_dir/bootstrap_go122.env
  else
    env_file=$root_dir/bootstrap_go122.env.example
  fi
elif [ "${env_file#/}" = "$env_file" ]; then
  env_file=$root_dir/$env_file
fi
load_nacos_address "$env_file"

nacos_namespace=public
nacos_group=DEFAULT_GROUP
nacos_data_id=expert_trade
: "${OPEN_UPLOAD_PAGE:=1}"
: "${GOTOOLCHAIN:=local}"
unset NACOS_NAMESPACE NACOS_GROUP NACOS_DATA_ID NACOS_USERNAME NACOS_PASSWORD NACOS_TIMEOUT_MS
unset APP_PORT APP_BASE_URL
export OPEN_UPLOAD_PAGE GOTOOLCHAIN

if [ -z "${NACOS_SERVER_ADDR:-}" ]; then
  echo "[ERROR] NACOS_SERVER_ADDR is required. Copy bootstrap_go122.env.example to bootstrap_go122.env and set a reachable host:port." >&2
  exit 1
fi
if [ -n "${EXTRA_PATH:-}" ]; then
  PATH=$EXTRA_PATH:$PATH
  export PATH
fi
if ! command -v go >/dev/null 2>&1; then
  echo "[ERROR] Go was not found in PATH. Install Go 1.22.x or set EXTRA_PATH." >&2
  exit 127
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "[ERROR] curl was not found in PATH; it is required for Nacos discovery and health checks." >&2
  exit 127
fi

nacos_config=$(curl -fsS --max-time 10 --get "http://$NACOS_SERVER_ADDR/nacos/v1/cs/configs" \
  --data-urlencode "dataId=$nacos_data_id" \
  --data-urlencode "group=$nacos_group" \
  --data-urlencode "tenant=$nacos_namespace") || {
    echo "[ERROR] Unable to read the runtime HTTP port from Nacos at $NACOS_SERVER_ADDR." >&2
    exit 1
  }
if command -v jq >/dev/null 2>&1; then
  APP_PORT=$(printf '%s' "$nacos_config" | jq -er '.service.http.port') || {
    echo "[ERROR] Nacos JSON does not contain service.http.port." >&2
    exit 1
  }
elif command -v python3 >/dev/null 2>&1; then
  APP_PORT=$(printf '%s' "$nacos_config" | python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["http"]["port"])') || {
    echo "[ERROR] Nacos JSON does not contain service.http.port." >&2
    exit 1
  }
elif command -v plutil >/dev/null 2>&1; then
  APP_PORT=$(printf '%s' "$nacos_config" | plutil -extract service.http.port raw -o - -) || {
    echo "[ERROR] Nacos JSON does not contain service.http.port." >&2
    exit 1
  }
else
  echo "[ERROR] jq, python3, or plutil is required to read service.http.port from Nacos." >&2
  exit 127
fi
app_base_url=http://127.0.0.1:$APP_PORT
case "$APP_PORT" in
  ''|*[!0-9]*) echo "[ERROR] APP_PORT must be numeric: $APP_PORT" >&2; exit 2 ;;
esac

tmp_dir=$root_dir/tmp
pid_file=$tmp_dir/api_nacos.pid
api_bin=$tmp_dir/api_nacos
api_log=$tmp_dir/api_nacos.log
mkdir -p "$tmp_dir"

if [ -f "$pid_file" ]; then
  old_pid=$(sed -n '1p' "$pid_file")
  case "$old_pid" in
    ''|*[!0-9]*) ;;
    *)
      if kill -0 "$old_pid" 2>/dev/null; then
        old_executable=
        if [ -L "/proc/$old_pid/exe" ]; then
          old_executable=$(readlink "/proc/$old_pid/exe" 2>/dev/null || true)
        else
          old_executable=$(ps -p "$old_pid" -o comm= 2>/dev/null | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' || true)
        fi
        if [ "$old_executable" = "$api_bin" ]; then
          echo "[INFO] Stopping previously managed API PID $old_pid"
          kill "$old_pid"
          wait_count=0
          while kill -0 "$old_pid" 2>/dev/null && [ "$wait_count" -lt 50 ]; do
            sleep 0.1
            wait_count=$((wait_count + 1))
          done
          if kill -0 "$old_pid" 2>/dev/null; then
            kill -9 "$old_pid"
          fi
        else
          echo "[WARN] Ignoring stale PID file; PID $old_pid executable is not $api_bin" >&2
        fi
      fi
      ;;
  esac
  rm -f "$pid_file"
fi

if command -v lsof >/dev/null 2>&1; then
  listeners=$(lsof -nP -tiTCP:"$APP_PORT" -sTCP:LISTEN 2>/dev/null || true)
  if [ -n "$listeners" ]; then
    echo "[ERROR] Port $APP_PORT is already in use by PID(s): $(printf '%s' "$listeners" | tr '\n' ' ')" >&2
    echo "        Stop that process or update service.http.port in Nacos." >&2
    exit 1
  fi
fi

echo "[INFO] Nacos: $NACOS_SERVER_ADDR dataId=$nacos_data_id group=$nacos_group namespace=$nacos_namespace"

if [ "$mode" = debug ]; then
  echo "[DEBUG] Starting API in the foreground. Press Ctrl+C to stop."
  exec go run ./cmd/api
fi

echo "[INFO] Building API executable"
go build -o "$api_bin" ./cmd/api
: > "$api_log"
nohup "$api_bin" >>"$api_log" 2>&1 </dev/null &
api_pid=$!
printf '%s\n' "$api_pid" > "$pid_file"
echo "[INFO] API PID $api_pid; log: $api_log"

health_url=${app_base_url%/}/healthz
upload_url=${app_base_url%/}/upload
attempt=0
while [ "$attempt" -lt 120 ]; do
  if ! kill -0 "$api_pid" 2>/dev/null; then
    rm -f "$pid_file"
    echo "[ERROR] API exited before becoming healthy. See $api_log" >&2
    tail -n 40 "$api_log" >&2 || true
    exit 1
  fi
  if curl -fsS --max-time 2 "$health_url" >/dev/null 2>&1; then
    echo "[INFO] API is healthy: $health_url"
    if [ "$OPEN_UPLOAD_PAGE" = 1 ] && command -v open >/dev/null 2>&1; then
      open "$upload_url" >/dev/null 2>&1 || true
    fi
    exit 0
  fi
  sleep 0.5
  attempt=$((attempt + 1))
done

kill "$api_pid" 2>/dev/null || true
rm -f "$pid_file"
echo "[ERROR] API did not become healthy within 60 seconds. See $api_log" >&2
tail -n 40 "$api_log" >&2 || true
exit 1
