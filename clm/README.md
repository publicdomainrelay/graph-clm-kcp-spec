# clm

The context language model library: one format and one set of ports, shared by
every host. `clm/core` is pure TypeScript with no `node:` import and no I/O;
`clm/adapters-node` is the Node side of the ports. The pi extension
(`pi-hydradb-clm`) and the Claude Code mod (`cc-clm-mod`) both re-export the
core, so the file one host writes and the file the other reads are one format.

## core

- The context document: a model zone the model owns and a managed zone between
  `<!-- SPECD_MANAGED_BEGIN -->` and `..._END -->` that the host regenerates.
- The spec block: the prose intent plus a fenced `yaml spec` block, rendered and
  parsed so a render followed by an apply with no edit is a no-op. The block is
  parsed with a real YAML parser, never a hand-rolled subset: a block a model
  produced must not be read "almost right".
- Reference extraction, the token budget, the FNV-1a content ids, the Cypher row
  builders, and the delta mirror of the Go `abc/delta` (the same JSON, held to
  the same golden files).
- The ports: `StateBridge`, `FileStore`, `Runner`, `GraphWriter`, `Clock`. The
  core reaches the world only through them.

## Ports

| port | contract |
| --- | --- |
| `StateBridge` | the Go CLI (`specctl clm render\|apply\|report`). kcp access, the delta authority and the graph writes have one implementation; every host reaches it as a process call. A host that cannot run a process substitutes its own. |
| `FileStore` | `read`, `write`, `exists`, and `ancestors`: every `.md` of `names` in the directories above `dir`, root first, the way the engine reads a `CLAUDE.md` stack. A host without an ancestor walk — the mod has none — answers the empty list. |
| `Runner` | one command by argument vector; there is no shell. |
| `GraphWriter` | a host with no Bolt driver leaves it out; the bridge writes the durable half (the status subresource) either way. |
| `Clock` | `now()`; a test substitutes a fixed clock. |

## The host contract

`ClmHost` is the shared state machine both hosts drive:

- `start()` renders the context from kcp into the state dir, outside the
  project tree. A bridge that cannot answer (no workspace, no such context)
  leaves the session without a document rather than failing it.
- `section()` is the document as one system section before a model request; a
  missing file adds no section.
- `touched()` reports the files a tool call touched. A report is an observation:
  losing one must never fail the work, so a `report` failure is swallowed.
- `finish()` applies the model zone when it changed. An apply failure is the
  opposite of a report failure — it is recorded in the result and printed by the
  host, and the model zone is not marked applied, so the next turn tries again
  instead of a silent failure reading as a turn where nothing changed.
