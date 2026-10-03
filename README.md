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

## Status

Phases 1 and 2 of 8 are done: **kcp holds specs, and code becomes facts in
`status` and in the graph.**

- API group `specs.publicdomainrelay.dev/v1alpha1`, kinds `Repository`,
  `SystemContext`, `SpecChange`, namespaced, with a status subresource and
  printer columns.
- `specctl apply -f | get | delete` against a kcp workspace.
- `specctl ingest --repo <path>` runs CodeGraph over a working tree, partitions
  it into one `SystemContext` per directory that holds source files, and fills
  `status.observed` (`files`, `interfaces` with signature, file, line and
  CodeGraph id, and a `fingerprint` over both) plus the conditions `SpecValid`,
  `CodeSynced` and `Drifted`. Ingest is idempotent: a second run changes no spec
  and writes no status.
- `specctl graph neighbors|rebuild` read and write the spec graph in HydraDB or
  ArcadeDB over Bolt, using only the Cypher core subset both engines run.
- Live tests that round trip a `SystemContext` through a real kcp, and that
  ingest a real git working tree twice and check the graph on both backends.

## Requirements

`go` (1.26 or newer), `kcp` v0.33, `kine`, `kubectl`, and `codegraph` on
`PATH` for ingest. The graph needs a Bolt endpoint: HydraDB on
`bolt://127.0.0.1:7687` (password in `/tmp/hdb/token` by default) or ArcadeDB
on `bolt://127.0.0.1:7688` (database `clm`). Phase 2 uses the graph, so only
`specctl apply|get|delete` work without one.

## Quick start

```bash
make kcp-up          # kcp on 6447, kine on 23797, state in .kcp-specd/
make example-phase1  # apply examples/calc/specs.yaml and read it back
make example-phase2  # ingest fixtures/calc, fill status.observed, write the graph
make kcp-down        # stop the cluster this repo started
```

`make example-phase1` prints a table of the contexts it applied, the same
objects through `kubectl`, and one `SystemContext` as YAML.
`make example-phase2` applies the same example, indexes `fixtures/calc` with
CodeGraph, ingests it, prints the observed facts and the conditions, and shows
one hop of the graph around `calc` in HydraDB and in ArcadeDB. Both targets are
idempotent: run them as often as you like.

The same steps by hand:

```bash
make build

# the graph endpoint; the Makefile exports these two for you
export SPECD_BOLT_URL=bolt://127.0.0.1:7687
export SPECD_BOLT_PASSWORD_FILE=/tmp/hdb/token

bin/specctl apply -f examples/calc/specs.yaml
bin/specctl ingest --repo fixtures/calc
bin/specctl get systemcontext calc -o yaml
bin/specctl graph neighbors calc
bin/specctl graph rebuild --bolt-url bolt://127.0.0.1:7688 \
  --bolt-user root --bolt-password clm-arcadedb-root --bolt-database clm

bin/specctl delete systemcontext calc
KUBECONFIG=.kcp-specd/specs.kubeconfig kubectl get systemcontexts
```

`ingest` reads the Bolt endpoint from the flags or the environment
(`SPECD_BOLT_URL`, `SPECD_BOLT_USER`, `SPECD_BOLT_PASSWORD`,
`SPECD_BOLT_PASSWORD_FILE`, `SPECD_BOLT_DATABASE`); with no `--bolt-url` and no
`SPECD_BOLT_URL` it only updates kcp. The `Makefile` exports the HydraDB
defaults, so plain `make example-phase2` needs no extra flags. A relative
`Repository.spec.path` in the graph commands resolves against the working
directory, so run them from the repository root.

`specctl` talks to the workspace `root:specs` on the admin kubeconfig; pass
`--workspace`, `--namespace`, `--kubeconfig` or `--context` to change that.
`deploy/install-specs.sh` also writes `.kcp-specd/specs.kubeconfig`, a
kubeconfig whose server already points at the workspace, so plain `kubectl`
works against it without extra flags.

## The API

| Kind | Purpose | Key fields |
| --- | --- | --- |
| `Repository` | a git working tree under management | `spec.path`, `spec.branch`, `spec.verify`, `status.headCommit` |
| `SystemContext` | one spec node (one system context) | `spec.repository`, `spec.upstream`, `spec.overlay`, `spec.intent`, `spec.requirements[]`, `spec.interfaces[]`, `spec.codeRefs[]` |
| `SpecChange` | one direction-tagged change, the unit of work | `spec.systemContext`, `spec.direction`, `spec.toSpecHash` / `spec.toCommit`, `status.phase` |

`SystemContext.status` carries the code facts (`observed.files`,
`observed.interfaces` with `signature`, `file`, `line` and `codegraphId`, and
`observed.fingerprint` over both), `observedCommit`, `realizedSpecHash`, and the
conditions `SpecValid`, `CodeSynced` and `Drifted`.

Requirements carry `id` (unique in the context), `level` (`MUST`, `SHOULD` or
`MAY`) and `text`. Every `codeRefs` entry is a CodeGraph id: `file:`, `function:`,
`method:`, `type:` or `package:`. Context-to-context references are `self`,
`sc.<name>` or `up.<name>`.

`specctl apply` validates before it writes: duplicate requirement ids, an
unknown level, a malformed reference, a missing required field or a malformed
spec hash are rejected locally with the field path, and nothing is sent.

## Ingest: code becomes facts

`specctl ingest --repo <path>`:

1. runs `codegraph init` (or `sync` when the index exists) on the working tree;
2. reads `.codegraph/codegraph.db` back through a pure Go sqlite driver. Ids are
   read, never computed;
