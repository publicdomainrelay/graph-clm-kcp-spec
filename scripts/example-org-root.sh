#!/usr/bin/env bash
#
# The run docs/examples/org-root.md records: specctl org on a recursive clone of
# the real org root, publicdomainrelay/socialweb-computer.
#
#   1  a shallow recursive clone (the root and every submodule at depth 1)
#   2  the members, what is wrong with the pointers, the manifest
#   3  org fetch: each member's architecture and policy branches, by name
#   4  one member's real architecture read in place at the pinned commit
#   5  how the root's pointers moved (the root is deepened to --depth HISTORY)
#
# Nothing is edited and nothing is pushed.
#
# Environment:
#   URL      org root clone url   (default: https://github.com/publicdomainrelay/socialweb-computer)
#   WORK     working directory    (default: a new temp dir)
#   MEMBER   member to read       (default: atproto-market)
#   HISTORY  root depth for step 5 (default: 200)
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)

URL=${URL:-https://github.com/publicdomainrelay/socialweb-computer}
WORK=${WORK:-$(mktemp -d)}
MEMBER=${MEMBER:-atproto-market}
HISTORY=${HISTORY:-200}

cd "$ROOT"
go build -o bin/specctl ./cmd/specctl
SPECCTL="$ROOT/bin/specctl"

echo "== 1. clone"
GIT_LFS_SKIP_SMUDGE=1 "$SPECCTL" org clone --depth 1 "$URL" "$WORK/org-root"
cd "$WORK/org-root"

echo "== 2. members, status, manifest"
"$SPECCTL" org ls
"$SPECCTL" org status || true
"$SPECCTL" org manifest | sed -n 1,40p

echo "== 3. fetch the members' orphan branches"
"$SPECCTL" org fetch | grep -v ': 0 branch' || true
"$SPECCTL" org ls

echo "== 4. $MEMBER's architecture, read in place"
"$SPECCTL" org outline --member "$MEMBER" | sed -n 1,20p

echo "== 5. how the pointers moved"
git fetch -q --depth="$HISTORY" origin
git -C "$MEMBER" fetch -q --depth=300 origin || true
"$SPECCTL" org history --member "$MEMBER" -n 3

echo "== the brief an agent gets at session start"
"$SPECCTL" org brief | sed -n 1,40p
