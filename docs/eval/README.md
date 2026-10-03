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
| unknown codebase | `claude-mod` | `../kcp-libs`, populate only | `run-2026-10-03-kcp-libs.md` |

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
first.

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

**Interface precision is 97.1% live, and the model is right.** The one context
below 100% is `greet`: the summarize declared `Greeter.greeting`, which is
public TypeScript — a class member with no `private` — and which the index
reports `is_exported: false`. Go methods had exactly this gap and it is closed
in `impl/codegraphsqlite`; the TypeScript half is named in the plan and not yet
closed, because closing it changes the observed surface of a fixture the live
tests of phases 4 to 9 are written against. The measure is reported as it is
rather than adjusted to flatter the run.

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
`Repository` manifest. 33 of the 34 summarized, in 261 seconds, with 100%
requirement anchoring and every spec passing the validator. Interface recall
was 85.7% and precision 89.1%: the model over-declares on real packages (a
`metrics` package declaring 14 interfaces where 13 are observed) and
under-declares on others, and both are visible in the Code -> spec table.

It also found two defects the three fixtures never could, because they were
written by the same hand:

- Populating stopped outright on `abc/archyaml`, where two files each declare a
  `Section` and a type and a struct are both named `Node`. The observed facts
  carried both, and every list they are written to is keyed by name, so the API
  server refused the status and that context could never populate at all.
  `abc/sync` now keys the observed surface by name, which is what the wire
  format always said it was. A real codebase had two symbols of one name in the
  first ten directories.
- One context, `abc/cache`, did not summarize. Two of its types have a method
  named `Add` and two have one named `ByIndex`; the model listed the whole
  public surface, honestly, and the draft parser refuses two interfaces of one
  name — as it must, because the declared surface is keyed by name too. The run
  is reported with the failure rather than retried until it passed.

## What is left

- **The TypeScript half of the observed surface.** Class members are public by
  default; the index reports them unexported. The Go half is fixed, the
  TypeScript half is not, and the 97.1% above is the size of it.
- **A declared surface keyed by name cannot describe a package where two types
  share a method name**, which is ordinary Go. The model's answer was right and
  the format cannot hold it; `agent.ParseDraft` fails the whole summarize
  rather than dropping half of it silently, which is the designed behaviour but
  not a useful one here. A per-receiver spelling of an interface name, or a
  reported drop, is the shape of the fix.
- **A codebase of 34 contexts takes four minutes to populate** at two
  concurrent summaries. The wall time is reported; nothing here is tuned for
  it.
