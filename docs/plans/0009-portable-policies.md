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

### G3. Declared interactions

- The SystemContext `interactions` schema: CRD + schemagen + CLM block + the
  apply guard.
- Populate/CodeToSpec propose interactions.
- The declared-vs-observed conformance rule.

### G4. Packs

- Pack format, import and pin.
- `rfp-guest-isolation` with gator suites over ArchitectureModel fixtures.
- Bindings for atproto-market and market-mini. They must reproduce
  plan-0008 phase B's real-run verdicts at each atproto-market ref, so any
  difference is explained.

### G5. Cross-project and greenfield proof

- Bind the same pack to a second real repository and run it.
- Greenfield: a new spec-only repo whose specs declare a host-to-guest exec
  flow is denied at spec time. Fixing the spec lets the change through.
  Realize then generates code, and the observed flows conform.
- Both runs are recorded in `docs/examples/portable-policies.md` with
  `scripts/example-portable-policies.sh`.

### G6. Portable generation

Plan-0008 phase D re-targeted to the model, plus `specctl policy bind`. The
real run generates the two user sentences into model policies and a binding
for atproto-market, and the results are compared with the hand-written pack.

### G7. Review and fix

DeepSeek analysis plus an Opus review; then fix what they find.

## Order with plan 0008

- 0008 A, B, C and E stand: the engine, the concrete layer, kcp, the gate and
  the library port. B's concrete policies stay as the calibration baseline
  and as the "repo-specific escape hatch" example.
- 0008 D (generation) is folded into G6, so generation targets the portable
  layer directly.
- Order: 0008 B, C → G1 → G2 → G3 → G4 → G5 → G6 → G7, with 0008 E in
  parallel with G1.
