#!/usr/bin/env bash
#
# Evaluate examples/policies/atproto-market against a fresh clone of
# publicdomainrelay/atproto-market at each ref in REFS, writing the text and
# JSON reports to OUT. This is the run docs/examples/atproto-market-policies.md
# records. The clone is never edited and never pushed.
#
# Environment:
#   REPO     git URL to clone                  (default: publicdomainrelay/atproto-market)
#   REFS     refs to evaluate, space separated (default: master pre-iroh spec/iroh-dumbpipe-20261004141803)
#   LIBRARY  policy library directory          (default: <repo>/examples/policies/atproto-market)
#   WORK     working directory for the clone    (default: a fresh mktemp -d)
#   OUT      directory for the reports          (default: a fresh mktemp -d)
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)

REPO=${REPO:-https://github.com/publicdomainrelay/atproto-market.git}
REFS=${REFS:-"master pre-iroh spec/iroh-dumbpipe-20261004141803"}
LIBRARY=${LIBRARY:-$ROOT/examples/policies/atproto-market}
WORK=${WORK:-$(mktemp -d)}
OUT=${OUT:-$(mktemp -d)}
CLONE=$WORK/atproto-market

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
  "$ROOT/bin/specctl" policy eval \
    --repo atproto-market \
    --commit "$commit" \
    --path "$CLONE" \
    --library "$LIBRARY" | tee "$OUT/$name.txt"
  "$ROOT/bin/specctl" policy eval \
    --repo atproto-market \
    --commit "$commit" \
    --path "$CLONE" \
    --library "$LIBRARY" -o json >"$OUT/$name.json"
  counts=$(grep -m1 '^violations:' "$OUT/$name.txt" || true)
  printf '%-36s %-10s %s\n' "$ref" "$commit" "$counts" | tee -a "$summary"
done

echo
echo "reports written to $OUT"
