# Plan 0009: portable policies, roles, effects and flows

Plan 0008 makes policies first-class. It does not yet make them portable.
This plan closes that gap.

## The gap

Plan 0008's policies read the CodeGraph: files, symbols, call edges and text.
Their parameters are the concrete things of one project:

- path globs;
- identifiers such as `createMarketBidder`, `runComputeContract` and `getNodeId`;
- regexes such as `ProxyCommand=.*dumbpipe` and `container exec`.

That works for atproto-market. It has three limits.

1. **Other projects.** To reuse "the guest reports out, the host never reaches
   in" in another repository, someone must find every identifier and pattern
   again. The logic is about roles and the direction of connections, but it
   is written in terms of one codebase's names. Moving the policy means
   rewriting most of it.
2. **Greenfield.** A project with only specs has no CodeGraph. Every
   plan-0008 policy is silent until code exists, which is too late. The
   decisions the policies guard are made in the spec: who talks to whom, who
   initiates, over which channel. A SpecChange that plans for the provider to
   ssh into the guest should be denied before any code is written.
3. **Language.** "Opens a network connection", "execs a process" and "serves
   an HTTP route" are written differently in TypeScript, Go and shell. A regex
   parameter list per policy per language does not scale.

The policies the user asked for are architectural invariants:

- **P-relay:** a test's ssh to a guest goes through the relay.
- **P-guest-reports:** the host never initiates toward the guest for network
  discovery; the guest initiates toward the host and carries its network
  information.

Both are statements about **roles**, **effects** and **flows**, not about
symbols. Policies must be written at that level. A per-project **binding**
maps the abstract model onto concrete code and specs. Only the binding
changes from project to project.

## Design: three layers

```
policy (portable)    reads ArchitectureModel: components, roles, effects, flows
   ^
binding (per repo)   roles -> contexts/globs/symbols; channel names; payload classes
   ^
facts                declared (specs: SystemContext interactions)  +  observed (CodeGraph + effect classifiers)
```

### 1. Effect classifier packs (language level, shared by all repos)

A classifier pack is data (YAML), versioned and embedded in specd. Each pack
is keyed by language: typescript/deno, go and shell first. It maps code
constructs to a fixed effect vocabulary.

| effect | meaning | attributes |
| --- | --- | --- |
| `net.dial` | opens an outbound connection | target expr, scheme, port |
| `net.listen` | accepts connections | address |
| `http.request` | outbound HTTP/XRPC call | url expr, method, nsid |
| `http.handle` | serves a route | method, path, nsid |
| `proc.exec` | runs a process | argv0, args |
| `ssh.connect` | ssh client invocation | proxyCommand, target |
| `container.exec` | exec/inspect into a container or VM | runtime, verb |
| `event.emit` | publishes a record or event | type (e.g. an NSID) |
| `event.receive` | consumes a record or event | type |
| `secret.read`, `file.write`, ... | extendable | |

Matchers are:

- call targets: a qualified name, or a member call such as
  `Deno.Command(argv0="ssh")`;
- the import specifiers of a module;
- string literals in call arguments;
- route registrations (codegraph `route` nodes).

`impl/effects` computes effects once per CodeGraph, in Go, and records each
effect's node id and file:line. Repos may add packs on their policy branch
(`classifiers/*.yaml`), for example their own RPC helper as `http.request`.
Each pack has unit fixtures per language.

### 2. Roles and bindings (repo level)

The policy branch's `policies.yaml` gains two sections.

`roles:` names the role and lists its selectors. Selectors are any of:
SystemContext names, labels, path globs and symbol regexes.

```yaml
roles:
  host:      {contexts: [market-bidder-compute, hono-bidder], globs: ["lib/*compute-provider*/**"]}
  guest:     {globs: ["lib/common/cloud-init-common/**"], declared: true}
  requester: {contexts: [requester-xrpc, request-vm-ssh]}
  relay:     {contexts: [did-key-ingress-proxy]}
  test:      {globs: ["test/**"]}
```

`vocabulary:` maps project names to abstract classes:

```yaml
vocabulary:
  events:   {network-report: ["com.publicdomainrelay.temp.compute.events.vm.onNetwork", "...registerIdentity"]}
  channels: {relay: ["websocat", "fedproxy", "dumbpipe", "iroh connect"]}
  payloads: {network-info: [address, nodeId, ticket, route, fedproxy]}
```

Roles also come from specs. A SystemContext may carry the label
`specs.publicdomainrelay.dev/role: host`, and greenfield specs declare roles
before any code exists.

### 3. Declared interactions (spec level, enables greenfield)

`SystemContext.spec` gains an optional `interactions` field:

```yaml
interactions:
  - {peer: guest, initiator: peer, channel: relay, carries: [network-info], purpose: network-discovery}
  - {peer: requester, initiator: self, channel: relay, carries: [event:network-report]}
```

Details:

- The CLM context documents gain the matching block, and the CLM apply guard
  covers it as it covers requirements.
- Populate and the CodeToSpec path propose interactions from observed flows,
  so existing repos get them filled in.
- The coverage judge checks that declared interactions are realized.

### 4. ArchitectureModel (the review kind portable policies read)

The ArchitectureModel is derived per evaluation. It is put in inventory and
is reviewable through `match.kinds`.

```yaml
kind: ArchitectureModel
spec:
  components: [{name, roles: [..], context, source: declared|observed|both}]
  effects:    [{id, component, kind, attrs, file, line, node}]
  flows:      [{from, to, initiator, channel, carries: [...], purpose, source: declared|observed, evidence: [effect ids]}]
  triggers:   [{from: effect id, to: effect id}]   # call-graph reachability between effect sites
```

How observed flows are built:

- A `net.dial`, `http.request` or `ssh.connect` effect in component A is
  matched to a target role through:
  - binding target hints (hosts, NSIDs, route paths matched to an
    `http.handle` effect in component B);
  - the channel vocabulary.
- `triggers` record, for example, that an `event.emit network-report` is
  reachable from an `http.handle` that receives the guest's report, or from a
  lifecycle callback instead.

