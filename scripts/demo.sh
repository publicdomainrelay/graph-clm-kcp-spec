#!/usr/bin/env bash
#
# The phase 10 demo: the loop the whole project exists for, end to end, against
# one cluster.
#
#   kcp up -> one Repository manifest -> the code spelled out as specs -> a spec
#   edit -> a structured delta -> an agent commit gated by tests -> the
#   effectiveness table -> one scenario driven through the CLM path.
#
# Every phase keeps its own example (make example-phase1 .. example-phase9);
# this target is the shortest path through all of them at once.
#
#   SPECD_AGENT=claude     run the body with the live model instead of the
#                          scripted one
#   SPECD_CLM_PATH=off     skip the closing CLM-path scenario
#   SPECD_CLM_PATH=pi      drive that scenario through the pi extension instead
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
BIN=${BIN:-$REPO/bin}
SPECCTL=${SPECCTL:-$BIN/specctl}
SPECD=${SPECD:-$BIN/specd}
KUBECTL=${KUBECTL:-kubectl}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-$REPO/.kcp-specd/admin.kubeconfig}
WORKSPACE=${WORKSPACE:-root:specs}
NAMESPACE=${NAMESPACE:-default}
RESYNC=${RESYNC:-500ms}
WAIT_SECONDS=${WAIT_SECONDS:-180}
AGENT=${SPECD_AGENT:-scripted}
CLM_PATH=${SPECD_CLM_PATH:-claude-mod}
EVAL_OUT=${SPECD_EVAL_OUT:-}
EVAL_WORKSPACE=${EVAL_WORKSPACE:-root:specs-eval}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-demo.XXXXXX")}
DATE=$(date -u +%Y-%m-%d)

export KUBECONFIG="$KUBECONFIG_PATH"
export SPECD_KUBECONFIG="$KUBECONFIG_PATH"

AGENT_FLAGS=()
if [ "$AGENT" = "claude" ] || [ "$AGENT" = "claude-mod" ] || [ "$AGENT" = "pi" ]; then
  AGENT_KIND="$AGENT"
else
  AGENT_KIND="scripted:$REPO/examples/phase6/scenario.yaml"
fi
if [ "$AGENT_KIND" = "claude-mod" ]; then
  AGENT_FLAGS+=(--clm-mod "$REPO/cc-clm-mod")
fi
if [ "$AGENT_KIND" = "pi" ]; then
  AGENT_FLAGS+=(--pi-extension "$REPO/pi-hydradb-clm")
fi

SERVER=""

K() { "$KUBECTL" --server="${SERVER}/clusters/${WORKSPACE}" -n "$NAMESPACE" "$@"; }

specd_pid=""
CONTEXTS="calc cmd-calc"
REPOSITORIES="demo-example calc"

forget() {
  for name in $CONTEXTS; do
    for change in $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}" 2>/dev/null); do
      K delete specchange "$change" >/dev/null 2>&1 || true
    done
    K delete systemcontext "$name" >/dev/null 2>&1 || true
  done
  for name in $REPOSITORIES phase4-example phase5-example phase6-example phase7-example p5repo; do
    K delete repository "$name" >/dev/null 2>&1 || true
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
  if [ -n "$SERVER" ]; then
    "$KUBECTL" --server="${SERVER}/clusters/${EVAL_WORKSPACE}" -n "$NAMESPACE" delete repository calc greet todo >/dev/null 2>&1 || true
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

for tool in "$SPECD" "$SPECCTL"; do
  if [ ! -x "$tool" ]; then
    echo "$tool is missing; run make build first" >&2
    exit 1
  fi
done

echo "=== kcp up (reused when it is already serving) ==="
"$REPO/deploy/start-kcp.sh"
SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}

echo "=== the spec workspace, and the eval workspace beside it ==="
SPECS_WORKSPACE=specs "$REPO/deploy/install-specs.sh" >/dev/null
SPECS_WORKSPACE=specs-eval WORKSPACE_KUBECONFIG="$REPO/.kcp-specd/specs-eval.kubeconfig" \
  "$REPO/deploy/install-specs.sh" >/dev/null
echo "  ${WORKSPACE} serves the specs API"
echo "  ${EVAL_WORKSPACE} serves it too, so an eval run never fights this one"

