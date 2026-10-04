# cc-clm-mod

The Claude Code host of the CLM loop.

specd launches the subagent that realizes a `SpecChange` with this folder
loaded:

```
deepseek-claude -p --output-format text --plugin-dir <repo>/cc-clm-mod
```

The mod then reports into the same context file, graph and kcp state the
controllers watch, so the alignment of code to spec is observed **while the work
happens**, not guessed afterwards.

## What it does

| Hook | Behaviour |
| --- | --- |
| `session.start` | resolve the context (`SPECD_CLM_CONTEXT`), render it from kcp, write the context document to the state dir (`SPECD_CLM_DOC`), outside the project tree |
| `prompt.compose` | inject that file as one more session system section |
| `tool.call` | refuse a path outside `SPECD_CLM_ROOT`, then report the file a `Read`/`Write`/`Edit`/`MultiEdit` touched |
| `turn.complete` | apply the model zone when the model changed it, then report the turn |
| `session.end` | the same, skipped when the session's exit budget has no room |

## The three files a mod is

```
.claude-plugin/plugin.json   the manifest
hooks/hooks.json             { "modules": ["./register.ts"] }
hooks/register.ts            export const register: Register = (on) => { ... }
```

The engine's rules shape `register.ts`: a mod has no Node and no sockets, it
reaches the host only through `$`, and `$` may be followed only into a function
declared in that same file. So:

- `hooks/plan.ts` holds the decisions (which tool touched which file, what the
  bridge's argument vector is) and imports nothing.
- `hooks/vendor/` is the shared `clm/core`, bundled by `npm run build:mod`
  because a mod imports only files inside its own folder.
- `hooks/register.ts` is the only file that touches `$`.

## The state bridge is Go

The mod never computes a delta, writes a graph row or talks to kcp. It runs

```
specctl clm render --context <name>
specctl clm apply  --context <name> < model-zone
specctl clm report --change <name> --event <json>
```

so kcp access, the delta authority (`abc/delta`) and the graph writes have one
implementation, and this host and the pi host agree by construction.

## The scope guard

A realize agent is graded by hidden acceptance tests that live in the repository
the agent was not given. `SPECD_CLM_ROOT` is the worktree it *was* given, and
the `tool.call` hook refuses anything that lands outside it before the tool
runs, so the refusal is an error result the model reads rather than a session
that silently wandered.

- A file tool (`Read`, `Write`, `Edit`, `MultiEdit`, `NotebookEdit`, `Grep`,
  `Glob`) is refused when `$.fs.stat(path, { resolve: true })` puts its
  `realPath` outside the root. A `Write` to a file that is not there yet is
  placed by its folder and the name kept, so it is judged the same way. A path
  that cannot be placed is refused: the tool might open it.
- A `Bash` command is refused when it names an absolute path outside the root
  and outside the system directories (`/usr`, `/bin`, `/tmp`, ...), or a `..`
  that climbs out. This half is best effort and says so: shell word splitting
  cannot see a path built from a variable, a relative walk that stays behind a
  command, a hard link or a case alias.

The decision is pure given the path resolver, so `claude plugin test .` covers
allow inside, deny outside, and deny of this repository's own `fixtures/` and
`scenarios/` without a session. The live half is
`TestPhase12ScopeGuardRefusesAFileOutsideTheRoot`.

## Environment

Set by specd on the model it launches:

| Variable | Meaning |
| --- | --- |
| `SPECD_CLM_CONTEXT` | the `SystemContext` this session is inside; unset means the mod does nothing |
| `SPECD_CLM_CHANGE` | the running `SpecChange` progress is reported against |
| `SPECD_CLM_REPO` | the managed working tree (default: the session's cwd) |
| `SPECD_CLM_ROOT` | the containment root the scope guard enforces; specd sets it to the worktree on the summarize and the realize call, and unset turns the guard off |
| `SPECD_CLM_DOC` | the exact context document file; specd sets it. It lives in the state dir, never in the project tree, and it is the one file outside the root the scope guard allows |
| `SPECD_CLM_REPOSITORY`, `SPECD_CLM_DOC_DIR` | without `SPECD_CLM_DOC`, the document is `$SPECD_CLM_DOC_DIR/<repository>/<context>.md` (default `$XDG_STATE_HOME/specd/clm`, else `~/.local/state/specd/clm`) |
| `SPECD_SPECCTL` | the `specctl` binary (default: `specctl` on `PATH`) |
| `KUBECONFIG` / `SPECD_KUBECONFIG` | the workspace kubeconfig |
| `SPECD_WORKSPACE`, `SPECD_NAMESPACE` | the logical cluster and namespace (defaults `root:specs`, `default`) |
| `SPECD_BOLT_*` | the graph endpoint; unset means the report goes to kcp alone |

## Developing

```
npm run build:mod     # vendor clm/core into hooks/vendor/core.js (+ hash)
npm test              # node --test: the vendored copy is not stale
npm run typecheck     # tsc -p tsconfig.json
claude plugin validate .
claude plugin test .  # the hooks' decisions, on the engine itself
```

`npm test` fails when `clm/core` changed and `build:mod` was not re-run, which
is the failure mode that would otherwise show up as a session behaving like an
older core.

`tsconfig.json` type-checks against `types/claude-code.d.ts`, a small committed
subset of the engine's declarations, so the check works offline. When the engine
has loaded the mod it writes the real declarations into
`.claude-plugin/types/`, and `tsc -p .claude-plugin/types` then checks the mod
against the engine's own file.
