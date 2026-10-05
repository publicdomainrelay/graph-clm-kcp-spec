# Plan 0008: policies over spec-driven development

Goal: make policies a first-class part of spec-driven development. A
policy is a Gatekeeper `ConstraintTemplate` plus one or more constraints. It
reads the same objects specd already holds (Repository, SystemContext,
SpecChange) and a new derived view of the code (CodeGraph, CodeDiff). Policies
are stored on an orphan branch next to the specs, mirrored into kcp, audited
on every indexed commit, and evaluated as a gate in every realize. Writing a
policy, testing it, and generating one from a sentence must each be one
copy-pasteable command.

Two policies are the acceptance examples, both for
`publicdomainrelay/atproto-market`:

- **P-relay.** Integration tests that drive a bidder and a requester MUST make
  every ssh connection over the relay (ssh `ProxyCommand` through a transport
  the RFP cloud-init deploys). They never ssh, `Deno.connect` or `curl` a
  guest address directly.
- **P-guest-reports.** The bidder and the compute provider MUST NEVER reach
  into the guest to produce the `vm.onNetwork` event (no exec, ssh, agent
  query or address lookup into the guest). The guest MUST reach out to them
  and report its address, routing, iroh or fedproxy information.

Upstream `atproto-market` `master` (7a2e9d9) is a real target for both. Its
bidder emits `vm.onNetwork` when provisioning resolves and pulls the iroh node
id with `computeProvider.getNodeId(providerId)`. That is the provider asking
about the guest, not the guest reporting. P-guest-reports must flag it. Our
branch `spec/iroh-dumbpipe-20261004141803` (atproto-market#1) has the guest
report its ticket itself and must pass.

## What exists and what we keep

(Inspection record: `docs/reviews/0005-opa-first-stab-inspection.md`.)
`deno-kcp` branch `opa-first-stab`, directory `opa/` (12.7k lines) is a first
attempt. Notes:

- It holds 10 policy sets and 238 violation ids in plain OPA. A Python loader
  (`tools/load_spec_tree.py`) turns a spec tree checkout plus a git diff into
  one JSON input. Everything sits under the `deno_kcp` package. There is no
  Gatekeeper, no kcp, and no link to specd's lifecycle.
- **We keep its good ideas:**
  - The violation is a fixed object (id, policy, location, message, details).
  - Severity, level and title are declared once per rule (metadata), not by
    the rule body.
  - The calibration discipline: thresholds come from the real corpus, and
    "unmeasurable is not wrong".
  - "What is deliberately not a policy": never recompute specd hashes, never
    judge requirement truth.
  - A catalogue rendered from metadata.
- **We change three things:**
  1. The object model is Gatekeeper's: `ConstraintTemplate` plus constraint,
     `input.review.object`, `input.parameters`, `data.inventory`,
     `violation[{"msg", "details"}]`, and `enforcementAction`
     deny/warn/dryrun. Policies are portable. `gator verify` runs them, and a
     real Gatekeeper on a real cluster could enforce the same templates
     against SystemContext objects.
  2. The input is built by specd in Go, from objects specd already owns and
     from the codegraph index. It is not built by a separate Python loader.
  3. The code is a call graph, not only diff text. P-guest-reports is a
     reachability property: what the onNetwork emitter can reach, and what
     reaches it. Regex over added lines cannot express that.

## Data model

All kinds are `specs.publicdomainrelay.dev/v1alpha1` unless named otherwise.

### Reviewable objects (what `constraint.spec.match.kinds` selects)

| kind | source | stored in kcp |
| --- | --- | --- |
| `Repository` | existing CRD | yes |
| `SystemContext` | existing CRD | yes |
| `SpecChange` | existing CRD | yes |
| `CodeGraph` | derived per evaluation (one per repository and commit) | no, too big; schema documented, Go type in `abc/policy` |
| `CodeDiff` | derived for a SpecChange (base..head of its worktree) | no |

`CodeGraph` (name = repository; labels carry the repository, branch and
commit):

```yaml
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: CodeGraph
metadata: {name: atproto-market, namespace: default, labels: {specs.publicdomainrelay.dev/commit: <sha>}}
spec:
  repository: atproto-market
  branch: master
  commit: <40 hex>
  files:  [{path, language, context, test: bool, sha256, size}]
  nodes:  [{id, kind, name, qualifiedName, file, startLine, endLine, exported, context, text}]
  edges:  [{source, target, kind, line}]
  texts:  {<path>: <file text>}
```

How each field is filled:

- Nodes and edges come from the codegraph sqlite index. Edge kinds are calls,
  imports, contains, references, instantiates, implements and extends.
- `context` comes from the arch partition: the SystemContext whose observed
  files hold the file.
- `text` is the node's source span, capped at 64 KiB.
- `texts` holds whole files up to 256 KiB.
- `test` is true when the path matches the repository's test globs.
- The object is deterministic: sorted, and stable for the same commit.

`CodeDiff` has `spec: {change, base, head, files: [{path, status, added: [{line,
text}], removed: [...] }]}`.

### Inventory (`data.inventory`, Gatekeeper's referential data)

Every evaluation loads the following, keyed the way Gatekeeper keys namespaced
objects (`data.inventory.namespace[ns][apiVersion][kind][name]`):

- the Repository;
- all SystemContexts;
- the CodeGraph;
- the arch (`Architecture`: arch.yaml as an object);
- for a gate, the SpecChange and its CodeDiff.

So a policy that reviews a SystemContext can read the code, and a policy that
reviews the CodeGraph can read the specs. `gator verify` cases pass the same
objects through `inventory:`.

### Policy metadata (annotations on the ConstraintTemplate)

| annotation | meaning |
| --- | --- |
| `specs.publicdomainrelay.dev/title` | one line |
| `specs.publicdomainrelay.dev/level` | MUST / SHOULD / MAY |
| `specs.publicdomainrelay.dev/severity` | error / warning / info (default from level: MUST error, SHOULD warning, MAY info) |
| `specs.publicdomainrelay.dev/requirements` | `<context>#<requirement id>,...` the requirements the policy enforces |
| `specs.publicdomainrelay.dev/generated-by` | PolicyChange name, when generated |

The violation that specd reports is Gatekeeper's `{msg, details}` joined with
the template metadata, the constraint name, the enforcementAction and the
reviewed object's reference. It is opa-first-stab's violation object in
Gatekeeper form.

### Shared Rego library

`lib.specd` is shipped by hydradb and inlined into each built template's
`targets[].libs`. It holds:

- object access:
  - `code_graph`, `contexts`, `context(name)`, `repository`, `requirement(ctx, id)`;
- file and node selection:
  - `files_matching(globs)`, `nodes_in_files(paths)`, `nodes_named(regex)`;
- graph walks:
  - `calls_from(id)`, `callers_of(id)`, `reachable_from(ids, kinds)` (OPA
    `graph.reachable` over the selected edge kinds), `reaching(ids, kinds)`
    (reverse);
- text:
  - `lines_matching(path, regex)`, `node_text_matches(node, regex)`;
- reporting:
  - `location(file, line)`, `violation(msg, details)`.

Authors rarely need to touch the inventory paths.

## Storage: the orphan branch `open-policy/<repo>[--<branch slug>]`

The branch is resolved like `open-architecture/`: a code branch with its own
policy branch uses it, and otherwise falls back to the default branch's.

```
policies.yaml                    PolicyLibrary manifest: repository, version, testGlobs, default enforcement
lib/specd.rego                   the shared lib (copied from hydradb at init; `specctl policy build` refreshes it)
templates/<name>/src.rego        the rule: package <name>, violation[{"msg","details"}] { ... }
templates/<name>/src_test.rego   opa unit tests
templates/<name>/template.yaml   ConstraintTemplate header: names.kind, parameters schema, annotations (no rego)
constraints/<name>.yaml          constraints (kind = the template's kind): match, parameters, enforcementAction
tests/<name>/suite.yaml          gator Suite (test.gatekeeper.sh/v1alpha1)
tests/<name>/*.yaml              case objects and inventory: one allowed, one denied, at least
dist/<name>.yaml                 built full ConstraintTemplate, libs inlined (generated)
reports/<code-branch>.yaml       last audit report per code branch (generated)
CATALOGUE.md                     rendered from template metadata (generated)
.gitattributes                   dist/ reports/ CATALOGUE.md linguist-generated
```

Records are append-only. `changes/<policychange>.yaml` keeps a compact record
of each PolicyChange, as for specs.

## kcp: CRDs and two-way sync

- **ConstraintTemplate.** Gatekeeper's own CRD (`templates.gatekeeper.sh/v1`,
  vendored under `deploy/crds/gatekeeper/`) is installed in the workspace.
