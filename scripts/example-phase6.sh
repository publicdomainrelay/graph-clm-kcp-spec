#!/usr/bin/env bash
#
# Phase 6 example: the spec -> code half of the loop, driven by a structured
# delta.
#
# A copy of fixtures/calc becomes a Repository that names its own scripted
# agent. specd indexes it, a server side apply adds one interface and one
# requirement to the calc context (so the keyed lists are edited without
# rewriting the others), the controller raises one SpecToCode change carrying a
# two entry delta, the agent edits a worktree on spec/calc/<hash8>, `go test
# ./...` gates the commit, the commit is fast-forwarded onto main, the tree is
# re-ingested and the context is CodeSynced with Drifted False.
#
# With FAILING=1 the same run uses a scenario whose Subtract cannot pass, so the
# change ends Failed with its branch kept.
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
FAILING=${FAILING:-0}
SCENARIO=${SCENARIO:-$REPO/examples/phase6/scenario.yaml}
SPEC_EDIT=${SPEC_EDIT:-$REPO/examples/phase6/spec-edit.yaml}
BASELINE=${BASELINE:-$REPO/examples/phase6/baseline.yaml}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-phase6.XXXXXX")}

if [ "$FAILING" = "1" ]; then
  SCENARIO="$REPO/examples/phase6/scenario-failing.yaml"
fi

export KUBECONFIG="$KUBECONFIG_PATH"

SERVER=$("$KUBECTL" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
SERVER=${SERVER%%/clusters/*}
K() { "$KUBECTL" --server="${SERVER}/clusters/${WORKSPACE}" -n "$NAMESPACE" "$@"; }

CONTEXTS="calc cmd-calc"
REPOSITORIES="phase6-example calc"

specd_pid=""
forget() {
  for name in $CONTEXTS; do
    for change in $(K get specchanges -o jsonpath="{.items[?(@.spec.systemContext==\"$name\")].metadata.name}" 2>/dev/null); do
      K delete specchange "$change" >/dev/null 2>&1 || true
    done
    K delete systemcontext "$name" >/dev/null 2>&1 || true
  done
  for name in $REPOSITORIES phase4-example phase5-example p5repo; do
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
  name: phase6-example
  namespace: $NAMESPACE
spec:
  path: $WORK
  branch: main
  verify: ["go", "test", "./..."]
  agent:
    kind: scripted:$SCENARIO
EOF

echo "--- the spec the context starts from, so the code is already CodeSynced ---"
# Server side apply from the start, so the edit below is a second manager
# adding entries rather than a migration of a client side apply.
K apply --server-side --field-manager=specd-baseline -f "$BASELINE" >/dev/null

echo "--- specd, with no --agent: the Repository names the scripted agent ---"
"$SPECD" --resync "$RESYNC" --retry-backoff 1s --max-attempts "${MAX_ATTEMPTS:-3}" \
  > "$WORK/specd.log" 2>&1 &
specd_pid=$!

wait_for "the first ingest" \
  '[ -n "$(K get systemcontext calc -o jsonpath="{.status.observed.fingerprint}" 2>/dev/null)" ]'

echo "--- the spec moves: one interface and one requirement, by server side apply ---"
K apply --server-side --field-manager=human -f "$SPEC_EDIT" >/dev/null
wait_for "the SpecToCode change to appear" \
  '[ -n "$(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].metadata.name}" 2>/dev/null)" ]'

echo "--- the delta the change carries ---"
K get specchanges -o jsonpath='{range .items[?(@.spec.direction=="SpecToCode")]}{.metadata.name}{"\n"}{end}'
K get specchange -o jsonpath='{range .items[?(@.spec.direction=="SpecToCode")]}{.spec.delta}{"\n"}{end}' | head -c 400
echo
"$SPECCTL" get specchange

if [ "$FAILING" = "1" ]; then
  wait_for "the SpecToCode change to fail" \
    '[ "$(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].status.phase}" 2>/dev/null)" = "Failed" ]'
  echo "--- the failure ---"
  K get specchange -o jsonpath='{range .items[?(@.spec.direction=="SpecToCode")]}{.status.message}{"\n"}{end}'
  echo "  branch kept: $(git -C "$WORK" branch --list 'spec/*' | tr -d ' ')"
  echo "  spec untouched: $(K get systemcontext calc -o jsonpath='{.status.realizedSpecHash}' | cut -c1-12)"
  echo "  main still at: $(git -C "$WORK" rev-parse --short HEAD)"
  echo "  nothing of the attempt reached main: $(git -C "$WORK" status --porcelain --untracked-files=no | wc -l) tracked file(s) modified"
  exit 0
fi

wait_for "the SpecToCode change to succeed" \
  '[ "$(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].status.phase}" 2>/dev/null)" = "Succeeded" ]'

echo "--- what the change reports ---"
K get specchange -o jsonpath='{range .items[?(@.spec.direction=="SpecToCode")]}branch {.status.branch}{"\n"}commit {.status.commit}{"\n"}verify {.status.verifyExitCode}{"\n"}files {.status.filesTouched}{"\n"}{end}'

echo "--- the commit the agent made, on the managed branch ---"
git -C "$WORK" log --format='%h %an <%ae> %s' -n 1
git -C "$WORK" show --stat --format= -n 1 | head -5

echo "--- the code really has it ---"
grep -n "func Subtract" "$WORK/calc/calc.go"
(cd "$WORK" && go test ./... >/dev/null && echo "  go test ./... passes on main")

echo "--- the reconciled state ---"
echo "  CodeSynced: $(K get systemcontext calc -o jsonpath='{.status.conditions[?(@.type=="CodeSynced")].status}')"
echo "  Drifted:    $(K get systemcontext calc -o jsonpath='{.status.conditions[?(@.type=="Drifted")].status}')"
echo "  realizedSpecHash is the spec: $(K get systemcontext calc -o jsonpath='{.status.realizedSpecHash}' | cut -c1-12)"
echo "  spec changes raised since: $(K get specchanges -o jsonpath="{.items[?(@.spec.direction==\"SpecToCode\")].metadata.name}" | wc -w)"

echo "--- specd stops on SIGTERM ---"
kill "$specd_pid"
wait "$specd_pid" 2>/dev/null || true
specd_pid=""
echo "  stopped"
