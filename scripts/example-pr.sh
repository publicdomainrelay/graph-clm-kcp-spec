#!/usr/bin/env bash
#
# One natural-language request becomes a pull request against a real repository
# this tool did not write. This is the general script; a per-repository example
# is a thin wrapper that sets REPO, SIBLINGS and PROMPT (see
# example-deno-kcp-pr.sh).
#
#   1. clone REPO (and the SIBLINGS its relative imports/workspace paths need)
#      into a temp dir;
#   2. specctl up: this clone's own kcp on kernel-assigned ports, CodeGraph
#      index, one SystemContext per directory, a DeepSeek summary of each
#      (FRESH=1, the default, indexes from scratch; FRESH=0 restores from the
#      remote's open-architecture/<repo> branch when it exists);
#   3. a headless Claude Code harness with the cc-clm-mod plugin reads the
#      architecture from kcp and changes only the spec, through arch_edit;
#   4. specd realizes each spec delta: an agent edits a worktree, the
#      repository's verify command gates the commit, the commit lands on the
#      branch;
#   5. PUSH=1 pushes the branch and the orphan open-architecture/<repo>*
#      branches and opens the pull request; otherwise the commands are printed.
#
# Needs: go, git, kcp, kine, kubectl, codegraph, the DeepSeek launcher
# `deepseek-claude` (or set HARNESS to another `claude`-compatible command),
# and gh for PUSH=1. The graph DB is optional: SPECD_BOLT_URL= turns it off.
#
# Env:
#   REPO        repository to change, cloned from $GITHUB/$REPO (required)
#   PROMPT      the change request; the harness receives it verbatim (required)
#   BRIEF       optional extra paragraphs for the harness, after PROMPT
#   SIBLINGS    repos cloned beside it so relative imports resolve. Each entry
#               is DIR[=GHREPO]; DIR is the directory name the workspace
#               imports (`../DIR/...`), GHREPO the repository under $GITHUB
#               when DIR differs from it. A DIR that exists in $ORG_ROOT is
#               cloned from there instead (the org root is the working set the
#               sibling revisions were developed against).
#   BASE        branch to check out before the work branch, and the pull
#               request's base; default is the repository's default branch
#   BRANCH      work branch to create (default spec/<repo>-<timestamp>)
#   VERIFY      command that gates every realize commit (run with `bash -lc`
#               inside the worktree); overrides the repository's detected
#               verify command. The worktree is placed in a temp parent whose
#               siblings are symlinks to the clones, so relative paths resolve.
#   ACCEPT      optional acceptance command added as a gating Repository
#               spec.acceptance step (run with `bash -lc` against the
#               repository tree)
#   GITHUB      the GitHub org base URL (default publicdomainrelay)
#   ORG_ROOT    directory holding the sibling checkouts (default the parent of
#               this checkout)
#   WORK        temp dir to clone into
#   HARNESS     the model command (default deepseek-claude)
#   PUSH KEEP FRESH POPULATE_TIMEOUT REALIZE_TIMEOUT   as for the deno-kcp run
#
set -euo pipefail

HYDRA=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$HYDRA/bin:$PATH"
export SPECD_SPECCTL="$HYDRA/bin/specctl"

