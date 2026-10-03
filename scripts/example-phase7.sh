#!/usr/bin/env bash
#
# Phase 7 example: one manifest populates an unknown codebase.
#
# A bare git repository is built out of fixtures/greet and fixtures/calc, two
# trees kcp has never seen. `kubectl apply -f examples/populate/repository.yaml`
# is the only thing applied: specd clones the source into its cache, indexes it,
# creates one SystemContext per partition, and works one CodeToSpec change per
# context off with the scripted agent, so status.phase runs
# Cloning -> Indexing -> Populating -> Populated and every context ends with a
# validated spec.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
BIN=${BIN:-$REPO/bin}
SPECD=${SPECD:-$BIN/specd}
KUBECTL=${KUBECTL:-kubectl}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$REPO/.kcp-specd/admin.kubeconfig}
WORKSPACE=${WORKSPACE:-root:specs}
NAMESPACE=${NAMESPACE:-default}
RESYNC=${RESYNC:-500ms}
WAIT_SECONDS=${WAIT_SECONDS:-120}
MANIFEST=${MANIFEST:-$REPO/examples/populate/repository.yaml}
SOURCE=${SOURCE:-unseen}
WORK=${WORK:-$REPO/.kcp-specd/example-phase7}

export KUBECONFIG="$KUBECONFIG_PATH"

SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}
K() { "$KUBECTL" --server="${SERVER}/clusters/${WORKSPACE}" -n "$NAMESPACE" "$@"; }

CONTEXTS="greet greet-format calc-calc calc-cmd-calc"
CHANGES=""

specd_pid=""
forget() {
  for name in $CONTEXTS $CHANGES; do
    for change in $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}" 2>/dev/null); do
      K delete specchange "$change" >/dev/null 2>&1 || true
    done
    K delete systemcontext "$name" >/dev/null 2>&1 || true
  done
  K delete repository "$SOURCE" >/dev/null 2>&1 || true
}
cleanup() {
  if [ -n "$specd_pid" ] && kill -0 "$specd_pid" 2>/dev/null; then
    kill "$specd_pid" 2>/dev/null || true
    wait "$specd_pid" 2>/dev/null || true
  fi
  if [ "${KEEP:-0}" = "1" ]; then
    echo "KEEP=1: leaving the objects in place and the tree at $WORK" >&2
    return
  fi
  forget
}
trap cleanup EXIT

wait_for() {
  local what=$1 predicate=$2
  local tries=0
  until eval "$predicate"; do
    tries=$((tries + 1))
    if [ "$tries" -ge "$((WAIT_SECONDS * 4))" ]; then
      echo "timed out waiting for $what" >&2
      return 1
    fi
    sleep 0.25
  done
  echo "  $what"
}

if [ ! -x "$SPECD" ]; then
  echo "$SPECD is missing; run make build first" >&2
  exit 1
fi

echo "--- an unseen codebase: a bare git repository of two fixtures ---"
rm -rf "$WORK"
mkdir -p "$WORK/tree/greet" "$WORK/tree/calc"
cp -r "$REPO/fixtures/greet/." "$WORK/tree/greet/"
cp -r "$REPO/fixtures/calc/." "$WORK/tree/calc/"
rm -rf "$WORK/tree/calc/.codegraph"
(cd "$WORK/tree" && git init -q -b main && git add -A \
  && git -c user.email=example@example.com -c user.name=example commit -qm fixture)
git clone -q --bare "$WORK/tree" "$WORK/unseen.git"
echo "  source: $WORK/unseen.git ($(git -C "$WORK/tree" rev-parse --short HEAD))"

forget

echo "--- the only manifest applied, examples/populate/repository.yaml ---"
K apply -f "$MANIFEST"

echo "--- specd, with no --agent and no --repo: the manifest says it all ---"
"$SPECD" --resync "$RESYNC" --retry-backoff 1s --max-attempts 3 \
  --cache-dir "$WORK/cache" > "$WORK/specd.log" 2>&1 &
specd_pid=$!

wait_for "the repository to reach Populated" \
  '[ "$(K get repository '"$SOURCE"' -o jsonpath="{.status.phase}" 2>/dev/null)" = "Populated" ]'
K get repository "$SOURCE" -o jsonpath='  phase {.status.phase}{"\n"}  cloned to {.status.resolvedPath}{"\n"}  contexts {.status.contexts.total} total, {.status.contexts.summarized} summarized, {.status.contexts.failed} failed{"\n"}'
echo "  Populated: $(K get repository "$SOURCE" -o jsonpath='{.status.conditions[?(@.type=="Populated")].status}')"

echo "--- every context the partition made, with a validated spec ---"
K get systemcontexts -o custom-columns='NAME:.metadata.name,REQS:.spec.requirements[*].id,SPECVALID:.status.conditions[?(@.type=="SpecValid")].status,DRIFTED:.status.conditions[?(@.type=="Drifted")].status' | grep -E "NAME|greet|calc"

echo "--- one of the specs the agent wrote ---"
K get systemcontext greet -o jsonpath='  intent: {.spec.intent}{"\n"}'
K get systemcontext greet -o jsonpath='{range .spec.requirements[*]}  {.level} {.id}: {.text}{"\n"}{end}'
echo "  origin: $(K get systemcontext greet -o jsonpath="{.metadata.annotations.specs\.publicdomainrelay\.dev/origin}")"

echo "--- the context document the summarize left in the clone ---"
sed -n '1,8p' "$WORK/cache/unseen/.specs/context/greet.md"

echo "--- no command but the apply was needed: $(K get specchanges -o jsonpath='{.items[*].metadata.name}' | wc -w) SpecChange(s), all Succeeded ---"
K get specchanges -o jsonpath='{range .items[*]}  {.metadata.name} {.status.phase}{"\n"}{end}'

echo "--- specd stops on SIGTERM ---"
kill "$specd_pid"
wait "$specd_pid" 2>/dev/null || true
specd_pid=""
echo "  stopped"