Declared flows come from interactions. A conformance rule checks that
observed flows ⊆ declared flows, and that each declared MUST flow has
evidence once code exists.

`lib.specd` gains model helpers:

- `flows_where(...)`;
- `components_with_role(r)`;
- `effects_of(component, kind)`;
- `triggered_by(effect)`;
- `declared(flow)` and `observed(flow)`.

## Portable packs

A policy pack is a versioned directory of templates. Constraints are
parameterized only by role, vocabulary and enforcement, never by file names
or identifiers. Packs:

- are embedded in specd (`policies/packs/<name>`), or come from a git ref or
  an OCI artifact (oras is already a dependency);
- are imported by `policies.yaml`:
  `imports: [{pack: rfp-guest-isolation, version: v1, source: embedded}]`;
- are resolved and pinned by sha256 at build.

The first pack is **`rfp-guest-isolation`**, P-relay and P-guest-reports
rewritten over the model:

- `no-host-reach-in`:
  - no flow with initiator in role `host` and target role `guest` where the
    purpose is `network-discovery`, or where it carries `network-info`;
  - no `container.exec` or `ssh.connect` effect in `host` targeting a guest.
- `guest-reports-network`:
  - a declared or observed flow `guest -> host`, initiator `guest`, that
    carries `network-info`, exists;
  - every `event.emit network-report` in `host` is triggered by the
    `http.handle` of that flow, not by a provisioning lifecycle effect.
- `relay-only-guest-ssh`: every `ssh.connect` reachable from a `test` that
  uses `requester` and `host` has its channel in vocabulary `relay`, and no
  `net.dial` from `test` reaches `guest`.

The pack is written once. It is bound to:

- atproto-market;
- fixtures/market-mini;
- a second real repository (deno-kcp's DenoPod provider, or another org repo
  with a guest/host split, chosen by measuring);
- a greenfield spec-only repository.

Only `policies.yaml` changes between them.

## Spec-time gate (greenfield)

- A SpecChange is evaluated **before realize**, against the ArchitectureModel
  built from the post-delta specs (declared facts only). A denying violation
  sends the change back to drafting with the messages, and nothing is
  realized.
- After realize, the plan-0008 gate also checks observed against declared.
  Policies are the same in both places; only the source of facts differs.

## Generation becomes portable

Plan-0008 phase D generates templates against CodeGraph identifiers. Moved
here, generation does two things:

1. **Policy.** It generates the policy over the model, from the sentence. It
   is project-independent and is reviewed once, then lives in a pack.
2. **Binding.** It generates the `roles` and `vocabulary` for one repository,
   from arch + specs + effects.

specd checks a generated binding:

- every role selects at least one component;
- every vocabulary entry matches at least one effect or spec term;
- the pack's gator suites pass under it;
- a mutation test holds: deleting the guest report flow from the model makes
  `guest-reports-network` deny.

`specctl policy bind --repo X --pack rfp-guest-isolation` proposes a binding
as a PolicyChange.

## Phases

### G1. Effects -- done (`07941df`, `985c7e2`, `0787a11`)

`impl/effects` and the classifier packs for TypeScript/Deno, Go and shell,
with per-language fixtures. `specctl policy effects --worktree P` prints the
effects. Calibrated on atproto-market and on hydradb itself:

- every `ssh`, `fetch`/`callService`, `Deno.Command`, route and
  `createRepoRecord` site in atproto-market is classified correctly;
- the misses and false positives are recorded and fixed.

What is built:

- `abc/policy` (pure): `EffectKind` and the fixed vocabulary, `Effect`
  (`id`, `kind`, `component`, `context`, `attrs`, `file`, `line`, `node`),
  `ClassifierPack`, `EffectRule` and its matchers (qualified call, member
  call, shell command with a verb, import specifier, string literal args,
  route node), attribute extraction, `Compile` and pure matching over the
  CodeGraph texts, nodes and edges. `CodeGraph.spec.effects` carries the
  result into the inventory; `lib.specd` reads it through `effects`,
  `effects_of(kind)`, `effects_of_component(component, kind)`,
  `effects_in(globs)` and `effect_targets(kind)`.
- `impl/effects`: the embedded packs `packs/typescript.yaml`, `go.yaml`,
  `shell.yaml`, extra packs from `<worktree>/classifiers/*.yaml` or from
  `--classifiers DIR`, and `Compute`/`Apply`. A rule marked `extra: true`
  (the XRPC helpers `callService`, `createRepoRecord`,
  `createSignedRepoRecord`, `putRecord`, `getRecord`) is off with
  `--no-extras`.
- `specctl policy effects`, and `specctl policy eval` now computes the
  effects onto the CodeGraph before evaluating.

Calibration, `scripts/effects-calibration.py` (grep truth per kind, language
scoped, with a code mask on the truth side; the worktree is the whole
checkout, tests included):

```
$ python3 scripts/effects-calibration.py --repo atproto-market \
    --worktree /home/johnandersen777/policy-g1-work/atproto-market
kind           truth  found  missed  extra
event.emit        11     11       0      0
event.receive     13     13       0      0
http.handle       60     60       0      0
http.request      34     34       0      0
net.listen        38     38       0      0
proc.exec         15     15       0      0
ssh.connect        2      2       0      0
total            173
```

atproto-market `master` 7a2e9d9, fresh clone, `codegraph` index 260/260
`.ts` files. The two `ssh.connect` sites are
`lib/requester-xrpc/mod.ts:691` and `:734`; `proxyCommand` follows the call
to `sshTunnelArgs` one hop so it carries the `websocat` channel, and
`ssh-keygen` is `proc.exec`, not `ssh.connect`.

```
$ python3 scripts/effects-calibration.py --repo hydradb --worktree .
kind           truth  found  missed  extra
container.exec      2      2       0      0
event.emit          2      2       0      0
event.receive       1      1       0      0
file.write        102    102       0      0
http.handle        11     11       0      0
http.request        7      7       0      0
net.dial            7      7       0      0
net.listen          5      5       0      0
proc.exec         111    111       0      0
ssh.connect         5      5       0      0
total             253
```

