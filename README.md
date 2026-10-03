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

Phase 1 of 8 is done: **kcp holds specs.**

- API group `specs.publicdomainrelay.dev/v1alpha1`, kinds `Repository`,
  `SystemContext`, `SpecChange`, namespaced, with a status subresource and
  printer columns.
- `specctl apply -f | get | delete` against a kcp workspace.
- A live test that round trips a `SystemContext` through a real kcp.

## Requirements

`go` (1.26 or newer), `kcp` v0.33, `kine`, `kubectl`. The graph database is not
needed until phase 2.

## Quick start

```bash
make kcp-up          # kcp on 6447, kine on 23797, state in .kcp-specd/
make example-phase1  # apply examples/calc/specs.yaml and read it back
make kcp-down        # stop the cluster this repo started
```

`make example-phase1` prints a table of the contexts it applied, the same
objects through `kubectl`, and one `SystemContext` as YAML. It is idempotent:
run it as often as you like.

The same steps by hand:

```bash
make build
bin/specctl apply -f examples/calc/specs.yaml
bin/specctl get systemcontext
bin/specctl get systemcontext calc -o yaml
bin/specctl delete systemcontext calc
KUBECONFIG=.kcp-specd/specs.kubeconfig kubectl get systemcontexts
```

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
`observed.interfaces`, `observed.fingerprint`), `observedCommit`,
`realizedSpecHash`, and the conditions `SpecValid`, `CodeSynced` and `Drifted`.

Requirements carry `id` (unique in the context), `level` (`MUST`, `SHOULD` or
`MAY`) and `text`. Every `codeRefs` entry is a CodeGraph id: `file:`, `function:`,
`method:`, `type:` or `package:`. Context-to-context references are `self`,
`sc.<name>` or `up.<name>`.

`specctl apply` validates before it writes: duplicate requirement ids, an
unknown level, a malformed reference, a missing required field or a malformed
spec hash are rejected locally with the field path, and nothing is sent.

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
abc/spec             typed Repository/SystemContext/SpecChange, pure validator
impl/kcpclient       dynamic client for a kcp workspace: CRUD, status, manifests
cmd/specctl          apply -f, get, delete
cmd/hydradb-bins     extracts the HydraDB binaries from their OCI image
deploy/start-kcp.sh  start kcp + kine, then install the workspace and CRDs
deploy/stop-kcp.sh   stop only the kcp and kine this repository started
deploy/install-specs.sh  create root:specs, apply the CRDs, write a kubeconfig
deploy/crds/         the three CustomResourceDefinitions
examples/calc/       a repository and two system contexts that reference each other
test/e2e             the live round trip against a real kcp
```

Dependencies point one way: `common` <- `abc` <- `impl` <- `cmd`. `abc` does no
I/O, so the validator runs in a unit test with no cluster and no mocks.

## Tests

```bash
make check      # gofmt and go vet
make test       # unit tests; live tests skip (-short)
make test-live  # SPECD_REQUIRE_LIVE=1, starts kcp if it is not already up
```

The live test starts the cluster with `deploy/start-kcp.sh` if needed and
leaves it running; `make kcp-down` stops it. Without `SPECD_REQUIRE_LIVE=1` a
missing `kcp`, `kine` or `kubectl` skips the test instead of failing.

## Ports and state

kcp listens on 6447 with kine on 23797 and keeps state in `.kcp-specd/`
(gitignored). `deploy/stop-kcp.sh` only ever signals processes whose command
line names that root directory, so it cannot disturb another kcp on the
machine.

## What is next

Phase 2 reads `.codegraph/codegraph.db`, fills `SystemContext.status.observed`
from real code facts and upserts the graph into HydraDB over Bolt.
