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
  NSID of the initiating effect) and `symbols` (a regex over the effect's node
  text). They are reviewed knowledge, not derived facts, so keep them narrow.
- `vocabulary` maps project names to the abstract classes the model uses.
- `imports` is the plan-0009 G4 pack import list; it is parsed and carried but
  not yet resolved.

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
  flows:      [{from, to, initiator, channel, carries: [...], purpose, source, evidence: [effect ids]}]
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
- Declared interactions (plan 0009 G3, not yet in the SystemContext schema)
  are accepted by the builder as `ModelInput.Interactions`. A declared flow
  that matches an observed one is merged and its `source` becomes `both`.
- **triggers** are call-graph reachability between effect sites: for each
  effect of kind `http.handle`, `event.receive`, `proc.exec`, `container.exec`
  or `http.request`, every initiating effect within
  `DefaultMaxReachHops` (3) `calls`/`instantiates` edges. Same-site and
  over-long walks are dropped. The relation is coarse: the TypeScript indexer
  emits one node per declaration, so a callback and its enclosing handler share
  a node, and a method call on an interface value has no edge. A pack that
  needs "this emitter runs from that handler" must read the trigger's `from`
  effect and its kind, not merely its presence.

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

| slug | reviews | denies | origin |
| --- | --- | --- | --- |
| `change-succeeded-with-failed-acceptance` | `SpecChange` | `status.phase: Succeeded` while an acceptance step reports `passed: false` and was not overridden | `change_integrity` |
| `requirement-text-has-machine-path` | `SpecChange` | a delta requirement's `to.text` naming `/home/...`, `/Users/...`, `/tmp/...` | `spec_structure` |
| `provisioning-container-in-test` | `CodeDiff` | an added test line running `docker/podman/container/nerdctl run\|exec` | `code_safety` |
| `provisioning-manual-key-material` | `CodeDiff` | an added line writing `authorized_keys` or running `ssh-keygen` | `code_safety` |
| `provisioning-cloud-init-bypass` | `CodeDiff` | an added line that names `user_data`/`cloud-init` and a skip/bypass marker | `code_safety` |
| `provisioning-new-guest-transport` | `CodeDiff` | an added line naming `websocat`, `wstunnel`, `chisel`, `frpc/frps`, `autossh`, `rathole` or `socat` without a `UserDataModule` marker | `code_safety` |
| `security-disabled-verification` | `CodeDiff` | an added line with `curl -k`, `--insecure`, `--validate=false`, `InsecureSkipVerify` or `rejectUnauthorized: false` | `change_security` |

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
real output against `publicdomainrelay/deno-kcp` at `0f1078d`, a commit that
adds the deploy manifests, a restart-safe runner and a live kcp lifecycle test:

```bash
bin/specctl policy eval --repo deno-kcp \
  --commit 0f1078d8ab68387d940bfea5e630e732716c7aa8 \
  --diff-base 0f1078d8ab68387d940bfea5e630e732716c7aa8^ \
  --path ~/src/publicdomainrelay-kcp/deno-kcp \
  --library policies/library
```

```
repository: deno-kcp  commit: 0f1078d8
templates: 7  constraints: 7
violations: 5 (deny 5, warn 0, dryrun 0)

deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         deploy/start-kcp.sh:21
         added line deploy/start-kcp.sh:21 is "curl -sk \"https://127.0.0.1:${KCP_SECURE_PORT}/readyz\" >/dev/null 2>&1", which turns certificate verification off; a check that trusts any certificate checks nothing
deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         internal/provider/live_lifecycle_test.go:257
         added line internal/provider/live_lifecycle_test.go:257 is "client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}", which turns certificate verification off; a check that trusts any certificate checks nothing
deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         internal/provider/live_lifecycle_test.go:298
         added line internal/provider/live_lifecycle_test.go:298 is "cmd := exec.Command(\"kubectl\", \"--kubeconfig\", kubeconfig, \"--server\", server, \"apply\", \"--validate=false\", \"-f\", \"-\")", which turns certificate verification off; a check that trusts any certificate checks nothing
deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         deploy/install-provider.sh:12
         added line deploy/install-provider.sh:12 is "KA() { \"$KUBECTL\" --kubeconfig=\"$KUBECONFIG_PATH\" apply --validate=false \"$@\"; }", which turns certificate verification off; a check that trusts any certificate checks nothing
deny     error      security-disabled-verification  CodeDiff default/deno-kcp
         deploy/install-provider.sh:21
         added line deploy/install-provider.sh:21 is "KWA() { \"$KUBECTL\" --kubeconfig=\"$KUBECONFIG_PATH\" --server=\"$PROVIDER_SERVER\" apply --validate=false \"$@\"; }", which turns certificate verification off; a check that trusts any certificate checks nothing
```

One of the five is the class `RESULTS.md` found at `accept.sh:141`: a `curl -sk`
probe of a TLS listener. The other four are three `--validate=false` on a
`kubectl apply` (two in `deploy/install-provider.sh` helpers, one in the live
test) and one `InsecureSkipVerify: true` in that test's http client. The
`curl -sk` probe of a local kcp with a self-signed certificate is the known
intentional case; a repository that means it silences the rule for those paths
with the constraint's `allowPatterns`, which is why the parameter exists. The
three `--validate=false` lines are the pattern the origin's own
`disabled_verification_pattern` names first, so this run reproduces the
calibration rather than discovering a new class.

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

Reports name `policy`, `constraint`, `enforcementAction`, the reviewed object,
`file:line` and the message; `-o json` also carries `details` and the violation
`id`.

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
