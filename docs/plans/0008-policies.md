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
| `specctl policy init --repo X` | create `open-policy/X` with policies.yaml and `lib/specd.rego` |
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

1. Vendored Gatekeeper ConstraintTemplate CRD, constraint CRD generation,
   install in `deploy/install-specs*.sh`, PolicyChange APIResourceSchema plus
   APIExport (schemagen; test the name agreement).
2. Two-way sync with `open-policy/` (persist, restore), branch fallback.
3. Audit on index: Repository status, `PolicyCompliant` condition, reports on
   the branch.
4. Gate in realize: agent feedback on deny, `PolicyDenied`, warn recorded in
   CHANGES.md, `accept --override policy:<constraint>`.
5. Live e2e (private kcp) on `fixtures/market-mini`:
   - a scripted agent writes violating code first, gets the deny messages,
     complies on attempt 2, and the change ends Succeeded;
   - a scripted agent that never complies ends Failed with `PolicyDenied`;
   - a warn-only constraint shows in status and CHANGES.md and does not block;
   - restore from the branch into a fresh kcp gives the same constraints.

### D. Generation

Folded into plan 0009 G6: generation targets the portable model layer, not
CodeGraph identifiers. See `docs/plans/0009-portable-policies.md`.


1. PolicyChange reconciler (harness: deepseek-claude, the same agent kinds as
   realize), with the checks in Lifecycle step 3, then `specctl policy
   generate|accept`.
2. `--requirement ctx#id`: the generated template carries the
   `requirements` annotation. The SystemContext shows `enforcedBy` in status.
   CHANGES.md names the requirement as policy-guarded.
3. Real run, recorded in `docs/examples/atproto-market-policies.md`:
   - generate both example policies from the two sentences at the top of this
     plan, against atproto-market;
   - show what the generator produced, its suite results and its head
     violations;
   - compare with the hand-written versions in B.

### E. Library port from opa-first-stab

1. `policies/library/`, embedded in specd and offered by `specctl policy
   init --with-library`. Port a calibrated subset into Gatekeeper form. The
   source is deno-kcp `opa-first-stab` `opa/`:
   - **SpecChange checks:**
     - change Succeeded with a failed gating acceptance;
     - requirement text carries a machine path;
   - **project provisioning rules over CodeDiff** (code_safety):
     - container run/exec in tests, manual authorized_keys, ssh-keygen;
     - a cloud-init bypass, a new guest transport not in a UserDataModule;
   - **security over CodeDiff:** TLS verification disabled.
2. Each port keeps its calibration note and adds a gator suite.
   CATALOGUE.md is rendered.

### F. Review and fix

- DeepSeek analysis of the whole plan against the code.
- Opus review of `open-policy/atproto-market*` and the hydradb diff.
- Fix what they find.

## Not in scope

- Running the real Gatekeeper admission webhook inside kcp.
- Policies over cluster state outside the spec objects.
- Recomputing specd hashes in Rego.
- Judging requirement truth (the coverage judge does that).