REPO=${REPO:?set REPO to the repository to change}
PROMPT=${PROMPT:?set PROMPT to the change request}
BRIEF=${BRIEF:-}
SIBLINGS=${SIBLINGS:-}
BASE=${BASE:-}
BRANCH=${BRANCH:-spec/$REPO-$(date +%Y%m%d%H%M%S)}
VERIFY=${VERIFY:-}
ACCEPT=${ACCEPT:-}
HARNESS=${HARNESS:-deepseek-claude}
GITHUB=${GITHUB:-https://github.com/publicdomainrelay}
ORG_ROOT=${ORG_ROOT:-$(cd "$HYDRA/.." && pwd)}
WORK=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/specd-${REPO}.XXXXXX")}
PUSH=${PUSH:-0}
KEEP=${KEEP:-0}
FRESH=${FRESH:-1}
POPULATE_TIMEOUT=${POPULATE_TIMEOUT:-1800}
REALIZE_TIMEOUT=${REALIZE_TIMEOUT:-3600}

say() { printf '\n=== %s\n' "$*"; }

[ -x "$HYDRA/bin/specctl" ] && [ -x "$HYDRA/bin/specd" ] || (cd "$HYDRA" && make build >/dev/null)

say "clone into $WORK"
for entry in $REPO $SIBLINGS; do
  dir=${entry%%=*}
  ghrepo=${entry#*=}
  [ -d "$WORK/$dir" ] && continue
  # A sibling in the org root may be a worktree, where .git is a file, so test
  # for a git repository rather than for a directory.
  if git -C "$ORG_ROOT/$dir" rev-parse --git-dir >/dev/null 2>&1; then
    git clone -q "$ORG_ROOT/$dir" "$WORK/$dir"
  else
    git clone -q "$GITHUB/$ghrepo" "$WORK/$dir"
  fi
done
cd "$WORK/$REPO"
if [ -n "$BASE" ]; then
  git fetch -q origin "$BASE"
  git switch -q -c "$BRANCH" "origin/$BASE"
else
  git switch -q -c "$BRANCH"
fi
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

if [ -n "$VERIFY" ] || [ -n "$ACCEPT" ]; then
  say "gate the Repository on the honest verify/acceptance command"
  repository=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["repository"])' "$WORK/session.json")
  specctl get repository "$repository" -o json > "$WORK/repository.json"
  REPO_MANIFEST="$WORK/repository.json" TRUSTED_VERIFY="$VERIFY" TRUSTED_ACCEPT="$ACCEPT" python3 - <<'PY'
import json, os
path = os.environ["REPO_MANIFEST"]
with open(path) as fh:
    manifest = json.load(fh)
if manifest.get("kind") == "List":
    manifest = manifest["items"][0]
    manifest.pop("status", None)
spec = manifest.setdefault("spec", {})
if os.environ["TRUSTED_VERIFY"]:
    spec["verify"] = ["bash", "-lc", os.environ["TRUSTED_VERIFY"]]
if os.environ["TRUSTED_ACCEPT"]:
    spec["acceptance"] = [{
        "name": "acceptance",
        "command": ["bash", "-lc", os.environ["TRUSTED_ACCEPT"]],
        "timeoutSeconds": 1800,
        "gate": True,
    }]
with open(path, "w") as fh:
    json.dump(manifest, fh)
PY
  specctl apply -f "$WORK/repository.json"
fi

say "harness: the request changes the spec, never a file"
cat > "$WORK/harness-prompt.txt" <<EOF
$PROMPT
$BRIEF

How to do it: this repository's architecture lives in kcp, not in files. You have tools mcp__cc-clm-mod__arch_outline, mcp__cc-clm-mod__arch_context, mcp__cc-clm-mod__arch_edit and mcp__cc-clm-mod__arch_changes that read and change it. Change the SPEC only. Never create or edit files in this repository yourself: once the spec changes, a controller runs another agent that edits the code and the repository's tests gate it, and that agent can read nothing but this repository and the spec you write.

1. Call arch_outline, then arch_context for the contexts the request concerns.
2. Research what the spec must say for the code agent to succeed, since it cannot look outside this repository. Read (do not edit) this repository and the sibling repositories cloned beside it under $WORK: $SIBLINGS.
3. Edit the context document(s): keep every existing requirement, add new MUST requirements with ids r.<kebab-case> that state precisely what to build, and name the files to create or change. Follow this repository's own CLAUDE.md rules.
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
print(" ".join(e["name"] for e in json.load(sys.stdin) if e.get("conditions", {}).get("CodeSynced") == "False"))')
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
default=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || echo origin/main)
[ -n "$BASE" ] && default="origin/$BASE"
git log --format='%h %s%n   %b' "$default..HEAD" | sed '/^   $/d'
git diff --stat "$default...HEAD"
bash -lc "${VERIFY:-$([ -f go.mod ] && echo 'go test ./...' || echo 'deno check')}" || true
for arch in $(git for-each-ref --format='%(refname:short)' 'refs/heads/open-architecture/'); do
  echo "$arch:"
  git log -5 --oneline "$arch"
done
echo "git status --porcelain: '$(git status --porcelain)'"

if [ "$PUSH" = "1" ]; then
  say "push the branch and the architecture, open the pull request"
  git push -q -u origin "$BRANCH"
  git push -q origin "refs/heads/open-architecture/*:refs/heads/open-architecture/*"
  prbase=${BASE:-$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's|^origin/||' || echo main)}
  gh pr create --repo "${GITHUB#https://github.com/}/${REPO}" --base "$prbase" --head "$BRANCH" \
    --title "$PROMPT" \
    --body "Produced by graph-clm-kcp-spec from the request: \"$PROMPT\". The spec this code realizes is on open-architecture/${REPO}--${BRANCH//\//-}."
else
  say "PUSH=0: to publish"
  echo "  cd $WORK/$REPO && git push -u origin $BRANCH && git push origin 'refs/heads/open-architecture/*:refs/heads/open-architecture/*'"
  echo "  gh pr create --repo ${GITHUB#https://github.com/}/$REPO --base ${BASE:-main} --head $BRANCH"
fi

if [ "$KEEP" != "1" ]; then
  specctl down
fi
echo "work tree: $WORK/$REPO"
