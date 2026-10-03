#!/usr/bin/env bash
#
# Create a tenant workspace and bind the specs API into it:
#
#   deploy/bind-workspace.sh tenant-a
#   deploy/bind-workspace.sh tenant-a --provider root:specs-provider --export specs.publicdomainrelay.dev
#
# The provider workspace and its APIExport are installed first (idempotent), the
# tenant workspace is created at the root, and an APIBinding in the tenant names
# the export. When the binding is Bound the specs API is served in the tenant's
# own logical cluster, and a kubeconfig that points at it is written to
# .kcp-specd/<workspace>.kubeconfig.
#
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <workspace> [--provider root:specs-provider] [--export <name>] [--namespace <ns>]" >&2
  exit 2
fi

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${ROOT:-$REPO/.kcp-specd}
KUBECTL=${KUBECTL:-kubectl}
WAIT_SECONDS=${WAIT_SECONDS:-120}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$ROOT/admin.kubeconfig}
NAMESPACE=${SPECS_NAMESPACE:-default}
PROVIDER_PATH=root:specs-provider
EXPORT_NAME=specs.publicdomainrelay.dev

WORKSPACE=$1
shift
while [ $# -gt 0 ]; do
  case "$1" in
    --provider) PROVIDER_PATH=$2; shift 2 ;;
    --export) EXPORT_NAME=$2; shift 2 ;;
    --namespace) NAMESPACE=$2; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

K() { "$KUBECTL" --kubeconfig="$KUBECONFIG_PATH" "$@"; }

if [ "${SPECS_SKIP_PROVIDER:-0}" != "1" ]; then
  KUBECONFIG_PATH="$KUBECONFIG_PATH" "$REPO/deploy/install-specs-provider.sh" >/dev/null
fi

SERVER=$(K config view --minify -o jsonpath='{.clusters[0].cluster.server}')
BASE=${SERVER%%/clusters/*}
TENANT_SERVER=${BASE}/clusters/root:${WORKSPACE}
TENANT_KUBECONFIG=$ROOT/${WORKSPACE}.kubeconfig

T() { "$KUBECTL" --kubeconfig="$KUBECONFIG_PATH" --server="$TENANT_SERVER" "$@"; }

# A workspace that is still Deleting cannot be recreated, and applying to it
# does not clear the finalizer, so wait it out before creating it again.
tries=0
while [ "$(K get workspace "$WORKSPACE" -o jsonpath='{.status.phase}' 2>/dev/null)" = "Deleting" ]; do
  tries=$((tries + 1))
  if [ "$tries" -ge "$WAIT_SECONDS" ]; then
    echo "workspace root:${WORKSPACE} is still Deleting after ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 1
done

if ! K get workspace "$WORKSPACE" >/dev/null 2>&1; then
  K apply --validate=false -f - <<YAML
apiVersion: tenancy.kcp.io/v1alpha1
kind: Workspace
metadata:
  name: ${WORKSPACE}
spec:
  type:
    name: universal
    path: root
YAML
fi

tries=0
until [ "$(K get workspace "$WORKSPACE" -o jsonpath='{.status.phase}' 2>/dev/null)" = "Ready" ]; do
  tries=$((tries + 1))
  if [ "$tries" -ge "$WAIT_SECONDS" ]; then
    echo "workspace root:${WORKSPACE} did not reach Ready within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 1
done

deadline=$((SECONDS + WAIT_SECONDS))
while true; do
  T apply --validate=false -f - >/dev/null 2>&1 <<YAML || true
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: ${EXPORT_NAME}
spec:
  reference:
    export:
      path: ${PROVIDER_PATH}
      name: ${EXPORT_NAME}
YAML
  phase=$(T get apibinding "$EXPORT_NAME" -o jsonpath='{.status.phase}' 2>/dev/null || true)
  if [ "$phase" = "Bound" ] && T api-resources --api-group=specs.publicdomainrelay.dev 2>/dev/null | grep -q systemcontexts; then
    break
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "the APIBinding in root:${WORKSPACE} is not Bound within ${WAIT_SECONDS}s (phase ${phase:-none})" >&2
    exit 1
  fi
  sleep 1
done

if ! T get namespace "$NAMESPACE" >/dev/null 2>&1; then
  T create namespace "$NAMESPACE" >/dev/null
fi

K config view --raw --minify --flatten -o yaml >"$TENANT_KUBECONFIG"
KUBECONFIG="$TENANT_KUBECONFIG" "$KUBECTL" config set-cluster "$(K config view --minify -o jsonpath='{.clusters[0].name}')" \
  --server="$TENANT_SERVER" >/dev/null
KUBECONFIG="$TENANT_KUBECONFIG" "$KUBECTL" config set-context "$(K config view --minify -o jsonpath='{.contexts[0].name}')" \
  --namespace="$NAMESPACE" >/dev/null

echo "workspace:  root:${WORKSPACE}"
echo "binding:    ${EXPORT_NAME} -> ${PROVIDER_PATH}"
echo "kubeconfig: ${TENANT_KUBECONFIG}"
