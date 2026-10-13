# PLAN 0011 - plans, findings and agent runs as objects; spec authority

Status: active. Owner: coordination agent. Executor: the coordination agent
itself (Claude Code), with DeepSeek only where a live model test runs.

This plan is the first one tracked by the mechanism it builds. Its items live
in kcp as a `Plan` object ([`0011-plan.yaml`](0011-plan.yaml)), each item
carries the acceptance commands that prove it, and specd marks an item `Done`
only when those commands pass at the checkout's HEAD. The markdown says why;
the object says what is done.

## Objective

Specs say where the system should be now. Nothing in kcp says how it gets
there. Today that layer is markdown: a review (`docs/reviews/*`) produces
findings, a plan (`docs/plans/*`) turns findings into items, items become spec
edits, the edits become `SpecChange`s, and "done means" closes the plan. Every
arrow in that chain is a person or the coordination agent reading files. The
iroh run (plan 0005) needed ten `SpecToCode` changes and three review-driven
amendments, grouped by hand.

The objective: make that chain state that kcp holds and specd reconciles.

```
  review / summary / drift / policy
        |
        v
     Finding  ----------------------------+
        |  resolved by                     |
        v                                  |
      Plan  (approved = spec intent reviewed before any code)
        |  items in dependency order       |
        v                                  |
  staged spec edit --> SystemContext --> SpecChange --> AgentRun(s)
        |                                         |
        +-- acceptance commands at HEAD <---------+
                         |
                     item Done --> Finding Resolved
```

Two principles carry the whole plan:

1. **code -> spec may propose; only a person, a model's spec edit or a plan
   accepts.** A summary never overwrites what someone declared.
2. **The controller stays deterministic.** A model is an actuator. Every model
   call is an object with inputs, outcome and timing, not a log line inside
   another object's status.

## What exists (inputs)

| Thing | Where | Use here |
| --- | --- | --- |
| `SpecChange` per context | `abc/spec/types.go` | the unit a plan item drives; one per (context, spec hash) episode |
| edit detection | `abc/sync.SpecEditDue`, `factory/specd/systemcontext.go` | a plan's staged edit enters through the same door as a person's |
| acceptance steps | `Repository.spec.acceptance`, `impl/realize` | the shape of an item's done criteria |
| orphan branch | `abc/oabranch`, `impl/persist` | where plans and findings persist next to the specs |
| summary writer | `impl/summarize` | wrote `intent`, `requirements` and `interfaces` wholesale, over anything a person wrote |
| agent factory | `impl/agentfactory`, `abc/agent.Agent` | the seam an agent run record wraps |
| cc-clm-mod tools | `cc-clm-mod/hooks/plan.ts` `ARCH_TOOLS` | argv mappings to `specctl`; new tools are new mappings |
| instances | `specctl ls`, `impl/session` | one kcp per (checkout, branch); the set a cross-repository plan spans |

## A - spec authority (done)

Evidence before the fix: `agent.DraftSpec` set `Intent`, `Requirements` and
`Interfaces` from the draft, so any `CodeToSpec` summary replaced a requirement
a person had added. `summarize.Run` also stamped the merged spec as realized, so
a person's edit that was not realized yet was marked done and never reached the
code. The write was a blind get-and-update after a model run of minutes.

What shipped:

- `abc/authority`: a spec is a set of units (`intent`, `requirement/<id>`,
  `interface/<name>`, `interaction/<id>`). A unit is declared when the
  annotation `specs.publicdomainrelay.dev/declared` names it, or when it differs
  between `status.realizedSpec` and `spec` (an edit not realized yet).
  `Merge` keeps every declared unit, records what the summary would have
  written as a `Proposal`, and computes the spec it may mark realized with the
  realized side of each declared unit, so a pending edit stays a spec to code
  change. A requirement whose only change is its code refs is not a change:
  refs are anchors the ingest re-derives.
- `impl/summarize`: writes through `authority.Merge` with the resource version
  it read; on a conflict it re-reads and treats whatever changed during the run
  as declared. The origin hash is stamped only when nothing is pending.
- `impl/archkcp` seed: the same realized-side rule.
- `factory/specd`: when it detects a spec edit it records the edit's units in
  the annotation, so after the realize lands the units stay declared.
- `SpecChange.status.proposals` (CRD revision 9) carries the proposals; the
  orphan branch keeps them in `changes/*.yaml`, and the spec files keep the
  declared annotation, so a restore keeps a person's requirement theirs.
- `specctl arch declared|declare|release|proposals`. Releasing a unit is how a
  proposal is accepted: the next summary writes it.

Tests: `abc/authority`, `impl/summarize` (declared kept, pending edit left
unrealized, an edit landing mid-run merged), `factory/specd` (declared units
recorded), `abc/mirror`, `impl/persist`, and the live
`TestAuthorityASummaryKeepsWhatAPersonWrote` against a real kcp.

Limits, recorded: structural fields (`upstream`, `overlay`, `dependsOn`, ...)
are not units; the summary never writes them and the ingest only adds to sets.
An annotation-only change reaches the orphan branch with the next persisted
change, not on its own.

## B - Plan

A `Plan` is a namespaced object in the repository's workspace.

