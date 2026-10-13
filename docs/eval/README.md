# Evaluating the specs and the context

The harness measures two things:

- **The specs**: the quality of the intent, requirements and interfaces the
  model writes from code, and the quality of the code an agent writes from a
  spec.
- **The context**: how much of the right context a pack puts in front of a
  model.

## Run it

```bash
# one Repository manifest per fixture, then one spec edit per scenario
specctl eval --fixtures fixtures --agent claude-mod --out ~/.tmp/eval.md

# the scripted baseline: no model, no network
specctl eval --fixtures fixtures --agent scripted --out ~/.tmp/eval.md

# the context, on the repository's own history
specctl eval history --repo . --commits 50 --out ~/.tmp/history.md
```

`specctl eval` needs a running instance (`specctl up`: kcp is where the objects
land). It uses the workspace `root:specs-eval`, or `$SPECD_EVAL_WORKSPACE`, so a
second checkout never disturbs the objects of another run. Every report is
written as Markdown with a JSON file of the same name beside it, so two runs
compare.

`specctl eval history` needs a graph with a current search index
(`specctl graph rebuild` refills it). It reads the checkout only.

## Models

DeepSeek only:

```bash
specctl eval --agent claude-mod     # deepseek-claude with cc-clm-mod
specctl eval --agent pi             # pi coding agent, --provider deepseek
specctl eval --agent scripted       # a YAML scenario; no model
```

No local model. A number measured against a local model describes that model,
not this system.

## What the spec harness measures

| Measure | Question |
| --- | --- |
| interface recall, precision, F1 | Does the spec name the interfaces the code has? |
| anchoring rate | Does every claim cite a CodeGraph id that resolves? |
| validator pass | Do the specs satisfy the schema and the rules? |
| round-trip Jaccard | Does re-reading the code after a realize change the spec? |
| delta precision | Do the recorded deltas name the properties that changed? |
| files outside the context | Did a realize touch a file another context owns? |
| spec to code pass rate | Does an agent reach the spec from the code? |
| judged facts | Does a judge agree with the model's claims? |
| drift | Does a code change show up as a delta, not as silence? |
| spec sufficiency | With the bodies stripped and the tests hidden, can the spec rebuild the code? |

## What the context harness measures

For each commit of a repository:

- the **query** is the commit subject;
- the **gold set** is the contexts that own the files the commit touched
  (`specctl arch owner`).

| Measure | Meaning |
| --- | --- |
| recall@k | How many gold contexts the pack names, over its neighbor and claims sections |
| precision | How many contexts the pack names are gold |
| tokens | The pack's own estimate |
| dropped | How many sections the budget dropped |

A commit that no context owns is skipped, never counted as a miss.

## Read a report

Each run prints one line per fixture or commit, then a summary. Compare two
runs with the JSON files:

```bash
diff <(jq -S . ~/.tmp/a.json) <(jq -S . ~/.tmp/b.json)
```

A single number rarely means much on its own. Watch a change in recall and
precision together, and watch the dropped count: a pack that drops sections is a
budget problem, not a retrieval problem.

## Limits

- The benchmark grades structure, not prose: a correct requirement written badly
  scores the same as one written well.
- A terse commit subject ("realize cmd-specctl: +1") gives the query no term to
  match, so it scores zero. Read the scored-commit count next to the mean.
- The spec suite counts what it can see. It does not measure whether a
  requirement is the right requirement.
