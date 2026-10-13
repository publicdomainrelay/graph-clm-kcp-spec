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
| `prompt.compose` | inject that file as one more session system section, appended last so the context is the model's most recent instruction, led by the shared context-management skill (`contextSkill()` of `clm/core`, or the repository's `.specs/context-skill.md`), with the context's open plan items (`specctl plan items`) below it so a realize agent sees why its change exists; then the cards of the contexts in use, and, in a realize session, the task card |
| `tool.call` | refuse a path outside `SPECD_CLM_ROOT`, then report the file a `Read`/`Write`/`Edit`/`MultiEdit` touched, and ask `specctl arch owner` which context owns it |
| `turn.complete` | apply the model zone when the model changed it, then report the turn |
| `session.end` | the same, skipped when the exit budget has less than a second left: a hanging exit is worse than a spec edit the next session re-renders from kcp |

The mod's `FileStore.ancestors` answers the empty list on purpose: a mod has no
ancestor walk, and the engine already reads the `CLAUDE.md` stack for the model,
so the context document is this host's contribution.

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
specctl clm apply  --context <name> --turn <n> < model-zone
specctl clm report --change <name> --event <json>
```

`apply` also turns the free-form notes of the model zone into graph memory: one
`Memory` vertex per `- title: body` line under the notes heading, linked by
`REFERENCES` to the code they name and by `SPECIFIES` to the claim units, and
`REMEMBERED` by the context's `AgentContext`. The shared core carries the same
tools directly: `specctl clm remember`, `specctl clm recall <terms>` and
`specctl clm neighbors <context>`.

so kcp access, the delta authority (`abc/delta`) and the graph writes have one
implementation, and this host and the pi host agree by construction.

Every call carries `--workspace` and `--namespace` explicitly, because the mod
runs inside the workspace the controller chose, not specctl's default.

## The work in front of the model

On `tool.call` for `Read`, `Write`, `Edit`, `MultiEdit` and `NotebookEdit` the
mod asks `specctl arch owner <path>` which context owns the path (the deepest
context whose observed files hold it) and keeps that context in a
most-recently-used set of at most four. On `prompt.compose` each of those
contexts contributes the compact card of `specctl context pack --context <name>`
(owner card plus neighbour cards), so the model reads what the file is part of,
not only what it just opened. The set changes only when a new file is touched,
so the injected prefix does not churn between prompts.

A session with no `SPECD_CLM_CONTEXT` is not left with nothing: it injects the
pack of the session's own directory (`specctl context pack --file <cwd>`, which
carries the repository card) and, when `specctl arch owner` names one, the card
of the context that owns that directory.

## The realize task card

`SPECD_CLM_CHANGE` marks a realize session. The mod then appends a task card
after everything else in the composed prompt: the change, the context, the
repository's verify command and its acceptance steps (read from the context's
pack), and the plain instruction to run only the verify and the acceptance of
this task, not the repository's whole suite, and not to commit or push. Because
it is the last section, it is the model's most recent instruction, which is what
makes it outrank the repository's own `CLAUDE.md` and `AGENTS.md`. An ordinary
session (no change) gets no task card.

## The scope guard

A realize agent is graded by hidden acceptance tests that live in the repository
the agent was not given. `SPECD_CLM_ROOT` is the worktree it *was* given, and
the `tool.call` hook refuses anything that lands outside it before the tool
runs, so the refusal is an error result the model reads rather than a session
that silently wandered. The variable is read lazily, because `tool.call` can run
before `session.start`; a session with no root is ordinary and unguarded.

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

## Interactive sessions: inspect and edit the architecture through kcp

In a repository where `specctl up` ran, a plain `claude --plugin-dir cc-clm-mod`
session (no `SPECD_CLM_CONTEXT`) finds that repository's kcp through
`specctl env` and registers eleven tools the model can call:

| tool | does |
| --- | --- |
| `mcp__cc-clm-mod__arch_outline` | every system context kcp holds: upstream, overlay, orchestrator, intent, interfaces, files, sync conditions |
| `mcp__cc-clm-mod__arch_context` | one context as its context document (prose intent, fenced `yaml spec` block, resolved refs) |
| `mcp__cc-clm-mod__arch_edit` | send the edited document back; kcp records the delta and specd opens a `SpecToCode` change that realizes it, gated by the tests |
| `mcp__cc-clm-mod__arch_changes` | the SpecChanges and their phase, to follow an edit until its code lands |
| `mcp__cc-clm-mod__arch_plans` | the Plan objects kcp holds, with their status |
| `mcp__cc-clm-mod__arch_plan` | send a Plan as YAML; it lands as a draft, and only a person approves it |
| `mcp__cc-clm-mod__arch_findings` | the findings kcp holds: where the code and the spec disagree |
| `mcp__cc-clm-mod__arch_release` | release a context's declared interfaces, one unit or `--all` |
| `mcp__cc-clm-mod__arch_owner` | which context owns a path: the deepest context whose observed files hold it |
| `mcp__cc-clm-mod__arch_neighbors` | what one context is joined to in the graph, one hop out |
| `mcp__cc-clm-mod__arch_search` | full text search over the graph: claim text, symbol names and docstrings |

The spec never sits in the project tree: the tools read and write kcp, and kcp
persists to the orphan branch `open-architecture/<repository>`.

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

The vendored declarations are a hand-written, closed re-export of the ports
rather than a re-export of `clm/core`'s sources, because the engine's own
tsconfig does not allow `.ts` import paths.
