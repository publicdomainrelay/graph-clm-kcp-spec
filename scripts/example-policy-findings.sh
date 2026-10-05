#!/usr/bin/env bash
#
# The "find existing violations and decide" workflow of plan 0010 U4, end to
# end. Two decisions are shown:
#
#   1. on fixtures/market-mini/violating -- list the findings, turn the
#      relay-only-ssh violation into a SpecChange request with `policy fix`,
#      waive the host reach-in with `policy waive`, and re-run to see it
#      reported as waived. Fast, no clone, no network.
#   2. with CLONE set to a checkout of a repository -- list the findings
#      against a base ref with `policy eval --inherited` and `policy findings
#      --base`, waive one, and re-run. The clone is never edited and never
#      pushed; the policy library is a copy under WORK.
#
# Environment:
#   CLONE   a code checkout to run the real half against (default: unset, the
#           real half is skipped)
#   BASE    the ref the change is on top of, inside CLONE      (default: HEAD~1)
#   LIBRARY the policy library to bind                     (default: examples/policies/market-mini
#                                                          for the fixture half, examples/policies/atproto-market
#                                                          for the clone half)
#   WORK    scratch directory                     (default: a fresh mktemp -d)
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)
SPECCTL=$ROOT/bin/specctl
WORK=${WORK:-$(mktemp -d)}
FIXTURE=$ROOT/fixtures/market-mini/violating
LIBRARY=${LIBRARY:-$ROOT/examples/policies/market-mini}
DEMO=$WORK/library

mkdir -p "$WORK"

if [ ! -x "$SPECCTL" ]; then
  echo "building bin/specctl"
  (cd "$ROOT" && go build -o bin/specctl ./cmd/specctl)
fi

cp -r "$LIBRARY" "$DEMO"
echo "== the fixture half: $FIXTURE with the library at $DEMO"

echo
echo "-- findings"
"$SPECCTL" policy findings --repo market-mini --worktree "$FIXTURE" --library "$DEMO" -o json > "$WORK/findings.json"
"$SPECCTL" policy findings --repo market-mini --worktree "$FIXTURE" --library "$DEMO" | tail -3

RELAY_KEY=$("$SPECCTL" policy findings --repo market-mini --worktree "$FIXTURE" --library "$DEMO" -o json |
  python3 -c 'import json,sys; print(next(f["key"] for f in json.load(sys.stdin) if f["constraint"] == "relay-only-ssh"))')
REACH_IN_KEY=$("$SPECCTL" policy findings --repo market-mini --worktree "$FIXTURE" --library "$DEMO" -o json |
  python3 -c 'import json,sys; print(next(f["key"] for f in json.load(sys.stdin) if f["constraint"] == "rfp-host-reach-in"))')

echo
echo "-- fix $RELAY_KEY: the SpecChange request the spec flow would run"
"$SPECCTL" policy fix "$RELAY_KEY" --repo market-mini --worktree "$FIXTURE" --library "$DEMO" -o text

echo
echo "-- waive $REACH_IN_KEY"
"$SPECCTL" policy waive "$REACH_IN_KEY" \
  --reason "the target is the demo fixture's own guest, not a real one" \
  --owner demo --repo market-mini --worktree "$FIXTURE" --library "$DEMO" --dir "$DEMO"

echo
echo "-- re-run: the waived finding is reported, not dropped"
"$SPECCTL" policy findings --repo market-mini --worktree "$FIXTURE" --library "$DEMO" | tail -4

if [ -z "${CLONE:-}" ]; then
  echo
  echo "CLONE is unset; the real-clone half is skipped. Set CLONE=<checkout> BASE=<ref> to run it."
  exit 0
fi

BASE=${BASE:-HEAD~1}
REPO_NAME=$(basename "$CLONE")
CLONE_LIBRARY=${CLONE_LIBRARY:-$ROOT/examples/policies/$REPO_NAME}
CLONE_DEMO=$WORK/clone-library
cp -r "$CLONE_LIBRARY" "$CLONE_DEMO"

echo
echo "== the real half: $CLONE against $BASE with the library at $CLONE_DEMO"

echo
echo "-- eval --inherited"
"$SPECCTL" policy eval --repo "$REPO_NAME" --worktree "$CLONE" --diff-base "$BASE" --inherited \
  --library "$CLONE_DEMO"

echo
echo "-- findings --base"
"$SPECCTL" policy findings --repo "$REPO_NAME" --worktree "$CLONE" --base "$BASE" --library "$CLONE_DEMO"

FIRST_KEY=$("$SPECCTL" policy findings --repo "$REPO_NAME" --worktree "$CLONE" --base "$BASE" --library "$CLONE_DEMO" -o json |
  python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["key"])')

echo
echo "-- waive $FIRST_KEY"
"$SPECCTL" policy waive "$FIRST_KEY" --reason "accepted: the address may be a public IPv4 the client can judge" \
  --owner demo --repo "$REPO_NAME" --worktree "$CLONE" --library "$CLONE_DEMO" --dir "$CLONE_DEMO"

echo
echo "-- re-run"
"$SPECCTL" policy findings --repo "$REPO_NAME" --worktree "$CLONE" --base "$BASE" --library "$CLONE_DEMO" | tail -4