echo "=== a working tree to manage ==="
cp -r "$REPO/fixtures/calc/." "$WORK/"
(cd "$WORK" && git init -q -b main && git add -A \
  && git -c user.email=example@example.com -c user.name=example commit -qm fixture)

forget
K apply -f - >/dev/null <<EOF
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Repository
metadata:
  name: demo-example
  namespace: $NAMESPACE
spec:
  path: $WORK
  branch: main
  verify: ["go", "test", "./..."]
  agent:
    kind: $AGENT_KIND
  populate:
    partition: directory
    summarize: true
    agent:
      kind: $AGENT_KIND
EOF

echo "=== specd, one Repository manifest, every context summarized ==="
"$SPECD" --resync "$RESYNC" --retry-backoff 1s --max-attempts 3 "${AGENT_FLAGS[@]}" \
  > "$WORK/specd.log" 2>&1 &
specd_pid=$!
wait_for "the Repository to reach Populated" \
  '[ "$(K get repository demo-example -o jsonpath="{.status.phase}" 2>/dev/null)" = "Populated" ]'
K get repository demo-example -o jsonpath='{.status.contexts}{"\n"}'
echo "--- the spec the code -> spec half wrote ---"
K get systemcontext calc -o jsonpath='{.spec.intent}{"\n"}'
K get systemcontext calc -o jsonpath='{range .spec.requirements[*]}  {.level} {.id}: {.text}{"\n"}{end}'
K get systemcontext calc -o jsonpath='{range .spec.interfaces[*]}  {.name} {.signature}{"\n"}{end}'

echo "=== the spec moves: Subtract, by server side apply ==="
K apply --server-side --field-manager=human -f "$REPO/examples/phase6/spec-edit.yaml" >/dev/null
wait_for "the SpecToCode change to succeed" \
  '[ "$(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].status.phase}" 2>/dev/null)" = "Succeeded" ]'

echo "--- the delta, which is what the agent was told ---"
K get specchange -o jsonpath='{range .items[?(@.spec.direction=="SpecToCode")]}{.spec.delta}{"\n"}{end}' | head -c 400
echo
"$SPECCTL" get specchange

echo "--- the agent commit, and the tests on it ---"
K get specchange -o jsonpath='{range .items[?(@.spec.direction=="SpecToCode")]}branch {.status.branch}{"\n"}commit {.status.commit}{"\n"}verify {.status.verifyExitCode}{"\n"}files {.status.filesTouched}{"\n"}progress {.status.progress}{"\n"}{end}'
git -C "$WORK" log --format='%h %an <%ae> %s' -n 1
grep -n "func Subtract" "$WORK/calc/calc.go"
(cd "$WORK" && go test ./... >/dev/null && echo "  go test ./... passes on main")

echo "--- the reconciled state ---"
echo "  CodeSynced: $(K get systemcontext calc -o jsonpath='{.status.conditions[?(@.type=="CodeSynced")].status}')"
echo "  Drifted:    $(K get systemcontext calc -o jsonpath='{.status.conditions[?(@.type=="Drifted")].status}')"

echo "=== specd stops on SIGTERM ==="
kill "$specd_pid"
wait "$specd_pid" 2>/dev/null || true
specd_pid=""
echo "  stopped"

echo
echo "=== the effectiveness table, scripted baseline, every fixture ==="
OUT_ARGS=""
if [ -n "$EVAL_OUT" ]; then
  OUT_ARGS="--out $EVAL_OUT"
fi
# shellcheck disable=SC2086
"$SPECCTL" eval --fixtures "$REPO/fixtures" --workspace "$EVAL_WORKSPACE" $OUT_ARGS

if [ "$CLM_PATH" = "off" ]; then
  echo "(SPECD_CLM_PATH=off: the CLM-path scenario was skipped)"
  exit 0
fi

echo
echo "=== one scenario driven through the CLM path ($CLM_PATH) ==="
CLM_ARGS=(--fixtures "$REPO/fixtures/calc" --scenarios add-subtract --summarize-agent scripted)
if [ "$CLM_PATH" = "pi" ]; then
  CLM_ARGS+=(--agent pi)
else
  CLM_ARGS+=(--agent claude-mod --clm-mod "$REPO/cc-clm-mod")
fi
"$SPECCTL" eval "${CLM_ARGS[@]}" --workspace "$EVAL_WORKSPACE"

echo
echo "the whole loop ran: $DATE"
