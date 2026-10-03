# Effectiveness reports

One file per run, a markdown table and the same numbers as JSON beside it. Every
number here comes from `specctl eval`, and every measure it prints is a unit
test in `abc/eval`, so nothing in these files is an opinion about a run.

Reproduce any of them with:

```bash
make build
SPECS_WORKSPACE=specs-eval WORKSPACE_KUBECONFIG=.kcp-specd/specs-eval.kubeconfig \
  deploy/install-specs.sh                      # once; the eval keeps its own workspace
bin/specctl eval --fixtures fixtures --out docs/eval/run-<date>.md
```

## The runs

| run | agent | scope | report |
| --- | --- | --- | --- |
| scripted baseline | `scripted` | 3 fixtures, 9 scenarios, both halves | `run-2026-10-03-scripted.md` |
| live, primary | `claude-mod` (`deepseek-claude` + `cc-clm-mod`) | 3 fixtures, 9 scenarios, both halves | `run-2026-10-03.md` |
| live, code -> spec | `pi` (local `llama-cpp`, `ternary-bonsai-2-27b`) | 3 fixtures, code -> spec and the round trip | `run-2026-10-03-pi.md` |
| unknown codebase | `claude-mod` | `../kcp-libs`, 34 contexts, populate only | `run-2026-10-03-kcp-libs.md` |
| unknown codebase, receiver-keyed | `claude-mod` | the same 34 contexts after the interface key took its receiver | `run-2026-10-03-kcp-libs-qualified.md` |

The gated live model tests are recorded beside them, as they ran:
`live-model-tests.log` holds the output of the five tests that spend a real
model call (`SPECD_REQUIRE_LIVE_MODEL=1`), all five passing.

## What the plan asked for, and where it is

| plan metric | where it is |
| --- | --- |
| interface recall, precision | `abc/eval.InterfaceScore`, the Code -> spec table |
| requirement anchoring | `abc/eval.AnchoringRate`, the same table |
| validator pass | `abc/eval.ValidatorPass`, the same table |
| spec -> code pass rate (verify + acceptance) | the Spec -> code table, and the headline |
| attempts used | the Spec -> code table |
| files touched outside the context | the same table, `outside` |
| wall time | both tables |
| round trip stability | `abc/eval.Jaccard`, the round trip column |
| populate: `Populated`, contexts, failed, wall time | the Populate table |
| delta precision | the `delta` column, entries against the scenario's intent |
| CLM path | the `claude-mod` run is the CLM path, `pi` the other host |
| `SpecChange.status.progress` | the `progress` column: the host reported while it ran |

## What the runs say

**The harness is sound.** The scripted baseline passes every scenario of every
fixture, which is the point of having it: a failure in the live runs after this
one belongs to the agent under test, not to the measurement around it. The same
scenarios pass with `deepseek-claude` and the mod loaded, so the loop is not
tuned to the deterministic agent.

**The live model reads code and edits it well.** Over three fixtures and nine
scenarios the primary live run passed 9 of 9, carried exactly the entries each
scenario intended (9 of 9), touched no file outside the context that was
changed, and needed one attempt each. Round trip stability was 100%: a second
summarize produced the same interface set and the same requirement count as the
first. Every measure in the committed report is 100%.

That is the last of several runs of the same command, and the model is not
deterministic. An earlier one measured interface precision 97.1%: `greet`'s
summarize declared `Greeter.greeting`, a public TypeScript class member the
index reports unexported. Both runs are honest; the difference is the model's,
and the gap underneath it is the index's, which is why the next section names
it.

**The mod reports while it works.** Every live scenario has five to seven
`SpecChange.status.progress` records, which is what proves the model was
working against the same state the controller watches rather than being
guessed at afterwards. The scripted baseline has none, and that is correct: no
host is inside it.

## Failure modes, and what they were

**One live scenario was graded against a contract the spec never stated.** The
first live run failed `greet/add-a-person-module`: the hidden test pins the
exact string `greetPerson` returns, and the requirement said only that it
"greets a person by their formatted name and age". The model implemented that
reading — greeting the formatted name — and its own test passed. That is a
fixture defect, not a model mistake: a scenario whose test asserts a behaviour
the spec does not state grades the agent against something it was never told.
The two `greet` scenarios now name the strings they expect, and the person
scenario states `Person`'s shape and `formatPerson`'s output as requirements of
their own. This is the failure mode the eval exists to find, and it found it in
the fixtures first.

