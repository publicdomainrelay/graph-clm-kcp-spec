# Policies

A policy is a Gatekeeper `ConstraintTemplate` plus one or more constraints. It
reviews the objects specd already holds (`Repository`, `SystemContext`,
`SpecChange`) and a derived view of the code, `CodeGraph` (and `CodeDiff` for a
change). Policies live on the orphan branch `open-policy/<repository>` next to
the specs, are audited on every indexed commit, and are evaluated offline by
`specctl policy eval` -- no kcp, no controller, no cluster. The two example
policies in this repository are the acceptance examples of
`docs/plans/0008-policies.md`; the real run is
`docs/examples/atproto-market-policies.md`.

`specctl policy build` compiles a template into Gatekeeper form, and
`specctl policy test` runs it through the same client `bin/gator` builds, so a
suite passes or fails identically under `specctl policy test` and
`bin/gator verify`.

`policies/library/` is a ready-made library of seven portable policies, ported
from deno-kcp's `opa-first-stab` and embedded in `specctl`; see
[The policy library](#the-policy-library).

## Concepts

### Gatekeeper objects

- **ConstraintTemplate** is the rule: `metadata.name` is `lower(kind)` (for
  example kind `RelayOnlySsh` gives the template name `relayonlyssh`), it
  carries the parameters schema under
  `spec.crd.spec.validation.openAPIV3Schema`, and the Rego under
  `spec.targets[].rego`, with the shared library under `spec.targets[].libs`.
- **Constraint** is an instance of a template: it names the template's kind,
  selects objects with `spec.match`, passes `spec.parameters`, and sets
  `spec.enforcementAction` (`deny`, `warn` or `dryrun`).
- The Rego package for a template is its slug (`relay-only-ssh` is the
  directory, file and constraint name; the template name is
  `lower(kind)`). The rule always produces
  `violation[{"msg": ..., "details": ...}]`.
- A constraint's `apiVersion` is `constraints.gatekeeper.sh/v1beta1`. The
  CRD name `<kind lower>.constraints.gatekeeper.sh` is not the object's group.

### Reviewable kinds

| kind | source | stored in kcp |
| --- | --- | --- |
| `Repository` | specd CRD | yes |
| `SystemContext` | specd CRD | yes |
| `SpecChange` | specd CRD | yes |
| `CodeGraph` | derived per evaluation | no |
| `CodeDiff` | derived for a change | no |
| `ArchitectureModel` | derived per evaluation, from the CodeGraph, the effects, the SystemContexts and the binding | no |

A policy selects what it reviews with `spec.match.kinds`. Almost every code
policy reviews `CodeGraph`, because that is where the call graph is. A policy
that is about *the change* rather than the resulting tree reviews `CodeDiff`,
which `specctl policy eval --diff-base REF` derives from a commit range (see
[Evaluating](#evaluating)); the gate derives the same object for a `SpecChange`.

### The binding: roles and vocabulary

`policies.yaml` carries the per-repository binding next to the manifest fields.
Only this file changes from project to project.

```yaml
roles:
  host:
    contexts: [market-bidder-compute, hono-bidder]
    globs: ["lib/market-bidder*/**"]
    labels: {tier: bidder}
    symbols: ["createMarketBidder"]
    targets:
      routes: ["/v1/on-network"]
      hosts: ["*.bidder.local"]
      nsids: ["com.publicdomainrelay.temp.market.*"]
      symbols: ["vm.onNetwork", "registerIdentity"]
  guest:
    globs: ["lib/common/cloud-init-common/**"]
    declared: true
    targets: {symbols: ["getNodeId"]}
vocabulary:
  events:   {network-report: [com.publicdomainrelay.temp.compute.events.vm.onNetwork]}
  channels: {relay: [websocat, fedproxy, dumbpipe, "iroh connect"]}
  payloads: {network-info: [address, nodeId, ticket]}
  purposes: {network-discovery: [getNodeId, nodeId, ticket]}
imports:
  - {pack: rfp-guest-isolation, version: v1, source: embedded}
```

- `roles.<name>.contexts|labels|globs|symbols` select components (see the model
  section above). `declared: true` marks a role that may come from specs alone.
- `roles.<name>.targets` are the hints that resolve a connection *to* that
  role: `routes`, `hosts`, `nsids` (glob-matched against a path, a host or an
  NSID of the initiating effect), `symbols` (a regex over the effect's node
  text) and `attrs` (a regex over the *values* of the effect's attributes).
  `attrs` is the narrow one: `attrs: [getNodeId]` matches a
  `container.exec verb=getNodeId` and ignores the word in a comment. They are
  reviewed knowledge, not derived facts, so keep them narrow.
- `vocabulary` maps project names to the abstract classes the model uses. The
  classes a pack needs are the pack's interface: `channels/relay`,
  `events/network-report`, `payloads/network-info`, `purposes/network-discovery`
  and `routes/report` for `rfp-guest-isolation`.
- `imports` names the packs this repository binds; see
  [Portable packs](#portable-packs).

### Policy metadata

Metadata lives in the ConstraintTemplate annotations, not in the rule body:

| annotation | meaning |
| --- | --- |
| `specs.publicdomainrelay.dev/title` | one line |
| `specs.publicdomainrelay.dev/level` | MUST / SHOULD / MAY |
| `specs.publicdomainrelay.dev/severity` | error / warning / info; defaults from the level (MUST -> error) |
| `specs.publicdomainrelay.dev/requirements` | `<context>#<requirement id>,...` the policy enforces; may be empty |
| `specs.publicdomainrelay.dev/generated-by` | PolicyChange name, when generated |

`CATALOGUE.md` is rendered from this metadata by `specctl policy build`.

### Enforcement

`enforcementAction` is per constraint. The audit and the gate read it:

- `deny` blocks (a gate failure, or `--strict` exits 1);
- `warn` is recorded and does not block;
- `dryrun` is recorded only.

A `Repository` may cap enforcement during a migration, and a single gate
decision can be waived with `specctl accept --override policy:<constraint>`.
That decision lives in `abc/policy` (`policy.Decide`) and the kcp gate applies
it (plan 0008 phase C): the audit records the violations,
`Repository.status.policy` totals them, and a `deny` in a realize fails the
change with reason `PolicyDenied`.

## The CodeGraph

`CodeGraph` is one object per repository and commit. `impl/codegraphfacts`
builds it from the codegraph sqlite index, the tree and the arch partition.

```yaml
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: CodeGraph
metadata: {name: atproto-market, namespace: default, labels: {...commit, branch, repository}}
spec:
  repository: atproto-market
  branch: master
  commit: <40 hex>
  files:  [{path, language, context, test, sha256, size}]
  nodes:  [{id, kind, name, qualifiedName, file, startLine, endLine, exported, context, text}]
  edges:  [{source, target, kind, line}]
  effects: [{id, component, kind, attrs, file, line, node}]
  texts:  {<path>: <file text>}
```

- `effects` is computed from the nodes, the file texts and the classifier
  packs (`impl/effects`; see plan 0009 G1), not read from the index. Every
  path that evaluates policies adds it to the same graph: `specctl policy
  eval`, the specd audit and the realize gate. The packs a worktree adds under
  its own `classifiers/` directory are loaded beside the embedded ones.
- Node and edge data come from the codegraph sqlite index. Edge kinds are
  `calls`, `imports`, `contains`, `references`, `instantiates`, `implements`
  and `extends`.
- `context` is the `SystemContext` whose observed files hold the file.
- `text` is the node's source span (capped at 64 KiB); `texts` holds whole
  files (capped at 256 KiB). `texts` is what the `*_line` helpers read, so a
  violation can carry a real file line.
- `test` is true when the path matches the repository's test globs (the
  library manifest's `testGlobs`, or `--test-glob`).
- The object is sorted and stable for the same commit.

Granularity matters: the TypeScript indexer emits one node per declaration
(function, method, class, ...), not per closure. A callback passed to a
function is part of its enclosing node's text, and a method call on a value of
an interface type (for example `provider.getNodeId(...)`) usually has no
resolved edge. Reachability is therefore used to bound *where* a pattern is
looked for, and a regex over the text of the reachable nodes carries the
detail. See "Writing a policy" below.

### Effect classifier packs

`impl/effects` turns the graph into the effect vocabulary with YAML packs
(`packs/typescript.yaml`, `go.yaml`, `shell.yaml`, plus any `classifiers/*.yaml`
in the checkout or `--classifiers DIR`). A rule matches a qualified call
(`call.name`), a shell command (`command.name`, optional `verbs`), an import
specifier, a route node, or string arguments, and may require an import
(`requiresImport`) or an extra pack opt-in (`extra`, disabled by
`--no-extras`).

Four matchers matter for the guest side:

- `call.any: true` matches a bare call and a member call (`createRepoRecord(`
  and `pds.createRepoRecord(`).
- `args.regex` must match the argument text of a call, or the whole line of a
  command. It is how a rule tells a call from a declaration: `createRepoRecord(`
  also starts `async function createRepoRecord(collection: string, ...)`, and
  the argument regex requires a literal or an expression, not `name: Type`.
- `command.args.regex` does the same for commands, so `curl` matches a real
  invocation and not a `- curl` package entry.
- `inStrings: true` matches inside string literals only. The cloud-init
  `user_data` is a TypeScript template string, so the guest's report is a
  `curl` inside a string: `ts-string-curl` and `ts-string-ssh` classify it as
  an `http.request` or an `ssh.connect` attributed to the guest role (the
  component of the file, through the binding's globs). Comments are still
  skipped, and an identifier named `ssh` is not a match. The rules stay narrow
  on purpose: other shell commands written in TypeScript strings are not
  effects (see the recall note in `docs/plans/0009-portable-policies.md`).

### The ArchitectureModel

`ArchitectureModel` is the portable view of one repository: who the components
are, which roles they play, which effects they perform, how they flow into each
other and what triggers what. A portable policy reads it instead of file names
and identifiers; only the per-repository binding changes. It is built by
`policy.BuildModel` from the CodeGraph (with the effects already on it), the
SystemContexts and the `roles` and `vocabulary` sections of `policies.yaml`.

```yaml
apiVersion: specs.publicdomainrelay.dev/v1alpha1
kind: ArchitectureModel
metadata: {name: atproto-market}
spec:
  repository: atproto-market
  components: [{name, roles: [...], context, source: declared|observed|both}]
  effects:    [{id, kind, component, context, attrs, file, line, node}]
  flows:      [{from, to, initiator, channel, carries: [...], purpose, level, forbidden, source, evidence: [effect ids]}]
  triggers:   [{from: effect id, to: effect id}]
```

- A **component** is a SystemContext when the specs declare one, and otherwise
  the role whose glob owns the file: the longest matching glob wins, then the
  role name. `source` is `declared` when only the specs know it, `observed`
  when only the code does, `both` when both do.
- `roles` are attached by the binding selectors: `contexts` (the component's
  SystemContext name), `labels` (an exact label subset), `globs` (any file of
  the component) and `symbols` (a regex over the qualified names of the
  component's nodes). The label `specs.publicdomainrelay.dev/role` on a
  SystemContext names one role directly, which is how a greenfield project
  declares a role before any code exists.
- **flows** come from the observed effects first. An initiating effect
  (`net.dial`, `http.request`, `ssh.connect`, `container.exec`, `event.emit`)
  resolves its target role through, in order: the `http.handle` effects that
  serve its path or NSID, the target hints of the roles, then a symbol hint
  matched against the effect's node text. `from`, `to` and `initiator` are
  roles; the evidence is the effect ids. An unresolved target is the role
  `unknown`, which is visible but matches nothing a policy expects. A
  component that would flow to itself is dropped.
- `channel` is the vocabulary channel whose terms appear in the effect's node
  text or attributes (`proxyCommand`, `argv0`, `url`, ...); `carries` are the
  payload classes whose terms appear there; `purpose` is the first matching
  purpose class. Matching is case-insensitive.
- **declared** flows come from the `interactions` of the SystemContexts
  (`ModelInput.Interactions`, and every context's own list, with the context as
  its self). A declared flow that matches an observed one is merged and its
  `source` becomes `both`; a declared `forbidden` flow stays its own entry, so
  a policy can match a real flow against it. An interaction's `initiator` is
  `self` or `peer` and is resolved to the role that initiates, so `from` is the
  context that declared the interaction and `to` is the peer; the role the
  initiator acts on is whichever endpoint is not the initiator. That directed
  shape (initiator, acted-on role, channel, purpose) is what
  `lib.specd.same_flow`, `specd.forbidden_matches` and Go's
  `policy.SameShape` compare.
- **triggers** are call-graph reachability between effect sites: for each
  effect of kind `http.handle`, `event.receive`, `proc.exec`, `container.exec`
  or `http.request`, every initiating effect within
  `DefaultMaxReachHops` (3) `calls`/`instantiates` edges. Same-site and
  over-long walks are dropped. The relation is coarse: the TypeScript indexer
  emits one node per declaration, so a callback and its enclosing handler share
  a node, and a method call on an interface value has no edge. A pack that
  needs "this emitter runs from that handler" must read the trigger's `from`
  effect and its kind, not merely its presence.

### Declared interactions and the spec-time gate

`SystemContext.spec.interactions` declares a flow before any code exists, so
the decision "who talks to whom, who initiates, over which channel" is
reviewable at the spec:

```yaml
interactions:
  - id: i.report
    peer: host
    initiator: self
    channel: relay
    carries: [network-info]
    purpose: network-discovery
    level: MUST
  - id: i.no-reach-in
    peer: host
    initiator: peer
    channel: relay
    carries: [network-info]
    purpose: network-discovery
    level: MUST
    forbidden: true
```

- the list is keyed by `id`, so a server side apply edits one interaction and
  leaves the rest of the list, and the spec hash covers it: a change to the
  interactions is a real spec change that raises a `SpecToCode` change.
- `peer` is a role name from `policies.yaml`, or a SystemContext name the
  binding gives a role.
- `initiator` is `self` or `peer`; `level` is `MUST`, `SHOULD` or `MAY`
  (unset means `SHOULD`).
- `forbidden: true` declares a must-never. It is kept in the model as its own
  declared entry, and a declared or observed flow of the same directed shape
  is a violation.
- The CLM context document renders and parses the block; `specctl clm apply`
  refuses a removal the document does not name under `removed:` in the spec
  block or pass to `--allow-remove`, exactly as for requirements.

`specctl policy model --propose-interactions` prints a pasteable block per
context from the observed flows, so an existing repository gets its
interactions filled in:

```bash
bin/specctl policy model --repo market-mini --worktree fixtures/market-mini/compliant \
  --library examples/policies/market-mini --propose-interactions
```

The **spec-time gate** evaluates a `SpecToCode` change before the agent runs.
It builds the ArchitectureModel from the post-delta specs alone (declared
facts, no code, no effects) and evaluates the library. `specd
--policy-library DIR` overrides where that library is read from; without the
flag the gate calls the same `loadPolicyGate` the realize gate and the audit
use (`factory/specd/specgate.go:71`), so it reads the repository's
`open-policy/<repo>` branch, else kcp, and a repository with no policy branch
gets no gate. An audit, a realize and a spec-time deny can therefore never
disagree about which policies are in force. A
`deny` violation is recorded on the change as `phase: Failed`, the message
names the constraints, and the condition `PolicyValid=False` carries
`reason: PolicyDeniedAtSpec`; nothing is realized, no worktree branch is
opened. Fixing the spec raises a new change (a new hash) that passes the gate
and realizes. `specctl retry` works as for any other failed change.

The same check runs offline, with no kcp:

```bash
bin/specctl policy eval --repo greenfield-market --specs-only \
  --path <a clone with the open-architecture branch> \
  --library policies/packs/conformance
```

`--specs-only` reads the `SystemContext`s from `specs/*.yaml` on the
`open-architecture/<repository>` branch, builds the declared model and
evaluates the library; `--strict` exits 1 on a deny. It is the offline twin of
the controller's gate and the fast way to iterate on a spec before applying it.

### The conformance pack

`policies/packs/conformance/` is the first portable pack: three templates over
the ArchitectureModel, parameterized by nothing but the model.

| template | enforcement | fires when |
| --- | --- | --- |
| `ConformanceObservedUndeclared` | warn | an observed flow has no declared interaction of the same directed shape |
| `ConformanceMustUnrealized` | warn | a declared `MUST` flow has no observed evidence and the model carries effects (code exists) |
| `ConformanceForbiddenFlow` | deny | a declared or observed flow matches a declared `forbidden` must-never |

The pack carries its own `policies.yaml`, `constraints/`, `dist/` and gator
suites; `impl/policyeval` has a test that fails when `dist/` or `CATALOGUE.md`
is stale and that runs every suite. A repository binds it with an import (see
below) or by copying it.

## Portable packs

A pack is a versioned directory of templates that read the ArchitectureModel
and the binding's vocabulary, never a file name or an identifier of one
repository. Only the binding changes from project to project.

### Format

```
policies/packs/rfp-guest-isolation/
  pack.yaml                 name, version, description, required roles,
                            required vocabulary classes, parameter defaults
  templates/<slug>/         template.yaml, src.rego, src_test.rego
  constraints/<slug>.yaml   one constraint per template
  tests/<slug>/             suite.yaml + inventory/*.yaml (allowed and denied)
  lib/ dist/ CATALOGUE.md   generated by specctl policy build
```

`pack.yaml` declares what a binding must provide:

```yaml
name: rfp-guest-isolation
version: v1
description: the RFP flow's guest isolation invariants over the ArchitectureModel
roles: [guest, host, test]
vocabulary: [channels/relay, events/network-report, payloads/network-info,
             purposes/network-discovery, routes/report]
parameters: {guestRole: guest, hostRole: host, relayClass: relay}
```

A rule reads `input.parameters.<name>` with the default inline, so the
constraint is the place a repository overrides a class name or a role name.

### Import and pin

```yaml
imports:
- pack: rfp-guest-isolation
  version: v1
  source: embedded
```

`source` is `embedded` (in the specd/specctl binary), `git:<url>@<ref>` (the
pack is `<url>/policies/packs/<name>` at that ref, or the repository root when
it carries a `pack.yaml` itself) or `oci:<ref>` (an oras artifact whose layers
are the pack's files).

`specctl policy build` resolves every import, merges the pack's templates and
constraints into the repository's library, and pins each pack by the sha256 of
its sources -- `pack.yaml`, `templates/`, `constraints/`, `tests/`, never the
generated `dist/` or the catalogue -- into `policies.lock`:

```bash
bin/specctl policy build --dir examples/policies/atproto-market
# CATALOGUE.md
# dist/guest-report-cloud-init.yaml
# ...
# dist/rfp-host-reach-in.yaml
# dist/rfp-relay-only-guest-ssh.yaml
# lib/specd.rego
# policies.lock
# pack rfp-guest-isolation@v1 a9d815a7e877 embedded
```

```yaml
imports:
- files: 43
  pack: rfp-guest-isolation
  sha256: a9d815a7e877460c63114c73685d0566990f04df988ce507107e3b2f21bee814
  source: embedded
  version: v1
```

A build whose pack no longer resolves to the pinned digest fails and asks for a
version bump; `--relock` rewrites the lock:

```
specctl policy build: policyeval: pack rfp-guest-isolation@v1 resolves to
a9d815a7e877..., policies.lock pins 0032a10e2157...; bump the version or run
specctl policy build --relock
```

Every other path resolves imports the same way: `policy eval --library DIR`,
`policy test --dir DIR`, the realize gate and the audit read the same library.

### Writing a pack

The templates are ordinary ConstraintTemplates; what makes them portable is
that they read the model through `lib.specd` and nothing else:

| helper | meaning |
| --- | --- |
| `model_components`, `components_with_role(r)`, `roles_of(c)` | components and their roles |
| `model_effects`, `model_effect(id)` | the effects, by id |
| `model_flows`, `flows_where(f)`, `declared(f)`, `observed(f)`, `forbidden(f)` | the flows |
| `model_triggers`, `triggered_by(id)` | call-graph reachability between effect sites |
| `model_vocabulary`, `vocabulary_terms(group, class)` | the binding's vocabulary |
| `component_in_role(c, r)`, `initiator_in_role(f, r)`, `acted_on_in_role(f, r)` | role tests |
| `event_class(e, class)`, `route_class(e, class)` | an emitted type, or a route, in a vocabulary class |
| `file_in_role(path, role)` | whose file a path is, for a rule that reads a CodeDiff |

Two habits keep a rule portable and honest:

- the role and the class names come from `input.parameters`, so a binding can
  rename them without touching the pack;
- a rule that reads an object outside the model (a CodeDiff) still decides
  whose file it is with `file_in_role`, never with a path of one repository.

### Binding a repository

Add the import, then declare the roles and the vocabulary classes the pack
requires. Nothing else changes: the repository keeps its own templates next to
the pack's.

```yaml
roles:
  guest: {globs: ["lib/common/cloud-init-common/**"], declared: true}
  host:  {globs: ["lib/market-bidder*/**", "hono-bidder/**"]}
  test:  {globs: ["test/**"]}
vocabulary:
  channels: {relay: [websocat, fedproxy, dumbpipe]}
  events:   {network-report: [COMPUTE_EVENTS_VM_ONNETWORK_NSID]}
  payloads: {network-info: [address, nodeId, ticket]}
  purposes: {network-discovery: [getNodeId, nodeId]}
  routes:   {report: [/v1/on-network]}
imports:
- {pack: rfp-guest-isolation, version: v1, source: embedded}
```

A missing role or class fails the build with the pack's name, which is the
check behind "the binding is the only per-repository input".

### Members: one model across repositories

An invariant about a host and a guest does not always live in one checkout.
`members:` names another repository the model is built over, with its own role
binding, so a pack rule sees a flow whose two ends are in two repositories:

```yaml
members:
- name: hono-compute-provider
  url: https://github.com/publicdomainrelay/hono-compute-provider
  ref: fb11e74
  path: lib                      # optional; limits the member to a subdirectory
  classifiers: [compute-provider.yaml]
  roles:
    host:
      globs: ["lib/compute-provider-local/**"]
    guest:
      targets: {attrs: [inspectIp, backend.exec]}
```

- Each member is cloned into `$SPECD_CACHE_DIR/policy-members/<name>` (default
  `.kcp-specd/cache/policy-members`), checked out at `ref`, and pinned to the
  commit it resolved to. `policy eval` and `policy model` print the pins;
  `policy build` writes them into `policies.lock` under `members:`. A build
  verifies the pin and refuses a member that moved, the way it refuses a pack.
- `--member name=path` clones the named member from a local checkout instead,
  for a ref that is not on its public remote. The lock still records the
  declared `url`.
- The member's components are named `<member>/<context>`, its effects, files,
  evidence and triggers carry the prefix, and its roles merge with the
  library's, so a portable rule reads the same abstract roles across
  repositories and never learns a repository name.
- `classifiers:` names packs under the library's `classifiers/` directory. It
  works on a member and on the library itself: a repository whose effect sites
  the shared packs do not know names them from the policy branch rather than
  editing code.

The ArchitectureModel is built over the members wherever a library is
evaluated -- `specctl policy eval` and `policy model`, the realize gate and the
kcp audit -- so a rule over the model gates a realize and an audit, not only an
offline run. The spec-time gate is declared-only: a member's observed reach-in
is not a fact before code exists.

`docs/examples/portable-policies.md` records the three bindings of the pack
(one repository plus a member, a provider alone, a spec-only repository) and
`scripts/example-portable-policies.sh` repeats them.

### The rfp-guest-isolation pack

The first bound pack: the two rules the RFP flow exists to keep, written once.

| template | enforcement | fires when |
| --- | --- | --- |
| `RfpHostReachIn` | deny | a flow the host initiates acts on the guest and carries network information or has network discovery as its purpose; or a `container.exec`/`ssh.connect` effect in a host component reaches the guest and no such flow rule reported it |
| `RfpGuestReportsNetwork` | deny | once the model carries effects, no declared or observed flow has the guest initiating toward another role carrying `network-info`; or an `event.emit` of the `network-report` class in a host component is not triggered by the `http.handle` of the `routes/report` route |
| `RfpRelayOnlyGuestSsh` | deny | an `ssh.connect` effect outside the guest role is carried by no `relay`-channel flow and carries no `proxyCommand` at all -- which catches an ssh written directly inside a test body as a file-level effect -- or a `net.dial` effect of a test component acts on the guest |
| `RfpGuestTransportProvenance` | deny | an added CodeDiff line that names a transport installs or runs it (an executing effect starts there and the line reads like an installation) in a file whose component is not the guest role |
| `RfpKeyMaterialProvenance` | deny | an added CodeDiff line that names key material is executed or written in a file whose component is not the guest role |

The last two are the model-form replacements for the library's
`provisioning-new-guest-transport` and `provisioning-manual-key-material`
CodeDiff rules. They read effects and roles, so a transport named in a selector,
a lexicon description or a test name, a `which dumbpipe` probe, and the
`authorized_keys` the guest's own `user_data` writes are all clean. Measured on
atproto-market PR #1 (`ffac22e`, base `d20070c`): the library alone reports 21
denies, the library plus the pack reports the one real violation and nothing
else.

Run the pack's own suites:

```bash
bin/specctl policy test --dir policies/packs/rfp-guest-isolation --gator
# opa: 83/83 passed
# suites: 14/14 cases passed
# PASS
```

The reach-in rule fires on the *declared* shape too: a flow with no evidence --
a spec that plans the host reaching in, before any code exists -- is denied at
spec time. `denied-a-declared-reach-in-before-any-code` and
`test_violation_when_a_declared_flow_has_no_evidence` pin it.

### Inventory

Every evaluation loads the referential data Gatekeeper keys as
`data.inventory.namespace[ns][apiVersion][kind][name]`:

- the `Repository`;
- all `SystemContext`s;
- the `CodeGraph`;
- the `ArchitectureModel`;
- the arch (`Architecture`);
- for a gate, the `SpecChange` and its `CodeDiff`.

`eval` reviews both the `CodeGraph` and the `ArchitectureModel`, so a
constraint may select either with `spec.match.kinds`.

A policy that reviews a `SystemContext` can therefore read the code, and a
policy that reviews the `CodeGraph` can read the specs. `lib.specd` hides the
inventory paths.

## Storage: the orphan branch `open-policy/`

Policies are stored like the architecture: a branch with no parent commit.

```
policies.yaml                    PolicyLibrary manifest: repository, version, testGlobs, default enforcement
lib/specd.rego                   the shared library (refreshed by `specctl policy build`)
lib/specd_test.rego              the library's own opa unit tests
templates/<slug>/src.rego        the rule: package <slug>, violation[{"msg","details"}]
templates/<slug>/src_test.rego   opa unit tests for the rule
templates/<slug>/template.yaml   ConstraintTemplate header: names.kind, parameters schema, annotations
constraints/<slug>.yaml          the constraint: match, parameters, enforcementAction
tests/<slug>/suite.yaml          gator Suite (test.gatekeeper.sh/v1alpha1)
tests/<slug>/inventory/*.yaml    case objects and inventory for the suite
dist/<slug>.yaml                 built full ConstraintTemplate, libs inlined (generated)
reports/<code-branch>.yaml       last audit report per code branch (generated)
CATALOGUE.md                     rendered from template metadata (generated)
```

`specctl policy init` creates it; `specctl policy restore` (kcp side) loads it
back. A repository without a policy branch falls back to
`examples/policies/<repository>` so `eval` works before a branch exists.

## The policy library

`policies/library/` is a calibrated library of seven policies ported from
deno-kcp's `opa-first-stab` (`opa/policies/`, `plans/RESULTS.md`) into Gatekeeper
form: same layout as `examples/policies/*`, same `lib/specd`, one template per
concern, and a gator suite with an allowed and a denied case each. It is
embedded in the `specctl` binary (`policies/library/embed.go`), so
`specctl policy init --with-library` copies it into a new policy dir or branch:

```bash
bin/specctl policy init --repo atproto-market --dir /tmp/policies --with-library
bin/specctl policy build --dir /tmp/policies
bin/specctl policy test  --dir /tmp/policies --gator
```

A library that already exists as a directory -- `examples/policies/<repo>`, or a
branch of another repository -- is seeded with `--from`, which copies its
templates, constraints and suites, takes `--repo` as the manifest's repository,
and rebuilds the rest:

```bash
bin/specctl policy init  --path /path/to/clone --repo deno-kcp --branch spec/x \
  --default-branch main --from examples/policies/deno-kcp
bin/specctl policy build --path /path/to/clone --repo deno-kcp --branch spec/x \
  --default-branch main
```

The first command writes the orphan branch `open-policy/deno-kcp--spec-x`
(`--branch` picks the code branch the policy branch belongs to; without it the
branch is `open-policy/deno-kcp`); the second refreshes `lib/specd.rego` and
renders `dist/` and `CATALOGUE.md` from what the branch now holds. Everything
the source directory carries is copied except the generated files, so the result
is what `specctl policy build` would have produced had the library been authored
on the branch.

| slug | reviews | denies | origin |
| --- | --- | --- | --- |
| `change-succeeded-with-failed-acceptance` | `SpecChange` | `status.phase: Succeeded` while an acceptance step reports `passed: false` and was not overridden | `change_integrity` |
| `requirement-text-has-machine-path` | `SpecChange` | a delta requirement's `to.text` naming `/home/...`, `/Users/...`, `/tmp/...` | `spec_structure` |
| `provisioning-container-in-test` | `CodeDiff` | an added test line running `docker/podman/container/nerdctl run\|exec` | `code_safety` |
| `provisioning-manual-key-material` | `CodeDiff` | an added line writing `authorized_keys` or running `ssh-keygen` | `code_safety` |
| `provisioning-cloud-init-bypass` | `CodeDiff` | an added line that names `user_data`/`cloud-init` and a skip/bypass marker | `code_safety` |
| `provisioning-new-guest-transport` | `CodeDiff` | an added line naming `websocat`, `wstunnel`, `chisel`, `frpc/frps`, `autossh`, `rathole` or `socat` without a `UserDataModule` marker | `code_safety` |
| `security-disabled-verification` | `CodeDiff` | an added line with `curl -k`, `--insecure` (which covers `--insecure-skip-tls-verify`), `InsecureSkipVerify`, `rejectUnauthorized: false`, `NODE_TLS_REJECT_UNAUTHORIZED=0` or `--tls-verify=false` | `change_security` |

Every template carries two extra annotations: `specs.publicdomainrelay.dev/origin`
names the source rule, and `specs.publicdomainrelay.dev/calibration` records
where the threshold or pattern came from in `RESULTS.md` and where this port
deviates from the origin. `policies/library/CATALOGUE.md` is the rendered list.

Patterns and globs are parameters (`containerPatterns`, `testGlobs`,
`patterns`, `disabledVerificationPatterns`, `allowPatterns`, ...), so another
repository reuses a template by changing only its constraint. Each rule has a
default in the Rego (`x = out { out := input.parameters.x } else = [...]`) so a
missing parameter narrows the rule rather than widening it.

### A real run

`specctl policy eval --diff-base REF` diffs `REF` against the evaluated commit
(`--commit`, or the worktree's `HEAD`) and adds the resulting `CodeDiff` to both
the reviewed objects and the inventory, so the provisioning and
disabled-verification templates have something to read. The run below is the
real output for the plan-0008 phase-H baseline: `publicdomainrelay/deno-kcp`
pull request #1 (`dc4c717e`) against `main` (`25d10f92`), on a clone that is
never edited or pushed. It is the library alone; the bound library of
`examples/policies/deno-kcp` reports the same seven violations, and the table
that reads each one labels it real or false positive:
`docs/examples/deno-kcp-pr.md`, section "Policies: baseline of PR #1".

```bash
git clone https://github.com/publicdomainrelay/deno-kcp && cd deno-kcp
git fetch origin main spec/bidder-and-bob-pds
bin/specctl policy eval --repo deno-kcp \
  --commit dc4c717e1b925e0c074237ed32dc7f093880d843 \
  --diff-base main \
  --path "$PWD" \
  --library policies/library
```

```
repository: deno-kcp  commit: dc4c717e
templates: 7  constraints: 7
violations: 7 (deny 7, warn 0, dryrun 0)

deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         deploy/examples/atproto/market/accept.sh:269
         added line deploy/examples/atproto/market/accept.sh:269 is "code=$(curl -skS -o /dev/null -w '%{http_code}' --max-time 10 \"$1\" 2>/dev/null) || true", which turns certificate verification off; a check that trusts any certificate checks nothing
deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         deploy/examples/atproto/market/apply.sh:220
         added line deploy/examples/atproto/market/apply.sh:220 is "echo \"  plc     curl -k https://127.0.0.1:2587/health\"", which turns certificate verification off; a check that trusts any certificate checks nothing
deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         deploy/examples/atproto/market/apply.sh:227
         added line deploy/examples/atproto/market/apply.sh:227 is "echo \"  curl -k -sS -X POST https://127.0.0.1:2583/xrpc/com.atproto.server.createAccount \\\\\"", which turns certificate verification off; a check that trusts any certificate checks nothing
```

(three of the seven are shown; `apply.sh:221-224` are the same printed
`curl -k` line for the relay and the three remaining services.)

One is the class `RESULTS.md` found at atproto-market's `accept.sh:141`: a
`curl -skS` probe of a TLS listener, here the acceptance's `http_code` helper,
so the check proves the listener answers and not that the leaf is the one the
workspace's OpenBao authority issued. The other six are the block `apply.sh`
prints to tell an operator how to health-check each port-forwarded service, and
the same PR moved exactly those lines from `curl http://…` to
`curl -k https://…`. Both shapes are real: the probe of a local service with a
self-signed certificate is the known intentional case a repository may silence
with the constraint's `allowPatterns`, and the printed guidance is what the
origin's separate `safety-stale-tls-guidance` warning rule was for -- the port
has no such template, so it lands here.

The run this section used to record was against deno-kcp `0f1078d`, a commit
that no longer resolves on the remote (the history was rewritten), and it
reported five violations, three of them `kubectl … --validate=false`. Those
three are a false positive that is now fixed: `--validate=false` is schema
validation, not TLS verification, and the pattern set no longer carries it
(see the `security-disabled-verification` calibration note). A gator case with
the three real `--validate=false` lines asserts 0 violations.

The two `SpecChange` templates are exercised by their gator suites here
(`specctl policy test --dir policies/library --gator`), and run against a live
`SpecChange` where the gate reviews one — plan 0008 phase C.

## lib.specd reference

`lib.specd` is shipped by hydradb, inlined into every built template's
`targets[].libs`, and written to `lib/specd.rego` by `init` and `build`. Import
it with `import data.lib.specd`. It is written in Rego v0 (the engine pins
`ast.RegoV0`), so `opa test` must run with `--v0-compatible`.

Every helper below is a rule; call them as `specd.<name>`. `globs` are
`glob.match` patterns matched against a repository-relative path (`**` matches
across directories); `pattern` is an unanchored Rego regular expression
(`re_match` is a partial match).

### Object access

| helper | returns |
| --- | --- |
| `specd.code_graph` | the reviewed repository's `CodeGraph` spec, or an empty graph |
| `specd.repository` | the `Repository` object |
| `specd.repository_name` | the repository name derived from the reviewed object |
| `specd.arch` | the `Architecture` object |
| `specd.contexts` | every `SystemContext` |
| `specd.context(name)` | one `SystemContext` |
| `specd.requirement(ctx, id)` | one requirement object from a context |

```rego
violation[specd.violation(msg, details)] {
	req := specd.requirement("hono-bidder", "r.relay-only")
	msg := sprintf("enforces %s", [req.id])
	details := {}
}
```

### File and node selection

| helper | returns |
| --- | --- |
| `specd.files_matching(globs)` | file objects whose path matches |
| `specd.tests_matching(globs)` | file objects that are tests and match |
| `specd.tests_matching_text(globs, pattern)` | test files whose whole-file text matches |
| `specd.files_matching_text(pattern)` | file objects whose whole-file text matches |
| `specd.nodes_in_files(paths)` | nodes whose file is in `paths` |
| `specd.nodes_in_context(name)` | nodes whose context is `name` |
| `specd.nodes_named(pattern)` | nodes whose name matches |
| `specd.nodes_qualified(pattern)` | nodes whose qualifiedName matches |
| `specd.nodes_matching_text(pattern)` | nodes whose source text matches |
| `specd.nodes_identified(pattern)` | nodes whose text, name or qualifiedName matches (a set) |
| `specd.nodes_matching_globs(globs, pattern)` | nodes in matching files whose text matches |
| `specd.node(id)` | one node by id |
| `specd.file_node(path)` | the `file` node for a path |
| `specd.nodes_with_id(ids)` | nodes whose id is in `ids` |
| `specd.definition_node(node)` | true for a function/method/... node, false for `file` and `import` |

### Graph walks

| helper | returns |
| --- | --- |
| `specd.edge_kinds` | the default edge-kind list |
| `specd.calls_from(id)` / `specd.callers_of(id)` | direct call neighbours |
| `specd.reachable_from(ids, kinds)` | OPA `graph.reachable` over the selected edge kinds |
| `specd.reaching(ids, kinds)` | the reverse walk |
| `specd.closure_from(ids, kinds)` | `ids` plus everything reachable from them |
| `specd.closure_reaching(ids, kinds)` | `ids` plus everything that reaches them |
| `specd.paths_between(ids, kinds)` | reachable paths |
| `specd.nodes_reachable_from(ids, kinds, pattern)` | reachable nodes whose text matches |

`reachable_from` follows OPA's semantics: a vertex that appears in no selected
edge is not a key of the adjacency map and is not returned, even when it is a
root. Use `closure_from` / `closure_reaching` when the roots themselves must
be considered (the usual case: "the emitter or anything it can reach").

```rego
violation[specd.violation(msg, details)] {
	target := specd.nodes_reachable_from({"fn:emit"}, ["calls"], "\\.getNodeId\\s*\\(")[_]
	msg := sprintf("%s reaches into the guest", [target.qualifiedName])
	details := specd.location(target.file, specd.node_match_line(target, "\\.getNodeId\\s*\\("))
}
```

### Effects and the model

| helper | returns |
| --- | --- |
| `specd.effects` | every effect of the reviewed graph |
| `specd.effects_of(kind)` | the effects of one kind |
| `specd.effects_of_component(component, kind)` | the effects of one component and kind |
| `specd.effects_in(globs)` | the effects whose file matches |
| `specd.effect_targets(kind)` | the `target` attribute of every effect of that kind |
| `specd.architecture_model` | the reviewed `ArchitectureModel` spec, or an empty one |
| `specd.model_components` / `specd.model_flows` / `specd.model_triggers` | the model's lists |
| `specd.components_with_role(role)` | the components carrying a role |
| `specd.roles_of(component)` | the roles of one component |
| `specd.flows_where(filter)` | the flows matching every key of the filter |
| `specd.triggered_by(effect_id)` | the triggers whose `to` is that effect |
| `specd.declared(flow)` / `specd.observed(flow)` | the flow source is declared, observed or both |

`flows_where` takes an object; every key must match. Keys are `from`, `to`,
`initiator`, `channel`, `purpose`, `source` and `carries`; `carries` accepts
one class or a list of classes. An empty filter matches every flow.

```rego
violation[specd.violation(msg, details)] {
	flow := specd.flows_where({"from": "host", "to": "guest", "purpose": "network-discovery"})[_]
	effect := specd.effects_of_component(specd.components_with_role("host")[0].name, "container.exec")[_]
	msg := sprintf("host reaches into the guest: %s", [effect.file])
	details := specd.location(effect.file, effect.line)
}
```

### Text and lines

| helper | returns |
| --- | --- |
| `specd.lines_matching(path, pattern)` | `[{line, text}]` for a whole file |
| `specd.node_text_matches(node, pattern)` | true when the node's text matches |
| `specd.first_line(path, pattern)` | the first 1-based file line that matches |
| `specd.node_match_line(node, pattern)` | the first matching line, offset from `node.startLine` |

`node_match_line` and `first_line` are what make a violation point at real
code. A rule that matches a node's text should report
`specd.location(node.file, specd.node_match_line(node, pattern))`.

```rego
violation[specd.violation(msg, details)] {
	node := specd.nodes_matching_text("Deno\\.connect")[_]
	msg := sprintf("%s dials directly", [node.qualifiedName])
	details := specd.location(node.file, specd.node_match_line(node, "Deno\\.connect"))
}
```

### Reporting

| helper | returns |
| --- | --- |
| `specd.location(file, line)` | `{"file": ..., "line": ...}` |
| `specd.violation(msg, details)` | `{"msg": ..., "details": ...}` |
| `specd.matches_globs(globs, path)` | true when the path matches a glob |
| `specd.globs_match(globs, path)` | the same, usable where a rule body is expected |

The engine reads `details.location` or the flat `details.file` / `details.line`
and fills `Violation.Location`, so `file:line` prints in the report and in
`-o json`.

## Writing a policy

1. Create or check out a library. `--dir` writes a plain directory; `--path`
   writes the orphan branch `open-policy/<repo>`.

   ```bash
   bin/specctl policy init --repo atproto-market --dir /tmp/policies
   bin/specctl policy init --repo atproto-market --dir /tmp/policies --with-library
   ```

   `--with-library` also copies the seven ported policies from
   `policies/library/` (see [The policy library](#the-policy-library)); leave it
   off to start empty.

2. Scaffold a template, a constraint and a gator suite with one allowed case
   and one denied case. `--pattern` is a first cut; edit the Rego after.

   ```bash
   bin/specctl policy new relay-only-ssh --kind RelayOnlySsh --dir /tmp/policies \
     --title "integration tests ssh to a guest only over the relay" \
     --pattern 'Deno\.connect' --glob 'test/**'
   ```

3. Edit `templates/<slug>/src.rego`. Keep the head
   `violation[specd.violation(msg, details)]`, use `lib.specd` for selection
   and for the line number, and put every knob in `input.parameters` so
   another repository reuses the template by changing parameters only. Add the
   parameters to `templates/<slug>/template.yaml`
   (`spec.crd.spec.validation.openAPIV3Schema`) and their values to
   `constraints/<slug>.yaml`.

4. Write `templates/<slug>/src_test.rego`: opa unit tests in Rego v0 syntax
   that call the rule with `with input as {...} with data.inventory as {...}`.
   Cover the deny and the allow, and the boundary (no emitter, no test).

5. Update the suite's inventories under `tests/<slug>/inventory/` and the
   assertions in `tests/<slug>/suite.yaml`.

6. Build and test.

   ```bash
   bin/specctl policy build --dir /tmp/policies
   bin/specctl policy test --dir /tmp/policies
   bin/specctl policy test --dir /tmp/policies --gator
   ```

7. Evaluate against real code (see below), read every violation, and fix the
   calibration -- not the expectation.

### A rule that must find a real line

```rego
package relayonlyssh

import data.lib.specd

violation[specd.violation(msg, details)] {
	driver := specd.tests_matching_text(input.parameters.testGlobs, input.parameters.driverIdentifiers)[_]
	reachable := specd.closure_from({specd.file_node(driver.path).id}, input.parameters.edgeKinds)
	call := specd.nodes_with_id(reachable)[_]
	specd.definition_node(call)
	re_match(input.parameters.sshPattern, call.text)
	not proxied(call)
	msg := sprintf("ssh invocation %s reachable from %s carries no allowed ProxyCommand transport", [call.qualifiedName, driver.path])
	details := specd.location(call.file, specd.node_match_line(call, input.parameters.sshPattern))
}
```

### A "require" rule

A requirement is a deny that fires when nothing satisfies it. Emit one
violation anchored at the first file so it still carries a real line:

```rego
violation[specd.violation(msg, details)] {
	files := specd.files_matching(input.parameters.guestGlobs)
	count(files) > 0
	not reports_out
	file := files[0]
	msg := sprintf("no module reports out: %s", [file.path])
	details := specd.location(file.path, 1)
}

reports_out {
	file := specd.files_matching(input.parameters.guestGlobs)[_]
	re_match(input.parameters.reportPattern, specd.code_graph.spec.texts[file.path])
}
```

## Testing

`specctl policy test --dir D` builds the library (writing `dist/`, refreshing
`lib/specd.rego`, rendering `CATALOGUE.md`), then runs:

- the opa unit tests of the library and of every template, in process;
- every `tests/*/suite.yaml` through the built-in Gatekeeper client.

Add `--gator` (or `SPECD_GATOR`) to also shell out to the real
`bin/gator verify`. `scripts/install-policy-tools.sh` installs pinned `opa` and
`gator` binaries into `bin/` by sha256.

Opa unit tests run in Rego v0 syntax:

```rego
package nodirectguestconnect

inventory := {"namespace": {"default": {"specs.publicdomainrelay.dev/v1alpha1": {"CodeGraph": {"x": { ... }}}}}}
review := {"kind": {"kind": "CodeGraph"}, "object": {"metadata": {"name": "x", "namespace": "default"}, "spec": {"repository": "x"}}}

test_violation_when_the_pattern_matches {
	call := {"parameters": {"globs": ["test/**"], "pattern": "Deno\\.connect"}, "review": review}
	violations := violation with input as call with data.inventory as inventory
	count(violations) == 1
}
```

A gator suite names the built template and the constraint relative to the
suite file, and asserts the violation count (and optionally a message
substring) per case:

```yaml
apiVersion: test.gatekeeper.sh/v1alpha1
kind: Suite
metadata: {name: relay-only-ssh}
tests:
  - name: relay-only-ssh
    template: ../../dist/relay-only-ssh.yaml
    constraint: ../../constraints/relay-only-ssh.yaml
    cases:
      - name: allowed
        object: inventory/codegraph-allowed.yaml
        inventory: [inventory/codegraph-allowed.yaml]
        assertions: [{violations: 0}]
      - name: denied
        object: inventory/codegraph-denied.yaml
        inventory: [inventory/codegraph-denied.yaml]
        assertions: [{violations: 2}]
```

The conformance test `impl/policyeval/conformance_test.go` runs every suite
under `examples/policies/`, `policies/` and `testdata/` through the built-in
engine and through `bin/gator verify` and fails when the two disagree;
`TestExampleDistAndCatalogueAreCurrent` also fails when a library's `dist/`,
`CATALOGUE.md` or `lib/specd.rego` is stale. `SPECD_REQUIRE_GATOR=1` makes a
missing gator fatal instead of a skip:

```bash
SPECD_REQUIRE_GATOR=1 go test ./impl/policyeval/...
```

`impl/policyeval/example_fixture_test.go` evaluates
`examples/policies/market-mini` against `fixtures/market-mini/{compliant,violating}`
with the real codegraph: the compliant variant must have zero deny violations
and the violating one must be denied by both policy groups. It skips when
`codegraph` is not on `PATH`.

## Evaluating

```bash
bin/specctl policy eval --worktree fixtures/market-mini/compliant
bin/specctl policy eval --worktree fixtures/market-mini/violating -o json
bin/specctl policy eval --repo atproto-market --commit 7a2e9d9 \
  --path ~/clones/atproto-market --library examples/policies/atproto-market
bin/specctl policy eval --repo deno-kcp --commit 0f1078d --path ~/clones/deno-kcp \
  --diff-base 0f1078d^ --library policies/library
bin/specctl policy eval --repo greenfield-market --specs-only \
  --path ~/clones/greenfield-market --library policies/packs/conformance --strict
```

The model behind an evaluation is one command:

```bash
bin/specctl policy model --worktree fixtures/market-mini/compliant
bin/specctl policy effects --worktree fixtures/market-mini/compliant --kind ssh.connect
bin/specctl policy model --repo atproto-market --worktree ~/clones/atproto-market \
  --library examples/policies/atproto-market -o json
```

`policy model` takes the same selection flags as `policy eval`
(`--worktree`/`--commit`/`--path`/`--branch`/`--test-glob`) plus `--library`
and `--classifiers`. It prints the components with their roles, the flows and
the triggers; `-o json` prints the `ArchitectureModel` object.

- `--worktree P` indexes a checkout; `--commit C --path R` exports that commit
  to a temporary directory and indexes it (the clone is never touched).
- `--diff-base REF` derives a `CodeDiff` between `REF` and the evaluated commit
  and reviews it alongside the `CodeGraph`, so a policy whose `match.kinds` is
  `CodeDiff` fires. The diff is computed with `git diff --unified=0
  --no-renames --no-prefix`, one `{path, status, added[], removed[]}` per file,
  and each added line carries its line number in the head file, so a violation
  points at a real line. `--diff-base` needs a real commit: with `--commit C`
  the diff runs in `--path`; with `--worktree P` it runs in `P`.
- `--library D` reads the library from a directory instead of the policy
  branch. Without it, the branch `open-policy/<repo>[--<branch slug>]` is
  read, falling back to `examples/policies/<repo>`.
- `--test-glob G` (repeatable) overrides the manifest's `testGlobs`.
- `-o json` prints the full `policy.Report`; the default prints a table.
- `--strict` exits 1 when a `deny` violation survives the repository's
  enforcement cap.
- `--specs-only` skips the code entirely: it reads the `SystemContext`s from
  `specs/*.yaml` on the `open-architecture/<repository>` branch, builds the
  declared-only `ArchitectureModel` and evaluates the library against it. It is
  the offline twin of `specd --policy-library` (see "Declared interactions and
  the spec-time gate"). `--worktree`, `--commit` and `--diff-base` do not apply.

Reports name `policy`, `constraint`, `enforcementAction`, the reviewed object,
`file:line` and the message; `-o json` also carries `details` and the violation
`id`.

## Limits

Known limits, stated rather than hidden. The first two bound what a policy can
see; the third bounds the classifier a policy reads.

### The indexer emits declarations, not bodies

The CodeGraph is read from the external `codegraph` index
(`@colbymchenry/codegraph`, `impl/codegraphsqlite`). That index holds one node
per declaration -- `function`, `method`, `class`, `constant`, `variable`,
`property`, `route`, `interface`, `struct`, `type_alias` -- plus one `file`
node and one `import` node per import. There is no node for a closure, an arrow
function or a call expression.

A `Deno.test("...", () => { ... })` body is therefore invisible: a test file is
its `file` node plus its imports, and nothing else. `new Deno.Command("ssh",
...)` inside such a body becomes a *file-level* effect -- its `node` is
`file:<path>`, its component is the file's role (the `test` role), and its only
flow is `test -> unknown` with no channel. A rule that denies an ssh outside
the guest role carried by no relay-channel flow still denies it (proved by the
suite case `denied-ssh-inside-a-test-body`), but a rule that needs the
declaring symbol, its line, or a channel from the test cannot see the body at
all.

This is the tool's limit, not the pack's: `codegraph 1.6.0` exposes no option,
config file or environment variable to index bodies (`init`, `index` and `sync`
take only `--force`, `--quiet`, `--verbose`, `--yes`), and its kind list has no
closure or expression kind. The workaround a pack has is to move the transport
into a named declaration the test calls, so the effect gets a real node (see
plan 0009 G4 gap (a)). The same granularity caveat appears under "The
CodeGraph" above: a callback is part of its enclosing node's text.

### Effect recall is measured against hand labels

The effect classifier (`impl/effects`) does not see every call. The honest
number is the hand-labelled sample, not the earlier calibration, which compared
the classifier against grep rules written from the classifier itself:

| kind | tp | fp | fn | precision | recall |
| --- | --- | --- | --- | --- | --- |
| `http.request` | 13 | 0 | 4 | 1.000 | 0.765 |
| `net.dial` | 0 | 0 | 1 | 1.000 | 0.000 |
| `proc.exec` | 3 | 0 | 40 | 1.000 | 0.070 |
| `ssh.connect` | 2 | 0 | 0 | 1.000 | 1.000 |
| total | 42 | 0 | 45 | 1.000 | 0.483 |

Precision is 1.000 on this sample -- the classifier does not invent effects --
but recall is 0.483: `proc.exec` is the weak kind, and `net.dial` is not
classified at all here. A policy that must catch every exec of a transport
cannot rest on the classifier alone; the calibration commands and the label
file are `scripts/effects-recall.py` and
`testdata/effects-recall/atproto-market-master.yaml`, and the reading is
recorded in plan 0009 G2.

### A tunneled ssh whose transport the vocabulary does not name

`relay-only-guest-ssh` denies an ssh outside the guest role that is carried by
no `relay`-channel flow *and* carries no non-empty `proxyCommand`. The second
half is deliberate -- an ssh whose proxy command is built elsewhere must stay
out of the report -- but it also means the rule cannot check the transport of
an ssh the model could not resolve a channel for. Such an ssh is not denied:
the rule sees its `proxyCommand`, so it treats it as tunneled, and it cannot
tell a relay from a transport the vocabulary does not name. What the rule
still catches is the regression it guards -- a direct ssh with no tunnel at all
-- plus any `net.dial` from a test that acts on the guest. `docs/plans/0009`
G4 states the same limit; the pack's `CATALOGUE.md` states it beside the rule.

## Troubleshooting

- **`undefined function data.lib.specd.<name>`** -- the library in the
  directory is older than the binary. Run `specctl policy build --dir D` (or
  `init`) to refresh `lib/specd.rego`, and rebuild `bin/specctl`.
- **A rule passes but should not** -- check the graph granularity: a
  TypeScript closure is part of its enclosing node, and an interface method
  call has no resolved edge. Anchor the pattern to a call syntax
  (`.getNodeId\s*\(`) rather than a bare name, and remember that comments
  inside the node's text are matched too.
- **A violation prints `file:0`** -- the rule built `specd.location` with a
  literal or no line. Use `specd.node_match_line` or `specd.first_line`.
- **`policy test` fails on a suite but the opa unit test passes** -- the suite
  runs the constraint's parameters from `constraints/<slug>.yaml` and the
  first-match review object from `tests/<slug>/inventory/`, not the unit
  test's inline parameters. Check globs and event identifiers against the
  inventory's file paths.
- **gator and the built-in engine disagree** -- they are the same client by
  construction; a mismatch means a stale `dist/`. Rebuild.
- **`no policies for <repo>`** -- create the library
  (`specctl policy init --repo X`) or point `--library` at an example.
- **`--worktree` scores stale code** -- the worktree is indexed in place
  (`<worktree>/.codegraph/`, gitignored) and an existing index is reused
  without re-checking the source. Delete it after editing the worktree:
  `rm -rf <worktree>/.codegraph`.

## The two example policies

`examples/policies/atproto-market` holds the phase B acceptance policies, with
`examples/policies/market-mini` as the fixture-parameterized twin:

- `relay-only-ssh` (P-relay): integration tests that drive a bidder and a
  requester must make every ssh over the relay -- a `ProxyCommand` whose
  transport is on the allowed list -- and must never dial a guest address
  directly.
- `guest-report-reach-in`, `guest-report-driven-emission`,
  `guest-report-driven-onnetwork`, `guest-report-cloud-init`
  (P-guest-reports, one policy as a set of templates): the host must not reach
  into the guest from the network emitter; the guest's network identity and the
  `vm.onNetwork` event must each be emitted from an inbound guest report rather
  than from the provisioning lifecycle (two constraints of the
  `guest-report-driven-emission` template, reported separately); and a
  cloud-init `UserDataModule` must publish the guest's address or routing
  outbound.

  The onNetwork constraint is the strict reading of the requirement: whatever
  the `vm.onNetwork` record carries, the host may emit it only in response to
  the guest's outbound report. A record produced by the provisioning lifecycle
  fails even when it carries no address (or only a provider-assigned
  container IP), because the host -- not the guest -- decided the guest was on
  the network.

`docs/examples/atproto-market-policies.md` runs them against
`publicdomainrelay/atproto-market` at three refs and records the real output.

`examples/policies/deno-kcp` is the second bound library: the seven library
templates plus `relay-only-ssh` re-bound to deno-kcp's roles and vocabulary
(host = the DenoPod provider, guest = the DenoPod workload manifests, requester
and relay = target roles, test = the integration harness). It is plan 0008
phase H's baseline library; `docs/examples/deno-kcp-pr.md` records pull request
#1's evaluation against it.
