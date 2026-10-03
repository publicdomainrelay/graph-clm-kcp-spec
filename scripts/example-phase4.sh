#!/usr/bin/env bash
#
# Phase 4 example: run specd against the local kcp and watch it discover drift.
#
# A copy of fixtures/calc becomes a Repository. specd indexes it, then a commit
# that adds Subtract must produce Drifted=True and one CodeToSpec SpecChange,
# and a human edit of the spec must produce one SpecToCode SpecChange.
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
WAIT_SECONDS=${WAIT_SECONDS:-60}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-phase4.XXXXXX")}

export KUBECONFIG="$KUBECONFIG_PATH"

K() { "$KUBECTL" --server="${SERVER}/clusters/${WORKSPACE}" -n "$NAMESPACE" "$@"; }

SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}

specd_pid=""
forget() {
  for name in calc cmd-calc; do
    for change in $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}" 2>/dev/null); do
      K delete specchange "$change" >/dev/null 2>&1 || true
    done
    K delete systemcontext "$name" >/dev/null 2>&1 || true
  done
  # The calc Repository of example-phase2 points at the plain fixtures/calc
  # directory. Left in place it would manage the same context names from a
  # different tree. make example-phase2 puts it back.
  K delete repository phase4-example >/dev/null 2>&1 || true
  K delete repository calc >/dev/null 2>&1 || true
}
cleanup() {
  if [ -n "$specd_pid" ] && kill -0 "$specd_pid" 2>/dev/null; then
    kill "$specd_pid" 2>/dev/null || true
    wait "$specd_pid" 2>/dev/null || true
  fi
  forget
  rm -rf "$WORK"
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

echo "--- a working tree to manage ---"
cp -r "$REPO/fixtures/calc/." "$WORK/"
# A codegraph index left in the fixture by an earlier ingest is not part of the
# working tree, and copying it would index the index.
rm -rf "$WORK/.codegraph"
git -C "$WORK" init -q -b main
git -C "$WORK" add -A
git -C "$WORK" -c user.email=example@example.com -c user.name=example commit -qm fixture
echo "  $WORK"

echo "--- the calc names belong to this example, so it starts from a clean slate ---"
forget
echo "  (run make example-phase2 afterwards to put the example state back)"

echo "--- the Repository points at it, and specd starts ---"
$KUBECTL --server="${SERVER}/clusters/${WORKSPACE}" apply -f - <<YAML
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Repository
metadata:
  name: phase4-example
  namespace: ${NAMESPACE}
spec:
  path: ${WORK}
  branch: main
  verify: ["go", "test", "./..."]
YAML

"$SPECD" --workspace "$WORKSPACE" --namespace "$NAMESPACE" --resync "$RESYNC" >"$WORK/specd.log" 2>&1 &
specd_pid=$!

wait_for "the first ingest filled status.observed" \
  '[ -n "$(K get systemcontext calc -o jsonpath="{.status.observed.fingerprint}" 2>/dev/null)" ]'
K get systemcontexts
printf '  conditions: '
K get systemcontext calc -o jsonpath='{range .status.conditions[*]}{.type}={.status} {end}{"\n"}'

echo "--- a commit that adds Subtract ---"
cat >>"$WORK/calc/calc.go" <<'GO'

// Subtract returns the difference of two integers.
func Subtract(a, b int) int {
	return a - b
}
GO
git -C "$WORK" add -A
git -C "$WORK" -c user.email=example@example.com -c user.name=example commit -qm "add Subtract"

wait_for "the controller raised Drifted for the new commit" \
  '[ "$(K get systemcontext calc -o jsonpath="{.status.conditions[?(@.type==\"Drifted\")].status}")" = "True" ]'
printf '  conditions: '
K get systemcontext calc -o jsonpath='{range .status.conditions[*]}{.type}={.status}/{.reason} {end}{"\n"}'
echo "  the work it queued:"
K get specchanges -o custom-columns=NAME:.metadata.name,DIRECTION:.spec.direction,FROM:.spec.fromCommit,TO:.spec.toCommit,PHASE:.status.phase

echo "--- a human edits the spec ---"
K patch systemcontext calc --type merge -p '{"spec":{"intent":"The calc package, now with subtraction."}}' >/dev/null
wait_for "the controller raised SpecToCode for the edit" \
  '[ -n "$(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].metadata.name}")" ]'
echo "  the work it queued:"
K get specchanges -o custom-columns=NAME:.metadata.name,DIRECTION:.spec.direction,FROM:.spec.fromSpecHash,TO:.spec.toSpecHash,PHASE:.status.phase

echo "--- nothing else moves while the changes wait for an agent ---"
# Only the objects this example owns: another Repository left over from an
# earlier example may be in the workspace, and it is none of this check's
# business.
snapshot() {
  for name in calc cmd-calc; do
    K get systemcontext "$name" -o jsonpath="{.metadata.name}={.metadata.resourceVersion} "
  done
  for name in calc cmd-calc; do
    K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}={.items[?(@.spec.systemContext==\"$name\")].metadata.resourceVersion} "
  done
}
before=$(snapshot)
sleep 4
after=$(snapshot)
if [ "$before" != "$after" ]; then
  echo "the controller is looping:" >&2
  echo "  before $before" >&2
  echo "  after  $after" >&2
  exit 1
fi
echo "  every resourceVersion is unchanged after four seconds"
echo "--- specd stops on SIGTERM ---"
kill "$specd_pid"
wait "$specd_pid"
specd_pid=""
echo "done"
