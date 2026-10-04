# graph-clm-kcp-spec

Two way state sync for spec driven development.

kcp (a Kubernetes control plane, no nodes) holds the specs. A spec is a custom
resource: `spec` is the desired state of a part of a system, `status` is what
the code is observed to be. A reconciler drives the code to the spec, exactly
like a Deployment drives pods. A graph database indexes specs, requirements and
code references so a language model gets a small, relevant neighborhood instead
of a 3000 line document.

```
            code -> spec  (ingest, summarize, drift)
  git repo  ------------------------------------------>  kcp (CRs = specs)
  (code)    <------------------------------------------  (desired state)
            spec -> code  (realize: agent edits code, tests gate)
```

The design lives in [`docs/plans/0001-kcp.md`](docs/plans/0001-kcp.md). That
plan is the source of truth; this file says how to run what is built.

## Why

**The problem: specs rot.** Code changes faster than anyone documents it. The
open architecture document of the sibling `deno-kcp` repo
(`.tools/open-architecture/arch.yaml`) shows it: 84 hand-written commits, over
3000 lines, and a separate 674-citation truth sweep just to find where the
document had drifted from the code. A hand-kept spec is stale soon after it is
written, and without a true spec neither people nor AI agents can reason about
a codebase or change it safely.

**The idea: spec is desired state, code is observed state.** That is the
Kubernetes model. kcp is a Kubernetes control plane with no nodes: pure state
management, with reconcile loops, `status`, conditions, generations and
multi-tenant workspaces. So kcp holds specs as custom resources, and
controllers keep specs and code converged. Drift stops being something a
manual audit finds; it is a reconcile signal.

```mermaid
flowchart LR
    subgraph repo["git repo (observed state)"]
        code["code"]
    end
    subgraph kcp["kcp workspace (desired state)"]
        sc["SystemContext CRs<br/>spec + status"]
        ch["SpecChange CRs<br/>delta + audit trail"]
    end
    code -- "code -> spec<br/>ingest, drift, summarize" --> sc
    sc -- "spec edit = delta" --> ch
    ch -- "spec -> code<br/>agent edits, tests gate" --> code
```

**Two way sync.**

- **code -> spec:** code changes, the controller sees drift, and a model
  updates the spec (intent, requirements, interfaces). Every claim is anchored
  to real CodeGraph ids.
- **spec -> code:** a person or a model edits a spec, the controller computes a
  structured delta, an agent changes the code to match, and tests gate the
  commit.

**Why a graph database.** A model cannot read 3000 lines for every task.
A graph database indexes specs, requirements, interfaces and code references,
so each task gets a small, relevant neighborhood: the context, its upstream, overlay
and orchestrator, and the code it points at. The graph is a derived index; kcp
stays the only source of truth, and the graph can be rebuilt from kcp and
CodeGraph at any time.

