#!/usr/bin/env bash
#
# Create the provider workspace of the multi workspace mode and publish the
# specs API from it: apply deploy/specs-provider.yaml (the workspace), the
# APIResourceSchemas generated from deploy/crds/, and deploy/specs-apiexport.yaml
# (the APIExport). Then wait until the export has an identity, which is what a
# tenant binding needs.
#
# Idempotent: re-running applies the same objects and rewrites nothing derived.
#
# deploy/bind-workspace.sh <name> is the tenant half; it calls this script.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${ROOT:-$REPO/.kcp-specd}
PROVIDER=${SPECS_PROVIDER:-specs-provider}
EXPORT_NAME=${SPECS_EXPORT:-specs.publicdomainrelay.dev}
KUBECTL=${KUBECTL:-kubectl}
WAIT_SECONDS=${WAIT_SECONDS:-120}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$ROOT/admin.kubeconfig}
DEPLOY_DIR=$(cd "$(dirname "$0")" && pwd)

K() { "$KUBECTL" --kubeconfig="$KUBECONFIG_PATH" "$@"; }

SERVER=$(K config view --minify -o jsonpath='{.clusters[0].cluster.server}')
BASE=${SERVER%%/clusters/*}
PROVIDER_SERVER=${BASE}/clusters/root:${PROVIDER}

W() { "$KUBECTL" --kubeconfig="$KUBECONFIG_PATH" --server="$PROVIDER_SERVER" "$@"; }

tries=0
until K get workspaces >/dev/null 2>&1; do
  tries=$((tries + 1))
  if [ "$tries" -ge "$WAIT_SECONDS" ]; then
    echo "kcp did not accept admin requests within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 1
done

K apply --validate=false -f "$DEPLOY_DIR/specs-provider.yaml" >/dev/null

tries=0
until [ "$(K get workspace "$PROVIDER" -o jsonpath='{.status.phase}' 2>/dev/null)" = "Ready" ]; do
  tries=$((tries + 1))
  if [ "$tries" -ge "$WAIT_SECONDS" ]; then
    echo "workspace root:${PROVIDER} did not reach Ready within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 1
done

# The same cold start race install-specs.sh handles: the workspace can be Ready
# while the apis.kcp.io API in it is still coming up, so reapply until every
# schema lands and the export reports its identity.
deadline=$((SECONDS + WAIT_SECONDS))
while true; do
  applied=true
  for schema in "$DEPLOY_DIR"/apiresourceschemas/*.yaml; do
    W apply --validate=false -f "$schema" >/dev/null 2>&1 || applied=false
  done
  # The policy half of the API is Gatekeeper's: the ConstraintTemplate CRD is
  # vendored, and specd creates the constraint CRD of every template.
  for crd in "$DEPLOY_DIR"/crds/gatekeeper/*.yaml; do
    W apply --validate=false -f "$crd" >/dev/null 2>&1 || applied=false
  done
  if [ "$applied" = true ] && W apply --validate=false -f "$DEPLOY_DIR/specs-apiexport.yaml" >/dev/null 2>&1; then
    # Only the identity is required here. The endpoint slice stays empty until a
    # tenant binds the export, so waiting for it belongs to the binding side
    # (deploy/bind-workspace.sh) and to the watcher (specd --mode export).
    if [ "$(W get apiexport "$EXPORT_NAME" -o jsonpath='{.status.conditions[?(@.type=="IdentityValid")].status}' 2>/dev/null)" = "True" ]; then
      break
    fi
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "the APIExport ${EXPORT_NAME} in root:${PROVIDER} has no identity within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 1
done

echo "provider:   root:${PROVIDER}"
echo "apiexport:  ${EXPORT_NAME}"
W get apiexportendpointslices "$EXPORT_NAME" -o jsonpath='{.status.endpoints[*].url}'
echo