3. partitions the tree into one context per directory that holds a source file
   (`calc/` becomes `calc`, `cmd/calc/` becomes `cmd-calc`, root files take the
   repository name). Test files stay in `observed.files` but contribute no
   interfaces;
4. writes `Repository.status.headCommit` and `indexedCommit`, and for each
   context fills `status.observed` and the three conditions. The fingerprint is
   sha256 over the canonical JSON of the sorted files and interfaces, so the
   same tree always produces the same digest;
5. sets `spec.codeRefs` to the observed `file:` refs plus any non-file refs the
   author wrote, and leaves `intent`, `requirements`, `interfaces`, `upstream`,
   `overlay` and `orchestrator` alone. A spec written this way carries the
   `specs.publicdomainrelay.dev/origin: ingest` annotation and a
   `status.realizedSpecHash`, so it never looks like a human edit;
6. when a Bolt endpoint is configured, rewrites the graph from kcp and
   CodeGraph.

## The graph

```
(SpecRepo {id,name,path}) -[:HAS_CONTEXT]-> (SpecContext {id,name,repo,intent,specHash})
(SpecContext) -[:REQUIRES]-> (SpecRequirement {id,context,reqId,level,text})
(SpecContext) -[:DECLARES]-> (SpecInterface {id,context,name,kind,signature})
(SpecContext|SpecRequirement) -[:REFERENCES]-> (CodeRef {id,codegraphId,kind,name,filePath})
(SpecContext) -[:UPSTREAM|OVERLAY|ORCHESTRATOR]-> (SpecContext)
```

Vertex ids are FNV-1a of a content key, masked to 53 bits, so re-ingest lands on
the same vertex (`common/ids`, the same scheme as `pi-hydradb-clm`). Every write
stays inside the Cypher core subset that HydraDB 0.2.0 and ArcadeDB both run:
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
(`deploy/install-specs.sh` depends on this). Phase 7 moves to
`APIResourceSchema` + `APIExport` + `APIBinding` so tenant workspaces can bind
the same API; phase 1 keeps the CRDs in the single workspace that owns the spec
state.

## Layout

```
common/specapi       group, version, kinds, resources, conditions, hashing
common/ids           FNV-1a vertex ids and Cypher literal helpers
abc/spec             typed Repository/SystemContext/SpecChange, pure validator
abc/sync             pure: partition a tree into contexts, observed facts, fingerprint, conditions
abc/graph            pure: the graph model, row builders, Cypher builders, GraphWriter
impl/kcpclient       dynamic client for a kcp workspace: CRUD, status, manifests
impl/codegraphsqlite run codegraph, read .codegraph/codegraph.db, resolve code refs
impl/ingest          the code -> facts -> kcp status pipeline and the graph rebuild
impl/gitrepo         reading the working tree (head commit, branch)
impl/boltgraph       Bolt client for HydraDB and ArcadeDB
cmd/specctl          apply -f, get, delete, ingest, graph neighbors|rebuild
cmd/hydradb-bins     extracts the HydraDB binaries from their OCI image
deploy/start-kcp.sh  start kcp + kine, then install the workspace and CRDs
deploy/stop-kcp.sh   stop only the kcp and kine this repository started
deploy/install-specs.sh  create root:specs, apply the CRDs, write a kubeconfig
deploy/crds/         the three CustomResourceDefinitions
examples/calc/       a repository and two system contexts that reference each other
fixtures/calc/       a tiny Go working tree: the calc package and its CLI
test/fixture         copies a fixture into a temp dir and commits it as a real git repo
test/e2e             the live round trip and the live ingest + graph runs
```

Dependencies point one way: `common` <- `abc` <- `impl` <- `cmd`. `abc` does no
I/O, so the validator, the partitioner and the graph row builders run in unit
tests with no cluster, no index and no database.

## Tests

```bash
make check      # gofmt and go vet
make test       # unit tests; live tests skip (-short)
make test-live  # SPECD_REQUIRE_LIVE=1; starts kcp, needs codegraph and a Bolt backend
```

The live tests start the cluster with `deploy/start-kcp.sh` if needed and leave
it running; `make kcp-down` stops it. Without `SPECD_REQUIRE_LIVE=1` a missing
`kcp`, `kine`, `kubectl`, `codegraph` or Bolt endpoint skips the test instead of
failing. The phase 2 test takes over the `calc` names in the workspace and
deletes them when it finishes, so run `make example-phase2` afterwards to put
the example state back. The ArcadeDB checks read
`SPECD_TEST_ARCADE_URL`, `SPECD_TEST_ARCADE_USER`, `SPECD_TEST_ARCADE_PASSWORD`
and `SPECD_TEST_ARCADE_DATABASE`; the HydraDB ones read
`SPECD_TEST_HYDRA_URL`, `SPECD_TEST_HYDRA_USER`,
`SPECD_TEST_HYDRA_PASSWORD` and `SPECD_TEST_HYDRA_PASSWORD_FILE`.

## Ports and state

kcp listens on 6447 with kine on 23797 and keeps state in `.kcp-specd/`
(gitignored). `deploy/stop-kcp.sh` only ever signals processes whose command
line names that root directory, so it cannot disturb another kcp on the
machine. The graph defaults to HydraDB on `bolt://127.0.0.1:7687` and ArcadeDB
on `bolt://127.0.0.1:7688`; neither is started by this repository.

## What is next

Phase 3 imports `deno-kcp/.tools/open-architecture/arch.yaml` into
`SystemContext` objects and exports them back, with a semantic diff over ids
and references as the test.