The engine is not the point: CLM + graph DB is. Any Bolt/openCypher engine
that runs the portable Cypher subset works. ArcadeDB is the default because it
is the more Cypher compliant of the two tested engines (38 of 40 probe cases
against HydraDB's 26 of 40, `pi-hydradb-clm/docs/cypher-conformance.md`).

**Why a context language model (CLM).** The model edits its own context
document, which becomes its working memory. The `pi-hydradb-clm` extension
syncs that document to kcp, so what the model learns or decides while it works
becomes a spec change automatically.

```mermaid
flowchart TB
    human["person<br/>kubectl / specctl"]
    pi["model in pi<br/>+ pi-hydradb-clm"]
    mod["model in Claude Code<br/>+ cc-clm-mod"]
    ctx[".specs/context/&lt;name&gt;.md<br/>model zone + managed zone"]
    cli["specctl clm<br/>render / apply / report<br/>the one state bridge"]
    kcp[("kcp<br/>SystemContext / SpecChange<br/>source of truth")]
    specd["specd controller"]
    gdb[("graph DB over Bolt<br/>ArcadeDB default, HydraDB option<br/>derived index")]
    cg[("CodeGraph index<br/>code facts")]
    repo["git repo"]

    human -- "edit spec" --> kcp
    pi -- "edits" --> ctx
    mod -- "edits" --> ctx
    ctx -- "delta, origin=clm" --> cli
    cli -- "PATCH spec, report progress" --> kcp
    cli -- "TOUCHED / OCCURRED edges" --> gdb
    kcp -- "render spec + status" --> cli
    cli -- "the context file" --> ctx
    kcp -- "watch" --> specd
    specd -- "status, SpecChange" --> kcp
    specd -- "realize: agent + mod + verify + commit" --> repo
    repo -- "codegraph sync" --> cg
    cg -- "observed facts" --> specd
    specd -- "index" --> gdb
    gdb -- "neighborhood for prompts" --> pi
```

**The end goal.** Spec driven development in which the spec is never stale:

1. Point at an unknown repository, apply one `Repository` manifest, and get a
   full set of specs.
2. Develop by editing specs (by hand or by a model); deltas drive the code
   changes.
3. Specs are machine-checkable and current, in the open architecture shape
   (`upstream`, `overlay`, `orchestrator`), across the publicdomainrelay repos,
   so people and agents work from one shared, true model of the system.

```mermaid
sequenceDiagram
    participant U as person or model
    participant K as kcp
    participant S as specd
    participant A as agent
    participant G as git repo
    U->>K: apply Repository (source, populate)
    S->>G: clone, codegraph index
    S->>K: SystemContext per partition (status.observed)
    S->>A: summarize (code -> spec)
    A-->>S: intent, requirements, interfaces
    S->>K: spec written (origin=ingest)
    U->>K: edit spec (one requirement)
    S->>K: SpecChange{SpecToCode, delta}
    S->>A: realize delta in worktree
    A->>G: edit files
    S->>G: verify, commit, merge
    S->>K: re-ingest, CodeSynced=True
```

## Status

All ten phases are done: **kcp holds specs, code becomes facts in `status`
and in the graph, a hand written `arch.yaml` round trips through kcp, `specd`
keeps the facts, the conditions and the work queue true to the code, an agent
turns the code back into a spec, a spec edit becomes a structured delta that
drives an agent to change the code under a test gate, one `Repository` manifest
populates a codebase kcp has never seen, the CLM loop is a library with two
hosts — the `pi-hydradb-clm` extension and a Claude Code mod — so the model that
realizes a change reports into the same state the controllers watch, the API
is multi-tenant: an APIExport in a provider workspace, tenant workspaces that
bind it, one `specd --mode export` that reconciles it all, and a `.specs/` git
mirror so a pull request carries the spec and the code together, and the whole
loop is measured: `specctl eval` runs five fixtures and an unknown real
codebase through it — including what the spec makes an agent able to rebuild,
what a human's code edit makes the spec say, and whether the model's answer
beats the scripted baseline at all — and reports what actually happened.**

- API group `specs.publicdomainrelay.dev/v1alpha1`, kinds `Repository`,
  `SystemContext`, `SpecChange`, namespaced, with a status subresource and
  printer columns.
- `specctl apply -f | get | delete` against a kcp workspace.
- `specd` watches the three kinds, re-ingests a `Repository` when its git HEAD
  moves, keeps `SpecValid`, `CodeSynced` and `Drifted` true to the code, and
  opens exactly one `SpecChange` per direction of work.
- `Repository.spec.source` is a local `path` or a `git: {url, ref}`; specd
  clones a git source into `--cache-dir` and follows it, and
  `Repository.spec.populate` says how to split the tree (`directory` or
  `package`), what to skip (`include`/`exclude` globs) and whether to send an
  agent over every new context (`summarize`, `agent`). `status.phase` runs
  `Cloning -> Indexing -> Populating -> Populated`, or `Failed`, and
  `status.contexts` counts total, summarized and failed.
- `specctl ingest --repo <path>` applies a `Repository` manifest and waits for
  `Populated`, so the CLI and the controller are one code path.
- Ingest runs CodeGraph over a working tree, partitions it, and fills
  `status.observed` (`files`, `interfaces` with signature, file, line and
  CodeGraph id, and a `fingerprint` over both) plus the conditions `SpecValid`,
  `CodeSynced` and `Drifted`. Ingest is idempotent: a second run changes no spec
  and writes no status.
- `specctl import-arch <arch.yaml>` turns every node of an open architecture
  document into a `SystemContext`, and `specctl export --format arch` writes it
  back; the ids, the refs and the preserved node bodies survive the trip.
- `specctl graph neighbors|rebuild` read and write the spec graph in ArcadeDB
  (the default backend) or HydraDB over Bolt, using only the Cypher core subset
  both engines run; `--bolt-backend hydradb` switches the defaults.
- `specd --agent claude|scripted:<file>` works a `CodeToSpec` change off: it
  builds the context bundle, asks the agent, validates the answer, writes
  `intent`, `requirements` and `interfaces` with the `origin: ingest`
  annotation, moves the synced baseline so `Drifted` goes False, and writes the
  context document. The write never looks like a human edit, so it raises no
  `SpecToCode` change; the loop is proved in a unit test and in a live run.
- `specctl ingest --summarize --agent <kind>` fills the spec of every context
  whose intent is still empty, without a controller running.
- Each context gets a CLM document at `<repo>/.specs/context/<name>.md`: the
  model's prose and a fenced `yaml spec` block above
  `<!-- SPECD_MANAGED_BEGIN -->`, and the code refs the index resolved below it,
  regenerated on every summarize. One format for both directions: the file the
  code -> spec path writes and the file a host inside a model renders parse each
  other.
- Every list in every CRD is a keyed list: `requirements` by `id`, `interfaces`
  and the observed interfaces by `name` and `conditions` by `type` are
  `x-kubernetes-list-type: map`, and `codeRefs`, `overlay`, `dependsOn` and
  `introduces` are sets. A server side apply that names one entry therefore
  edits that entry and leaves the rest of the list, and every other field,
  exactly as it was.
- `status.realizedSpec` and `status.syncedObserved` snapshot the last realized
  spec and the last synced fact set, so a delta is computable from kcp alone in
  both directions. `abc/delta` is the pure `Diff`/`Apply` algebra
  (`DiffObserved`/`ApplyObserved` for facts) and its stable JSON is pinned by
  golden files in `testdata/delta/`; `SpecChange.spec.delta` carries it and
  `specctl get specchange` prints the `+2 ~1 -1` summary.
- `SpecToCode` is one worktree: branch `spec/<context>/<hash8>` off the managed
  branch, the agent edits files only, `Repository.spec.verify` gates the commit,
  and a zero exit lands the commit as `specd <specd@localhost>` on the managed
  branch with a re-ingest that adopts it in one status write, so the tool's own
  work raises no opposite change. A failing verify keeps the branch, leaves the
  spec and the managed branch untouched and hands the output to the next
  attempt.
- `Repository.spec.agent.kind` selects the agent per repository (`claude`,
  `claude-mod`, `pi` or `scripted:<file>`) and wins over the controller's
  `--agent`.
- `clm/core` is the CLM logic that is the same in every host, as pure TypeScript
  with no `node:` import and no I/O: the context document (a prose intent plus a
  fenced spec block above a managed zone of resolved refs), the token budget,
  FNV-1a ids, the Cypher row builders, a mirror of `abc/delta` validated against
  the same golden files, and the ports `StateBridge`, `FileStore` and `Runner`.
  `clm/adapters-node` is the Node side of those ports, including the state
  bridge over `specctl clm`.
- `cc-clm-mod` is the Claude Code host: a plugin of function hooks that renders
  the context at session start, injects it before every model request, reports
  every file a tool touched, and applies the model zone at the end of a turn.
  `clm/core` is vendored into it by `npm run build:mod`, with a test that fails
  when the vendored copy is stale.
- `specctl clm render|apply|report` is the one state bridge, so kcp access, the
  delta authority and the graph writes have one implementation (Go) that both
  hosts and every language reach the same way. `apply` writes with
  `origin: clm` and folds its edit into the context's running `SpecToCode`
  change instead of moving the target that change is working to, so a realizing
  agent can never spawn a change for itself.
- `SpecChange.status.progress` is a bounded list of what the host reported while
  the change ran (turn, tool, files, note, time), and `report` writes the
  matching `TOUCHED` and `OCCURRED` edges into the graph, so
  `kubectl get specchange -w` shows the work while it happens.
- `deploy/apiresourceschemas/` is generated from `deploy/crds/` by
  `impl/schemagen`, and a test fails when the two grow apart, so the API a
  tenant binds and the API the single workspace serves cannot diverge.
  `deploy/install-specs-provider.sh` creates `root:specs-provider`, applies the
  schemas and the APIExport `specs.publicdomainrelay.dev`;
  `deploy/bind-workspace.sh <ws>` creates a tenant workspace, binds that export
  and writes its kubeconfig.
- `specd --mode export` watches every workspace bound to the export through the
  APIExport virtual workspace (one dynamic informer per kind at
  `<endpoint>/clusters/*`). Each object carries its `kcp.io/cluster` annotation,
  so the controller writes every status and every `SpecChange` back to the
  logical cluster the object came from: the specs stay per workspace, the API is
  shared. Plain workspace mode is the default and is unchanged.
- `specctl sync --repo <path> --direction pull|push|both` mirrors the specs as
  one YAML per `SystemContext` under `<repo>/.specs/` (spec only, canonical key
  order, no status). A file and kcp are in conflict only when both moved since
  the last sync recorded in the `synced-hash` annotation; the sync then refuses
  and names the hashes, and `--prefer kcp|git` says which side wins. A push
  writes with `origin: git`, so the controller reads it as the desired state
  change it is.
- `specd --specs-mirror` writes each context's `.specs/<name>.yaml` into the
  realize worktree before the commit, so one commit carries the spec and the
  code together.
- `impl/piagent` is the pi host over the same agent contract, with the npm
  package as its default command, and the pi extension writes
  `(PiMemory)-[:SPECIFIES]->(SpecRequirement)` for the requirements a remembered
  code reference anchors to.
- `fixtures/` holds five repositories with tests — `calc` (a Go library),
  `greet` (a Deno/TypeScript module), `todo` (a Go JSON service over
  `net/http`), `shared` (two Go types that share a method name) and `ledger`
  (a Go double-entry service with 12 source files over four contexts) — and
  each carries `scenarios/*.yaml`: a spec patch to apply, the hidden
  acceptance tests that grade it, the interfaces the change should make
  observable, and the deterministic realize steps. The ledger scenarios are
  the hard ones: a delta that spans two contexts, a requirement-only
  behaviour change, a removal and a rename, and one driven through the CLM
  path (`via: clm`) where the model, not the harness, changes the spec. A
  removal is spelled `{"$patch": "delete"}`, because a patch merged by key
  can add and change an entry but cannot take one away. The harness files are
  named in the fixture's `.gitignore`, which the index honours as well as git,
  so a fixture stays a working tree and a scenario file can never become a
  context of its own.
- Each fixture also carries `expected.yaml`, the behavioural facts a correct
  spec must state ("Balance returns the sum of the entries of one account"),
  and `drift/*.yaml`, a code change committed the way a person commits one:
  the drift raises a `CodeToSpec` change and the spec it writes is graded on
  the interfaces it gained and lost, the facts its prose states, and the
  interface entries of its diff. That is goal 5 — human code edit, drift,
  code -> spec, graded spec — measured rather than described.
- `specctl eval --fixtures fixtures [--agent claude|claude-mod|pi]
  [--scenarios <glob>] [--out docs/eval/run-<date>.md]
  [--baseline <a previous report.json>]` measures the loop, one fixture at a
  time: it copies the tree into a fresh git repository, applies one
  `Repository` manifest and lets the controller populate it, then applies
  each scenario's spec edit, waits for the `SpecToCode` change, and grades
  the result with the verification command and the hidden acceptance tests,
  which are copied in only for grading. It resets the tree and the spec
  between scenarios and defaults to the workspace `root:specs-eval`, so a run
  never disturbs the objects another suite owns. `--baseline` puts a live run
  beside the scripted one and flags a run that is equal on every measure as
  **not discriminating**.
- The measures are pure and unit tested in `abc/eval`. Every measure carries
  the sample count behind it: with no samples it prints `not measured`, never
  0% or 100%, and a context with an empty surface (or a scenario the run
  could not take) is excluded from the mean and counted by name. The code ->
  spec score is the share of the fixture's expected facts the spec states,
  graded per fact by a model judge with a fixed rubric (`impl/judge`) and,
  under the scripted baseline, by a deterministic keyword fallback. Spec
  sufficiency is the strongest test: `impl/stripbodies` removes every
  implementation body (Go through `go/ast` to `panic("unimplemented")`,
  TypeScript to a throw), the tests are hidden, the agent rebuilds the code
  from the spec alone, and the original tests run again. Alongside those are
  interface recall, precision and F1, requirement anchoring, validator pass,
  round trip Jaccard, delta precision, the files a realize touched outside
  its context, attempts, wall time, and the code -> spec facts a drift
  produced. The report is a markdown table and the same numbers as JSON
  beside it, and a `--baseline` run adds a side by side table saying whether
  the eval discriminated at all.
- `fixtures/external/kcp-libs` is a fixture that is not in this repository: one
  `Repository` manifest with a `git` source clones a read-only checkout of the
  sibling `kcp-libs` into the controller's cache and populates it, so the
  measures are also taken on a real codebase nobody wrote for the harness.
- A Go method is exported when its name is capitalised, which the index does
  not say, so the reader applies Go's own rule; without it a package whose API
  is mostly methods is observed as an almost empty surface.
- Live tests that round trip a `SystemContext` through a real kcp, that ingest a
  real git working tree twice and check the graph on both backends, that take
  the open architecture document through kcp and diff the two models, that drive
  the controller through drift and both directions of `SpecChange`, that patch
  one requirement of a keyed list without rewriting its list, that realize a
  spec edit through a worktree, a verify gate and a re-ingest on both the
  passing and the failing path, that ingest
  a Deno/TypeScript module, not only Go, and that run `deepseek-claude` over
  `fixtures/calc` and hold its answer to the same contract the scripted agent is
  held to.

## Requirements

`go` (1.26 or newer), `kcp` v0.33, `kine`, `kubectl`, `codegraph` on `PATH` for
ingest, and `git` for a `Repository` and for `specd`. The graph needs a Bolt
endpoint and defaults to ArcadeDB on `bolt://127.0.0.1:7688` (user `root`,
password `clm-arcadedb-root`, database `clm`); HydraDB on
`bolt://127.0.0.1:7687` (password in `/tmp/hdb/token`) is an option, selected
with `--bolt-backend hydradb` or `SPECD_BOLT_BACKEND=hydradb`. The graph
commands need one;
`apply`, `get`, `delete`, `import-arch` and `export` work without one (pass
`--no-graph` to `ingest` and `import-arch`).

`--agent claude` needs a model command on `PATH`: `deepseek-claude` by default,
or whatever `--agent-command` and `--agent-args` name. Only
`SPECD_REQUIRE_LIVE_MODEL=1` runs it; everything else, the whole `specd` loop
included, runs `--agent scripted:<file>` and needs no model. `deno` is needed
for the TypeScript live test, which runs `fixtures/greet`'s own tests before it
indexes the module, so the fixture is known to be a real working tree and not
only something `codegraph` can parse.

## Quick start

```bash
make kcp-up          # kcp on 6447, kine on 23797, state in .kcp-specd/
make generate-schemas # rewrite deploy/apiresourceschemas/ after a CRD change
make example-phase1  # apply examples/calc/specs.yaml and read it back
make example-phase2  # ingest fixtures/calc, fill status.observed, write the graph
make example-phase3  # import testdata/open-architecture/arch.yaml and export it back
make example-phase4  # run specd, commit a change, watch drift and the SpecChange
make example-phase5  # run specd with an agent, watch the spec fill itself in
make example-phase6  # edit the spec, watch the agent land the code and the tests pass
make example-phase7  # one manifest populates a codebase kcp has never seen
make example-phase8  # the mod path: render, apply, fold, report
make demo            # every phase, in order, against one cluster
make kcp-down        # stop the cluster this repo started
```

`make example-phase1` prints a table of the contexts it applied, the same
objects through `kubectl`, and one `SystemContext` as YAML.
`make example-phase2` applies the same example, indexes `fixtures/calc` with
CodeGraph, ingests it, prints the observed facts and the conditions, and shows
one hop of the graph around `calc` in ArcadeDB, the default backend, and in
HydraDB, the option.
`make example-phase3` imports the archived open architecture document, prints
one hop of the graph around `sc.deno-kcp` (by arch id), exports the document
back out of kcp, and runs the live round trip test. All three targets are
idempotent except that the phase 3 live test cleans up after itself, so run the
target again to put the imported contexts back.
`make example-phase4` copies `fixtures/calc` to a temporary working tree, starts
`specd`, commits a `Subtract` function, prints the `Drifted` condition and the
`CodeToSpec` change it raised, patches the spec of the same context, prints the
`SpecToCode` change, shows that no other resourceVersion moves for four
seconds, and stops `specd` with SIGTERM. It owns the `calc` and `cmd-calc`
names, so it deletes the `calc` Repository and those two contexts first and
last; run `make example-phase2` afterwards to put that example state back. The
controller watches every `Repository` in the workspace, so two Repositories
that name the same context would otherwise take turns writing its status.
`make example-phase5` copies `fixtures/calc` to a temporary working tree, starts
`specd` with the scripted agent, commits `Subtract`, and prints the spec the
agent wrote: the intent, the requirements and the interfaces, the `origin:
ingest` annotation, `Drifted=False` and zero `SpecToCode` changes. It prints the
context document the summarize left in the tree, then runs
`specctl ingest --summarize` over the same tree to show the CLI entry point,
and stops `specd` with SIGTERM. It owns the same `calc` and `cmd-calc` names, so
run `make example-phase2` afterwards to put that example state back.
`make example-phase7` builds a bare git repository out of `fixtures/greet` (a
tree kcp has never seen) and `fixtures/calc`, applies only
`examples/populate/repository.yaml`, and prints the phase, the context counts,
every summarized spec and the context document the agent left in the clone; the
controller clones, indexes, raises one `CodeToSpec` change per context and works
them all off with the scripted agent, with no other command.
`make example-phase2` and `make example-phase5` start `specd` around their
`specctl ingest`, because the CLI waits for the controller now.
`make example-phase6` copies `fixtures/calc` to a temporary working tree, applies
a `Repository` that names its own scripted agent, starts `specd` with no
`--agent` at all, adds one interface and one requirement to the `calc` context
with a server side apply, and prints the two entry delta the controller raised,
the worktree branch, the commit the agent made under `specd
<specd@localhost>`, the files it touched, the verify exit code, `CodeSynced=True`
and `Drifted=False`. `make example-phase6-failing` runs the same example with a
scenario whose `Subtract` cannot pass, so the change ends `Failed` with its
branch kept and the managed branch untouched. Both own the `calc` and `cmd-calc`
names.

The same steps by hand:

```bash
make build
make kcp-up

# the graph endpoint defaults to ArcadeDB on bolt://127.0.0.1:7688 (root /
# clm-arcadedb-root, database clm); for HydraDB instead:
#   export SPECD_BOLT_BACKEND=hydradb   # 7687, neo4j, token in /tmp/hdb/token

# specctl ingest applies a Repository and waits for Populated, so the controller
# has to be watching the workspace; start it in its own terminal
bin/specd --workspace root:specs --resync 2s
```

```bash
# ... and run the rest in another terminal. One ingest: apply the example
# contexts, then one Repository manifest, and read the observations back.
bin/specctl apply -f examples/calc/specs.yaml
bin/specctl ingest --repo fixtures/calc
bin/specctl get systemcontext calc -o yaml
bin/specctl graph neighbors calc

# the same neighborhood in HydraDB, rebuilt from kcp and codegraph
bin/specctl graph rebuild --bolt-backend hydradb
bin/specctl graph neighbors calc --bolt-backend hydradb

bin/specctl delete systemcontext calc

bin/specctl import-arch testdata/open-architecture/arch.yaml --repository deno-kcp
bin/specctl graph neighbors sc.deno-kcp
bin/specctl get systemcontext -o name | head
bin/specctl export --format arch --repository deno-kcp -o /tmp/arch-export.yaml

KUBECONFIG=.kcp-specd/specs.kubeconfig kubectl get systemcontexts
KUBECONFIG=.kcp-specd/specs.kubeconfig kubectl get specchanges

# one manifest populates a codebase kcp has never seen (make example-phase7
# builds the bare repository this url names)
KUBECONFIG=.kcp-specd/specs.kubeconfig kubectl apply -f examples/populate/repository.yaml
KUBECONFIG=.kcp-specd/specs.kubeconfig kubectl get repositories -o wide

# multi workspace: publish the API from a provider workspace, then bind tenants
make install-specs-provider
./deploy/bind-workspace.sh tenant-a
KUBECONFIG=.kcp-specd/tenant-a.kubeconfig kubectl get systemcontexts
# one controller reconciles every tenant bound to the export
bin/specd --mode export --provider-workspace root:specs-provider

# the specs beside the code: pull, edit, push, and refuse a real conflict
bin/specctl sync --repo /path/to/repo --direction pull
bin/specctl sync --repo /path/to/repo --direction both --prefer git
```

`ingest` reads the Bolt endpoint from the flags or the environment
(`SPECD_BOLT_BACKEND`, `SPECD_BOLT_URL`, `SPECD_BOLT_USER`,
`SPECD_BOLT_PASSWORD`, `SPECD_BOLT_PASSWORD_FILE`, `SPECD_BOLT_DATABASE`). The
backend fills the options no flag and no environment variable set, and ArcadeDB
is the default; `SPECD_BOLT_URL=` (set but empty) turns the graph off. The
`Makefile` exports `SPECD_BOLT_BACKEND=arcadedb`, so plain `make example-phase2`
needs no extra flags, and the graph is written where the index is written, which
is `specd`: pass the bolt flags to the controller, not to `specctl ingest`.

A relative `Repository.spec.source.path` resolves against the working directory
of whichever process reads it, so run the commands from the repository root. A
working tree that is not a git repository is still indexed: it has no commits,
so it cannot drift, and it is re-indexed when a caller asks with the
`specs.publicdomainrelay.dev/populate-request` annotation (which
`specctl ingest` always sets).

`specctl` talks to the workspace `root:specs` on the admin kubeconfig; pass
`--workspace`, `--namespace`, `--kubeconfig` or `--context` to change that. In
the multi workspace mode point it at a tenant instead:
`--workspace root:tenant-a` (or use the kubeconfig `bind-workspace.sh` wrote).
`specctl sync` derives the `Repository` from `--repo` when the path is the tree
a `Repository` resolved to, so the two sides cannot name different ones.
`deploy/install-specs.sh` also writes `.kcp-specd/specs.kubeconfig`, a
kubeconfig whose server already points at the workspace, so plain `kubectl`
works against it without extra flags.

## The API

| Kind | Purpose | Key fields |
| --- | --- | --- |
| `Repository` | a codebase under management, and the one manifest that populates an unknown one | `spec.source.path` / `spec.source.git`, `spec.branch`, `spec.verify`, `spec.agent`, `spec.populate` (partition, include, exclude, summarize, agent), `status.phase`, `status.contexts`, `status.resolvedPath`, the `Indexed` and `Populated` conditions |
| `SystemContext` | one spec node (one system context) | `spec.repository`, `spec.upstream`, `spec.overlay`, `spec.orchestrator`, `spec.dependsOn[]`, `spec.introduces[]`, `spec.intent`, `spec.requirements[]`, `spec.interfaces[]`, `spec.codeRefs[]`, `spec.arch` |
| `SpecChange` | one direction-tagged change, the unit of work | `spec.systemContext`, `spec.direction`, `spec.delta`, `spec.toSpecHash` / `spec.toCommit`, `status.phase`, `status.branch`, `status.commit`, `status.verifyExitCode`, `status.filesTouched` |

`SystemContext.status` carries the code facts (`observed.files`,
`observed.interfaces` with `signature`, `file`, `line` and `codegraphId`, and
`observed.fingerprint` over both), `observedCommit`, the synced baseline
(`syncedCommit`, `syncedFingerprint` and `syncedObserved`), `observedGeneration`,
`realizedSpecHash` and `realizedSpec`, and the conditions `SpecValid`,
`CodeSynced` and `Drifted`. `realizedSpec` is the last spec a realize or an
ingest acknowledged and `syncedObserved` the facts that baseline was taken from:
together they are the old side of either delta, so a delta is always computable
from kcp alone.

Every list in the CRDs is a keyed list: `requirements` by `id`, `interfaces` by
`name`, the observed interfaces by `name` and `conditions` by `type` are
`x-kubernetes-list-type: map`, and `codeRefs`, `overlay`, `dependsOn` and
`introduces` are `x-kubernetes-list-type: set`. `deploy/install-specs.sh` applies
them, and a server side apply edits one entry without rewriting the list.

Requirements carry `id` (unique in the context), `level` (`MUST`, `SHOULD` or
`MAY`) and `text`. Every `codeRefs` entry is a CodeGraph id: `file:`, `function:`,
`method:`, `type:` or `package:`. Context-to-context references are `self`,
`sc.<name>`, `up.<name>`, `ov.<name>` or `orch.<name>`, and an open architecture
id such as `sc.kind.denopod` is a reference too; it names the context
`sc-kind-denopod`, because the dots are part of the id.

`specctl apply` validates before it writes: duplicate requirement ids, an
unknown level, a malformed reference, a missing required field or a malformed
spec hash are rejected locally with the field path, and nothing is sent. A
field whose type is wrong is reported with its path too, for example
`spec.requirements.codeRefs`.

## Ingest: one manifest becomes code facts and specs

`specctl ingest --repo <path>` is a thin wrapper: it applies a `Repository`
manifest and waits for the controller to reach `Populated`. The work is
`impl/populate`, the same code path specd's `Repository` reconcile runs, so the
CLI and the controller cannot disagree about what ingesting means. (A `specd`
must be running; the controller owns the index, the graph and the agent.)

The pipeline:

1. resolves the source: a `spec.source.path` is used as it is, a
   `spec.source.git.url` is cloned into `--cache-dir` (`SPECD_CACHE_DIR`,
   `.kcp-specd/cache` by default) and fetched again on every resync, so a remote
   that moves forward is followed;
2. runs `codegraph init` (or `sync` when the index exists) on the working tree;
3. reads `.codegraph/codegraph.db` back through a pure Go sqlite driver. Ids are
   read, never computed;
4. partitions the tree: one context per directory that holds a source file
   (`calc/` becomes `calc`, `cmd/calc/` becomes `cmd-calc`, root files take the
   repository name) with `partition: directory` (the default), or one per
   package or module root with `partition: package`. `include` and `exclude`
   are globs over the repository-relative path, where `**` crosses directories
   and a pattern without a slash also matches a base name. Test files stay in
   `observed.files` but contribute no interfaces;
5. writes `Repository.status.headCommit` and `indexedCommit`, and for each
   context fills `status.observed` and the three conditions. The fingerprint is
   sha256 over the canonical JSON of the sorted files and interfaces, so the
   same tree always produces the same digest. Both the observed list and a
   spec's `spec.interfaces` are keyed the same way: a method by its qualified
   name, `Type.Method` (the Go receiver type or the TypeScript class), and
   everything else by its bare name. Two types that both offer a method named
   `List` are therefore two entries, which a list keyed by the bare name could
   not hold — the API server refuses a duplicate key, so such a package could
   not be specified at all. The index reports every TypeScript class member as
   unexported; the reader applies the language's own rule instead, so a member
   is public unless it says `private` or `protected`, or carries a `#` name.
   A spec stored before the receiver was part of the key is migrated by the
   ingest itself, when the facts name exactly one candidate;
6. sets `spec.codeRefs` to the observed `file:` refs plus any non-file refs the
   author wrote, and leaves `intent`, `requirements`, `interfaces`, `upstream`,
   `overlay` and `orchestrator` alone. A spec written this way carries the
   `specs.publicdomainrelay.dev/origin: ingest` annotation and a
   `status.realizedSpecHash`, so it never looks like a human edit;
7. when `populate.summarize` is true, raises one `CodeToSpec` change per
   context whose `intent` is still empty, at most `--max-concurrent-summaries`
   running at once, and works each one off with `populate.agent` (or `spec.agent`
   or the controller's `--agent`);
8. writes `status.phase` (`Cloning -> Indexing -> Populating -> Populated`, or
   `Failed`) and `status.contexts {total, summarized, failed}`, and rewrites the
   graph when a Bolt endpoint is configured.

## The controller: conditions and drift

`bin/specd` watches `Repository`, `SystemContext` and `SpecChange` in the
workspace and reconciles each one on a workqueue. It watches with dynamic
informer watches by default; `--watch poll` lists the workspace on a timer
instead, for an API server whose watch a client cannot hold open. Only
`cmd/specd` installs signal handlers: SIGINT and SIGTERM drain the workers and
stop the watches, and nothing in the libraries does I/O on its own.

`Repository` reconcile resolves the source, then asks the working tree (or the
cache clone) for its git HEAD. It indexes again when the commit moved, when the
manifest asks with the `specs.publicdomainrelay.dev/populate-request`
annotation, or on the first run, so the observed facts, the fingerprint and the
graph follow the code. The HEAD is invisible to the API server, so each
`Repository` requeues itself every `--resync` (5s by default), and a git source
is fetched each time. A source that cannot be resolved gets `Indexed=False`,
`phase: Failed` and `CloneFailed`, `SourceInvalid` or `PathMissing`, instead of
an error loop.

A `Repository` with `spec.populate` runs the same pipeline the `specctl ingest`
wrapper waits for; `Cloning -> Indexing -> Populating -> Populated` is
observable in `status.phase`, `status.contexts` counts the contexts, and the
`Populated` condition is `True` only when every context has a spec.

`SystemContext` reconcile indexes nothing. It turns the stored facts into the
three conditions through the same pure deciders ingest uses, so the two can
never write over each other, and it records `observedGeneration`:

| Condition | True when |
| --- | --- |
| `SpecValid` | the validator accepts the spec: refs resolve, levels are known, ids are unique |
| `CodeSynced` | every declared interface is observed, nothing undeclared is exported, and every requirement `codeRefs` entry resolves to an observed file or interface |
| `Drifted` | the observed fingerprint differs from `status.syncedFingerprint` |

`syncedFingerprint` and `syncedCommit` are the baseline the spec was last
brought into agreement with. Ingest establishes them on the first ingest of a
context and never moves them afterwards, so a context that drifted stays
`Drifted=True` even when a later commit elsewhere re-ingests the same tree; the
baseline moves when the drift is worked off (phase 6). `status.observedCommit`
and `status.observed` are always the current code.

Two kinds of work are raised, both `Pending` in this phase because the agents
arrive in phases 5 and 6:

- **CodeToSpec**, while the context is drifted and `syncedCommit` and
  `observedCommit` are a usable pair. The name is
  `<context>-c2s-<from8>-<to8>`, so two reconciles of the same drift land on the
  same object.
- **SpecToCode**, when the spec no longer hashes to `status.realizedSpecHash`,
  which is what a human edit looks like: ingest writes the spec with the
  `origin: ingest` annotation and moves `realizedSpecHash` with it, but it only
  moves the hash when no spec write was already pending, so a human edit is
  never absorbed by an ingest that runs afterwards. The name is
  `<context>-s2c-<to8>`.

A change of a direction is only created when nothing of that direction is still
`Pending` or `Running` for the context, so a tree that stays drifted queues one
unit of work, not one per reconcile, and a controller restart does not queue
another. `SpecChange` reconcile keeps a change `Pending` and enforces the
admission rule from the plan: at most one change may be `Running` per
`SystemContext`, and the lowest name wins, so a second one is marked `Failed`
instead of racing.

```bash
bin/specd --workspace root:specs --resync 5s --watch informer
bin/specd --watch poll --poll-interval 2s      # list instead of watch
```

`--codegraph`, `--log-level`, `--workers`, `--qps` and `--burst` are the rest of
the flags; `--bolt-url` and its four siblings (see the graph section) make every
ingest rewrite the graph as well. Every reconcile that writes nothing is a
no-op: `status` is compared before it is patched, which is what keeps the
controller from looping against itself.

## The CLM loop: code becomes a spec

Without an agent, a `CodeToSpec` change stays `Pending` and waits for a human;
that is what `make example-phase4` shows. With `--agent`, the same change is
worked off, and this is what the controller does with it:

1. It takes the change from `Pending` to `Running`, after checking that no other
   change of the same context is running.
2. It builds the **context bundle**: the spec, `status.observed`, the model zone
   of the context document, one hop of the spec graph in both directions, and
   the `codegraph context` and `codegraph node` excerpts behind the context's
   code refs. `Fit` keeps whole sections in priority order until the token
   budget (`--bundle-budget`, 8000 by default) is spent and names what it
   dropped, so the ask is never silently truncated.
3. It asks the agent. `--agent claude` runs `deepseek-claude -p --output-format
   text` with the prompt on **standard input** (the launcher word splits its
   argv, so a prompt passed as an argument would arrive as a handful of words),
   in the repository under management, with a timeout. `--agent
   scripted:<file>` answers from a YAML scenario instead, and every test uses
   it.
4. It parses the answer strictly. A fenced or prose-wrapped JSON object is
   tolerated; an unknown level, a duplicate id, an empty requirement or a
   missing intent is not. A code ref the observed facts do not answer to is
   dropped and reported rather than written; a bare name (`Add`) is read as the
   interface of that name and stored as its canonical CodeGraph id, so a model
   ref and a human ref that mean the same symbol land on the same vertex.
5. It validates the draft with the same validator a human edit passes, writes
   `intent`, `requirements` and `interfaces` with the `origin: ingest`
   annotation, sets `status.realizedSpecHash` to the hash of what it wrote, and
   moves `status.syncedFingerprint` and `syncedCommit` onto the observed facts
   so `Drifted` goes False.
6. It writes `<repo>/.specs/context/<name>.md`: the agent's prose in the model
   zone, the resolved code refs between the `SPECD_MANAGED_BEGIN` and
   `SPECD_MANAGED_END` markers, regenerated from the facts. The model owns
   everything above the markers and nothing below them.

The last two steps are what keep the two directions from fighting. A spec write
is not a human edit, so it raises no `SpecToCode` change — and because a spec
update and the status update that acknowledges it are two API calls, the object
carries `specs.publicdomainrelay.dev/origin-hash`, the hash of the spec the tool
wrote, so a reconcile that runs between the two reads sees the tool's own write
and not an edit. A spec edit that makes the hash differ from both the realized
hash and the origin hash is a human edit, and raises a `SpecToCode` change for
phase 6 to realize.

An episode that keeps failing is retried as a new `SpecChange`
(`<base>-a2`, `<base>-a3`, ...) after a backoff that doubles from
`--retry-backoff`, and stops for good after `--max-attempts` (3 by default), so
a broken agent cannot fill the workspace with retries. The failed records stay
as the audit trail.

Two agent options:

```bash
bin/specd --agent claude --resync 5s                 # the real model
bin/specd --agent scripted:examples/phase5/scenario.yaml --resync 5s
```

and the same summarize without a controller at all:

```bash
bin/specctl ingest --repo fixtures/calc --summarize --agent scripted:examples/phase5/scenario.yaml
```

`ingest --summarize` runs the summarize directly rather than creating
`CodeToSpec` changes, so it works with no controller running; both entry points
call the same `impl/summarize`, so the spec that lands is identical.

## Spec becomes code, driven by a delta

The other direction is the same shape, with one addition that is the whole
point: **the manifest delta must be computable, and the agent must be told the
delta, never "here is the whole spec, guess what changed."**

**Every list is a keyed list.** `x-kubernetes-list-type: map` with
`x-kubernetes-list-map-keys` for `requirements` (by `id`), `interfaces` (by
`name`), the observed interfaces (by `name`) and `conditions` (by `type`);
`x-kubernetes-list-type: set` for `codeRefs`, `overlay`, `dependsOn` and
`introduces`. That is what makes a manifest delta-able: a server side apply that
names one requirement adds that requirement and leaves the other entries, and
every other field, exactly as they were.

```bash
kubectl --server-side --field-manager=human apply -f examples/phase6/spec-edit.yaml
```

A JSON merge patch replaces a list wholesale, so it has to carry the whole list;
a server side apply merges by key. `specctl get specchange` prints the result as
one line, `+2` for two added entries. The hash of a spec is taken over its
canonical form (keyed lists ordered by key, sets sorted), so reordering a list in
a manifest is not an edit that raises work.

**The delta is structured and pure.** `abc/delta` is `Diff(old, new) Delta` and
its inverse `Apply`, with `DiffObserved` for the code -> spec direction. Each
keyed entry is `added`, `removed` or `changed` with the fields that differ;
`SpecChange.spec.delta` carries it, and the JSON form is pinned by golden files
in `testdata/delta/` because phase 8 mirrors it in TypeScript.

```json
{
  "interfaces": [{"op": "added", "name": "Subtract", "to": {"name": "Subtract", "kind": "function", "...": "..."}}],
  "requirements": [{"op": "added", "id": "r.subtract", "to": {"id": "r.subtract", "level": "SHOULD", "...": "..."}}]
}
```

The two sides of the diff come from kcp alone: `status.realizedSpec` is the
spec the last realize or ingest acknowledged, and `status.syncedObserved` is the
fact set the synced baseline was taken from, so a delta never needs a second
source.

**One `SpecToCode` change is one worktree.** The reconciler:

1. Reads the context, the repository and its `verify` command, and computes the
   delta against `status.realizedSpec`. A repository that names no agent, and a
   controller started with no `--agent`, leave the change `Pending` for a
   human.
2. Makes a worktree on branch `spec/<context>/<hash8>` off the managed branch.
3. Asks the agent. `impl/claudecli` puts the rendered delta first, then the spec
   the code must reach, then the verify command, and the rule *edit files only,
   do not commit*. `impl/scriptedagent` applies the scenario's write, patch and
   delete steps instead.
4. Runs `Repository.spec.verify` in the worktree. Zero is the gate.
5. Commits everything the agent left, `.specs/context/*.md` included, as
   `specd <specd@localhost>`, fast-forwards it onto the managed branch with
   `--ff-only` (so a branch a human moved is a failure, never a rewrite), and
   deletes the change's branch.
6. Re-ingests the tree and hands ingest the spec it realized, so the new file
   refs, the new fingerprint, the new commit and the realized hash land in one
   status write. That is what ends the episode: no drift is reported for the
   tool's own work, so the controller cannot raise the opposite change.

On a non-zero exit the change is `Failed` with the exit code, the files it
touched and the verify output; the branch is kept for a human, the managed
branch and the spec do not move, and the next attempt is handed that output as
its instruction. Attempts back off and stop after `--max-attempts`.

```bash
bin/specd --agent claude --resync 5s                       # the real model
bin/specd --resync 5s                                      # agent named by the manifest
bin/specd --agent scripted:examples/phase6/scenario.yaml    # deterministic
```

`Repository.spec.agent.kind` selects the agent per repository
(`claude`, `scripted:<file>`, and `pi` once phase 8 wires it), and it wins over
the controller's `--agent`, so one manifest can say how its own changes are
worked off.

## Two CLM hosts, one state bridge

The CLM logic is a library with two hosts: the `pi-hydradb-clm` extension and
`cc-clm-mod`, a Claude Code mod. Both do the same four things — render the
context at session start, inject it before every model request, report every
file a tool touched, apply the model zone at the end of a turn — and both reach
the state the same way, by running `specctl clm`:

```
specctl clm render --context <name>          the context document, from kcp
specctl clm apply  --context <name> < zone   the model zone becomes a delta
specctl clm report --change <name> --event   one progress record, and the edges
```

The bridge is Go on purpose. kcp access, the delta authority (`abc/delta`) and
the graph writes then have one implementation, and a host needs neither a kube
client nor a Bolt driver — which matters, because a mod has no Node and no
sockets: it reaches the host only through `$.fs` and `$.process.run`.

`apply` writes with `origin: clm`, which specd reads as a spec edit. The
exception is a context whose own `SpecToCode` change is `Running`: there the
edit is recorded on that change and the spec holds still, because the spec is
the target that change is realizing, and a model rewriting it mid-realize would
move the target while the code is being brought to it. That is also what makes
it impossible for a realizing agent to spawn a change for itself.

`report` appends to `SpecChange.status.progress` — a bounded list of turn, tool,
files, note and time — and writes `TOUCHED` and `OCCURRED` edges into the graph,
the touched file landing on the same `CodeRef` vertex an ingest writes for it.
So while a change runs:

```bash
kubectl get specchange -w          # status.progress grows with the files touched
cat <repo>/.specs/context/calc.md  # the document the model is reading
specctl graph neighbors calc       # the edges the report wrote, one hop out
```

`specd --clm-mod <repo>/cc-clm-mod` runs the realize with the mod loaded (and
`--clm-mod` alone is enough: it becomes the agent when no other is named). The
model gets `SPECD_CLM_CONTEXT`, `SPECD_CLM_CHANGE`, the workspace kubeconfig,
the `specctl` path and the Bolt endpoint in its environment.

It also gets `SPECD_CLM_ROOT`: the worktree the model may work in, which is the
same directory specd runs it in, on the summarize call and the realize call
alike. The mod's `tool.call` hook holds every path a tool names against it. A
`Read`, `Write`, `Edit`, `MultiEdit`, `NotebookEdit`, `Grep` or `Glob` whose
path resolves — through `$.fs.stat(path, { resolve: true })`, so a symlink is
followed — outside that root is refused before the tool runs, and the refusal
is the model's to read. A `Bash` command is refused when it names an absolute
path outside the root and outside the system directories (`/usr`, `/bin`,
`/tmp`, ...), or a `..` that climbs out. The point is not tidiness: a realize
agent that can read `fixtures/` can read the hidden acceptance tests it is
being graded by, and a guest is only as contained as the worktree it was given.
The Bash half is best effort by construction — a path reached through a shell
variable, a relative walk, a hard link or a case alias is not caught — which is
why the guard is an allow-list on what resolves inside the root, not a
deny-list on spellings.

`make example-phase8` shows the whole mod path deterministically, with no model:
it renders the document, makes the edit a model would make, applies it, shows
the one entry delta and the raised `SpecToCode` change, folds a second edit into
the running change, and reports a touched file. The gated live run —
`SPECD_REQUIRE_LIVE=1 SPECD_REQUIRE_LIVE_MODEL=1 go test ./test/e2e/ -run TestPhase8LiveModel -count=1`
— does the same with `deepseek-claude` and the mod actually loaded.

## Multi workspace, and the spec mirror in git

The API is published once and bound many times. A provider workspace
(`root:specs-provider`) holds an `APIResourceSchema` per kind — generated from
the CRDs, so there is one source of truth — and an `APIExport`
(`specs.publicdomainrelay.dev`). A tenant workspace binds that export and gets
the three kinds in its own logical cluster.

```mermaid
flowchart TB
    provider["root:specs-provider<br/>APIResourceSchemas + APIExport"]
    va["root:tenant-a<br/>APIBinding"]
    vb["root:tenant-b<br/>APIBinding"]
    vw["APIExport virtual workspace<br/>endpoint slice URL + /clusters/*"]
    sd["specd --mode export"]
    ga[("tenant A repo")]
    gb[("tenant B repo")]

    provider -- "publishes" --> va
    provider -- "publishes" --> vb
    va -- "objects of every bound workspace" --> vw
    vb -- "objects of every bound workspace" --> vw
    vw -- "watch, kcp.io/cluster per object" --> sd
    sd -- "status and SpecChange per logical cluster" --> vw
    sd -- "index, drift, summarize" --> ga
    sd -- "index, drift, summarize" --> gb
```

One `specd --mode export` watches `<endpoint>/clusters/*`, so every tenant's
objects arrive on one watch, each carrying the `kcp.io/cluster` annotation of
the workspace it lives in. The controller routes every read and write back to
that cluster through the same virtual workspace, so specs stay per workspace
while the API is shared, and drift in one tenant cannot touch another. The plain
workspace mode is still the default: `--mode` is the only switch.

The second half of phase 9 puts the spec in the repository beside the code.
`specctl sync --repo <path>` writes one YAML per `SystemContext` to
`<repo>/.specs/<name>.yaml` — spec only, canonical key order, no status, and
without the code refs the index derives (those are an observation, not a
decision, and writing them would rewrite every file on every commit) — and
reads them back:

```bash
specctl sync --repo . --direction pull    # kcp -> .specs/*.yaml
specctl sync --repo . --direction push    # .specs/*.yaml -> kcp
specctl sync --repo . --direction both    # push, then pull
```

The last successful sync is recorded in the `synced-hash` annotation. A file and
kcp are in **conflict** only when both moved since then; the sync refuses and
names the two hashes and the baseline, and `--prefer kcp` or `--prefer git` says
which side wins. A pull never clobbers a file that moved while kcp stood still,
and a push never overwrites a kcp that moved. A push writes with `origin: git`,
so the controller reads it as the desired state change it is.

`specd --specs-mirror` writes the context's file into the realize worktree
before the commit, so a spec -> code change lands as one commit carrying both
the spec and the code, which is what makes the mirror a pull request.

`make example-phase9` is the whole example: two tenants bound to one export, a
`Repository` and a codebase in each, one export-mode controller, a commit in
tenant A that drifts only tenant A, a pull that fills `.specs/`, and a conflict
that is refused until `--prefer git` resolves it. The live test
`TestPhase9TwoTenantsOneExportController` is the same against a real kcp.

## The open architecture document

`deno-kcp/.tools/open-architecture/arch.yaml` is a hand written spec of a whole
system: a header, then sections of nodes, where a node nests inside its parent
through `upstream`, `overlay`, `orchestrator` or `children` and an id is shared
by reference. `specctl import-arch` turns it into `SystemContext` objects and
`specctl export --format arch` writes it back. The three revisions used as
fixtures live in `testdata/open-architecture/`.

```
$ specctl import-arch testdata/open-architecture/arch.yaml --repository deno-kcp
repository   deno-kcp
document     arch-document-deno-kcp
contexts     166
pruned       0
graph        true
```

- Every node with an id becomes one `SystemContext`, named after the id:
  `sc.kind.denopod` becomes `sc-kind-denopod`. The id itself stays in
  `spec.arch.id`, with `spec.arch.kind` (`node` or `document`), the section and
  the position, the parent id and the slot it sat in, the derived `upstream`,
  `overlay`, `orchestrator`, `dependsOn` and `introduces` refs, the code paths
  as `spec.codeRefs`, and `spec.arch.node`, the node body with its inline
  children replaced by their id refs. The id and the kind are labels too
  (`specs.publicdomainrelay.dev/arch-id`, `specs.publicdomainrelay.dev/arch-kind`).
- One more `SystemContext` (`spec.arch.kind: document`) carries the top-level
  header and the section skeleton, so an empty section survives the trip too.
- Import is idempotent, and it prunes the objects under that repository that the
  document no longer holds (`--no-prune` keeps them). The `Repository` is
  created or updated so the graph commands can resolve code refs against it.
- A node whose `upstream` is an inline manifest rather than a ref is its own
  upstream, exactly as the arch layout describes; the manifest stays in the
  preserved node body.
- Export is the inverse: the nodes are grouped by the recorded section, ordered
  by the recorded position, and each node is rebuilt by re-inlining its children
  at the slots they recorded. The top-level keys come out sorted, and a YAML
  date such as `generated: 2026-09-28` comes out quoted, which is the same JSON
  value and the form the schema asks for.

## The graph

```
(SpecRepo {id,name,path}) -[:HAS_CONTEXT]-> (SpecContext {id,name,repo,intent,specHash})
(SpecContext) -[:REQUIRES]-> (SpecRequirement {id,context,reqId,level,text})
(SpecContext) -[:DECLARES]-> (SpecInterface {id,context,name,kind,signature})
(SpecContext|SpecRequirement) -[:REFERENCES]-> (CodeRef {id,codegraphId,kind,name,filePath})
(SpecContext) -[:UPSTREAM|OVERLAY|ORCHESTRATOR|DEPENDS_ON|INTRODUCES]-> (SpecContext)
```

Vertex ids are FNV-1a of a content key, masked to 53 bits, so re-ingest lands on
the same vertex (`common/ids`, the same scheme as `pi-hydradb-clm`). Every write
stays inside the Cypher core subset that ArcadeDB and HydraDB 0.2.0 both run:
`UNWIND $rows AS row MERGE (n {id: row.id}) SET ...` for vertices, a one-hop
typed `MATCH ... CREATE` for edges, `DETACH DELETE` for removal, and reads that
project properties with a label or an id predicate.

The graph is a derived index, so a write is a rebuild: delete the five managed
labels, then write every repository, context, requirement, interface and code
ref again from kcp plus CodeGraph. `specctl graph rebuild` does that on demand
and prints the resulting vertex counts.

### CRDs in a workspace

kcp v0.33 accepts `CustomResourceDefinition` objects inside a workspace. A CRD
applied to `root:specs` makes the group served in that logical cluster, with the
status subresource, printer columns and namespaced scope all honoured
(`deploy/install-specs.sh` depends on this), which is the single workspace mode
phases 1 to 8 use.

Phase 9 adds the multi workspace mode on top of the same CRDs:
`deploy/apiresourceschemas/` is generated from them by `impl/schemagen`,
`deploy/install-specs-provider.sh` applies those schemas and the APIExport into
`root:specs-provider`, and `deploy/bind-workspace.sh <ws>` binds a tenant
workspace to the export. The CRDs stay the one source of truth: a test fails
when a generated schema no longer matches the CRD it came from.

## Layout

```
common/specapi       group, version, kinds, resources, conditions, hashing
common/ids           FNV-1a vertex ids and Cypher literal helpers
abc/spec             typed Repository/SystemContext/SpecChange, arch ids, pure validator
abc/archyaml         pure: read arch.yaml into a flat node model, write it back
abc/sync             pure: partition a tree into contexts, observed facts, fingerprint, conditions
abc/graph            pure: the graph model, row builders, Cypher builders, GraphWriter
abc/agent            pure: the context bundle, the token budget, the strict draft parser, the context document, the delta render
abc/delta            pure: Diff/Apply of two specs and of two observed fact sets, and the compact summary
abc/mirror           pure: the `.specs/<name>.yaml` document and the sync conflict rule
abc/eval             pure: interface recall/precision, anchoring, round trip Jaccard, delta precision, the report
impl/kcpclient       dynamic client for a kcp workspace: CRUD, status, manifests, server side apply
impl/codegraphsqlite run codegraph, read .codegraph/codegraph.db, resolve code refs
impl/codegraphcli    run codegraph context|node, the only place the code itself is rendered
impl/ingest          the code -> facts -> kcp status pipeline and the graph rebuild
impl/populate        one Repository: resolve the source, index, raise one summarize per context
impl/bundle          build what one context looks like to a model; read and write its CLM document
impl/summarize       one code -> spec unit of work: bundle, agent, validate, write, move the baseline
impl/agentfactory    one --agent option string into an agent, shared by specd and specctl
impl/claudecli       the model agent: a configurable command, the prompt on stdin, a timeout
impl/scriptedagent   the deterministic agent: drafts and realize steps from a scenario file
impl/realize         one spec -> code unit of work: worktree, agent, verify gate, commit, land, re-ingest
impl/specsync        the `.specs` mirror: pull, push, the synced-hash baseline and the conflict refusal
impl/schemagen       CustomResourceDefinition -> APIResourceSchema, the drift test and the generator
impl/archkcp         arch.yaml <-> SystemContext objects on a kcp workspace
impl/gitrepo         the managed tree: head, branch, worktrees, the specd commit, the --ff-only land
impl/boltgraph       Bolt client; ArcadeDB is the default backend, HydraDB an option
abc/watch            the watch contract: resources in, Added/Updated/Deleted out
impl/watchinformer   dynamic informer watches against the workspace
impl/exportwatch     the APIExport virtual workspace: every bound workspace on one watch
impl/watchpoll       the list-and-diff fallback for a watch that cannot be held
abc/clm              pure: the model zone of a context document, the spec block parse/render, the merge of what a model owns
impl/clm             render, apply and report: the state bridge the CLM hosts call
impl/piagent         the pi host of the same agent contract (npx package by default)
impl/eval            the effectiveness harness: a fixture into a git repo, the loop over it, the measures
factory/specd        wires the watch, the workqueue, the three reconcilers and the agent
cmd/specctl          apply -f, get, delete, ingest [--summarize], import-arch, export, graph neighbors|rebuild, clm render|apply|report, sync, eval
cmd/specd            the controller binary; the only place that handles signals
cmd/hydradb-bins     extracts the HydraDB binaries from their OCI image
deploy/start-kcp.sh  start kcp + kine, then install the workspace and CRDs
deploy/stop-kcp.sh   stop only the kcp and kine this repository started
deploy/install-specs.sh  create root:specs, apply the CRDs, write a kubeconfig
deploy/crds/         the three CustomResourceDefinitions
deploy/apiresourceschemas/  the same three CRDs as APIResourceSchemas, generated
deploy/specs-provider.yaml  the provider workspace
deploy/specs-apiexport.yaml the APIExport a tenant workspace binds
deploy/install-specs-provider.sh  create the provider workspace and publish the API
deploy/bind-workspace.sh          create a tenant workspace and bind the export
examples/calc/       a repository and two system contexts that reference each other
examples/phase5/     the scripted agent the phase 5 example drives specd with
examples/phase6/     the baseline spec, the spec edit and the two scripted agents of the phase 6 example
examples/populate/   the one Repository manifest and the scenario the phase 7 example applies
fixtures/calc/       a tiny Go working tree: the calc package and its CLI, with its scenarios
fixtures/greet/      a tiny Deno/TypeScript module: a root module and format/, with its scenarios
fixtures/todo/       a Go JSON service over net/http: a store, an HTTP front end and a command
fixtures/shared/     two Go types that each offer Add and List: the two-receiver keying case
fixtures/external/   a fixture that is not carried here: one manifest pointing at a real checkout
docs/eval/           effectiveness reports, markdown and JSON, one run per date and agent
scripts/demo.sh      the phase 10 demo: the loop end to end, then the eval table
clm/core             pure TypeScript: the context document, the delta mirror, the row builders, the ports
clm/adapters-node    the Node ports: child_process, node:fs, and the specctl state bridge
cc-clm-mod           the Claude Code mod: the plugin, its vendored core, its hooks and its tests
testdata/open-architecture/  three revisions of arch.yaml and its schema
testdata/delta/      the golden delta JSON both Go and TypeScript are held to
testdata/ids.json    the golden graph ids both Go and TypeScript are held to
test/fixture         copies a fixture into a temp dir and commits it as a real git repo
test/e2e             the live round trip, ingest + graph, arch.yaml and the two CLM directions
```

Dependencies point one way: `common` <- `abc` <- `impl` <- `factory` <- `cmd`.
`abc` does no I/O, so the validator, the partitioner, the graph row builders and
the CLM spec block run in unit tests with no cluster, no index and no database.
The TypeScript does the same: `clm/core` is pure and imports nothing from Node,
`clm/adapters-node` is the only place `node:child_process` and `node:fs` appear,
and the two hosts (`pi-hydradb-clm`, `cc-clm-mod`) sit on top of both.

## Tests

```bash
make check      # gofmt and go vet
make test       # unit tests; live tests skip (-short)
make test-live  # SPECD_REQUIRE_LIVE=1; starts kcp, needs codegraph and a Bolt backend
make test-live SPECD_BOLT_BACKEND=hydradb   # the same, graph checks on HydraDB 7687

# the multi workspace end to end: two tenants, one export mode controller
SPECD_REQUIRE_LIVE=1 go test ./test/e2e/ -run TestPhase9TwoTenantsOneExportController -count=1 -v

# the effectiveness harness, with no cluster in it: every scenario's realize
# steps are applied to a copy of its fixture and its own tests must pass
go test ./impl/eval/ -run TestScriptedScenarios -count=1 -v

# the eval itself, in its own workspace (create it with
# SPECS_WORKSPACE=specs-eval WORKSPACE_KUBECONFIG=.kcp-specd/specs-eval.kubeconfig deploy/install-specs.sh)
bin/specctl eval --fixtures fixtures --out docs/eval/run-<date>-scripted.md
bin/specctl eval --fixtures fixtures --agent claude-mod --clm-mod cc-clm-mod
bin/specctl eval --fixtures fixtures --agent pi --pi-extension pi-hydradb-clm --code-only

# the same run beside the scripted baseline: the comparison is written next to
# the report and says whether the live run discriminated at all
bin/specctl eval --fixtures fixtures --agent claude-mod --clm-mod cc-clm-mod \
  --baseline docs/eval/run-<date>-scripted.json --out docs/eval/run-<date>-hard.md

# an unknown real codebase: one manifest, a read-only clone, no drafts and no
# scenarios. SPECD_EVAL_UNKNOWN_REPO overrides the checkout the fixture names.
SPECD_EVAL_UNKNOWN_REPO=../kcp-libs bin/specctl eval --fixtures fixtures/external \
  --agent claude-mod --clm-mod cc-clm-mod --code-only --round-trip=false

# the pi host over a real model (the default command is the npm package;
# SPECD_PI_ARGS names a provider or model when the environment needs one)
SPECD_REQUIRE_LIVE_MODEL=1 go test ./test/e2e/ -run TestPhase8PiHostSummarizesCalc -count=1 -v

# the three tests that spend a real model call
SPECD_REQUIRE_LIVE_MODEL=1 go test ./test/e2e/ -run TestPhase5LiveModel -count=1 -v
SPECD_REQUIRE_LIVE_MODEL=1 SPECD_REQUIRE_LIVE=1 go test ./test/e2e/ -run TestPhase6LiveModelRealizesSubtract -count=1 -v
SPECD_REQUIRE_LIVE_MODEL=1 SPECD_REQUIRE_LIVE=1 go test ./test/e2e/ -run TestPhase7LiveModelPopulatesAnUnknownCodebase -count=1 -v
SPECD_REQUIRE_LIVE_MODEL=1 SPECD_REQUIRE_LIVE=1 go test ./test/e2e/ -run TestPhase8LiveModelRealizesWithTheMod -count=1 -v

# the scope guard inside a live session: deepseek-claude with cc-clm-mod loaded
# is asked to read a file this repository owns and the mod refuses it
SPECD_REQUIRE_LIVE_MODEL=1 go test ./test/e2e/ -run TestPhase12ScopeGuardRefusesAFileOutsideTheRoot -count=1 -v
```

**One run at a time.** The live suite and `specctl eval` drive controllers
against the same kcp workspace and the same working trees, so two at once do not
measure twice: they overwrite the objects and the trees the other is reading.
`test/e2e` takes an exclusive `flock` on `.kcp-specd/live.lock` in its
`TestMain` and holds it for the whole package; `specctl eval` takes the same
lock around its run (`--live-lock`, or `SPECD_LIVE_LOCK`, points it elsewhere
and an empty value takes none). A run that finds the lock held prints one line
naming the file and waits, and because it is an `flock` the kernel drops it when
a killed run's process is gone, so there is no stale lock to clear.
`go test ./impl/runlock` proves it: one test pins that the second acquisition
waits and says so, and another starts two `go test ./test/e2e` runs at once and
reads the begin and end marks each writes around its window.

The TypeScript has its own three: `cd clm && npm test` (the core, including the
delta and the ids against the same golden files Go uses), `cd cc-clm-mod &&
npm test && npm run typecheck && claude plugin validate . && claude plugin test .`,
and `cd pi-hydradb-clm && npm test` (live against ArcadeDB by default).

The live tests start the cluster with `deploy/start-kcp.sh` if needed and leave
it running; `make kcp-down` stops it. The phase 7 test builds a bare git
repository out of the two fixtures, applies one `Repository` manifest with a git
source and a scripted agent, and asserts `Populated` with every context
summarized and `SpecValid=True`; `SPECD_REQUIRE_LIVE_MODEL=1` runs the same
manifest with `deepseek-claude`. Without `SPECD_REQUIRE_LIVE=1` a missing
`kcp`, `kine`, `kubectl`, `codegraph`, `deno`, `git` or Bolt endpoint skips the
test instead of failing. `impl/boltgraph` has its own live test that writes, reads and deletes
its own vertices, so it never disturbs the example graph; `impl/gitrepo` builds
a temporary git repository. The phase 2 test takes over the `calc` names in the
workspace and deletes them when it finishes, and the phase 3 test owns
everything under the `deno-kcp` repository, so run `make example-phase2` or
`make example-phase3` afterwards to put the example state back. The phase 4
tests start a real `specd` in the test process and stop it before they clean up,
so no controller is left watching the workspace afterwards; the phase 5 ones run
it with `--agent scripted:<file>`, so the loop is tested without a model, and
only `SPECD_REQUIRE_LIVE_MODEL=1` runs `deepseek-claude`: the phase 5 test needs
no cluster, only `codegraph`, and the phase 6 one drives the whole realize path
against a real worktree. The phase 6 tests own the `calc` and `cmd-calc` names
too, and the keyed list test proves a server side apply edits one requirement
without rewriting the others against the live API server. The ArcadeDB
checks, the default, read
`SPECD_TEST_ARCADE_URL`, `SPECD_TEST_ARCADE_USER`, `SPECD_TEST_ARCADE_PASSWORD`
and `SPECD_TEST_ARCADE_DATABASE`; the HydraDB ones read
`SPECD_TEST_HYDRA_URL`, `SPECD_TEST_HYDRA_USER`,
`SPECD_TEST_HYDRA_PASSWORD` and `SPECD_TEST_HYDRA_PASSWORD_FILE`. `impl/boltgraph`
and the phase 2 graph check default to ArcadeDB and take
`SPECD_TEST_BACKEND=hydradb` to run against HydraDB instead.

## Done means

`make demo` on a clean machine with `kcp`, `kine`, `kubectl`, `codegraph`,
`go`, `deno`, `git` and a Bolt backend on `PATH`: starts kcp, installs the
`root:specs` and `root:specs-eval` workspaces, starts specd, applies one
`Repository` manifest for a working tree and waits for `Populated`, edits the
spec (the demo's own server side apply; a model would do it through the CLM
path), shows the delta the agent was told — one interface and one requirement —
the agent commit on the managed branch with
`go test ./...` passing on it, prints the effectiveness table for the scripted
baseline over every fixture, and drives one scenario through the CLM path with
`cc-clm-mod` loaded (or `pi`; `SPECD_CLM_PATH=off` skips it). `make check`
and `make test` are green, and the reports of the runs that were actually made
live in `docs/eval/`.

```bash
make build
make demo                                   # scripted baseline, plus one CLM scenario
make demo SPECD_AGENT=claude                # the same body with the live model
make demo SPECD_EVAL_OUT=docs/eval/run-$(date -u +%Y-%m-%d).md
make demo-phases                            # phases 1 to 9, one example each
```

## Ports and state

kcp listens on 6447 with kine on 23797 and keeps state in `.kcp-specd/`
(gitignored). An eval run keeps its objects in `root:specs-eval` and clones a
`git` source into `.kcp-specd/cache`. The multi workspace mode adds the
workspaces
`root:specs-provider` and one per tenant (`root:phase9-a`, `root:phase9-b`, ...)
plus a kubeconfig per tenant in `.kcp-specd/<workspace>.kubeconfig`. They live
in the same state directory, so `make kcp-down` followed by `rm -rf .kcp-specd`
removes them. `deploy/stop-kcp.sh` only ever signals processes whose command
line names that root directory, so it cannot disturb another kcp on the
machine. The graph defaults to ArcadeDB on `bolt://127.0.0.1:7688` (HydraDB on
`bolt://127.0.0.1:7687` is the option); neither is started by this repository.

## What is next

The plan is complete. What the eval reports as still weak is the honest place
to start: the measures that fall short of 100% on the live runs in
`docs/eval/`, the TypeScript half of the observed surface (class members are
public by default and the index reports them unexported, the same gap Go
methods had), and the graph's share of the context bundle when the budget is
tight. Everything else is a matter of more fixtures and more scenarios.
