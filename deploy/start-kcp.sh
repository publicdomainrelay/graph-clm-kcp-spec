#!/usr/bin/env bash
#
# Start the local kcp and kine that hold the spec state.
#
# Kernel-assigned ports by default: KCP_SECURE_PORT=0 and a KINE_ENDPOINT whose
# port is 0 both mean "let the kernel choose", and the bound ports are written
# to <root>/endpoint.json. Two instances therefore never collide. Setting a
# fixed KCP_SECURE_PORT or a KINE_ENDPOINT with a real port still works and
# pins that port, which is what an operator with firewall rules wants.
#
# The process work lives in `specctl kcp start` (impl/kcpproc); this script
# only resolves the environment and installs the specs API. Idempotent: an
# already ready kcp for the same root is reused.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${ROOT:-$REPO/.kcp-specd}
SPECCTL=${SPECCTL:-$REPO/bin/specctl}
KCP_SECURE_PORT=${KCP_SECURE_PORT:-0}
KINE_ENDPOINT=${KINE_ENDPOINT:-http://127.0.0.1:0}
SPECS_WORKSPACE=${SPECS_WORKSPACE:-specs}
SPECS_NAMESPACE=${SPECS_NAMESPACE:-default}
SPECS_INSTALL=${SPECS_INSTALL:-1}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$ROOT/admin.kubeconfig}
WORKSPACE_KUBECONFIG=${WORKSPACE_KUBECONFIG:-$ROOT/${SPECS_WORKSPACE}.kubeconfig}

KINE_PORT=${KINE_ENDPOINT##*:}
case "$KINE_PORT" in
  '' | *[!0-9]*) KINE_PORT=0 ;;
esac

if [ ! -x "$SPECCTL" ]; then
  echo "building specctl" >&2
  (cd "$REPO" && go build -o "$SPECCTL" ./cmd/specctl)
fi

"$SPECCTL" kcp start --root "$ROOT" --port "$KCP_SECURE_PORT" --kine-port "$KINE_PORT"

export KUBECONFIG="$KUBECONFIG_PATH"

if [ "$SPECS_INSTALL" != "1" ]; then
  exit 0
fi

exec "$REPO/deploy/install-specs.sh"
