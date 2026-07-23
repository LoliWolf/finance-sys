#!/bin/sh
set -eu
exec "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/run_api_nacos.sh" start "$@"