hydradb at the G1 commit. `file.write` is `os.WriteFile`/`os.Create`,
`http.handle` includes the `mux.HandleFunc` route nodes of `fixtures/`, and
`ssh.connect` comes from the fixture `ssh.Dial` behind the `crypto/ssh`
import.

Fixed by calibration: comment and string decoys (a `Deno.Command("ssh")`
inside a string or a comment is not a site); a TypeScript regex literal
(`/["']/`) no longer swallows the rest of the file (it cost 71 effects
before the fix); command substitution inside double quotes in shell; the
extraction group default; `net.Dial`'s address is argument 1, not 0;
`crypto/ssh` also matches without the `golang.org/x` prefix; `proxyCommand`
keeps the whole expression so a vocabulary can match `websocat` in an
interpolated template.

Remaining, honestly:

- The classifier sees what the codegraph index sees. That index skips a
  directory named `coverage`, so `impl/coverage/*.go` (2 files, one
  `exec.CommandContext`) is invisible to every policy, not only to effects.
  It is the only Go file gap in hydradb; atproto-market is complete.
- Shell files are not indexed by codegraph at all, so `shell.yaml` is
  exercised by the unit fixtures and by repositories whose graph carries
  shell. The RFP cloud-init shell lives inside TypeScript strings, which the
  mask removes; a guest-side command written in `user_data` is not an effect
  today. G4/G5 should revisit this (the guest side is exactly where
  `container.exec` and `ssh.connect` matter).
- `argv0` from a variable (`new Deno.Command(cmd, ...)`) is `proc.exec` with
  no `argv0`; a `container.exec` verb that is computed, not literal, is not
  extracted.
- A file bigger than the CodeGraph text cap (256 KiB) is classified over its
  first 256 KiB. No file in either corpus reaches the cap.
- `specctl policy effects` reports an empty context when the checkout has no
  SystemContexts; G2 maps components and roles onto that field.

### G2. Model and bindings -- done

What is built:

- `abc/policy/model.go`: `ModelComponent`, `ModelFlow`, `ModelTrigger`,
  `ArchitectureModel` with `ComponentsWithRole`, `EffectsOfComponent`,
  `FlowsWhere(FlowFilter)`, `TriggeredBy`, `Declared`, `Observed`, and the
  `ArchitectureModelKind`.
- `abc/policy/binding.go`: `Binding` = `roles` + `vocabulary` + `imports`
  (the G4 placeholder), `RoleBinding` with `contexts`, `labels`, `globs`,
  `symbols` and `targets` hints, `RoleTargets` with `hosts`, `nsids`, `routes`
  and `symbols`. `PolicyLibrary` gained `roles`, `vocabulary` and `imports`, so
  `policies.yaml` carries them and the model is built from the same file as the
  rest of the library.
- `abc/policy/modelbuild.go`: `BuildModel(ModelInput)` = CodeGraph + effects +
  `[]ModelContext` (name + labels) + `Binding` + `[]DeclaredInteraction` ->
  `ArchitectureModel`. Components are the SystemContexts and the role-owned
  file groups (longest matching glob, then role name); roles come from
  `contexts`, `labels`, `globs`, `symbols` and the
  `specs.publicdomainrelay.dev/role` label. Observed flows resolve the target
  role through the `http.handle` effects of the path or NSID, then the role
  target hints, then a symbol hint over the effect's node text; `channel`,
  `carries` and `purpose` come from the vocabulary, case-insensitive, over the
  effect's node text and attributes. Triggers are calls/instantiates
  reachability from `http.handle`, `event.receive`, `proc.exec`,
  `container.exec` and `http.request` to the initiating effects, bounded at 3
  hops and 4096 nodes, deterministic, self-sites dropped. Declared interactions
  are merged and promote a matching observed flow to `both`.
- `ArchitectureModel` is a reviewable kind: `specctl policy eval` puts it in
  both the inventory and the reviewed set, so `match.kinds: [ArchitectureModel]`
  works; `docs/policies.md` documents it, the binding and the helpers.
- `specctl policy model --worktree P [--library D] [-o json]` prints
  components, roles, flows and triggers.
- `lib.specd` gained `architecture_model`, `model_components`, `model_flows`,
  `model_triggers`, `components_with_role`, `roles_of`, `flows_where`,
  `triggered_by`, `declared`, `observed`, with 14 new opa v0 unit tests (39 in
  the library, all passing).
- `examples/policies/atproto-market/policies.yaml` and
  `examples/policies/market-mini/policies.yaml` carry roles and vocabulary;
  the first is bound to atproto-market, the second to the fixture.
- `abc/policy/modelbuild_test.go` covers components, roles, observed flows,
  channels/payloads/purposes from the vocabulary, triggers, declared
  interactions and the merge.

#### The model on atproto-market

Fresh clone under `/home/johnandersen777/policy-g2-work/atproto-market`, never
edited or pushed, `specctl policy model --repo atproto-market --worktree ...
--library examples/policies/atproto-market`:

- master `7a2e9d9`, 204 effects, 5 components (guest, host, relay, requester,
  test; no SystemContexts exist at this ref, so every component is
  `source: observed` and named by its role). The reach-in is there:
  `host -> guest initiator=host channel=relay carries=[network-info]
  purpose=network-discovery` with evidence `container.exec
  lib/market-bidder-compute/mod.ts:282`. That is exactly plan 0008 phase B's
  `guest-report-reach-in` site, now derived from an effect instead of a regex.
  The emission side is `host -> requester` with the `event.emit` sites
  `lib/market-bidder-compute/mod.ts:257` (vm.onNetwork) and `:284`
  (registerIdentity).
- `pre-iroh` `d20070c`, 189 effects, 15 flows: no `host -> guest` flow at all,
  which is plan 0008 phase B's zero-violation verdict at that ref. The model
  reproduces the three refs' verdicts, so G4's pack can be calibrated against
  it.
- `spec/iroh-dumbpipe-20261004141803` `ffac22e`, 208 effects, 18 flows. The
  guest reports out: `guest -> requester initiator=guest channel=relay
  carries=[network-info] purpose=network-discovery`, evidence `http.request
  lib/common/cloud-init-common/mod.ts:449` and `:450` -- the `curl` inside the
  cloud-init template string. The handler side is `http.handle POST
  /v1/on-network` at `lib/market-bidder/mod.ts:475`.
