#!/usr/bin/env bash
#
# Start the local kcp and kine that hold the spec state.
#
# Idempotent: an already ready cluster on the same ports and root is reused.
# State lives in .kcp-specd/ (gitignored, never shared with deno-kcp).
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${ROOT:-$REPO/.kcp-specd}
KINE_ENDPOINT=${KINE_ENDPOINT:-http://127.0.0.1:23797}
KCP_SECURE_PORT=${KCP_SECURE_PORT:-6447}
KCP_BIN=${KCP_BIN:-kcp}
KINE_BIN=${KINE_BIN:-kine}
KUBECTL=${KUBECTL:-kubectl}
KCP_FEATURE_GATES=${KCP_FEATURE_GATES:-WorkspaceMounts=true}
SPECS_WORKSPACE=${SPECS_WORKSPACE:-specs}
SPECS_INSTALL=${SPECS_INSTALL:-1}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$ROOT/admin.kubeconfig}

export KUBECONFIG="$KUBECONFIG_PATH"

mkdir -p "$ROOT"

KINE_HOSTPORT=${KINE_ENDPOINT#http://}

kine_ready() {
  (exec 3<>"/dev/tcp/${KINE_HOSTPORT/:/\/}") 2>/dev/null
}

kcp_ready() {
  [ -f "$ROOT/admin.kubeconfig" ] &&
    curl -sk "https://127.0.0.1:${KCP_SECURE_PORT}/readyz" >/dev/null 2>&1
}

kubeconfig_port() {
  [ -f "$ROOT/admin.kubeconfig" ] || return 1
  sed -n 's|^ *server: https://127\.0\.0\.1:\([0-9]*\).*|\1|p' "$ROOT/admin.kubeconfig" | head -1
}

# A kcp answering on our port is not proof it is the one this kubeconfig
# describes. If a stale cluster held the port on an earlier run, the next run
# lands on a different one and the kubeconfig silently points at the wrong
# server, which surfaces much later as a confusing NotFound. Refuse that case;
# do NOT delete the kubeconfig. deno-kcp deliberately uses different ports
# against this one shared root, so a differing port with nothing listening on
# ours is the normal second run, and kcp overwrites the kubeconfig when it
# starts anyway.
if kc_port=$(kubeconfig_port); then
  if [ "$kc_port" != "$KCP_SECURE_PORT" ] &&
    curl -sk "https://127.0.0.1:${KCP_SECURE_PORT}/readyz" >/dev/null 2>&1; then
    echo "a kcp is answering on ${KCP_SECURE_PORT} but $ROOT/admin.kubeconfig describes port ${kc_port}." >&2
    echo "That is a leftover cluster from an earlier run. Run deploy/stop-kcp.sh, then retry." >&2
    exit 1
  fi
elif curl -sk "https://127.0.0.1:${KCP_SECURE_PORT}/readyz" >/dev/null 2>&1; then
  echo "a kcp is answering on ${KCP_SECURE_PORT} but $ROOT has no admin.kubeconfig." >&2
  echo "That cluster outlived its state directory. Run deploy/stop-kcp.sh, then retry." >&2
  exit 1
fi

if ! kine_ready; then
  "$KINE_BIN" \
    --endpoint "sqlite://$ROOT/kine.db" \
    --listen-address "${KINE_ENDPOINT#http://}" \
    --metrics-bind-address=0 \
    >"$ROOT/kine.log" 2>&1 &
  echo "$!" >"$ROOT/kine.pid"
fi

for _ in $(seq 1 60); do kine_ready && break; sleep 0.5; done
if ! kine_ready; then
  echo "kine did not become ready; see $ROOT/kine.log" >&2
  exit 1
fi

if ! kcp_ready; then
  "$KCP_BIN" start \
    --root-directory="$ROOT" \
    --etcd-servers="$KINE_ENDPOINT" \
    --bind-address=127.0.0.1 \
    --secure-port="$KCP_SECURE_PORT" \
    --feature-gates="$KCP_FEATURE_GATES" \
    >"$ROOT/kcp.log" 2>&1 &
  echo "$!" >"$ROOT/kcp.pid"
fi

for _ in $(seq 1 180); do kcp_ready && break; sleep 0.5; done
if ! kcp_ready; then
  echo "kcp did not become ready; see $ROOT/kcp.log" >&2
  exit 1
fi

echo "kcp ready"
echo "kubeconfig: $ROOT/admin.kubeconfig"

if [ "$SPECS_INSTALL" != "1" ]; then
  exit 0
fi

exec "$REPO/deploy/install-specs.sh"
