#!/usr/bin/env bash
#
# Phase 8 example: the mod path, deterministic.
#
# The Claude Code mod has no kube client and no Bolt driver, so it reaches the
# state by running `specctl clm render|apply|report`. This runs exactly those
# three verbs the way the mod does and shows what kcp and the graph see:
#
#   render  prints the context document (prose intent + a fenced spec block +
#           the resolved refs) -- what the mod writes to .specs/context/calc.md
#   apply   the model's spec edit becomes a delta computed in Go, written with
#           origin: clm, and the controller raises ONE SpecToCode change
#   fold    a model that refines the spec while its change is Running does not
#           spawn a second change and does not move the target: the edit lands
#           on the running change's record
#   report  progress grows while the change runs, with the files touched
#
# With LIVE_MODEL=1 the same phase runs for real: specd starts deepseek-claude
# with --plugin-dir cc-clm-mod, and the assertions move to the gated live model
# test (SPECD_REQUIRE_LIVE_MODEL=1).
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
BIN=${BIN:-$REPO/bin}
SPECD=${SPECD:-$BIN/specd}
SPECCTL=${SPECCTL:-$BIN/specctl}
KUBECTL=${KUBECTL:-kubectl}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$REPO/.kcp-specd/admin.kubeconfig}
WORKSPACE=${WORKSPACE:-root:specs}
NAMESPACE=${NAMESPACE:-default}
RESYNC=${RESYNC:-500ms}
WAIT_SECONDS=${WAIT_SECONDS:-60}
LIVE_MODEL=${LIVE_MODEL:-0}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-phase8.XXXXXX")}

export KUBECONFIG="$KUBECONFIG_PATH"

SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}
K() { "$KUBECTL" --server="${SERVER}/clusters/${WORKSPACE}" -n "$NAMESPACE" "$@"; }
# The mod calls specctl with the flags a controller would pass; do the same.
S() { "$SPECCTL" "$@" --kubeconfig "$KUBECONFIG_PATH" --workspace "$WORKSPACE" --namespace "$NAMESPACE"; }

CONTEXTS="calc cmd-calc"
REPOSITORIES="phase8-example calc"

specd_pid=""
forget() {
  for name in $CONTEXTS; do
    for change in $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}" 2>/dev/null); do
      K delete specchange "$change" >/dev/null 2>&1 || true
    done
    K delete systemcontext "$name" >/dev/null 2>&1 || true
  done
  for name in $REPOSITORIES phase8-fold-running; do
    K delete repository "$name" >/dev/null 2>&1 || true
    K delete specchange "$name" >/dev/null 2>&1 || true
  done
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

for tool in "$SPECD" "$SPECCTL"; do
  if [ ! -x "$tool" ]; then
    echo "$tool is missing; run make build first" >&2
    exit 1
  fi
done

echo "--- a working tree to manage ---"
cp -r "$REPO/fixtures/calc/." "$WORK/"
(cd "$WORK" && git init -q -b main && git add -A \
  && git -c user.email=example@example.com -c user.name=example commit -qm fixture)

forget
K apply -f - >/dev/null <<EOF
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Repository
metadata:
  name: phase8-example
  namespace: $NAMESPACE
spec:
  path: $WORK
  branch: main
  verify: ["go", "test", "./..."]
EOF

echo "--- the spec the context starts from, so the code is already CodeSynced ---"
K apply --server-side --field-manager=specd-baseline -f - >/dev/null <<EOF
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SystemContext
metadata:
  name: calc
  namespace: $NAMESPACE
spec:
  repository: phase8-example
  upstream: self
  intent: Arithmetic on two integers.
  interfaces:
    - name: Add
      kind: function
      signature: func Add(a, b int) int
      file: calc/calc.go
    - name: Multiply
      kind: function
      signature: func Multiply(a, b int) int
      file: calc/calc.go
  codeRefs: ["file:calc/calc.go"]
EOF

echo "--- specd, with no --agent: the changes stay Pending for a human ---"
"$SPECD" --resync "$RESYNC" --retry-backoff 1s --max-attempts 3 > "$WORK/specd.log" 2>&1 &
specd_pid=$!

wait_for "the first ingest" \
  '[ -n "$(K get systemcontext calc -o jsonpath="{.status.realizedSpecHash}" 2>/dev/null)" ]'

echo "--- render: the document the mod writes to .specs/context/calc.md ---"
S clm render --context calc > "$WORK/model-zone.md"
head -n 12 "$WORK/model-zone.md"
echo "  ... $(wc -l < "$WORK/model-zone.md") line(s) in all"

echo "--- the model adds one interface to the spec block ---"
python3 "$REPO/scripts/clm-add-interface.py" "$WORK/model-zone.md" Subtract

echo "--- apply: the delta is computed in Go, and kcp is patched with origin: clm ---"
S clm apply --context calc < "$WORK/model-zone.md" > "$WORK/delta.json" 2> "$WORK/apply.log"
head -n 20 "$WORK/delta.json"
cat "$WORK/apply.log"
echo "  origin: $(K get systemcontext calc -o jsonpath='{.metadata.annotations.specs\.publicdomainrelay\.dev/origin}')"
echo "  origin-hash: '$(K get systemcontext calc -o jsonpath='{.metadata.annotations.specs\.publicdomainrelay\.dev/origin-hash}')' (empty: a clm write is an edit)"

echo "--- the controller raises ONE SpecToCode change for it ---"
wait_for "the SpecToCode change" \
  '[ -n "$(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].metadata.name}" 2>/dev/null)" ]'
S get specchange
CHANGE=$(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].metadata.name}" | awk '{print $1}')
echo "  delta: $(K get specchange "$CHANGE" -o jsonpath='{.spec.delta}')"

echo "--- a model that refines the spec while the change is Running ---"
# No agent is configured, so the change is put into Running by hand: that is
# the state the realize reconciler sets before it launches the model.
K patch specchange "$CHANGE" --subresource=status --type=merge -p '{"status":{"phase":"Running"}}' >/dev/null
python3 "$REPO/scripts/clm-add-interface.py" "$WORK/model-zone.md" Subtract Divide

S clm apply --context calc < "$WORK/model-zone.md" > "$WORK/delta2.json" 2> "$WORK/apply2.log"
cat "$WORK/apply2.log"
echo "  spec still declares: $(K get systemcontext calc -o jsonpath='{.spec.interfaces[*].name}')"
echo "  spec changes for calc: $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"calc\")].metadata.name}" | wc -w) (the running one; the edit folded)"

echo "--- report: the file a tool touched, while the change runs ---"
S clm report --change "$CHANGE" --event '{"turn":1,"tool":"Write","files":["calc/calc.go"],"note":"wrote Subtract"}'
echo "  progress: $(K get specchange "$CHANGE" -o jsonpath='{.status.progress}')"

echo "--- what the graph holds ---"
echo "  TOUCHED and OCCURRED edges are written by the report; the live test asserts them:"
echo "    SPECD_REQUIRE_LIVE=1 go test ./test/e2e/ -run TestPhase8TheModPathReportsIntoKcp -count=1"
echo "  the gated model run:"
echo "    SPECD_REQUIRE_LIVE_MODEL=1 go test ./test/e2e/ -run TestPhase8LiveModelRealizesWithTheMod -count=1"

echo "--- specd stops on SIGTERM ---"
kill "$specd_pid"
wait "$specd_pid" 2>/dev/null || true
specd_pid=""
echo "  stopped"