- Triggers distinguish the two: at master there is no `/v1/on-network` handler
  at all, so the vm.onNetwork emit is triggered only by provisioning-side
  effects (`hono-bidder/mod.ts:428` and the compute calls); on the spec branch
  the handler at `lib/market-bidder/mod.ts:475` triggers the emits at
  `:490` and `:495`. The relation is coarse (see the docs): node granularity is
  one node per declaration, so a policy that reads it must check the trigger's
  `from` kind.

#### Making the guest side visible

G1 recorded that the cloud-init shell lives inside TypeScript strings and was
therefore invisible. Fixed cheaply in the classifier:

- `inStrings: true` on a command rule matches inside string literals only
  (comments excluded), so `ts-string-curl` and `ts-string-ssh` classify the
  guest's report and any ssh inside `user_data`. Verified on master: the three
  real `curl` commands in cloud-init (`:88`, `:92`, `:376`) are
  `http.request`, and nothing else is.
- `command.args.regex` (new: the args matcher now also applies to commands)
  keeps a `- curl` package entry and `systemctl enable --now ssh` out. Both
  were false positives before the fix: four `ssh.connect` and three
  `http.request` in cloud-init.
- `call.any: true` matches bare and member calls, and the extra rules
  (`callService`, `createRepoRecord`, `createSignedRepoRecord`, `putRecord`,
  `getRecord`, `applyWrites`) carry an `args.regex` that rejects declarations.
  Before this, the plan-0008 target itself was missed: `createRepoRecord(...)`
  at `lib/market-bidder-compute/mod.ts:257` is a bare call, and the rules
  required a member call. `applyWrites` was added for the same reason
  (`lib/requester-xrpc/mod.ts:512`, `:542`, `:557`).
- `compute-provider-guest-query` maps `getNodeId` to `container.exec`
  (`inspect into a container or VM`), the effect that carries master's reach-in.

#### Recall, hand-labelled

The G1 calibration compared the classifier to grep rules that mirror it. This
sample does not: `testdata/effects-recall/atproto-market-master.yaml` holds
labels read from the source for four files,
`scripts/effects-recall.py --worktree ... --labels ...` compares them with the
classifier per kind (a label is found when the same kind is reported within one
line):

```
kind              tp  fp  fn  precision   recall
container.exec     1   0   0      1.000    1.000
event.emit        17   0   0      1.000    1.000
event.receive      2   0   0      1.000    1.000
http.handle        4   0   0      1.000    1.000
http.request      13   0   4      1.000    0.765
net.dial           0   0   1      1.000    0.000
proc.exec          3   0  40      1.000    0.070
ssh.connect        2   0   0      1.000    1.000
total             42   0  45      1.000    0.483
```

Honest reading:

- Precision is 1.000 on these files after the fixes above; the sample is small,
  four files and 42 true sites, so it bounds nothing globally.
- `http.request` recall 0.765: the misses are indirect aliases, `s.fetchHandler`
  (`lib/requester-xrpc/mod.ts:1522`, `:1539`) and `realFetch`
  (`request-vm-ssh/mod.ts:45`, `:47`). Not cheaply fixable in a shared pack;
  a repository pack can name them.
- `proc.exec` recall 0.070: 40 shell commands inside the cloud-init template
  strings (`systemctl`, `chmod`, `tar`, `rm`, ...) are not effects. The pack
  now sees the two commands the flows need, `curl` and `ssh`; the rest stay a
  recorded gap.
- `net.dial` 0.000: `serve.addRelay` dials the relay websocket inside the serve
  module; the caller never opens one, so there is no site to classify.

### G3. Declared interactions -- done (`ddb5c76`, `144c16f`, `795724f`)

What is built:

- **The schema.** `SystemContext.spec.interactions` is a keyed list (`id`,
  `peer`, `initiator`, `channel`, `carries`, `purpose`, `level`, `forbidden`)
  in `abc/spec`, in the CRD, in `deploy/apiresourceschemas` (schemagen
  revision 6, the APIExport names bumped with it) and in the delta of a
  `SpecChange`. `abc/spec.Canonicalize` sorts it by id and defaults the level
  to `SHOULD`, so an unset level and an explicit `SHOULD` hash the same, and
  `HashSystemContextSpec` covers the interactions: editing them is a real spec
  change. `ValidateSystemContext` refuses a missing id, a duplicate id, a
  missing peer, an initiator that is not `self` or `peer`, an unknown level and
  an empty carry. `abc/mirror` and `abc/oabranch` round trip the block to
  `specs/*.yaml` and name an interaction change in the branch record. The
  delta algebra (`abc/delta`, `spec.Delta`, the CLM mirror in `clm/core/delta.ts`)
  diffs it by id; a new golden `testdata/delta/interactions-edit.json` pins the
  JSON for both sides, and the CLM test compares them.
- **The CLM block.** `clm/core` renders and parses `interactions:` in the spec
  block (Go in `abc/clm`, TypeScript in `clm/core`), `impl/clm` refuses a
  removal the document does not name under `removed:` or pass to
  `--allow-remove` -- interactions and requirements share the marker -- and
  `cc-clm-mod`'s vendored core is rebuilt. `pi-hydradb-clm` reads the same core.
- **The model.** `abc/policy` builds declared flows from the interactions: each
  context's own list with the context as its self, `initiator: self|peer`
  resolved to the role that initiates, a `forbidden` marker kept as its own
  declared entry (dedupe never merges it with a flow that is not forbidden)
  and a matching observed flow merged to `both`. `ModelFlow` gained `level`
  and `forbidden`. `lib.specd` gained `forbidden(flow)`, `flow_level(flow, l)`,
  `same_flow(a, b)` and `forbidden_matches(flow)`, with opa unit tests; the
  directed shape they compare -- initiator, the role it acts on, channel,
  purpose -- is mirrored by Go's `policy.SameShape`. The model now carries the
  namespace `default`, without which an inventory lookup from a rule found
  nothing.