```yaml
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: Plan
metadata: {name: p0011}
spec:
  title: plans, findings and agent runs as objects
  objective: one paragraph
  approved: false          # nothing is applied before a person approves
  approvedBy: ""
  items:
    - id: plan-resource
      summary: one line
      repository: hydradb   # optional; an instance only acts on its own
      dependsOn: [authority]
      findings: [f-0003-b1] # Findings this item resolves
      edits:                # staged spec edits, empty = a tracked item
        - context: calc
          intent: "..."
          requirements: [{id, level, text}]   # upsert by id
          removeRequirements: [r.old]
      acceptance:           # done criteria, run at HEAD in the checkout
        - name: unit
          command: [go, test, ./abc/plan/]
status:
  phase: Draft | Approved | Running | Complete | Failed
  items:
    - id: plan-resource
      phase: Blocked | Pending | Realizing | Verifying | Done | Failed | Elsewhere
      appliedSpecHashes: {calc: <hash>}
      changes: [calc-s2c-...]
      commit: <HEAD the acceptance ran at>
      acceptance: [{name, exitCode, passed, outputTail}]
      message: ...
  conditions: [Approved, Complete]
```

Reconcile (pure decision in `abc/plan`, I/O in `factory/specd`):

1. Not approved: phase `Draft`. The status previews each item's delta per
   context (`delta.Summary`), so the spec intent is reviewed before any code.
2. An item is `Blocked` until every `dependsOn` item is `Done`, and while
   another item of the plan holds an edit to one of its contexts that is not
   realized yet (one in-flight edit per context keeps episodes distinct).
3. An item with edits whose contexts do not yet carry them: apply each edit to
   its `SystemContext`, mark the touched units declared, set origin `plan` and
   annotation `specs.publicdomainrelay.dev/plan-item: <plan>/<item>`. specd's
   edit detection raises the `SpecToCode` change; the item records the spec
   hash and goes `Realizing`.
4. `Realizing` ends when the `SpecChange` whose `toSpecHash` is the recorded
   hash `Succeeded` (go `Verifying`) or every attempt `Failed` (item `Failed`).
5. `Verifying` (and every item without edits) runs the item's acceptance at the
   checkout HEAD, once per HEAD commit. All pass: `Done`. A failure stays
   `Verifying` and runs again when HEAD moves.
6. Every item `Done`: plan `Complete`. Any item `Failed`: plan `Failed`, the
   rest stop where they are.

`specctl plan apply -f|ls|show|approve|preview`. Plans persist to the orphan
branch as `plans/<name>.yaml` (spec and an item phase summary) and restore with
it.

## C - Finding

```yaml
kind: Finding
spec: {source: review|proposal|drift|policy|manual, ref, context, unit, summary, severity}
status: {state: Open|Planned|Resolved|Dismissed, plan, item, resolvedCommit}
```

- A summary proposal creates a `Finding` (source `proposal`, a stable name from
  context and unit) so a disagreement between the code and a declared unit is
  something a plan can resolve, not only a line in a change status.
- An approved plan marks its items' findings `Planned`; a `Done` item marks them
  `Resolved` with the commit.
- `specctl finding add|ls|dismiss`. Findings persist to `findings/<name>.yaml`.

## D - AgentRun

Every model call specd makes becomes an object, written by a decorator around
`abc/agent.Agent` in `impl/agentrun`, wired by the factory:

```yaml
kind: AgentRun
spec: {purpose: summarize|realize, change, context, agent: {kind, command}}
status: {phase: Running|Succeeded|Failed, startedAt, finishedAt, durationSeconds, message}
```

The `SpecChange` names its runs (`status.agentRuns`). This is the record half:
retries, budgets and per-purpose model choice read and write these objects in a
later plan, once workers reconcile them instead of specd calling the agent in
process.

## E - the mod

`cc-clm-mod` gains tools that map to the new commands, the same way the four
arch tools map to `specctl` today:

- `arch_plans` - plans with their item phases (`specctl plan ls`).
- `arch_plan` - create or update a draft plan from staged edits
  (`specctl plan apply -f -`, never approved by the tool).
- `arch_findings` - open findings and proposals.
- `arch_release` - release a declared unit so the summary may write it.

The session context section gains the open plan items whose edits touch the
session's context, so a realize agent sees why its change exists.

Not in this plan, with the reason: a policy check before each edit. The policy
gate evaluates a commit's diff against the whole model; a per-tool-call check
needs an incremental evaluator, which plan 0010's work has not built.

## F - plans across repositories

Applications do not share a kcp. A cross-repository plan is the same `Plan`
applied to each instance it names (`repository` on the item). An instance acts
only on its own items and shows the others as `Elsewhere`.
`specctl plan apply --all-instances` writes the plan to every running instance
`specctl ls` lists, and `specctl plan status --all-instances` merges their item
phases into one view. The kcp-libs#1 before deno-kcp#1 case is two items, one
per repository, with `dependsOn` across them: the merged view resolves it.

## G - dogfood

- `docs/plans/0011-plan.yaml` is this plan as a `Plan`, items A to G, each with
  the acceptance commands that prove it (`go test -run ...`).
- It is applied to an instance of this repository and approved; specd runs the
  acceptance at HEAD and the items turn `Done` as the work lands. The run is
  recorded below with the item table `specctl plan show p0011` printed.
- A live e2e drives a plan with a staged edit on a fixture repository with the
  scripted agent: draft, preview, approve, apply, realize, verify, done, finding
  resolved.

## Done means

- every item is `Done` in the `p0011` object, or is recorded here as a limit
  with the reason;
- `gofmt`, `go vet ./...`, `go test ./...` and the live suite are green;
- this file records the dogfood run.
