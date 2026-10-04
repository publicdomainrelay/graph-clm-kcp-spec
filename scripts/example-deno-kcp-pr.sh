#!/usr/bin/env bash
#
# One natural-language request becomes a pull request against a real repository
# this tool did not write: publicdomainrelay/deno-kcp.
#
#   1. clone deno-kcp (and kcp-libs, which its go.mod expects beside it) into a
#      temp dir, and the two services the request is about (atproto-market for
#      the bidder, hono-pds) so the harness can read them;
#   2. specctl up: this clone's own kcp on kernel-assigned ports, CodeGraph
#      index, one SystemContext per directory, a DeepSeek summary of each
#      (FRESH=1, the default, indexes from scratch; FRESH=0 restores from the
#      remote's open-architecture/deno-kcp branch when it exists);
#   3. a headless Claude Code harness with the cc-clm-mod plugin reads the
#      architecture from kcp and changes only the spec, through arch_edit;
#   4. specd realizes each spec delta: an agent edits a worktree, deno-kcp's own
#      `go test ./...` gates the commit, the commit lands on the branch;
#   5. PUSH=1 pushes the branch and the orphan open-architecture/deno-kcp branch
#      and opens the pull request; otherwise the commands are printed.
#
# Needs: go, git, kcp, kine, kubectl, codegraph, the DeepSeek launcher
# `deepseek-claude` (or set HARNESS to another `claude`-compatible command),
# and gh for PUSH=1. The graph DB is optional: SPECD_BOLT_URL= turns it off.
#
set -euo pipefail

HYDRA=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$HYDRA/bin:$PATH"
export SPECD_SPECCTL="$HYDRA/bin/specctl"

WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-deno-kcp.XXXXXX")}
BRANCH=${BRANCH:-spec/bidder-and-bob-pds-$(date +%Y%m%d%H%M%S)}
PROMPT=${PROMPT:-add a running bidder instance to the example and a PDS for bob under his own namespace}
HARNESS=${HARNESS:-deepseek-claude}
GITHUB=${GITHUB:-https://github.com/publicdomainrelay}
PUSH=${PUSH:-0}
KEEP=${KEEP:-0}
FRESH=${FRESH:-1}
POPULATE_TIMEOUT=${POPULATE_TIMEOUT:-1200}
REALIZE_TIMEOUT=${REALIZE_TIMEOUT:-2400}

say() { printf '\n=== %s\n' "$*"; }

[ -x "$HYDRA/bin/specctl" ] && [ -x "$HYDRA/bin/specd" ] || (cd "$HYDRA" && make build >/dev/null)

say "clone into $WORK"
for repo in deno-kcp kcp-libs atproto-market hono-pds; do
  [ -d "$WORK/$repo" ] || git clone -q "$GITHUB/$repo" "$WORK/$repo"
done
cd "$WORK/deno-kcp"
git switch -q -c "$BRANCH"
git log --oneline -1

say "specctl up: build the architecture in kcp"
if [ "$FRESH" = "1" ]; then
  specctl up --remote "" --out "$WORK/session.json"
else
  specctl up --out "$WORK/session.json"
fi
deadline=$((SECONDS + POPULATE_TIMEOUT))
while true; do
  status=$(specctl status)
  case "$status" in
    *"Populated ("*) break ;;
    *"Failed ("*) echo "$status"; echo "populate failed" >&2; exit 1 ;;
  esac
  [ $SECONDS -ge $deadline ] && { echo "$status"; echo "populate did not finish" >&2; exit 1; }
  sleep 10
done
specctl status
specctl arch outline | grep -v '^  \(interfaces\|files\):'

say "harness: the request changes the spec, never a file"
cat > "$WORK/harness-prompt.txt" <<EOF
$PROMPT

How to do it: this repository's architecture lives in kcp, not in files. You have tools mcp__cc-clm-mod__arch_outline, mcp__cc-clm-mod__arch_context, mcp__cc-clm-mod__arch_edit and mcp__cc-clm-mod__arch_changes that read and change it. Change the SPEC only. Never create or edit files in this repository yourself: once the spec changes, a controller runs another agent that edits the code and the repository's tests gate it, and that agent can read nothing but this repository and the spec you write.