- **The conformance pack.** `policies/packs/conformance/` with three templates
  over the model: `ConformanceObservedUndeclared` (warn, an observed flow with
  no declared interaction of the same shape), `ConformanceMustUnrealized`
  (warn, a declared `MUST` flow with no observed evidence once the model
  carries effects) and `ConformanceForbiddenFlow` (deny, a declared or observed
  flow matching a declared must-never). Each has a constraint and a gator suite
  with an allowed and a denied case (6/6), the rules read optional flow fields
  with `object.get` (a flow without `evidence`, `channel`, `purpose` or
  `initiator` is common), and `impl/policyeval` fails when `dist/`,
  `CATALOGUE.md` or the embedded lib goes stale.
- **Proposing interactions.** `specctl policy model --propose-interactions`
  prints a pasteable `interactions:` block per component from the observed
  flows, with the initiator read off the model and the level proposed as
  `SHOULD`. The CodeToSpec drafting prompt is not yet given the flows: the
  summarize path has no binding or effects, so that half is G6's (it needs the
  binding G4 lands). The CLI is the minimum the plan asked for and it works on
  a real checkout.
- **The spec-time gate.** `policyeval.CheckSpecs` builds the ArchitectureModel
  from declared facts only (the post-delta specs, the binding, no code, no
  effects) and evaluates the library; `specd --policy-library DIR` calls it in
  the realize path before the agent runs. A deny is recorded on every member of
  the batch as `phase: Failed`, the message names the constraints, the
  condition `PolicyValid=False` carries `reason: PolicyDeniedAtSpec`, no
  worktree branch is opened and nothing is realized; a library that cannot be
  read fails the change with `PolicyGateError` rather than passing silently,
  and a change the gate now accepts has the condition cleared to `True`. The
  gate is off unless `--policy-library` is set (or `SPECD_POLICY_LIBRARY`).
  `specctl policy eval --specs-only` is the same check offline: it reads the
  contexts from the `open-architecture/<repository>` branch, builds the
  declared model and prints the report and `spec gate: allowed|denied`, with
  `--strict` for exit 1.
- **The live proof.** `fixtures/greenfield-market` is a spec-only repository
  (a README and a plan, no code). `test/e2e/policy_gate_live_test.go` starts a
  private kcp, applies the repository and two contexts, settles them against a
  clean spec, then edits the host to declare `initiator: self` toward the guest:
  the `SpecToCode` change comes back `Failed` with
  `policy denied at spec time: ... matches the declared must-never guest -> host`,
  `PolicyValid=False/PolicyDeniedAtSpec`, HEAD unchanged and no `spec/*` branch.
  The fixed spec (the guest initiates) passes the gate, realizes with the
  scripted agent and lands `market/host-report.md`. Green in 12 s.

Checked by hand too: the same two runs through `specctl policy eval
--specs-only --library policies/packs/conformance` on a temporary clone whose
`open-architecture/greenfield-market` branch carries the violating spec
(`spec gate: denied: ...`, 1 deny violation) and the fixed one (`clean`,
`spec gate: allowed`, exit 0 under `--strict`).

Remaining, honestly:

- Pack import (`imports: [{pack: ...}]`) and pinning landed in G4;
  `policy bind` is G6. The conformance pack is still a directory a repository
  copies -- it has no `pack.yaml`, so it is not importable yet -- and the
  binding is the repository's own `policies.yaml` (the e2e runs the pack with
  an empty binding, where a context name is its own role).
- The drafting prompt does not yet receive the observed flows (G6, above).
- `interactions` are not seeded from an open architecture document and
  `specctl export --format arch` does not carry them (the arch node body is
  free-form, so nothing is lost).

### G4. Packs -- done

What is built:

- **The pack format.** `policies/packs/<name>/` with `pack.yaml` (`name`,
  `version`, `description`, the required `roles` and `vocabulary` classes, the
  parameter defaults), `templates/<slug>/`, `constraints/`, `tests/`, and the
  generated `lib/`, `dist/` and `CATALOGUE.md`. `policyeval.LoadRaw` reads a
  directory with a `pack.yaml` as a pack (its `Repository` is the pack name,
  its roles and vocabulary stay empty) and `Load`/`LoadFS` read either.
  `policy.PackManifest.Missing(binding)` names the roles and classes a binding
  does not declare, and a build fails on them.
- **Import and pin.** `policies.yaml` gained the real `imports` list
  (`{pack, version, source}`); `source` is `embedded`, `git:<url>@<ref>` or
  `oci:<ref>`. `impl/policyeval/packs.go` resolves each one
  (`packSourceFS`; embedded from `policies/packs`' `go:embed` registry, git by
  clone + checkout with the pack at `<repo>/policies/packs/<name>` or the
  repository root, oci by an oras pull whose layers are laid out by their
  `org.opencontainers.image.title`), merges the pack's templates and
  constraints into the importing library (a name or kind collision fails), and
  digests the pack's *sources* (`pack.yaml`, `templates/`, `constraints/`,
  `tests/` -- never the generated `dist/`, so a build does not break its own
  pin) with sha256. `specctl policy build` writes `policies.lock`, and a later
  build whose pack resolves to a different digest fails until the version is
  bumped or `--relock` is passed. Verified by hand: bumping one hex digit of
  the pinned sha256 makes the next build refuse.
- **`policies/packs/rfp-guest-isolation` v1**, five templates over the
  ArchitectureModel and the binding's vocabulary: `RfpHostReachIn`,
  `RfpGuestReportsNetwork`, `RfpRelayOnlyGuestSsh` (the user's two rules,
  P-relay included), and the two model-form replacements
  `RfpGuestTransportProvenance` and `RfpKeyMaterialProvenance`. Each has a
  gator suite with allowed and denied ArchitectureModel (or CodeDiff)
  fixtures, and opa unit tests: `specctl policy test --dir
  policies/packs/rfp-guest-isolation --gator` is 82/82 unit tests and 13/13
  suite cases, and the built-in engine and the real gator agree case for case.
  The fixture library `fixtures/market-mini` runs the pack too:
  `TestExamplePoliciesOverFixtures` builds the ArchitectureModel over the
  fixture and the compliant variant is clean under the pack, the violating one
  trips `rfp-host-reach-in` and `rfp-guest-reports-network`.