- **Constraint kinds.** For each ConstraintTemplate, specd creates the
  constraint CRD (`<kind lower>.constraints.gatekeeper.sh`, version
  `v1beta1`), the same way Gatekeeper's controller does. Its `spec.parameters`
  schema comes from the template, and it also carries `spec.match` and
  `spec.enforcementAction`. specd reports `status.created` the way
  Gatekeeper's `byPod` status does, in a simplified form.
- **PolicyChange** (new, in our APIExport) mirrors SpecChange:
  - spec:
    - `repository`, `branch`;
    - `prompt` (natural language);
    - `requirements` (`ctx#id` list);
    - `contexts`;
    - `enforcementAction` (default `dryrun`);
    - `apply` (bool, default false).
  - status: `phase` moves Drafting, Testing, Evaluated, Applied (or Failed).
    It also records `template`, `constraints`, `tests`
    `{passed, failed, output}`, `violations` (against the head CodeGraph),
    `attempt`, `agentLog`, `policyCommit` and conditions.
- **Direction kcp to branch.** A ConstraintTemplate or constraint
  create/update/delete in kcp is persisted by specd to the policy branch, in
  the same commit discipline as specs.
- **Direction branch to kcp.** `specctl policy restore --repo X [--branch B]`
  loads the branch into kcp. Repository creation loads the policy branch when
  it exists.
- **Repository fields:**
  - `Repository.spec.policy`: `{branch: <override>, enforcement: <cap>,
    disabled: bool}`. The `enforcement` cap can downgrade deny to warn during
    a migration.
  - `Repository.status.policy`: `{policyCommit, evaluatedCommit, totals: {deny,
    warn, dryrun}, violations: [first 50]}`.

## Lifecycle: where policies run

1. **Audit.** Runs on every indexed commit of a Repository and on every
   policy change:
   - evaluate all constraints against the Repository, all SystemContexts and
     the head CodeGraph;
   - fill `Repository.status.policy`;
   - set the condition `PolicyCompliant` on each SystemContext (False with
     the violation count and the first messages when a deny or warn violation
     names that context, or names a file in it);
   - write the report to the policy branch `reports/<code-branch>.yaml`.
2. **Gate.** Runs in every realize, after verify and before acceptance:
   - evaluate against the worktree head (CodeGraph indexed from the worktree)
     and the change's CodeDiff, with the SpecChange as reviewed object and in
     inventory.
   - **deny** violations fail the gate like a verify failure. Their messages
     are fed to the agent on the next attempt (the same retry loop and
     `--reason` mechanics). When attempts are exhausted, the SpecChange fails
     with reason `PolicyDenied`.
   - **warn** violations are recorded in `SpecChange.status.policy` and in
     CHANGES.md. They do not block.
   - **dryrun** violations are recorded only.
   - `specctl accept --override policy:<constraint> --reason ...` waives one
     constraint for one change. This reuses `acceptanceOverrides` and is
     recorded as AcceptanceOverridden.
3. **Generate.** A PolicyChange runs the configured harness (DeepSeek). It
   gets the prompt, the arch, the specs of the named contexts, the `lib.specd`
   API, and the existing templates. It writes the template, the constraint and
   the gator suite. specd then checks the result:
   - `opa check --strict` / compile;
   - the gator suite passes: at least one allowed case and one denied case;
   - evaluation against the head CodeGraph succeeds;
   - the denied case still fails after the agent's own fixture is swapped
     for one specd mutates from an allowed case.

   Failures go back to the agent. On success the phase is Evaluated, with the
   head violations shown. `specctl policy accept <name>` (or `apply: true`)
   commits to the policy branch and applies to kcp.

## Commands (all copy-pasteable, all documented)

| command | does |
| --- | --- |
| `specctl policy init --repo X [--with-library \| --from DIR]` | create `open-policy/X` with policies.yaml and `lib/specd.rego`; `--from DIR` seeds it from an existing policy directory |
| `specctl policy new <name> --kind K --review CodeGraph` | scaffold src.rego, test, template.yaml, constraint, suite |
| `specctl policy build [--dir D]` | build `dist/`, refresh lib, render CATALOGUE.md |
| `specctl policy test [--dir D]` | `opa test` on src + gator verify on suites (built-in engine; `--gator` also shells out to the real `gator`) |
| `specctl policy eval --repo X [--commit C \| --worktree P] [-o json]` | one-off audit without a controller |
| `specctl policy apply -f F` / `restore` / `ls` / `report` | kcp side |
| `specctl policy generate --repo X --prompt "..." [--requirement ctx#id] [--wait]` | create a PolicyChange |
| `specctl policy accept <policychange>` | commit and apply a generated policy |

`scripts/install-policy-tools.sh` installs pinned `opa` and `gator` release
binaries into `bin/`, checked by sha256.

## Phases

Each phase is done when gofmt, `go vet ./...`, `go test ./...` and
`SPECD_REQUIRE_LIVE=1` live tests are green, docs are written, and changes
are pushed.

### A. Engine, CodeGraph, offline CLI (blocks everything)

