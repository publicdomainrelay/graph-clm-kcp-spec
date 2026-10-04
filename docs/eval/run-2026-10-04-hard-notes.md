# What the 2026-10-04 live runs mean

Handwritten, and it reads the two machine-written reports beside it:
`run-2026-10-04-hard.md` (claude-mod, the full measure set) and
`run-2026-10-04-pi.md` (pi, the same fixtures over the same hosted provider).
The numbers in this file are copied from those reports and from the
`-compare.md` files; nothing here is an estimate.

## The run discriminates now

Phase 10's review found the eval could not tell a live run from the scripted
baseline: both scored 100% on every measure. The comparison now says
`not discriminating: no` for both hosts — 6 of 11 shared measures differ for
`claude-mod`, 5 of 11 for `pi` — and the scripted baseline is still 100% on
everything it can take, which is what makes it a baseline rather than a
participant.

| measure | scripted | claude-mod | pi |
| --- | --- | --- | --- |
| spec -> code pass rate (verify + acceptance) | 100.0% (14) | 93.3% (15) | 93.3% (15) |
| code -> spec facts stated (judged) | 100.0% (9) | 95.9% (9) | 93.3% (9) |
| spec sufficiency | not measured | 100.0% (12) | not measured |
| drift (a human code edit) | 100.0% (4) | 50.0% (4) | 75.0% (4) |
| interface precision | 100.0% (9) | 81.8% (11) | 90.0% (10) |
| requirement anchoring | 100.0% (12) | 95.0% (12) | 100.0% (12) |

## The failure modes, in the order they matter

**A model changed the specification and left the code alone.** `ledger`'s
`remove-save` deletes `Store.Save` from the spec and expects the method to go
with it. Both hosts carried exactly the intended delta — one removed
interface — and both passed `go test ./...` and the hidden acceptance test,
and both left `func (s *Store) Save` in `storage/json.go`. The scenario failed
thirty seconds later with `the observed surface is wrong: still observed
Store.Save`. Nothing else in the harness can see this: the change's own gate
passes because the method and the test that used it agree with each other, and
the acceptance test only exercises `Load`. The control is `rename-sum`, which
also asserts a removal and passes in both runs, so the check is not refusing
removals as such. This is the failure the phase exists to make visible, and it
is left in the report rather than tuned away.

**The spec named the symbol and not the behaviour.** Drift is where the two
hosts differ most: `claude-mod` scored 2 of 4, `pi` 3 of 4. Where a person
added a function, both wrote the interface and the delta entry and stated the
behaviour (1 of 1 judged fact). Where a person *changed* a behaviour without
moving an interface — `human-rejects-negative`, `Validate` now refusing a
negative amount — neither run's spec said so, and the device that catches it
is the judged fact, because recall and precision over that context are 100%.
That is the whole argument for grading prose: a spec can name every symbol and
say nothing about any of them.

**A context with nothing exported is where a model invents a surface.**
`ledger/cmd-ledger` and `todo/cmd-todo` are `main` packages: nothing is
exported, so nothing is observed. `claude-mod` declared four interfaces for
each, and `pi` one for `calc/cmd-calc`. Recall cannot fall — there is nothing
to miss — so the cost lands in precision (81.8% and 90.0%) and in anchoring
(40.0% on `cmd-todo`). A context that declares *and* observes nothing is
excluded and counted; this is not that case, and the column is where it shows.

## Two harness defects the run found

**The pi host's CLM bridge named no workspace.** `clm/adapters-node` called
`specctl clm render|apply|report` without `--workspace`, so every call landed
on specctl's own default workspace, where the context of the run does not
exist. The bridge threw, an empty catch swallowed it, and the scenario read as
a model that changed nothing. The fix passes the scope the environment names
(what `cc-clm-mod`'s own bridge has always done), `ClmHost.finish` now reports
a failed apply instead of dropping it, both hosts print it, and a failed apply
no longer marks the edit as applied, so the next turn tries again.

**A skipped scenario counted as a failure.** The scripted baseline cannot take
the CLM scenario; the report marks it `skipped` and leaves it out of every
measure, but `specctl eval` still exited 1. A clean baseline read as a red run,
and in a chain of runs it stopped the next one from starting. Fixed, with a
unit test.

## What is still not measured

- `pi` has no sufficiency measure in its report. Twelve rebuilds were left to
  the `claude-mod` run; the measure prints `not measured` with 0 samples rather
  than a number nobody took.
- The TypeScript half of the observed surface is closed, but the graph's share
  of the context bundle when the budget is tight is not measured at all.
- One live run is one sample. The differences between the two hosts here —
  drift 50% against 75%, precision 81.8% against 90.0% — are the same model
  answering the same prompts twice, and they should be read as variance until
  more runs say otherwise.
