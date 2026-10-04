#!/usr/bin/env bash
#
# Phase 5 example: the code -> spec half of the loop.
#
# A copy of fixtures/calc becomes a Repository. specd indexes it, a commit adds
# Subtract, and the CodeToSpec change the drift raises is worked off by the
# scripted agent: the spec gains an intent, requirements and interfaces, the
# context document appears in the tree, Drifted goes False, and no SpecToCode
# change is raised. Then the same summarize is run from the CLI, over contexts
# whose intent is empty, to show the other entry point.
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
RESYNC=${RESYNC:-500ms}
WAIT_SECONDS=${WAIT_SECONDS:-60}
SCENARIO=${SCENARIO:-$REPO/examples/phase5/scenario.yaml}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-phase5.XXXXXX")}
export SPECD_CLM_DOC_DIR="${SPECD_CLM_DOC_DIR:-$WORK/clm-docs}"

export KUBECONFIG="$KUBECONFIG_PATH"

SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}
K() { "$KUBECTL" --server="${SERVER}/clusters/${WORKSPACE}" -n "$NAMESPACE" "$@"; }

CONTEXTS="calc cmd-calc"
REPOSITORIES="phase5-example calc"

specd_pid=""
forget() {
  for name in $CONTEXTS; do
    for change in $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}" 2>/dev/null); do
      K delete specchange "$change" >/dev/null 2>&1 || true
    done
    K delete systemcontext "$name" >/dev/null 2>&1 || true
  done
  # A Repository left over from another example would manage the same context
  # names from a different tree. make example-phase2 puts the calc one back.
  for name in $REPOSITORIES phase4-example p5repo; do
    K delete repository "$name" >/dev/null 2>&1 || true
  done
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
  name: phase5-example
  namespace: $NAMESPACE
spec:
  path: $WORK
  branch: main
  verify: ["go", "test", "./..."]
EOF

echo "--- specd, with the scripted agent doing the code -> spec work ---"
"$SPECD" --agent "scripted:$SCENARIO" --resync "$RESYNC" --retry-backoff 1s \
  --max-attempts 3 > "$WORK/specd.log" 2>&1 &
specd_pid=$!

wait_for "the first ingest" \
  '[ -n "$(K get systemcontext calc -o jsonpath="{.status.observed.fingerprint}" 2>/dev/null)" ]'

echo "--- the code moves: Subtract joins the calc package ---"
cat >> "$WORK/calc/calc.go" <<'EOF'

// Subtract returns the difference of two integers.
func Subtract(a, b int) int {
	return a - b
}
EOF
(cd "$WORK" && git add -A \
  && git -c user.email=example@example.com -c user.name=example commit -qm "add Subtract")

wait_for "the CodeToSpec change to succeed" \
  '[ "$(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"calc\")].status.phase}" 2>/dev/null)" = "Succeeded" ]'

echo "--- the spec the agent wrote ---"
K get systemcontext calc -o jsonpath='{.spec.intent}{"\n"}'
K get systemcontext calc -o jsonpath='{range .spec.requirements[*]}  {.level} {.id}: {.text}{"\n"}{end}'
K get systemcontext calc -o jsonpath='{range .spec.interfaces[*]}  {.name} {.signature}{"\n"}{end}'
echo "  origin: $(K get systemcontext calc -o jsonpath="{.metadata.annotations.specs\.publicdomainrelay\.dev/origin}")"
echo "  Drifted: $(K get systemcontext calc -o jsonpath='{.status.conditions[?(@.type=="Drifted")].status}')"
echo "  SpecToCode changes: $(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].metadata.name}" | wc -w)"

echo "--- the context document in the managed tree ---"
sed -n '1,12p' "$SPECD_CLM_DOC_DIR"/*/calc.md
echo "  .specs in the project tree: $([ -e "$WORK/.specs" ] && echo YES || echo no)"

echo "--- the same summarize, from the CLI, over contexts with an empty intent ---"
for name in $CONTEXTS; do
  for change in $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}" 2>/dev/null); do
    K delete specchange "$change" >/dev/null 2>&1 || true
  done
  K delete systemcontext "$name" >/dev/null 2>&1 || true
done
# specctl ingest applies a Repository and waits for Populated; the specd above
# is the one that runs the agent, and it owns the graph endpoint.
"$SPECCTL" ingest --repo "$WORK" --repo-name phase5-example --summarize \
  --agent "scripted:$SCENARIO"

echo "--- specd stops on SIGTERM ---"
kill "$specd_pid"
wait "$specd_pid" 2>/dev/null || true
specd_pid=""
echo "  stopped"
