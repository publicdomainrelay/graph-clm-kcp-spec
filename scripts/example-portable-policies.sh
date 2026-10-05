#!/usr/bin/env bash
#
# The three runs docs/examples/portable-policies.md records, on one pack:
#
#   A  atproto-market at its pre-iroh ref plus hono-compute-provider as a
#      member repository: the reach-in lives in the provider, and the
#      cross-repository model sees it.
#   B  hono-compute-provider alone, the same pack, only policies.yaml differs.
#   C  the spec-only greenfield fixture: the pack denies a declared host-to-guest
#      flow before any code exists, and the fixed spec passes.
#
# Every clone is fresh and under WORK; none is edited and none is pushed.
#
# Environment:
#   WORK   working directory for the clones  (default: /home/johnandersen777/policy-g5-work)
#   OUT    directory for the reports         (default: $WORK/portable-policies-reports)
#
# The live half of C is its own test:
#
#   TMPDIR=/home/johnandersen777/e2e-tmp SPECD_REQUIRE_LIVE=1 \
#     go test ./test/e2e -run TestPortablePackGatesGreenfieldSpecs -count=1 -v
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)

WORK=${WORK:-/home/johnandersen777/policy-g5-work}
OUT=${OUT:-$WORK/portable-policies-reports}
ORG=${ORG:-$(cd "$ROOT/.." && pwd)}

MARKET_REF=d20070c
PROVIDER_REF=fb11e74

mkdir -p "$WORK" "$OUT"
cd "$ROOT"
go build -o bin/specctl ./cmd/specctl

clone() {
  local name=$1 ref=$2 dir="$WORK/$1"
  if [ -d "$dir/.git" ]; then
    git -C "$dir" fetch --quiet origin || true
  else
    git clone --quiet "$ORG/$name" "$dir"
  fi
  git -C "$dir" checkout --quiet --force --detach "$ref"
  printf '%s at %s\n' "$name" "$(git -C "$dir" rev-parse --short HEAD)"
}

echo "== clones =="
clone atproto-market "$MARKET_REF"
clone hono-compute-provider "$PROVIDER_REF"
echo

echo "== A: atproto-market + hono-compute-provider as a member =="
BIN=bin/specctl
MARKET="$WORK/atproto-market"
PROVIDER="$WORK/hono-compute-provider"
"$BIN" policy model \
  --repo atproto-market --worktree "$MARKET" \
  --library examples/policies/atproto-market-cross-repo \
  --member hono-compute-provider="$PROVIDER" >"$OUT/a-model.txt"
sed -n '1,12p' "$OUT/a-model.txt"
echo
"$BIN" policy eval \
  --repo atproto-market --worktree "$MARKET" \
  --library examples/policies/atproto-market-cross-repo \
  --member hono-compute-provider="$PROVIDER" | tee "$OUT/a-eval.txt"
echo

echo "== A without the member: the reach-in is invisible =="
"$BIN" policy eval \
  --repo atproto-market --worktree "$MARKET" \
  --library examples/policies/atproto-market | tee "$OUT/a-no-member.txt"
echo

echo "== B: hono-compute-provider alone, the same pack =="
"$BIN" policy eval \
  --repo hono-compute-provider --worktree "$PROVIDER" \
  --library examples/policies/hono-compute-provider | tee "$OUT/b-eval.txt"
echo

echo "== C: the spec-only greenfield repository =="
SPECS="$WORK/greenfield-market"
rm -rf "$SPECS"
mkdir -p "$SPECS"
cp -r "$ROOT/fixtures/greenfield-market/." "$SPECS/"
git -C "$SPECS" init -q -b main
git -C "$SPECS" add -A
git -C "$SPECS" -c user.email=policy@example -c user.name=policy commit -qm fixture
git -C "$SPECS" checkout -q -b open-architecture/greenfield-market
mkdir -p "$SPECS/specs"
cat >"$SPECS/specs/guest.yaml" <<'YAML'
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SystemContext
metadata:
  name: guest
  namespace: default
spec:
  repository: greenfield-market
  upstream: self
  intent: The guest boots from the host's cloud-init and reports its own network information out.
  interactions:
  - id: i.report
    peer: host
    initiator: self
    channel: relay
    carries: [network-info]
    purpose: network-discovery
    level: MUST
YAML
cat >"$SPECS/specs/host.yaml" <<'YAML'
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SystemContext
metadata:
  name: host
  namespace: default
spec:
  repository: greenfield-market
  upstream: self
  intent: The host provisions a guest and reaches in to read its address.
  interactions:
  - id: i.reach-in
    peer: guest
    initiator: self
    channel: relay
    carries: [network-info]
    purpose: network-discovery
    level: MUST
YAML
git -C "$SPECS" add -A
git -C "$SPECS" -c user.email=policy@example -c user.name=policy commit -qm "the host plans the reach-in"

set +e
"$BIN" policy eval --repo greenfield-market --specs-only --path "$SPECS" \
  --library examples/policies/greenfield-market --strict | tee "$OUT/c-denied.txt"
denied=$?
set -e
printf 'spec gate exit: %d\n\n' "$denied"

cat >"$SPECS/specs/host.yaml" <<'YAML'
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: SystemContext
metadata:
  name: host
  namespace: default
spec:
  repository: greenfield-market
  upstream: self
  intent: The host provisions a guest and accepts the report the guest sends it.
  interactions:
  - id: i.report-in
    peer: guest
    initiator: peer
    channel: relay
    carries: [network-info]
    purpose: network-discovery
    level: MUST
YAML
git -C "$SPECS" add -A
git -C "$SPECS" -c user.email=policy@example -c user.name=policy commit -qm "the guest initiates the report"

set +e
"$BIN" policy eval --repo greenfield-market --specs-only --path "$SPECS" \
  --library examples/policies/greenfield-market --strict | tee "$OUT/c-allowed.txt"
allowed=$?
set -e
printf 'spec gate exit: %d\n\n' "$allowed"

if [ "$denied" -ne 1 ] || [ "$allowed" -ne 0 ]; then
  echo "example-portable-policies: want a denied spec (exit 1) and an allowed one (exit 0)" >&2
  exit 1
fi

echo "== the live half of C: deny, realize, audit =="
echo "TMPDIR=\${TMPDIR:-/home/johnandersen777/e2e-tmp} SPECD_REQUIRE_LIVE=1 \\"
echo "  go test ./test/e2e -run TestPortablePackGatesGreenfieldSpecs -count=1 -v"
echo
echo "reports written to $OUT"
