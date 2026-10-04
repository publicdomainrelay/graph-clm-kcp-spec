#!/usr/bin/env bash
#
# Phase 9 example: multi workspace, and the spec mirror in git.
#
# Two tenant workspaces bind the same APIExport (specs.publicdomainrelay.dev,
# published by root:specs-provider). Each tenant holds its own Repository and its
# own codebase, and one specd started with --mode export reconciles both through
# the APIExport virtual workspace, writing each status back to the logical
# cluster the object came from. Drift in one tenant leaves the other alone.
#
# Every change of a tenant's kcp state lands on the orphan branch
# open-architecture/<repository> of its tree, never in the tree itself. A
# reviewed edit on that branch flows back into kcp with `specctl sync`, and an
# edit of the same key on both sides is reported, not overwritten.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
BIN=${BIN:-$REPO/bin}
SPECD=${SPECD:-$BIN/specd}
SPECCTL=${SPECCTL:-$BIN/specctl}
KUBECTL=${KUBECTL:-kubectl}
source "$REPO/scripts/kcp-endpoint.sh"
specd_resolve_endpoint "$REPO"
NAMESPACE=${NAMESPACE:-default}
WAIT_SECONDS=${WAIT_SECONDS:-180}
TENANT_A=${TENANT_A:-phase9-a}
TENANT_B=${TENANT_B:-phase9-b}
PROVIDER=${PROVIDER:-root:specs-provider}
EXPORT_NAME=${EXPORT_NAME:-specs.publicdomainrelay.dev}
WORK=${WORK:-$ROOT/example-phase9}

export KUBECONFIG="$KUBECONFIG_PATH"

SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}
A() { "$KUBECTL" --server="${SERVER}/clusters/root:${TENANT_A}" -n "$NAMESPACE" "$@"; }
B() { "$KUBECTL" --server="${SERVER}/clusters/root:${TENANT_B}" -n "$NAMESPACE" "$@"; }
P() { "$KUBECTL" --server="${SERVER}/clusters/${PROVIDER}" "$@"; }

for tool in "$SPECD" "$SPECCTL"; do
  if [ ! -x "$tool" ]; then
    echo "$tool is missing; run make build first" >&2
    exit 1
  fi
done