1. Call arch_outline, then arch_context for the contexts the request concerns (deploy-examples-atproto-market is "the example"; look at test-integration too, its examples registry lists the example files the tests check).
2. Research what the spec must say for the code agent to succeed, since it cannot look outside this repository. Read (do not edit) the existing manifests in deploy/examples/atproto/market and the organisation's sibling repositories cloned beside this one: $WORK/atproto-market/hono-bidder (mod.ts, cli-args-env.ts, config.json) and $WORK/hono-pds. Find the bidder's entry module, the flags and env it needs to run here (PLC directory, relay, keys, serve port, a compute provider that needs no cloud credentials) and which workspace it belongs in. The example's manifests name sibling repositories under /home/johnandersen777/src/publicdomainrelay-kcp, which apply.sh rewrites to the real org root; keep that convention.
3. Edit the context document(s): keep the existing requirements, add new MUST requirements with ids r.<kebab-case> that state precisely what to build: a workspace root:bob with its OpenBao object; a DenoPod pds in root:bob namespace default, built like alice's, serving pds.default.bob.svc.kcp.local on a port no other service uses; a running bidder DenoPod (name it bidder, say which workspace and why) with the exact entry path, argv, env and port; apply.sh, rbac and the README table updated; the test-side examples registry updated so the tests cover the new files. Name the files to create or change.
4. Send each edited document with arch_edit. Then call arch_changes and reply with a short summary of the delta you recorded.
EOF
"$HARNESS" -p --output-format text --plugin-dir "$HYDRA/cc-clm-mod" < "$WORK/harness-prompt.txt" | tee "$WORK/harness.out"

say "specd realizes the spec deltas"
deadline=$((SECONDS + REALIZE_TIMEOUT))
until grep -q SpecToCode <<<"$(specctl get specchanges)"; do
  [ $SECONDS -ge $deadline ] && { echo "no SpecToCode change appeared; did the harness call arch_edit?" >&2; exit 1; }
  sleep 10
done
retried=0
while true; do
  table=$(specctl get specchanges | grep -v -- '-c2s-' || true)
  if ! grep -q 'Running\|Pending' <<<"$table"; then
    unrealized=$(specctl arch outline -o json | python3 -c '
import json, sys
print(" ".join(e["name"] for e in json.load(sys.stdin) if e.get("conditions", {}).get("CodeSynced") == "False" and e["name"].startswith(("deploy-examples-atproto-market", "test-integration"))))')
    if [ -n "$unrealized" ] && [ "$retried" = 0 ] && grep -q Failed <<<"$table"; then
      for context in $unrealized; do specctl retry "$context"; done
      retried=1
      sleep 20
      continue
    fi
    break
  fi
  [ $SECONDS -ge $deadline ] && { echo "realize did not finish" >&2; break; }
  sleep 20
done
echo "$table"

say "the result: commits, the gate, the orphan branch, a clean tree"
git log --format='%h %s%n   %b' "origin/main..HEAD" | sed '/^   $/d'
git diff --stat origin/main...HEAD
go test ./... 2>&1 | grep -v 'no test files'
for arch in $(git for-each-ref --format='%(refname:short)' 'refs/heads/open-architecture/'); do
  echo "$arch:"
  git log -5 --oneline "$arch"
done
echo "git status --porcelain: '$(git status --porcelain)'"

if [ "$PUSH" = "1" ]; then
  say "push the branch and the architecture, open the pull request"
  git push -q -u origin "$BRANCH"
  git push -q origin "refs/heads/open-architecture/*:refs/heads/open-architecture/*"
  gh pr create --repo publicdomainrelay/deno-kcp --base main --head "$BRANCH" \
    --title "examples(atproto/market): $PROMPT" \
    --body "Produced by graph-clm-kcp-spec from the request: \"$PROMPT\". The spec this code realizes is on open-architecture/deno-kcp--${BRANCH//\//-}."
else
  say "PUSH=0: to publish"
  echo "  cd $WORK/deno-kcp && git push -u origin $BRANCH && git push origin 'refs/heads/open-architecture/*:refs/heads/open-architecture/*'"
  echo "  gh pr create --repo publicdomainrelay/deno-kcp --base main --head $BRANCH"
fi

if [ "$KEEP" != "1" ]; then
  specctl down
fi
echo "work tree: $WORK/deno-kcp"