1. **Decided: the first choice.** The engine is
   `github.com/open-policy-agent/frameworks/constraint/pkg/client` with
   `github.com/open-policy-agent/gatekeeper/v3/pkg/target.K8sValidationTarget`
   and the `drivers/rego` driver, pinned to `frameworks/constraint`
   `v0.0.0-20260928232141-53076f8d5ce5` and `gatekeeper/v3` `v3.23.1`. This is
   exactly the client `gator verify` builds in
   `pkg/gator/opa.go` (`Targets(&target.K8sValidationTarget{})`, a `rego`
   driver, `EnforcementPoints(util.GatorEnforcementPoint)`), so the offline
   engine and the real `gator` agree by construction. It builds cleanly against
   our k8s v0.36 deps: `gatekeeper/v3@v3.23.1` requires `k8s.io/apimachinery`
   `v0.36.2` and the frameworks require `v0.36.4`, and MVS picks ours. A probe
   proved the whole contract before any code was written: `input.review`
   (kind/name/namespace/operation/object), `input.parameters`,
   `data.inventory.namespace[ns][apiVersion][kind][name]` through `AddData`,
   `libs` published under `data.lib.`, `spec.match.kinds` selection,
   `enforcementAction`, and `violation[{"msg","details"}]` all behave as the
   plan needs. Templates compile as **Rego v0** (`driver.go:227`
   `rego.SetRegoVersion(ast.RegoV0)`), so `lib.specd` and every `src.rego` are
   written in Rego v0 syntax, and `opa test` is run with `--v0-compatible`.
   No fallback to bare `opa/v1/rego` was needed.
   - **Conformance, done** (`6cc91b8`): `impl/policyeval/conformance_test.go`
     runs every suite under `examples/` and `testdata/` through the built-in
     engine and through `bin/gator verify -v` and compares each case;
     `SPECD_REQUIRE_GATOR=1` makes a missing binary fatal instead of a skip.
2. **Done** (`8974cc0`). `abc/policy`: Template with the specd annotations and
   the ConstraintTemplate header round trip, Constraint, `Match` with kinds,
   scope, namespaces, excluded namespaces, labelSelector, namespaceSelector and
   the name glob, Violation and Report, Enforcement, the CodeGraph and CodeDiff
   types, the `open-policy/` layout with `Resolve` reusing `oabranch.Slug`, and
   the gate decision with the Repository cap and one-shot overrides.
   - A constraint's `apiVersion` is `constraints.gatekeeper.sh/v1beta1`, not
     `<kind>.constraints.gatekeeper.sh`; Gatekeeper's client rejects the latter.
     `<kind lower>.constraints.gatekeeper.sh` is the **CRD** name, not the
     object's group.
   - Gatekeeper requires `ConstraintTemplate.metadata.name == lower(kind)`, so
     a policy is a **slug** (`relay-only-ssh`) for its directory, files and
     constraint name, and its template name is `lower(kind)`
     (`relayonlyssh`). `policy.TemplateSlug` is the directory name; the
     violation carries the slug.
   - `match.name` is exact in Gatekeeper. `abc/policy` also accepts a glob, and
     `impl/policyeval` honours it by stripping the name from the object it
     hands the engine and filtering the results through `Match.Matches`, so the
     pure matcher and the engine cannot disagree.
3. **Done** (`8974cc0`). `impl/codegraphfacts` builds the deterministic
   CodeGraph from the codegraph sqlite index, the tree and the arch partition
   (node text 64 KiB, file text 256 KiB, test globs, file sha256 and size).
   `impl/policyeval` is the engine: the framework client, the gator Suite
   runner driven by it (assertions run through gator's own `Assertion.Run`, so
   only the engine is ours), and an in-process opa unit test runner.
   `impl/policygit` reads and writes the branch through `oagit.Store`, an
   orphan branch when the parent is empty and append-only changes otherwise.
