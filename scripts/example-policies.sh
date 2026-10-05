#!/usr/bin/env bash
#
# Evaluate a repository's bound policy library against a fresh clone at each
# ref in REFS, writing the text and JSON reports to OUT. With the defaults this
# is the atproto-market run docs/examples/atproto-market-policies.md records.
# The clone is never edited and never pushed.
#
# Environment:
#   REPO      git URL, owner/name, or bare name to clone, e.g. REPO=deno-kcp
#                                             (default: publicdomainrelay/atproto-market)
#   NAME      repository name; derived from REPO when unset
#   REFS      refs to evaluate, space separated
#                                             (default for atproto-market: master pre-iroh spec/iroh-dumbpipe-20261004141803;
#                                              otherwise: main)
#   DIFF_BASE ref the evaluated commit is diffed against, so CodeDiff policies run
#                                             (default: unset)
#   LIBRARY   policy library directory        (default: <repo>/examples/policies/$NAME)
#   WORK      working directory for the clone  (default: a fresh mktemp -d)
#   OUT       directory for the reports        (default: a fresh mktemp -d)
#
# The deno-kcp baseline of pull request #1 is one invocation:
#
#   NAME=deno-kcp REPO=deno-kcp DIFF_BASE=main \
#     REFS=spec/bidder-and-bob-pds scripts/example-policies.sh
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)

REPO=${REPO:-https://github.com/publicdomainrelay/atproto-market.git}
case "$REPO" in
  *://* | *@*) ;;
  *) REPO="https://github.com/publicdomainrelay/${REPO##*/}.git" ;;
esac
NAME=${NAME:-$(basename "$REPO" .git)}
if [ -z "${REFS:-}" ]; then
  if [ "$NAME" = atproto-market ]; then
    REFS="master pre-iroh spec/iroh-dumbpipe-20261004141803"
  else
    REFS="main"
  fi
fi
DIFF_BASE=${DIFF_BASE:-}
LIBRARY=${LIBRARY:-$ROOT/examples/policies/$NAME}
WORK=${WORK:-$(mktemp -d)}
OUT=${OUT:-$(mktemp -d)}
CLONE=$WORK/$NAME

mkdir -p "$WORK" "$OUT"

if [ ! -d "$CLONE/.git" ]; then
  echo "cloning $REPO into $CLONE"
  git clone -q "$REPO" "$CLONE"
fi
git -C "$CLONE" fetch -q --all --tags

if [ ! -x "$ROOT/bin/specctl" ]; then
  echo "building bin/specctl"
  (cd "$ROOT" && go build -o bin/specctl ./cmd/specctl)
fi

resolve() {
  local ref=$1
  if git -C "$CLONE" rev-parse --verify -q "$ref^{commit}" >/dev/null; then
    git -C "$CLONE" rev-parse --short "$ref^{commit}"
    return
  fi
  git -C "$CLONE" rev-parse --short "origin/$ref^{commit}"
}

slug() {
  echo "$1" | tr '/.' '--' | tr -c 'a-zA-Z0-9-' '-'
}

summary="$OUT/summary.txt"
: >"$summary"
printf '%-36s %-10s %s\n' "ref" "commit" "violations" | tee -a "$summary"

for ref in $REFS; do
  commit=$(resolve "$ref")
  name=$(slug "$ref")
  echo
  echo "### $ref ($commit)"
  diff_args=()
  if [ -n "$DIFF_BASE" ]; then
    diff_args=(--diff-base "$DIFF_BASE")
  fi
  "$ROOT/bin/specctl" policy eval \
    --repo "$NAME" \
    --commit "$commit" \
    --path "$CLONE" \
    "${diff_args[@]}" \
    --library "$LIBRARY" | tee "$OUT/$name.txt"
  "$ROOT/bin/specctl" policy eval \
    --repo "$NAME" \
    --commit "$commit" \
    --path "$CLONE" \
    "${diff_args[@]}" \
    --library "$LIBRARY" -o json >"$OUT/$name.json"
  counts=$(grep -m1 '^violations:' "$OUT/$name.txt" || true)
  printf '%-36s %-10s %s\n' "$ref" "$commit" "$counts" | tee -a "$summary"
done

echo
echo "reports written to $OUT"
