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
| `session.start` | resolve the context (`SPECD_CLM_CONTEXT`), render it from kcp, write `.specs/context/<name>.md` |
| `prompt.compose` | inject that file as one more session system section |
| `tool.call` | after `Read`/`Write`/`Edit`/`MultiEdit`, report the file it touched |
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

## Environment

Set by specd on the model it launches:

| Variable | Meaning |
| --- | --- |
| `SPECD_CLM_CONTEXT` | the `SystemContext` this session is inside; unset means the mod does nothing |
| `SPECD_CLM_CHANGE` | the running `SpecChange` progress is reported against |
| `SPECD_CLM_REPO` | the managed working tree (default: the session's cwd) |
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
