#!/usr/bin/env bash
#
# Phase 2 example: code becomes facts in status and in the graph.
#
# `specctl ingest` is a thin wrapper now: it applies a Repository manifest and
# waits for the controller to reach Populated, so specd has to be running for
# it. The controller indexes a copy of fixtures/calc with codegraph, partitions it into
# one SystemContext per directory, fills status.observed and the conditions, and
# rewrites the graph; the callback then shows one hop of the graph in ArcadeDB,
# the default backend, and in HydraDB, the option.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
BIN=${BIN:-$REPO/bin}
SPECD=${SPECD:-$BIN/specd}
SPECCTL=${SPECCTL:-$BIN/specctl}
KUBECTL=${KUBECTL:-kubectl}
source "$REPO/scripts/kcp-endpoint.sh"
specd_resolve_endpoint "$REPO"
WORKSPACE=${WORKSPACE:-root:specs}
NAMESPACE=${NAMESPACE:-default}
RESYNC=${RESYNC:-1s}
HYDRA=${HYDRA:-1}

export KUBECONFIG="$KUBECONFIG_PATH"

SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}
K() { "$KUBECTL" --server="${SERVER}/clusters/${WORKSPACE}" -n "$NAMESPACE" "$@"; }

specd_pid=""
cleanup() {
  if [ -n "$specd_pid" ] && kill -0 "$specd_pid" 2>/dev/null; then
    kill "$specd_pid" 2>/dev/null || true
    wait "$specd_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

for tool in "$SPECD" "$SPECCTL"; do
  if [ ! -x "$tool" ]; then
    echo "$tool is missing; run make build first" >&2
    exit 1
  fi
done

echo "--- the example contexts, as YAML, through kcp ---"
"$SPECCTL" apply -f "$REPO/examples/calc/specs.yaml"

echo "--- specd, the controller that owns the index ---"
"$SPECD" --resync "$RESYNC" --retry-backoff 1s > "$ROOT/example-phase2.specd.log" 2>&1 &
specd_pid=$!

echo "--- one manifest: specctl applies a Repository and waits for Populated ---"
CALC_TREE=$(mktemp -d "${TMPDIR:-/tmp}/specd-phase2-calc.XXXXXX")
cp -r "$REPO/fixtures/calc/." "$CALC_TREE/"
rm -rf "$CALC_TREE/.codegraph"
(cd "$CALC_TREE" && git init -q -b main && git add -A && git -c user.email=example@example.com -c user.name=example commit -qm fixture)
"$SPECCTL" ingest --repo "$CALC_TREE" --repo-name calc

echo "--- the same objects through kubectl, on the workspace kubeconfig ---"
K get systemcontexts
echo "--- the calculated fingerprint and the conditions ---"
K get systemcontext calc \
  -o jsonpath='{.status.observed.fingerprint}{"\n"}{range .status.conditions[*]}{.type}={.status} {end}{"\n"}'

echo "--- one hop of the graph around calc in ArcadeDB, the default backend ---"
"$SPECCTL" graph neighbors calc

if [ "$HYDRA" = "1" ]; then
  echo "--- the same neighborhood in HydraDB, rebuilt from kcp and codegraph ---"
  "$SPECCTL" graph rebuild --bolt-backend hydradb
  "$SPECCTL" graph neighbors calc --bolt-backend hydradb
fi

echo "--- specd stops on SIGTERM ---"
kill "$specd_pid"
wait "$specd_pid" 2>/dev/null || true
specd_pid=""
echo "  stopped"
