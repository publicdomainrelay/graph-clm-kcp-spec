# Survey run 3: validation after the fix pass

Third recorded run, on a fresh database, to check that the defects found in runs
1 and 2 are actually gone. The procedure is written up in
`skills/codebase-survey-eval/SKILL.md`.

## Setup

Fresh ArcadeDB 26.9.1 in an empty working directory, database `clm` created at
boot, session key `eval-validation-1`, same prompt and model as run 2.

## Grades against the evaluation checklist

| Check | Run 2 | Run 3 |
| --- | --- | --- |
| Managed zone vs model zone | 5,433 vs 1,317 tokens (4.1×) | **1,303 vs 1,126 (1.2×)** |
| Context file total | 6,750 tokens | **2,429 tokens** |
| Turn summaries at exactly 400 chars | 10 of 10 | **0 of 28** |
| `PiFile` nodes vs distinct paths | 41 / 23 | **23 / 23** |
| Duplicate `REFERENCES` edges | present | **252 edges, 252 unique** |
| Concepts with code attached | 11 of 12 | **12 of 12** |
| `PiCodeRef` nodes | 206 | 144 |

Every failure signature in the checklist is gone.

## What the run produced

12 concepts, 144 code references, 252 `REFERENCES` edges, 23 files, 28 turns.
Total context cost fell by two thirds, almost all of it in the managed zone.

Turn summaries are now readable, and they show what the agent actually did:

```
turn 0: Let me start by reading the live context file and exploring the
        repository. | called: hydradb_context, bash
turn 2: Let me read the main files. Start with Go main, README, then
        pi-hydradb-clm sources. | called: read
```

The agent's closing summary opens with the architecture in its own words —
"context file is brain, graph is index" — and the reference distribution matches
the concepts: `context-doc.ts` 50, `extension.ts` 52, `graph.ts` 22, `ids.ts` 18,
`target.ts` 14, `main.go` 10.

## Fix applied after the run

The unresolved-reference list in the managed zone still carried non-references:
an absolute filesystem path, `.codegraph/codegraph.db`, four ASCII graph
diagrams, `npm test`, `--test-force-exit`, and `session.dispose()`. These come
from taking every backticked token as a candidate.

`isReferenceCandidate` now requires a token to start with an alphanumeric or
underscore, reject whitespace, reject a leading dash, and reject absolute paths,
none of which can be a repo-relative CodeGraph id. Against this run's context
file it drops 9 of 61 backticked tokens, all of them genuinely not code
references.

## Remaining known gaps

- The CodeGraph index in this repository predates `codegraph.ts`,
  `context-file.ts`, and `skills/`, so references to them land in the unresolved
  list. That is correct behaviour, and the managed zone says to run
  `codegraph sync`; it is not a defect in the extension.
- The model zone of the context file has no size cap. Its size is now reported
  in the injected header (model zone, index, total) so the agent can see when it
  is writing too much, but nothing truncates it. Truncating the agent's own
  notes would defeat the design, so this is deliberate.
