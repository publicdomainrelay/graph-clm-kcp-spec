#!/usr/bin/env bash
#
# Create the specs workspace in the local kcp, apply the spec CRDs into it, and
# write a kubeconfig whose server already points at that workspace.
#
# Idempotent: re-running applies the same objects and rewrites the derived
# kubeconfig from the admin one.
#
# kcp v0.33 accepts CustomResourceDefinitions inside a workspace: applying one
# makes the group served in that logical cluster, with the status subresource
# and printer columns honoured. Multi-workspace (phase 7) switches to
# APIResourceSchema + APIExport + APIBinding so tenant workspaces bind the same
# API; phase 1 keeps the CRDs in the one workspace that owns the spec state.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${ROOT:-$REPO/.kcp-specd}
WORKSPACE=${SPECS_WORKSPACE:-specs}
NAMESPACE=${SPECS_NAMESPACE:-default}
KUBECTL=${KUBECTL:-kubectl}
WAIT_SECONDS=${WAIT_SECONDS:-120}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$ROOT/admin.kubeconfig}
WORKSPACE_KUBECONFIG=${WORKSPACE_KUBECONFIG:-$ROOT/specs.kubeconfig}
DEPLOY_DIR=$(cd "$(dirname "$0")" && pwd)

K() { "$KUBECTL" --kubeconfig="$KUBECONFIG_PATH" "$@"; }

SERVER=$(K config view --minify -o jsonpath='{.clusters[0].cluster.server}')
BASE=${SERVER%%/clusters/*}
WORKSPACE_SERVER=${BASE}/clusters/root:${WORKSPACE}
WORKSPACE_PATH=root:${WORKSPACE}

W() { "$KUBECTL" --kubeconfig="$KUBECONFIG_PATH" --server="$WORKSPACE_SERVER" "$@"; }

tries=0
until K get workspaces >/dev/null 2>&1; do
  tries=$((tries + 1))
  if [ "$tries" -ge "$WAIT_SECONDS" ]; then
    echo "kcp did not accept admin requests within ${WAIT_SECONDS}s" >&2
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
    echo "workspace ${WORKSPACE_PATH} did not reach Ready within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 1
done

for crd in "$DEPLOY_DIR"/crds/*.yaml; do
  W apply --validate=false -f "$crd" >/dev/null
done

tries=0
until W api-resources --api-group=specs.publicdomainrelay.dev 2>/dev/null | grep -q systemcontexts; do
  tries=$((tries + 1))
  if [ "$tries" -ge "$WAIT_SECONDS" ]; then
    echo "the specs API is not served in ${WORKSPACE_PATH} within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 1
done

if ! W get namespace "$NAMESPACE" >/dev/null 2>&1; then
  W create namespace "$NAMESPACE" >/dev/null
fi

K config view --raw --minify --flatten -o yaml >"$WORKSPACE_KUBECONFIG"
KUBECONFIG="$WORKSPACE_KUBECONFIG" "$KUBECTL" config set-cluster "$(K config view --minify -o jsonpath='{.clusters[0].name}')" \
  --server="$WORKSPACE_SERVER" >/dev/null
KUBECONFIG="$WORKSPACE_KUBECONFIG" "$KUBECTL" config set-context "$(K config view --minify -o jsonpath='{.contexts[0].name}')" \
  --namespace="$NAMESPACE" >/dev/null

echo "workspace:  ${WORKSPACE_PATH}"
echo "namespace:  ${NAMESPACE}"
echo "kubeconfig: ${WORKSPACE_KUBECONFIG}"
W api-resources --api-group=specs.publicdomainrelay.dev