specd_pid=""
cleanup() {
  if [ -n "$specd_pid" ] && kill -0 "$specd_pid" 2>/dev/null; then
    kill "$specd_pid" 2>/dev/null || true
    wait "$specd_pid" 2>/dev/null || true
  fi
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

echo "--- two tenants binding one APIExport ---"
"$REPO/deploy/bind-workspace.sh" "$TENANT_A"
"$REPO/deploy/bind-workspace.sh" "$TENANT_B"
A api-resources --api-group=specs.publicdomainrelay.dev | grep systemcontexts | sed 's/^/  tenant A sees: /'
P get apiexportendpointslices "$EXPORT_NAME" -o jsonpath='  the export serves {.status.endpoints[0].url}{"\n"}'

echo "--- a clean slate in both tenants (the example is repeatable) ---"
for action in A B; do
  for kind in specchanges systemcontexts repositories; do
    "${action}" delete "$kind" --all --ignore-not-found >/dev/null 2>&1 || true
  done
done
echo "  removed every spec object the tenants held"

echo "--- one codebase per tenant ---"
rm -rf "$WORK"
mkdir -p "$WORK"
cp -r "$REPO/fixtures/calc/." "$WORK/calc/"
rm -rf "$WORK/calc/.codegraph"
cp -r "$REPO/fixtures/greet/." "$WORK/greet/"
rm -rf "$WORK/greet/.codegraph"
rm -rf "$WORK/calc/.codegraph" "$WORK/greet/.codegraph"
for tree in calc greet; do
  (cd "$WORK/$tree" && git init -q -b main && git add -A \
    && git -c user.email=example@example.com -c user.name=example commit -qm fixture)
done
echo "  tenant A: $WORK/calc ($(git -C "$WORK/calc" rev-parse --short HEAD))"
echo "  tenant B: $WORK/greet ($(git -C "$WORK/greet" rev-parse --short HEAD))"

A apply -f - >/dev/null <<YAML
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Repository
metadata: {name: phase9-a-repo, namespace: ${NAMESPACE}}
spec: {path: ${WORK}/calc, branch: main, verify: [go, test, ./...]}
YAML
B apply -f - >/dev/null <<YAML
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Repository
metadata: {name: phase9-b-repo, namespace: ${NAMESPACE}}
spec: {path: ${WORK}/greet, branch: main, verify: [deno, test]}
YAML

echo "--- one specd, in export mode: it watches every workspace bound to the export ---"
"$SPECD" --mode export --provider-workspace "$PROVIDER" --export-name "$EXPORT_NAME" \
  --resync 500ms --retry-backoff 1s --max-attempts 3 >"$WORK/specd.log" 2>&1 &
specd_pid=$!

wait_for "tenant A to ingest calc" '[ -n "$(A get systemcontext calc -o jsonpath="{.status.observed.fingerprint}" 2>/dev/null)" ]'
wait_for "tenant B to ingest its module" '[ -n "$(B get systemcontext phase9-b-repo -o jsonpath="{.status.observed.fingerprint}" 2>/dev/null)" ]'

echo "--- the two logical clusters, each with its own specs ---"
A get systemcontexts -o custom-columns='NAME:.metadata.name,FILES:.status.observed.files[*],DRIFTED:.status.conditions[?(@.type=="Drifted")].status' | sed 's/^/  A /'
B get systemcontexts -o custom-columns='NAME:.metadata.name,FILES:.status.observed.files[*],DRIFTED:.status.conditions[?(@.type=="Drifted")].status' | sed 's/^/  B /'
B_FINGERPRINT=$(B get systemcontext phase9-b-repo -o jsonpath='{.status.observed.fingerprint}')

echo "--- tenant A's code moves; tenant B must not notice ---"
cat >>"$WORK/calc/calc/calc.go" <<'GO'

// Subtract returns the difference of two integers.
func Subtract(a, b int) int {
	return a - b
}
GO
(cd "$WORK/calc" && git add -A && git -c user.email=example@example.com -c user.name=example commit -qm "add Subtract")
wait_for "Drifted=True in tenant A" '[ "$(A get systemcontext calc -o jsonpath="{.status.conditions[?(@.type==\"Drifted\")].status}" 2>/dev/null)" = "True" ]'
A get specchanges -o jsonpath='  A change {.items[0].metadata.name} {.items[0].status.phase}{"\n"}'
echo "  B fingerprint unchanged: $([ "$(B get systemcontext phase9-b-repo -o jsonpath='{.status.observed.fingerprint}')" = "$B_FINGERPRINT" ] && echo yes || echo NO)"
echo "  B changes: $(B get specchanges -o jsonpath='{.items[*].metadata.name}') (none expected)"

BRANCH=open-architecture/phase9-a-repo
echo "--- the orphan branch: tenant A's kcp state, one commit per change, never in the tree ---"
wait_for "the $BRANCH branch" 'git -C "$WORK/calc" rev-parse --verify -q "refs/heads/$BRANCH" >/dev/null'
git -C "$WORK/calc" log --oneline "$BRANCH" | head -5 | sed 's/^/  /'
git -C "$WORK/calc" ls-tree -r --name-only "$BRANCH" | sed 's/^/    /'
echo "  shares history with main: $(git -C "$WORK/calc" merge-base main "$BRANCH" >/dev/null 2>&1 && echo YES || echo no)"
echo "  .specs in the project tree: $([ -e "$WORK/calc/.specs" ] && echo YES || echo no)"

edit_branch() {
  local edit="$WORK/oa-edit"
  rm -rf "$edit"
  git -C "$WORK/calc" worktree add -q "$edit" "$BRANCH"
  if grep -q '^  intent:' "$edit/specs/calc.yaml"; then
    sed -i "s/^  intent: .*/  intent: $1/" "$edit/specs/calc.yaml"
  else
    sed -i "/^spec:$/a\\  intent: $1" "$edit/specs/calc.yaml"
  fi
  git -C "$edit" -c user.email=reviewer@example.com -c user.name=reviewer commit -qam "$1"
  git -C "$WORK/calc" worktree remove --force "$edit"
}

echo "--- specd stops on SIGTERM, the tenants stay bound ---"
kill "$specd_pid"
wait "$specd_pid" 2>/dev/null || true
specd_pid=""
echo "  stopped"

echo "--- a reviewed edit on the branch flows into kcp: specctl sync ---"
edit_branch "reviewed on the branch"
"$SPECCTL" sync --repo "$WORK/calc" --repository phase9-a-repo --workspace "root:${TENANT_A}"
A get systemcontext calc -o jsonpath='  kcp intent: {.spec.intent}, origin {.metadata.annotations.specs\.publicdomainrelay\.dev/origin}{"\n"}'

echo "--- both sides moved the same key: the sync reports the conflict and keeps kcp ---"
A patch systemcontext calc --type merge -p '{"spec":{"intent":"changed in kcp"}}' >/dev/null
edit_branch "changed on the branch"
if "$SPECCTL" sync --repo "$WORK/calc" --repository phase9-a-repo --workspace "root:${TENANT_A}" >"$WORK/conflict.txt" 2>&1; then
  echo "  the sync did not report the conflict; that is a bug" >&2
  exit 1
fi
sed 's/^/  /' "$WORK/conflict.txt"
A get systemcontext calc -o jsonpath='  kcp intent kept: {.spec.intent}{"\n"}'
A get repository phase9-a-repo -o jsonpath='  status.openArchitecture.conflicts: {.status.openArchitecture.conflicts}{"\n"}'
if [ "${KEEP:-0}" != "1" ]; then
  for tenant in "$TENANT_A" "$TENANT_B"; do
    "$KUBECTL" delete workspace "$tenant" --wait=false >/dev/null 2>&1 || true
  done
fi