- **The rules read only the model.** New shared helpers in `lib.specd`:
  `model_effects`/`model_effect`, `model_roles`, `model_vocabulary`/
  `vocabulary_terms`, `component_in_role`, `initiator_in_role`,
  `acted_on_in_role`, `event_class`, `route_class`, `file_in_role`, with 15 new
  opa unit tests. `lib.specd`'s `repository_name` also resolves for a
  `CodeDiff` review (a diff carries no repository field; it is named after the
  repository), so a rule that reads a diff still resolves the code graph.
- **Bindings.** `examples/policies/atproto-market`, `market-mini` and
  `deno-kcp` import the pack; their `policies.yaml` roles and vocabulary are
  the only per-repository input, and each carries its own `policies.lock`.
  atproto-market and market-mini both need the `guest.targets.attrs:
  [getNodeId]` hint for their `container.exec verb=getNodeId` reach-in; without
  it the target role resolves to `unknown` and the reach-in is invisible to the
  pack. `policykcp.Files` and the import both know which templates came from a
  pack (`Library.Imported`), so the repository's policy branch and the kcp sync
  carry the repository's own templates while kcp receives the resolved set: the
  live policy suite holds 9 templates and 10 constraints in kcp with 4 of them
  the repository's own.
  The require of `RfpGuestReportsNetwork` fires once the model carries effects,
  so a repository whose specs declare no interaction yet is not denied at spec
  time -- the reach-in rule guards the declared shape until the first realize.
  Measured on the live suite: before that guard, the spec-time gate denied
  every market-mini spec edit and four e2e tests failed; after it, all pass.

#### Model and classifier work the pack needed

Four changes, each because a rule could not be written honestly without it:

- **`RoleTargets.attrs`.** A hint matched against the *values* of an effect's
  attributes, not against the text around its site. `guest.targets.attrs:
  [getNodeId]` is what master's `container.exec verb=getNodeId` reach-in needs.
  With the old `symbols: [getNodeId]` the spec branch resolved a `host ->
  guest` flow from the word `getNodeId` inside a *comment* ("getNodeId is not
  part of the pinned ComputeProvider contract"), which the pack would have
  reported as a reach-in that does not exist. `hintSymbols` now match the
  site's own text and attributes; the rule is documented at the field.
- **`event.emit` carries its type.** The TypeScript pack extracts the first
  argument of `createRepoRecord`/`createSignedRepoRecord`/`putRecord` as
  `attrs.type` (the plan's effect vocabulary always said `type`, e.g. an
  NSID). Without it the pack cannot tell the `vm.onNetwork` emit from the
  `market.bid` one, because the vocabulary is resolved into flow attributes
  that a merged flow shares across many evidence effects.
- **The model carries the binding.** `ArchitectureModelSpec` gained `roles`
  and `vocabulary`, and `ModelComponent` gained `globs`, so a template reads
  the classes its pack declares and can tell whose file a path is.
- **Two-hop vocabulary lookup.** `flowIndex.hintText` now also reads the
  declarations the effect's site calls (bounded at 3 hops and 256 nodes,
  cached). On the spec branch the ssh `ProxyCommand` is
  `defaultProxyCommand(target, transport)` whose body holds `dumbpipe` and
  `websocat`; without the walk the ssh effect resolved no channel and
  `RfpRelayOnlyGuestSsh` would have reported the relayed ssh as unrelayed.
  Target resolution keeps reading the site alone, so the flow set does not
  drift.

The effects are unchanged at all three refs (203 / 189 / 202, counted after
this work); the flow sets change only in their attributes and their dedupe:
master 15 flows, pre-iroh 14, spec 16, where G2 recorded 15 for pre-iroh and
18 for the spec branch. The difference is the attribute and symbol hints: a
flow that G2 merged out of two targets is now two flows, or one, depending on
which hint resolves. The verdicts above do not depend on it.

#### The pack against plan-0008 phase B/B2

`bin/specctl policy eval --repo atproto-market`, fresh clone under
`/home/johnandersen777/policy-g4-work/atproto-market`, never edited or pushed.
The `library` column is the concrete plan-0008 library as it stands on this
branch; `pack` is `rfp-guest-isolation` bound to the same repository.

| ref | commit | library (concrete) | pack | same sites? |
| --- | --- | --- | --- | --- |
| `master` | `7a2e9d9` | 3 deny: `guest-report-reach-in` `:282`, `guest-report-driven-emission` `:284`, `guest-report-driven-onnetwork` `:257` | 3 deny: `RfpHostReachIn` (the `host -> guest` flow, evidence `container.exec` `:282`), `RfpGuestReportsNetwork` `:257` and `:284` | yes, site for site |
| `pre-iroh` | `d20070c` | 1 deny: `guest-report-driven-onnetwork` `:304` | 1 deny: `RfpGuestReportsNetwork` `:304` | yes |
| `spec/iroh-dumbpipe-20261004141803` | `ffac22e` | 1 deny: `guest-report-driven-onnetwork` `:313` | 1 deny: `RfpGuestReportsNetwork` `:313` | yes |

Every difference from the concrete policies:

