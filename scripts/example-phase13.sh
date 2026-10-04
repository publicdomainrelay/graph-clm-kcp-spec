#!/usr/bin/env bash
#
# Clone and go, twice: a repository nobody indexed, its orphan
# open-architecture branch, and a second clone that restores the architecture
# from the remote instead of rebuilding it. Each clone runs its own kcp on
# kernel-assigned ports, side by side, all state outside both trees.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
SPECCTL=${SPECCTL:-$REPO/bin/specctl}
SPECD=${SPECD:-$REPO/bin/specd}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-phase13.XXXXXX")}
export SPECD_STATE_DIR="$WORK/state"
unset SPECD_KUBECONFIG SPECD_CLM_DOC_DIR

cleanup() {
  for clone in "$WORK/a/calc" "$WORK/b/elsewhere"; do
    [ -d "$clone" ] && (cd "$clone" && "$SPECCTL" down >/dev/null 2>&1 || true)
  done
}
trap cleanup EXIT

echo "--- someone else's repository: a bare remote with code and no open-architecture branch ---"
mkdir -p "$WORK/upstream"
cp -r "$REPO/fixtures/calc/." "$WORK/upstream/"
rm -rf "$WORK/upstream/.codegraph"
(cd "$WORK/upstream" && git init -q -b main && git add -A && git -c user.email=u@example.com -c user.name=upstream commit -qm "upstream code")
git clone -q --bare "$WORK/upstream" "$WORK/remote/calc.git"
git -C "$WORK/remote/calc.git" branch --list 'open-architecture/*' | sed 's/^/  /'
echo "  (no branch)"

echo "--- clone A: specctl up indexes it and makes the first orphan commit ---"
git clone -q "$WORK/remote/calc.git" "$WORK/a/calc"
cd "$WORK/a/calc"
"$SPECCTL" up --summarize=false --push --specd "$SPECD" --clm-mod "" --out "$WORK/a.json" | sed 's/^/  /'
for _ in $(seq 1 60); do git -C "$WORK/remote/calc.git" rev-parse -q --verify refs/heads/open-architecture/calc >/dev/null && break; sleep 1; done
"$SPECCTL" status | sed 's/^/  /'
"$SPECCTL" arch outline | sed 's/^/  /'
echo "  git status --porcelain: '$(git status --porcelain)'"
echo "  shares history with main: $(git merge-base main open-architecture/calc >/dev/null 2>&1 && echo YES || echo no)"
git ls-tree -r --name-only open-architecture/calc | sed 's/^/    /'

echo "--- a decision in clone A's kcp lands on the branch and the remote ---"
KUBECONFIG="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["adminKubeconfig"])' "$WORK/a.json")" \
  kubectl --server="$("$SPECCTL" env -o server)" patch systemcontext calc --type merge \
  -p '{"spec":{"intent":"Integer arithmetic for the calc command, decided in clone A."}}' >/dev/null
for _ in $(seq 1 60); do git -C "$WORK/remote/calc.git" show open-architecture/calc:specs/calc.yaml 2>/dev/null | grep -q 'clone A' && break; sleep 1; done
git -C "$WORK/remote/calc.git" log --oneline open-architecture/calc | sed 's/^/  /'

echo "--- clone B, elsewhere: specctl up restores from the remote branch, on its own kcp ---"
git clone -q "$WORK/remote/calc.git" "$WORK/b/elsewhere"
cd "$WORK/b/elsewhere"
"$SPECCTL" up --summarize=false --specd "$SPECD" --clm-mod "" --out "$WORK/b.json" | sed 's/^/  /'
"$SPECCTL" arch outline | sed 's/^/  /'
python3 - "$WORK/a.json" "$WORK/b.json" <<'PY'
import json, sys
a, b = (json.load(open(path)) for path in sys.argv[1:])
print(f"  clone A kcp {a['kcpURL']} kine {a['kinePort']}")
print(f"  clone B kcp {b['kcpURL']} kine {b['kinePort']}")
print(f"  separate instances: {'yes' if a['kcpPort'] != b['kcpPort'] and a['kcpRoot'] != b['kcpRoot'] else 'NO'}")
PY
echo "  git status --porcelain: '$(git status --porcelain)'"
