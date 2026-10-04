#!/usr/bin/env bash
#
# Stop the local kcp and kine that deploy/start-kcp.sh started.
#
# The work lives in `specctl kcp stop`: it reads <root>/endpoint.json for the
# pids and, when that file is gone (a root directory can be removed while the
# cluster still runs), falls back to the kcp and kine processes whose command
# line names this root. Every candidate is checked against /proc before it is
# signalled, so a recycled pid cannot be killed by accident. Idempotent:
# nothing to stop is reported, not an error.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${ROOT:-$REPO/.kcp-specd}
SPECCTL=${SPECCTL:-$REPO/bin/specctl}

if [ ! -x "$SPECCTL" ]; then
  echo "building specctl" >&2
  (cd "$REPO" && go build -o "$SPECCTL" ./cmd/specctl)
fi

exec "$SPECCTL" kcp stop --root "$ROOT"