| difference | why |
| --- | --- |
| The pack reports the master reach-in from the *flow* (`host -> guest`, initiator `host`, purpose `network-discovery`, carries `network-info`), not from a `getNodeId` regex over the emitter's reachable set. | The flow is the same fact derived from the effect's target role instead of from a name pattern. The separate effect rule (`container.exec`/`ssh.connect` in a host component reaching the guest) stays as the backstop for an effect whose flow has no network purpose or payload; it is suppressed for a flow the first rule already reported, so master is 1 violation and not 2. |
| `guest-reports-network`'s require is "the guest initiates toward another role carrying `network-info`", not "guest -> host". | At master and `pre-iroh` the observed report flow resolves to `guest -> unknown` and on the spec branch to `guest -> requester`; the handler that receives it lives in `lib/market-bidder` (host). The strict "guest -> host" reading would deny at refs phase B accepts. The constraint takes an optional `reportPeerRoles` list for a repository that wants the narrow form. |
| The emission rule requires the trigger root to be an `http.handle` **on the `routes/report` route**, not any `http.handle`. | The model's triggers are coarse (one node per declaration): on master the emits in `lib/market-bidder-compute` are reachable from `hono-bidder/mod.ts:428` (the bidder's own route) and from test-file route registrations, so "triggered by some http.handle" is satisfied at every ref and would deny nothing. Naming the report route is what distinguishes `lib/market-bidder/mod.ts:475` (`POST /v1/on-network`) from the bidder's `/oauth-client-metadata.json`. |
| `relay-only-guest-ssh` checks every `ssh.connect` effect outside the guest role, not only those reachable from a test that uses the requester, and it accepts an ssh whose channel does not resolve when the effect carries a `proxyCommand`. | "Reachable from a test" is not in the model: a test file is file and import nodes, so an ssh inside a test body is a file-level effect whose component is the test role and whose flow target is unresolved. The rule is the generalization (any ssh outside the guest must be tunneled, and the relay when the vocabulary names the tunnel) plus the file-level catch. The `proxyCommand` clause is what keeps the compliant market-mini fixture clean: its requester builds `ProxyCommand=${transport.proxyCommand()}`, an injected transport object, so neither the site nor the call walk names a relay term. A tunneled ssh whose transport the vocabulary does not name is therefore out of the rule's reach; a direct ssh, which is the regression the rule guards, is not. |
| Three constraints instead of five concrete templates. | `guest-report-driven-emission` and `guest-report-driven-onnetwork` are one rule (the network-report event class) reported per emit site, so master is 2 violations from one constraint rather than 1+1 from two. |

#### Gap (a): an ssh inside a Deno.test body

`test/...` is file and import nodes only, so `new Deno.Command("ssh", ...)`
inside a `Deno.test` body has no declaration node: the classifier attaches the
effect to the file node (its `node` is `file:...`), its component is the `test`
role component and its only flow is `test -> unknown` with no channel.
`RfpRelayOnlyGuestSsh` denies it because it is an ssh outside the guest role
carried by no relay-channel flow -- no reachability from the test is needed.
Proved twice: the suite case `denied-ssh-inside-a-test-body`
(`tests/rfp-relay-only-guest-ssh/inventory/model-denied-file-level-ssh.yaml`,
whose effect carries `node: file:test/bidder_test.ts`) and the unit test
`test_violation_when_a_test_body_spawns_ssh_as_a_file_level_effect`.

#### Gap (b): the transport and key-material rules, without the false denies

The library's `provisioning-new-guest-transport` and
`provisioning-manual-key-material` read added CodeDiff lines against globs and
regexes. On atproto-market PR #1 (`ffac22e`, base `d20070c`) they report **21
denies, all false**: transport names in selectors
(`transport === "iroh" ? "dumbpipe" : "websocat"`), a lexicon description of
the legacy `websocat` ProxyCommand, test names, test assertions on
`authorized_keys`, and the `authorized_keys` the guest's own `user_data`
writes.

The pack's replacements read effects and roles:

- `RfpGuestTransportProvenance` denies an added line that names a transport
  *and* has an executing effect (a `proc.exec`, `container.exec`, `net.listen`
  or `file.write` starts on that line) *and* reads like an installation
  (`installPatterns`), in a file that no guest-role component owns
  (`file_in_role` over the binding globs).
- `RfpKeyMaterialProvenance` denies an added line that names key material
  (`authorized_keys`, `ssh-keygen`, a private-key header) and is executed or
  written, in a file that no guest-role component owns.

Measured on PR #1 head with the library and the pack:

```
library alone                    21 deny (19 false)
library + pack                    2 deny -- one real violation, named by both
                                  guest-report-driven-onnetwork and
                                  RfpGuestReportsNetwork at
                                  lib/market-bidder-compute/mod.ts:313
library + pack at pre-iroh       2 deny -- the same one violation, at :304
                                  (library alone over that diff: 52 deny)
```

So the one deny left is the strict-reading violation phase B2 found, and the
transport rules contribute none.