4. **Done** (`8974cc0`). `impl/policyeval/lib/specd.rego` is the library from
   the data model section: `code_graph`, `contexts`, `context(name)`,
   `repository`, `arch`, `requirement(ctx, id)`, `files_matching`,
   `tests_matching`, `nodes_in_files`, `nodes_in_context`, `nodes_named`,
   `nodes_qualified`, `calls_from`, `callers_of`, `adjacency`,
   `reverse_adjacency`, `reachable_from`, `reaching`, `paths_between`,
   `lines_matching`, `files_matching_text`, `node_text_matches`, `location`
   and `violation`, with 15 opa unit tests. It is embedded in the binary and
   written to `lib/specd.rego` by `init` and `build`.
   - `graph.reachable` skips a vertex that is not a key of the graph, so
     `adjacency`/`reverse_adjacency` first complete the vertex set; a leaf is
     then reachable, and the result includes the roots (OPA's semantics).
5. **Done** (`7994fdf`, `6cc91b8`). `specctl policy init|new|build|test|eval`
   and `scripts/install-policy-tools.sh` (pinned opa v1.21.0 and gator
   v3.23.1, both by sha256, idempotent). `test` runs the opa unit tests and the
   suites through the built-in engine, and `--gator` runs the real binary too.
   `eval` reads a policy branch, or `examples/policies/<repo>` when there is
   none, and audits `--worktree` or `--commit` without kcp.
6. **Done** (`6cc91b8`). `fixtures/market-mini/{compliant,violating}`: a Deno
   workspace with a bidder, a compute provider, a requester whose ssh uses
   `ProxyCommand` from the transport a cloud-init `UserDataModule` deploys, and
   an integration test that drives the bidder and the requester. The
   `violating` variant dials the guest directly (`Deno.connect`, `ssh -p`) and
   emits `vm.onNetwork` from a `computeProvider.getNodeId` reach-in; the
   compliant one emits it from the inbound guest report handler. Both pass
   `deno check`. `examples/policies/market-mini` carries the one policy phase A
   needs, and the conformance test at `impl/policyeval/conformance_test.go`
   runs it through our engine and the real gator and compares every case.
   Phase B replaces that placeholder with the two real policies.

### B. The two example policies and the real run

1. **Done** (`a213d68`, `ae3a91c`). `examples/policies/atproto-market/` holds:
   - `relay-only-ssh` (P-relay);
   - P-guest-reports as a set of three templates: `guest-report-reach-in` (a
     deny for a reach-in from the network emitter's reachable set),
     `guest-report-driven-emission` (a deny when the network identity emitter
     is not reachable from an inbound guest report handler) and
     `guest-report-cloud-init` (a require that a cloud-init module publishes
     the guest's address or routing outbound);
   - each with src, unit tests, template, constraint with parameters (globs,
     identifiers and patterns, so another repository reuses it by changing
     only parameters), and a gator suite with allowed and denied cases.

   `lib.specd` gained the helpers the policies need
   (`tests_matching_text`, `file_node`, `nodes_with_id`,
   `nodes_matching_globs`, `nodes_identified`, `nodes_reachable_from`,
   `closure_from`/`closure_reaching`, `node_match_line`, `first_line`,
   `definition_node`, `matches_globs`) with opa unit tests, and
   `impl/policyeval` now reads `json.Number` detail lines so a violation
   carries a real `file:line` instead of 0. A gatekeeper-specific quirk
   surfaced during calibration: the TypeScript indexer emits one node per
   declaration, so an interface method call (`provider.getNodeId(...)`) has no
   resolved edge and a closure must be paired with a text pattern; and a
   ConstraintTemplate name must be `lower(kind)`, hence the slug/kind split.
2. **Done** (`ae3a91c`, `docs/examples/atproto-market-policies.md`). Run with
   `specctl policy eval` against a fresh clone (`scripts/example-policies.sh`),
   `master` `7a2e9d9` / `pre-iroh` `d20070c` /
   `spec/iroh-dumbpipe-20261004141803` `ffac22e`:

   | ref | commit | result |
   | --- | --- | --- |
   | `master` | 7a2e9d9 | 2 deny: `guest-report-reach-in` (`lib/market-bidder-compute/mod.ts:282`, the `getNodeId` reach-in) and `guest-report-driven-emission` (`:284`, the `registerIdentity` emission from `providerIdPromise.then`) |
   | `pre-iroh` | d20070c | 0 |
   | `spec/iroh-dumbpipe-20261004141803` | ffac22e | 0 |

   Both master violations were read in the source and confirmed. `pre-iroh`
   and the spec branch were read too: they emit no host-derived identity and
   the guest reports itself (the spec branch reports its dumbpipe ticket to
   the requester's `/v1/on-network` handler). `relay-only-ssh` passes at all
   three refs; a run with an impossible transport allowlist proved it reaches
   the ssh nodes (`lib/requester-xrpc/mod.ts:691` at master, `:979`/`:1012` at
   the spec branch) rather than passing vacuously.
   - **Superseded by item 5**: the table above reads only the *identity*
     emitter. `pre-iroh` and the spec branch are not clean once the
     `vm.onNetwork` record itself is read.
3. **Done** (`docs/policies.md`): concepts, the CodeGraph/CodeDiff and
   inventory data model, the `open-policy/` layout, the `lib.specd` reference,
   writing a policy step by step, testing (opa v0 unit tests, gator suites,
   `--gator`, conformance), evaluating, enforcement and troubleshooting.
4. **Done** (`docs/examples/atproto-market-policies.md`): the copy-paste run
   with real output at each ref, the results table, what each violation means,
   and the reachability proof. `scripts/example-policies.sh` (REPO, REFS,
   LIBRARY, WORK, OUT) does the whole run and writes the text and JSON reports
   plus a summary.

   The fixture twin is `examples/policies/market-mini`; the fast test
   `impl/policyeval/example_fixture_test.go` evaluates it over
   `fixtures/market-mini/{compliant,violating}` with the real codegraph (0
   deny on compliant, both policy groups deny on violating), and every suite
   runs through the built-in engine and the real gator in
   `impl/policyeval/conformance_test.go`.

5. **Done** (B2 follow-up). P-guest-reports is about *who produced the
   `vm.onNetwork` event*, not about what the record carries, and the phase B
   calibration only read the identity emitter (`guest-report-driven-emission`'s
   `emitterPatterns` were `computeIdentity|REGISTER_IDENTITY|VM_REGISTER_IDENTITY`).
   A `vm.onNetwork` emitted by the host from the provisioning lifecycle
   therefore passed. The follow-up:
   - adds `guest-report-driven-onnetwork`, a second constraint of the same
     `GuestReportDrivenEmission` template (`emitterPatterns:
     COMPUTE_EVENTS_VM_ONNETWORK_NSID`, `subject: the vm.onNetwork event`), so
     the identity rule and the onNetwork rule are reported separately. The
     template gained the optional `subject` parameter for that, the default
     keeping the old message;
   - fixes the template's `driven_by_handler`: a handler is now a **definition
     node** (`specd.definition_node(handler)`). Before, a *file* node whose
     text matched `guest.onNetwork` (the lexicon constant `GUEST_ONNETWORK_NSID`
     in `lib/common/market-lexicons/nsids.ts`) counted as a handler and, through
     `imports` edges, made every emitter in the module graph "driven" -- which
     is why the spec branch passed at phase B. The unit tests cover a file
     node, a handler that calls the emitter, and an emitter that is the
     handler;
   - re-runs. Every ref now denies: `master` `7a2e9d9` 3 deny
     (`guest-report-reach-in` `lib/market-bidder-compute/mod.ts:282`,
     `guest-report-driven-emission` `:284`, `guest-report-driven-onnetwork`
     `:257`), `pre-iroh` `d20070c` 1 deny (`:304`), `spec/iroh-dumbpipe-20261004141803`
     `ffac22e` 1 deny (`:313`). All read in the source; the compliant
     guest-report handler at `lib/market-bidder/mod.ts:491` still passes. So
     **atproto-market#1 fails the strict reading too** -- it removed the
     identity reach-in but kept the host-emitted `vm.onNetwork` (the
     non-routable container IP, emit-before-report). The compliant change is
     the same at every ref: emit only from the inbound handler whose
     `body.address` is the guest's report, and delete the
     provisioning-lifecycle emission;
   - records where the address comes from. The policy sees
     `computeProvider.provision(...)` and `result.metadata.ip`; the provider
     (`publicdomainrelay/hono-compute-provider`,
     `lib/compute-provider-local/mod.ts:377` `backend.inspectIp` and `:391`
     `pollSshExec`) is a sibling repository and is not in atproto-market's
     CodeGraph, so `guest-report-reach-in` cannot see that inspect. Cross-repo
     rules are plan 0009;
   - mirrors in `examples/policies/market-mini` and `fixtures/market-mini`
     (the violating variant now emits `vm.registerIdentity` as well as
     `vm.onNetwork`, both from the provisioning lifecycle; the compliant one
     emits both from the inbound report handlers), gator suites for the new
     constraint in both libraries, and a fixed
     `impl/policyeval/example_fixture_test.go` (absolute fixture path, and it
     checks the four constraint names, not the template slug).
   - `docs/examples/atproto-market-policies.md` is rewritten to the real
     results and explains the strict reading, what each ref would have to
     change, and the compute-provider asymmetry.

### C. kcp integration and the gate

1. **Done** (`2bd25bd`). Gatekeeper's `ConstraintTemplate` CRD
   (`templates.gatekeeper.sh/v1`) is vendored under `deploy/crds/gatekeeper/`
   and installed by `install-specs.sh` and `install-specs-provider.sh`, so
   `specctl up`, `specctl eval` and every live test serve it. specd creates the
   constraint CRD of a template with the frameworks' own helper
   (`client.CreateCRD`), served at `v1beta1` with `spec.parameters` from the
   template, and reports `status.created` and the `byPod` errors. The new
   `PolicyChange` kind goes through schemagen into `deploy/crds`,
   `deploy/apiresourceschemas` and the APIExport; the schema revision is 6 and
   the name-agreement tests hold it. `Repository.spec.policy`
   (`branch`/`enforcement`/`disabled`) and `Repository.status.policy`
   (`policyCommit`/`evaluatedCommit`/`totals`/`violations`) are in the CRD and
   in `abc/spec`.
2. **Done** (`c703962`). Two-way sync in `impl/policykcp`: template and
   constraint objects, `Files`/`Stale` for the branch, `Read`/`Apply` for kcp.
   specd restores the branch into kcp when a Repository is created (or its
   status has no policy commit yet), persists kcp edits back to the branch
   (rebuilding `dist/` and `CATALOGUE.md`), and reconciles both directions on
   every Repository resync. `specctl policy apply -f F|--library D`, `restore`,
   `ls` and `report` drive it by hand.
3. **Done** (`c703962`). The audit runs after the index on every Repository
   reconcile whose commit or policy commit moved: it evaluates the library
   against the head CodeGraph with the Repository and its SystemContexts in
   inventory, fills `Repository.status.policy`, sets `PolicyCompliant` on each
   context (False when a deny or warn violation names it or a file it owns),
   and writes `reports/<code branch>.yaml` to the policy branch.
4. **Done** (`c703962`). The gate runs in `runGates` after verify and before
   acceptance, reviewing the worktree CodeGraph, the change's CodeDiff and the
   SpecChanges, all of them also in inventory. A deny returns a `PolicyError`:
   the change fails with `PolicyDenied: <constraint>: <msg>` and those messages
   are the next attempt's agent log. Warn and dryrun are recorded in
   `SpecChange.status.policy` and warn also in `CHANGES.md`.
   `specctl accept --override policy:<constraint>` reuses
   `acceptanceOverrides`, is consumed once the commit lands and sets
   `AcceptanceOverridden`. The Repository enforcement cap applies.
5. **Done** (`6d1dfe4`, `dab3863`, `TestPolicy*` in
   `test/e2e/policy_live_test.go`): a private kcp, `fixtures/market-mini` and
   `examples/policies/market-mini`.
   - the first attempt writes the violating `lib/requester/mod.ts`, fails with
     the deny messages in its agent log, and the second attempt complies and
     lands (the scripted agent learned per-attempt step lists);
   - an agent that never complies ends the episode `Failed` with
     `PolicyDenied` and nothing lands;
   - a warn-only constraint lands, shows in `SpecChange.status.policy` and in
     the change record on the branch;
   - `specctl accept --override policy:relay-only-ssh` waives the deny once and
     is consumed;
   - restore into a fresh kcp yields the same templates and constraints, and
     `specctl policy ls|restore|apply` drive kcp;
   - the audit sets `PolicyCompliant` False on the context that owns the
     violating file and fills `Repository.status.policy` totals;
   - `TestPolicyLibrariesApplyWholeIntoKcp` seeds
     `examples/policies/market-mini`, `examples/policies/atproto-market` and
     `policies/library` onto policy branches, restores each branch into kcp and
     asserts every template's constraint CRD is `Established` and every
     constraint is applied.

   **What held the phase C branch back** (`dab3863`): the branch merged with
   phase B, so `examples/policies/market-mini` grew from the phase A
   placeholder to the four real templates and three live tests timed out. Three
   separate causes, none of them in the restore itself:

   - **The tests were written against the placeholder.** They asserted one
     template named `nodirectguestconnect`, its deny message and the override
     `policy:no-direct-guest-connect`; the real library holds four templates
     and `relay-only-ssh` is the one that judges a requester. The violating
     scenario was a probe under `test/` as well, which the real policy cannot
     see: the codegraph indexer gives a test file a file node and its imports,
     not the body of `Deno.test`, so `relay-only-ssh` reaches the production
     code the test imports. The scenario rewrites `lib/requester/mod.ts` now.
   - **kcp metadata leaked into the comparison.** kcp adds `kcp.io/cluster` to
     every object it serves, and the kcp form of a template carries the slug as
     an annotation while the branch keeps it in the directory name. Both sides
     of `policykcp.Distinct` therefore always differed, so the reconcile took
     the kcp-to-branch direction once per restore and wrote cluster metadata
     into the policy branch (it stopped there only because the branch then
     matched kcp). `ParseTemplateObject` drops both now and `Files` writes the
     branch header the way `specctl policy build` does.
   - **A failing template hid the rest.** `Apply` returned at the first error,
     so a restore that could not create one constraint CRD left kcp half
     applied and the wait timed out with nothing to read. It now attempts every
     member, records the reason in the ConstraintTemplate `status.byPod` errors
     and returns every failure joined; specd logs the restore, persist and
     read failures; the live waits report the Repository policy condition and
     the per-template statuses they last saw (`waitForState`).

   Two more fixes came out of the same run: `codegraphsqlite.Ensure` resolved a
   relative repository path against the child process working directory, which
   is the repository path itself, so a cold index landed in a nested directory
   and `TestExamplePoliciesOverFixtures` failed on a clean checkout; and the
   audit and the gate now compute `CodeGraph.spec.effects` through
   `impl/effects` (`effects.Dirs` for the worktree's own classifier packs) the
   way `specctl policy eval` does, so a policy that reads effects behaves the
   same in kcp as offline.

### D. Generation -- done

Folded into plan 0009 G6: generation targets the portable model layer, not
CodeGraph identifiers. See `docs/plans/0009-portable-policies.md` for what was
built and `docs/examples/atproto-market-policies.md` ("Generated") for the real
run against atproto-market.

1. **Done.** The PolicyChange reconciler runs the configured harness
   (`deepseek-claude` by default, `scripted:<file>` in tests) and checks the
   result before anything lands: compile, opa units, the gator suite, the
   mutation check derived from the suite's own allowed fixture, portability,
   and the evaluation against the head model. Refused attempts go back to the
   harness with their messages; `specctl policy generate|bind|accept|changes`
   drive it. One difference from the plan's Lifecycle step 3: the "denied case
   still fails after the agent's own fixture is swapped for one specd mutates"
   is implemented as a derived mutation of the **allowed** fixture case, and
   specd requires the rule to deny at least one derived case rather than to
   deny the specific fixture the harness wrote.
2. **Done.** The generated template carries
   `specs.publicdomainrelay.dev/requirements` from `spec.requirements` (specd
   writes it from the request, not from the harness's output), the audit
   records `status.enforcedBy` on the SystemContext, and CHANGES.md on the
   architecture branch marks the requirement `(policy-guarded)`.
3. **Done.** The real run is recorded in
   `docs/examples/atproto-market-policies.md`: both sentences generated against
   a fresh clone, the binding generated for the pack, the head violations at
   the three refs, and the comparison with the hand-written templates of B and
   the pack's G4 verdicts.

### E. Library port from opa-first-stab

1. **Done.** `policies/library/` holds seven ported templates, each with a
   constraint, a gator suite (an allowed and a denied case) and rendered
   `dist/` and `CATALOGUE.md`:
   - `change-succeeded-with-failed-acceptance` (SpecChange, from
     `change_integrity`);
   - `requirement-text-has-machine-path` (SpecChange, from `spec_structure`);
   - `provisioning-container-in-test`, `provisioning-manual-key-material`,
     `provisioning-cloud-init-bypass`, `provisioning-new-guest-transport`
     (CodeDiff, from `code_safety`);
   - `security-disabled-verification` (CodeDiff, from `change_security`).
   Patterns and globs are constraint parameters; every template carries
   `specs.publicdomainrelay.dev/origin` and `.../calibration` annotations
   recording the `RESULTS.md` finding or threshold it came from and every
   deviation from the origin. The package is embedded
   (`policies/library/embed.go`, `Files()`) and copied by
   `specctl policy init --with-library`. The embed lives in the
   `policies/library` Go package, not in `cmd/specd`, because specd has no
   policy surface yet -- the kcp-side offer of the library (serving it to a
   controller, or a specd-hosted catalogue) is phase C. `cmd/specctl` links the
   package today; `policies/library/library_test.go` keeps the embed in sync
   with the templates, the constraints and the suites.
2. **Done.** `specctl policy eval --diff-base REF` derives a `CodeDiff` with
   `impl/codediff` (`git diff --unified=0 --no-renames --no-prefix`, per-file
   added/removed lines with their head/base line numbers) and adds it to the
   reviewed objects and the inventory. The real run is recorded in
   `docs/policies.md` ("A real run"): against deno-kcp `0f1078d` the
   `security-disabled-verification` rule raises five denies -- three
   `--validate=false`, one `InsecureSkipVerify: true` and one `curl -sk`, the
   same class `RESULTS.md` found at `accept.sh:141`.
   `impl/policyeval/conformance_test.go` now also walks `policies/*`, so
   `SPECD_REQUIRE_GATOR=1 go test ./impl/policyeval/...` runs the library's
   suites through the built-in engine and the real gator.

### F. Review and fix

- DeepSeek analysis of the whole plan against the code.
- Opus review of `open-policy/atproto-market*` and the hydradb diff.
- Fix what they find.

#### Fix list 1

The coordinator's review/fix-phase list (this phase with plan 0009 G7), worked
through on `fix-list-1`. A parallel worker holds `factory/specd`'s
`SpecChange`/PolicyChange code, so nothing here touches it.

**1. A test's `specctl up` left specd, kcp and kine behind.** A run under
`/tmp` (started 01:40, 2026-10-05) left a `specd` with ppid 1 plus the kcp and
kine of a `specctl up` root alive for three hours. Both existing guards miss
it: `Pdeathsig` cannot span a process that exits, and `specctl up` exits by
design, so the chain from the test binary to the daemons is broken; and the
ledger check runs in the test process's `TestMain`, so a test binary killed
abnormally never reaches it. `specd` was not in the ledger at all.

Fix, on `fix-list-1` (`6a82e68`):

- `specctl up` reads `SPECD_DIE_WITH` (a test-only env; unset in production,
  so `specctl up` daemons stay detached). When it is set and `up` has just
  started the kcp, `up` starts a detached *keeper* for the root
  (`cmd/specctl/keeper.go`, `specctl keeper --root R --die-with PID`).
  `kcpproc.Watch` polls PID every 200 ms and, when it dies, terminates every
  kcp, kine and specd whose command line names the root; it also returns as
  soon as none is left, so `specctl down` ends it too. The keeper is spawned
  after the kcp starts, before `install-specs.sh` and specd, so a failure
  part-way still leaves the daemons covered.
- `kcpproc.RecordSpecd` appends specd to the same `SPECD_KCP_LEDGER` file, and
  `kcpproc.Leaks` names it beside the kcp and kine, so `impl/kcpproc`,
  `impl/runlock` and `test/e2e` `TestMain`s kill a leaked specd and fail the
  package.
- `kcpproc.ScanPids` and the keeper's scan share one `/proc` walk and one set
  of name matches (`namesKcp`, `namesKine`, `namesSpecd`).

Why a keeper and not `Pdeathsig`: Linux delivers `Pdeathsig` when the parent
*thread* dies, and the parent of the daemons is `specctl up`, which must
return for the caller to continue. No `Pdeathsig` chain can therefore span
`specctl up`, and a pid-watching keeper can. `Pdeathsig` remains the mechanism
for `kcpproc.Start` in process (plan 0007 item 8); the two cover different
shapes.

Proof: `test/e2e/keeper_live_test.go`. `TestSpecctlUpDiesWithItsStarter`
re-execs the test binary as `TestSpecctlUpHelper`
(`SPECD_E2E_HELPER=1`, so `TestMain` skips the cluster setup), which runs a
real `specctl up` with `SPECD_DIE_WITH=its own pid` and reports the session;
the parent SIGKILLs it and asserts kcp, kine and specd are gone within 10 s.
Measured twice: with `DieWithPid` forced to 0 the three survive the SIGKILL
and the test fails (`kcp ..., kine ... and specd ... (alive true/true/true)
outlived the process that started them`); with the keeper they die, and the
test passes.

**2. codegraph does not index `Deno.test` bodies.** Not an option: `codegraph
1.6.0` (`@colbymchenry/codegraph`, the external indexer
`impl/codegraphsqlite` shells out to) exposes no flag, config file or env var
to index bodies -- `init`, `index` and `sync` take only `--force`, `--quiet`,
`--verbose` and `--yes` -- and its kind list has no closure, arrow or
expression kind. The limit is now stated in a new `docs/policies.md` section,
"Limits" ("The indexer emits declarations, not bodies"): a `Deno.test` body
has no node, its effects are file-level (`node: file:...`, component = the
file's role, flow `test -> unknown` with no channel), and a rule that needs
the declaring symbol or a channel cannot see it. Plan 0009 G4 gap (a) is the
measurement; the suite case `denied-ssh-inside-a-test-body` is the proof that
the rule still fires.

**3. The spec-time gate's library.** Verified unified, no code change:
`Controller.specGateLibrary` (`factory/specd/specgate.go:71-84`) uses
`--policy-library` only as an override and otherwise calls the same
`loadPolicyGate` the realize gate and the audit use, so a repository with no
policy branch gets no gate and the three can never disagree. `docs/policies.md`
("Declared interactions and the spec-time gate") now says so and names the
override. Not pinned by a test: the live gate test passes `PolicyLibrary`
explicitly, so the fallback branch is read from the code, and `factory/specd`
is the parallel worker's file.

**4. A tunneled ssh whose transport the vocabulary does not name.** Documented
in the pack, in `policies/packs/rfp-guest-isolation/README.md` (new; the
`CATALOGUE.md` is generated and byte-compared by
`TestExampleDistAndCatalogueAreCurrent`, so free text cannot go there), with a
short statement in the `docs/policies.md` "Limits" section: the rule sees a
non-empty `proxyCommand` and treats the ssh as tunneled, so an ssh whose
channel the model did not resolve is outside its reach; it still denies a
direct ssh and a test's `net.dial` on the guest.

**Verification.** `gofmt -l .` clean, `go vet ./...` clean, `go test ./...
-short -count=1` green, and `TMPDIR=/home/johnandersen777/e2e-tmp
SPECD_REQUIRE_LIVE=1 go test ./... -count=1` green in chunks (the whole
non-`test/e2e` set in one run; `test/e2e` split by `-run`, every test of the
package covered). One flake seen once and not reproduced:
`TestPhase7OneManifestPopulatesAnUnknownCodebase` reported `changes = 5, want
one per context` because one `CodeToSpec` change had a second, no-op attempt
(`...-a2`, "the spec already said this"); it passed on a rerun of the same
chunk and alone. It is the same reconcile-race class as the known
`TestPhase5CodeToSpecWithTheScriptedAgent` flake and is not item 1's
regression. No kcp, kine or specd whose root is under a test temp directory
was alive after the runs (checked `/proc` for root paths under `e2e-tmp`; the
only live instances belong to other agents' roots).

**5. The hand-labelled recall numbers.** They are in plan 0009 G2 ("Recall,
hand-labelled": 42 tp, 0 fp, 45 fn; precision 1.000, recall 0.483;
`proc.exec` 0.070, `net.dial` 0.000) and the same table and reading is now in
`docs/policies.md` "Limits", so a policy author reads the weak kinds beside
the vocabulary instead of only in the plan. The circular G1 comparison
against grep rules is named there as not being the recall claim.

### H. Retry deno-kcp#1 under policies, as a new PR

The user asked whether the policies improve
https://github.com/publicdomainrelay/deno-kcp/pull/1. PR #1 was built before
any policy existed. Phase H measures it, then runs the same request again
with the gate on. The result is a new pull request; #1 stays as it is.

1. **Baseline. -- done.**
   - Bind the policy library and the portable rules to deno-kcp, as
     `policies.yaml` roles and vocabulary:
     - host = the DenoPod provider;
     - guest = the DenoPod workloads;
     - requester = the example's market requester;
     - relay = kcp-libs dnsshim / the relay listener.
   - Evaluate PR #1's head against `main`:
     `specctl policy eval --diff-base main`, using the library and the
     binding.
   - Record every violation, and read each one to label it real or false
     positive. Known false positive: `kubectl --validate=false` read as
     disabled TLS verification. Fix it in `policies/library`.

   **Result.** `examples/policies/deno-kcp/` holds the seven library templates
   plus `relay-only-ssh` re-bound for deno-kcp (`policies.yaml`, the
   constraints and the gator inventories are the only files that differ from
   the library). The binding gives host, guest and test a file group and makes
   requester and relay target roles: they are peers this repository reaches
   over the network, and deno-kcp's own model is thin (3 components, 13
   effects, 2 flows to unresolved targets), because the DenoPods are other
   repositories' code. The globs are spelled `internal/*/*.go` rather than
   `internal/**`: `**` crosses directories in the Rego matcher but not in the
   Go model builder's `path.Match`.

   `specctl policy eval --repo deno-kcp --commit dc4c717e --diff-base main
   --library examples/policies/deno-kcp` on a fresh clone (PR #1's head
   `dc4c717e` against `main` `25d10f92`) reports 8 templates, 8 constraints and
   **7 deny violations, all `security-disabled-verification`, all real**: one
   executed `curl -skS` in `accept.sh:269` and six printed `curl -k`
   health-check lines in `apply.sh:220-224,227` that the same PR had just moved
   from `http://` to `https://`. `policies/library` alone reports the same
   seven; `main` is clean. The full table and the commands are in
   `docs/examples/deno-kcp-pr.md`, section "Policies: baseline of PR #1";
   `scripts/example-policies.sh` now takes `NAME`, a bare or URL `REPO` and
   `DIFF_BASE`, so the run is one invocation.

   Two false-positive classes were fixed in `policies/library` from reading the
   other rules against real code. `--validate=false` left the
   `security-disabled-verification` pattern set (it is schema validation, not
   TLS verification), with a gator case asserting 0 violations for the three
   real `kubectl … --validate=false` lines deno-kcp carries. The three
   provisioning CodeDiff rules now skip comment lines and ignore `**/*.md`, and
   a row matches once even when several patterns hit it; over the
   atproto-market iroh change that takes the run from 36 denies to 21. The
   remaining ones are recorded as a gap in the templates' calibration notes:
   a substring over a diff cannot tell naming a transport from running one,
   which is what plan 0009's model form (a `proc.exec` effect whose `argv0` the
   cloud-init does not deploy) is for.

   Baselines for steps 2-4, on the branch the retry starts from: `main`
   `25d10f92` is red for `internal/provider`'s
   `TestReconcilePodMintsATokenAndReportsOutputs` (one mint expected, two
   made), and the defects #1's later commits fixed -- the provider's unbounded
   initial list and its namespace-and-name-only informer store key -- are still
   on `main`, which is why the retry's acceptance had to find and fix them
   again.
2. **Done.** `specctl policy init --from DIR` (new) writes an existing policy
   directory onto a tree or orphan branch, and `scripts/example-pr.sh` gained
   `POLICY_LIBRARY` (a directory, off by default) and `POLICY_ENFORCEMENT`
   (`<name glob>=<deny|warn|dryrun>`, last match wins; default
   `*=warn security-disabled-verification=deny provisioning-*=deny`), so a run
   seeds `open-policy/<repo>[--<branch slug>]` before specd starts, hands the
   same directory to specd as `SPECD_POLICY_LIBRARY` for the spec-time gate,
   pushes the policy branch with `PUSH=1`, and records the audit, every
   change's gate decision, the head evaluation and a summary into `$WORK`.
   Two script bugs the run exposed are fixed with it: the push of a fresh
   clone's `open-architecture/<repo>` baseline is not a fast-forward against
   the remote's (only this run's own branch is pushed now), and `policy report`
   needs `--repo`.
   - Run:
     `WORK=/tmp/specd-deno-kcp-policy-20261005 BRANCH=spec/bidder-and-bob-pds-policy-20261005
     PUSH=1 POLICY_LIBRARY=$HYDRA/examples/policies/deno-kcp scripts/example-deno-kcp-pr.sh`,
     same PROMPT and BRIEF as #1, with the `kcp-libs` sibling on
     `fix/openbao-no-default-issuer` (kcp-libs#1) and the `hono-pds` crawler
     commit from hono-pds#1 carried as a working-tree patch.
   - The spec-time gate denied nothing. The realize gate denied once:
     `security-disabled-verification` on `apply.sh:222`
     (`echo "  bob pds curl -k https://..."`), the messages went back to the
     agent as the next attempt's agent log, and the next attempt landed
     `f31ce445` without it: one deny out of three attempts for that batch. Two
     `requirement-text-has-machine-path` warnings were recorded (the harness's
     own requirement text names `/home/johnandersen777/...`), not blocking.
   - Two fix rounds, both acceptance-driven and both through the spec flow as
     `MUST` requirements on `internal-provider`:
     `r.provider-initial-list-is-bounded-and-retried` (`2a0798f5`, three
     attempts, two of them gated on the failing acceptance) and
     `r.watch-cache-keys-every-workspace` (`f5b7c10c`, first attempt).
3. **Published** as
   [publicdomainrelay/deno-kcp#2](https://github.com/publicdomainrelay/deno-kcp/pull/2)
   on `spec/bidder-and-bob-pds-policy-20261005`, with the orphan branches
   `open-architecture/deno-kcp--spec-bidder-and-bob-pds-policy-20261005` and
   `open-policy/deno-kcp--spec-bidder-and-bob-pds-policy-20261005`. The body
   compares it to #1: 7 deny (all `security-disabled-verification`) versus 0;
   the one gate denial and the agent's correction; two fix rounds through the
   flow; live acceptance green under `gate: true` (16 of 16 checks, 42.9 s,
   `apply attempts=0`, `relaySawCommit yes`); 15 files +1151/-76 against #1's
   17 files +1235/-151, and 10 new requirements over 3 contexts (+139/-3).
4. **Recorded** in `docs/examples/deno-kcp-pr.md`, section "Round with policies
   (PR #2)": the one-command run, what `POLICY_LIBRARY` does to the branch, the
   timings, the gate denial and its correction, the #1-versus-#2 table, the two
   fix rounds, and the environment facts this round added (the acceptance
   step's `ORG_ROOT` must be the org root; a green acceptance is 43 s, a
   failing one 8.5 minutes).

Order: after phase C's gate is fixed and merged (`policy-cm`).

### I. Retry atproto-market#1 under policies, as a new PR

https://github.com/publicdomainrelay/atproto-market/pull/1 (iroh/dumbpipe, head
`spec/iroh-dumbpipe-20261004141803`) fails the strict P-guest-reports rule
`guest-report-driven-onnetwork`. B2 measured this. The bidder emits
`vm.onNetwork` from `providerIdPromise.then(...)`, not from the guest's report.

1. **Done.** The phase B/B2 results for #1 are the baseline: 1 deny
   (`onnetwork` at `lib/market-bidder-compute/mod.ts:313`), `relay-only-ssh`
   clean. Added to it: the same two findings read at `pre-iroh` (`:304`), and
   the second attempt's finding that the *line* had moved between the two refs
   and the old key moved with it -- see item 6.

   The first attempt at this phase (branch `policy-i`) was stopped by the
   coordinator: under whole-repo gating its agent removed the bidder's
   host-emitted `vm.onNetwork`, and the user decided that emission stays. That
   work directory and branch were not reused.
2. **Done, with two deviations from the plan text.**
   - `scripts/example-atproto-market-iroh-pr.sh` with the same PROMPT and a
     fresh clone on the `pre-iroh` base, `POLICY_LIBRARY=examples/policies/atproto-market`,
     `POLICY_ENFORCEMENT='*=deny'` (every constraint in the library and in the
     imported `rfp-guest-isolation@v2`), and `ACCEPT='deno test --allow-all
     test/bidder_container_integration_test.ts'` added to #1's command. The
     library is not `policies/library` and the pack is not
     `rfp-provisioning-provenance`: only the user's two rules, per U1.
   - The user's decision is a durable waiver on the run's policy branch before
     specd starts, one `exceptions/<key>.yaml` per finding, recorded with
     `specctl policy waive` (the commands are in the example doc).
   - The acceptances for the emission are what the gate reported: `waived` on
     every attempt that reached the gate, including the three that landed; the
     audit at the landed head reads `violations 2 (deny 2)`, both the emission.
3. **Published.** [atproto-market#2](https://github.com/publicdomainrelay/atproto-market/pull/2), #1 untouched, head `ffe23fa`, on
   `spec/iroh-dumbpipe-policy2-20261005` with the orphan branches
   `open-architecture/...` at `bdbaa21` and `open-policy/...` at `bd9a663`. Its
   body compares it to #1 (findings at the base, inherited and waived; new
   violations in #1's diff against this branch's diff; the gate denials and the
   corrections; acceptance, green; the spec and diff size) and states plainly
   that the host-emitted `vm.onNetwork` is kept by decision.
4. **Recorded.** `docs/examples/atproto-market-iroh-pr.md`, "Round with policies
   (PR #2)": the one-command run, the waiver commands, the anchor measurement,
   the per-attempt gate table, both sessions' rounds and their reasons, and what
   landed.
5. **Still open.** Bind `hono-compute-provider` and report the `inspectIp` /
   `pollSshExec` reach-in once the 0009 G5 multi-repository model lands; that fix
   belongs to a PR in that repository.
6. **Prerequisite found and fixed (`e12560a`).** `policy.Key` was
   `(constraint, object, file, line)`. The realize edits the file above the
   emission, so the line moves; a pre-existing violation that changes key is a
   *new* one to a change-scoped gate, and a durable waiver written at one line
   stops matching at another. Measured: `pre-iroh` has the emission at
   `lib/market-bidder-compute/mod.ts:304` and #1's head at `:313`. hydradb now
   anchors a violation to the declaration that encloses its location, with the
   model effect's kind and attributes (`abc/policy/anchor.go`), and `Key`, the
   baseline and a waiver read the anchor; the two refs now give identical keys
   and #1's head reads `0 new, 2 inherited`.

   **Finished in the second session.** The first session landed two changes
   (`8b36a2b` lib-abc-requester, `924781f` the iroh cloud-init module, whose
   install extracts the archive's `./dumbpipe` behind a bounded retry) and then
   stopped: the three contexts that carry the requester flow and the container
   harness never passed the acceptance gate, because the guest's report never
   reached the requester and `sshReady` stayed false.

   The second session read the first session's portless-URL diagnosis as half
   right and fixed the other half, which was a spec change and not a code change:
   the portless report URL is correct (the relay registers a portless `did:web:`
   name), and what was missing was the harness building the gateway-reachable path
   the OAuth suite already has. `r.iroh-acceptance-guest-reachable-report` (added),
   `r.iroh-ticket-report` and `r.iroh-ticket-delivery` (amended) say so. Two
   automatic attempts and one `specctl retry` later, all four contexts are
   realized:

   - `ffe23fa realize lib-common-cloud-init-common, lib-requester-xrpc, atproto-market: +6 ~3`,
     `13 files changed, 1057 insertions(+), 143 deletions(-)` against `pre-iroh`,
     the landed head `ffe23fa`, `lib-did-key-ingress-proxy` unchanged (`the agent
     changed nothing, the baseline moved`).
   - acceptance green: `accept acceptance: passed (exit 0, 28.9s)`,
     `ok | 1 passed | 0 failed (27s)`; `receiptOk: true, sshReady: true,
     sshExitCode: 0`, `ProxyCommand=<dumbpipe> connect <ticket>`, and
     `SSH_OK_VIA_IROH` printed from inside the guest.
   - the gate, across both sessions: 41 attempts carried the two accepted
     `vm.onNetwork` findings as `waived` (35 failed, 6 landed), and 4 attempts
     were denied by `rfp-relay-only-guest-ssh` at
     `test/bidder_container_integration_test.ts:251`. `policy findings --base
     pre-iroh` reads `0 new, 0 inherited, 2 waived` at the landed head; the
     emission is untouched at `lib/market-bidder-compute/mod.ts:304`.

   Second-session timings, `specctl up` to the last realized change: 55 min, of
   which about 25 min were lost to the recovery below.

   **Two defects the second session found.** A `specd` killed mid-realize leaves
   its `SpecChange`s `Running` on the branch, and a restarted `specd` never
   re-drives a `Running` change, so the queue behind them deadlocks; marking them
   `Failed` with the message the tool itself uses on restore
   (`restored: the branch recorded this change as Running`) released it. And the
   whole-run record is only as good as its last attempt: worth a fix in `specd`,
   not in the run.

Order: after `policy-cm` and 0009 G3 are merged, and after phase H (deno-kcp)
has proved the gate on a real run.

## Not in scope

- Running the real Gatekeeper admission webhook inside kcp.
- Policies over cluster state outside the spec objects.
- Recomputing specd hashes in Rego.
- Judging requirement truth (the coverage judge does that).
