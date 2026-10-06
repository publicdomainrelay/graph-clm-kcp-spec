#!/usr/bin/env bash
#
# A change across repositories, from the org root, with a stub agent, on the
# fixture polyrepo (fixtures/orgroot). Offline: no kcp, no model.
#
#   1  build the polyrepo: bare remotes, orphan architecture and policy branches
#      on the members, a superproject that pins them as submodules, some history
#   2  a recursive clone of the root, the way an agent dispatched there sees it
#   3  org brief / ls / status
#   4  org run: the stub agent edits market and provider, each in its own repo,
#      and the root records both pointers in one commit
#   5  the root history and the member specs, read across the repositories
#
# Environment:
#   WORK   working directory (default: a new temp dir; the polyrepo's submodule
#          urls name it, so it must stay where it is built)
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)
WORK=${WORK:-$(mktemp -d)}

cd "$ROOT"
go build -o bin/specctl ./cmd/specctl
SPECCTL="$ROOT/bin/specctl"

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_COUNT=2
export GIT_CONFIG_KEY_0=protocol.file.allow GIT_CONFIG_VALUE_0=always
export GIT_CONFIG_KEY_1=init.defaultBranch GIT_CONFIG_VALUE_1=main
export GIT_AUTHOR_NAME=Agent GIT_AUTHOR_EMAIL=agent@example.com
export GIT_COMMITTER_NAME=Agent GIT_COMMITTER_EMAIL=agent@example.com

echo "== 1. the polyrepo"
ORGFIXTURE_OUT="$WORK/polyrepo" go test ./test/orgfixture -run TestMaterialize -count=1 >/dev/null
ls "$WORK/polyrepo/remotes"

echo "== 2. a recursive clone of the org root"
"$SPECCTL" org clone "file://$WORK/polyrepo/remotes/socialweb-computer.git" "$WORK/socialweb-computer"
cd "$WORK/socialweb-computer"

echo "== 3. what the agent is told, and the state it starts from"
"$SPECCTL" org brief | sed -n 1,16p
"$SPECCTL" org status

echo "== 4. one change across two repositories"
"$SPECCTL" org run --plan "$ROOT/examples/org-root/plan.yaml" --agent "scripted:$ROOT/examples/org-root/scenario.yaml" --push
git log -1 --format=%B

echo "== 5. history and specs across the repositories"
"$SPECCTL" org history -n 4
"$SPECCTL" org outline --member market
"$SPECCTL" org status