For the rules to gate a realize and not only an offline run, the CodeDiff a
realize reviews had to name its repository: `CodeDiffSpec.Repository` is set by
the realize gate and by `policy eval --diff-base`, so a rule that reads added
lines resolves the CodeGraph of the same evaluation (`lib.specd`'s
`repository_name` reads `spec.repository` first and still falls back to the
diff's name). `impl/realize/policy_diff_test.go` pins it: a probe constraint
that denies an added line carrying an effect sees the effect, and the test
fails when the field is not set. The `which dumbpipe` probe is what the
`installPatterns` list is for: `ensureDumbpipe` checks for the binary and
downloads it, and a probe names the transport without installing one.

#### Gap (c): the spec-time gate reads the same library as the realize gate

`Controller.specGate` no longer requires `--policy-library`. `specGateLibrary`
uses the flag as an override and otherwise calls the same `loadPolicyGate` the
realize gate uses (the repository's `open-policy/<repo>` branch, else kcp), so
an audit, a realize and a spec-time deny can no longer disagree about which
policies are in force. A repository with no policy branch gets no gate, as
before.

Remaining, honestly:

- The pack is bound to atproto-market, market-mini and deno-kcp but only
  atproto-market is measured against a real run; market-mini is exercised by
  the fixture library and deno-kcp by its own suites. G5 is where deno-kcp gets
  a real run.
- `git:` and `oci:` pack sources are implemented and compile, but only
  `embedded` is exercised: no test pulls a pack from a repository or a
  registry. The oras path has no unit test of its layout.
- The pack's `RfpGuestReportsNetwork` require is satisfied on atproto-market by
  a flow whose target role is `unknown` at two refs; a repository that wants
  the report to reach a named role must set `reportPeerRoles`.
- An `inherits`/`override` relation between a repository template and a pack
  template does not exist: a collision is an error and the repository must
  rename.
- Effects still do not see the guest's shell: a transport installed by a
  `user_data` string is not an effect, so
  `RfpGuestTransportProvenance` treats the guest role's files as allowed
  wholesale rather than checking that the transport is a `UserDataModule`.

### G5. Cross-project and greenfield proof -- done

What is built:

- **`members:`.** `policies.yaml` gained a member list (name, url, ref,
  optional path, its own role binding, the classifier packs its code is read
  with, optional test globs) and `PolicyLibrary.Classifiers` names the
  classifier packs for the repository's own code. `impl/policyeval/members.go`
  clones each member into `$SPECD_CACHE_DIR/policy-members/<name>`, checks out
  the ref, resolves the commit and verifies it against `policies.lock`;
  `policies.lock` gained a `members:` pin list and the report gained a
  `members:` field, so an evaluation names every checkout it read.
  `impl/policyeval/model.go` builds a model per member and
  `policy.BuildModel` merges them: components are `<member>/<context>`,
  effects, files, evidence and trigger ids carry the prefix, a member's roles
  merge with the importing library's (`Binding.Merge`), and a flow of the same
  shape merges its evidence instead of doubling. `--member name=path` clones a
  member from a local checkout and still records the declared url.
- **The model is built wherever a library is evaluated.** It was in
  `specctl policy eval`/`model` and the spec-time gate only; the realize gate
  (`impl/realize/policy.go`) and the kcp audit (`factory/specd/policy.go`) now
  build it too and review it, so the pack's model rules gate a realize and an
  audit, not only an offline run.
- **Proof A (cross-repo).** atproto-market `pre-iroh` `d20070c` plus
  hono-compute-provider `fb11e74` as a member: 347 effects, 19 flows, and the
  pack denies `host -> guest` with evidence at
  `hono-compute-provider/lib/compute-provider-local/mod.ts:455` (plus `:381`,
  `:539` and `:186` in the same flow). Without the member the same repository
  has no `host -> guest` flow at all. Reading it honestly: `inspectIp` is the
  `container inspect` address read and `backend.exec` inside `pollSshExec` is a
  `container.exec` into the guest, not an ssh, so `RfpRelayOnlyGuestSsh` has
  nothing to say about it; `pollSsh`'s raw `net.dial` to port 22 is a reach-in
  the pack misses, recorded as a gap. The `inspectIp`/`exec` classifier rules
  live in the library's `classifiers/compute-provider.yaml`, not in the shared
  TypeScript pack.
- **Proof B (second real repository).** hono-compute-provider alone, the same
  pack, only `policies.yaml` differs: 2 denies. `rfp-host-reach-in` is real
  (`lib/compute-provider-local/mod.ts:455`); `rfp-guest-reports-network` is
  false for this checkout, because the guest's report is written in another
  repository -- which is what `members:` exists for, and A binds it.
- **Proof C (greenfield).** The pack could not deny a declared flow at all:
  `RfpHostReachIn` read its site through the flow's evidence, so a declared
  flow with no evidence left `site_file` undefined and the rule head
  unsatisfied. The site now falls back to empty, pinned by the suite case
  `denied-a-declared-reach-in-before-any-code`, the unit test
  `test_violation_when_a_declared_flow_has_no_evidence` (83/83 unit tests,
  14/14 suite cases) and the fixture `model-declared-denied.yaml`.
  `examples/policies/greenfield-market` binds the pack to the spec-only fixture
  by context name: `specctl policy eval --specs-only --strict` exits 1 on the
  host-initiated spec and 0 on the guest-initiated one.
  `test/e2e/policy_portable_live_test.go` writes the portable library to the
  repository's own `open-policy/greenfield-market` branch and drives the loop
  under kcp: the violating edit is `PolicyValid=False/PolicyDeniedAtSpec` with
  HEAD unmoved, the fixed spec realizes with the scripted agent
  (`examples/policy-gate/scenario-greenfield-portable.yaml` writes the guest's
  report and the host's report handler), and the audit reads the observed model
  and finds it clean. Green in 13 s.
- `docs/examples/portable-policies.md` and
  `scripts/example-portable-policies.sh` repeat all three; the README links
  them.

Remaining, honestly:

- `pollSsh`'s `Deno.connect` to the guest is not in the reach-in kind list.
- A repository that holds one side of a guest/host split must bind the other as
  a member or accept a false require; there is no "this role is not here"
  declaration.
- The spec-time gate is declared-only: a member's observed reach-in is not a
  fact at spec time.
- The `members:` clone happens even when a run only needs the primary library's
  own templates; `policy test` skips it (it passes no cache dir), `policy eval`
  and `policy model` do not.
- `git:` and `oci:` pack sources still have no test that pulls a pack.

### G6. Portable generation

Plan-0008 phase D re-targeted to the model, plus `specctl policy bind`. The
real run generates the two user sentences into model policies and a binding
for atproto-market, and the results are compared with the hand-written pack.

### G7. Review and fix

DeepSeek analysis plus an Opus review; then fix what they find.

The coordinator's fix list for this phase (with plan 0008 F) is worked through
on `fix-list-1` and recorded in plan 0008, "F. Review and fix / Fix list 1".
Three of its items close gaps named here:

- Gap (a) -- the `Deno.test` body the indexer does not see -- is confirmed as
  the external indexer's limit, not a setting: `codegraph 1.6.0` has no option
  to index bodies. It is now stated in `docs/policies.md`, "Limits", and in
  the pack's `README.md`.
- Gap (c) -- the spec-time gate's library -- is verified in code
  (`factory/specd/specgate.go:71-84`) and stated in `docs/policies.md`.
- The unnamed-transport limit and the hand-labelled recall table (G2) are now
  in `docs/policies.md`, "Limits", and the transport limit is also in the
  pack's `README.md`.


## Order with plan 0008

- 0008 A, B, C and E stand: the engine, the concrete layer, kcp, the gate and
  the library port. B's concrete policies stay as the calibration baseline
  and as the "repo-specific escape hatch" example.
- 0008 D (generation) is folded into G6, so generation targets the portable
  layer directly.
- Order: 0008 B, C → G1 → G2 → G3 → G4 → G5 → G6 → G7, with 0008 E in
  parallel with G1.