**The index under-reports TypeScript's public surface.** Whenever a live
summarize names a class member — `Greeter.greeting` is the one that has shown
up — it lands in the Extra column, because codegraph reports `is_exported:
false` for every method node and a TypeScript class member without `private` is
public. Go methods had exactly this gap, it is closed in
`impl/codegraphsqlite`, and the TypeScript half is named in the plan and not
yet closed, because closing it changes the observed surface of a fixture the
live tests of phases 4 to 9 are written against. The measure is reported as it
comes out of each run rather than adjusted to flatter it.

**The local model is much weaker than the hosted one, and that is the point of
running both.** `pi` over a local `ternary-bonsai-2-27b` reached 100% interface
recall but 85.7% precision and 84.2% requirement anchoring. Two causes:

- `todo/cmd-todo` has one file and no exported function (its `main` is
  unexported). The local model declared ten interfaces for it — an API it
  imagined — so the declared surface is entirely extra. `deepseek-claude`
  declared none, which is correct.
- `calc/calc` anchored three of its four requirements. The fourth named a code
  reference the observed facts do not answer to; `ParseDraft` drops such a ref
  and reports it as dropped rather than storing a claim nothing backs.

Neither is a harness fault. They are what a smaller model does with a context
it cannot verify, and they are the reason anchoring is measured separately from
recall: a spec can name every symbol and still be anchored to nothing.

**The unknown codebase is where the loop met code nobody wrote for it.**
`../kcp-libs` is 34 contexts of real Go across `common/`, `abc/`, `impl/` and
`factory/`, cloned read-only into the controller's cache and populated by one
`Repository` manifest. All 34 summarized, in 231 seconds, with 100% requirement
anchoring and every spec passing the validator. Interface recall was 88.7% and
precision 89.1%: on real packages the model both over-declares (an `impl/metrics`
context declaring 14 interfaces where 13 are observed, three of which the facts
answer to) and under-declares elsewhere, and both are visible in the Code ->
spec table. Nothing about those numbers is a fault in the loop; they are what
reading a codebase nobody wrote for the harness looks like.

It also found defects the three fixtures never could, because they were written
by the same hand:

- Populating stopped outright on `abc/archyaml`, where two files each declare a
  `Section` and a type and a struct are both named `Node`. The observed facts
  carried both, and every list they are written to is keyed by name, so the API
  server refused the status and that context could never populate at all.
  `abc/sync` now keys the observed surface by name, which is what the wire
  format always said it was. A real codebase had two symbols of one name in the
  first ten directories.
- An earlier run of the same command left one context, `abc/cache`, unsummarized
  and the repository `Failed`. Two of its types have a method named `Add` and
  two have one named `ByIndex`; the model listed the whole public surface,
  honestly, and the draft parser refuses two interfaces of one name — as it
  must, because the declared surface is keyed by name too. The committed run is
  the one where the model's answer did not name both, so all 34 populated; the
  limitation is real either way and is named below.

## Both of those gaps are closed

`run-2026-10-03-kcp-libs-qualified.md` is the same command over the same
checkout after the fix, and it is the evidence.

- **The TypeScript half of the observed surface.** Class members are public by
  default; the index reports them unexported. `impl/codegraphsqlite` now applies
  the language's own rule — a member is public unless it says `private` or
  `protected`, or carries a `#` name, and a member of a class that is not
  exported is not observed at all. The `greet` fixture carries the class case.
- **A surface keyed by name could not describe a package where two types share
  a method name**, which is ordinary Go. An interface is now keyed by its
  qualified name for a method, `Type.Method`, and by its bare name otherwise.
  `abc/cache` — the context whose types each offer `List`, `GetByKey` and
  `ByIndex`, and the one the earlier run left unsummarized — is 20 of 20 in the
  new run. Recall and precision are 98.8% and 98.2% against 88.7% and 89.1%. A
  spec stored before the key changed is migrated by the ingest itself when the
  facts name exactly one candidate; a name two types share is left for a person.

## What is left

- **A codebase of 34 contexts takes about four minutes to populate** at two
  concurrent summaries, and the new run took 231 seconds, the same as before.
  The wall time is reported; nothing here is tuned for it.
- **The Bash half of the scope guard is best effort.** A path reached through a
  shell variable, a relative walk, a hard link or a case alias is not caught;
  only what resolves inside `SPECD_CLM_ROOT` is allowed. The file tools are
  exact.
