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

### G1. Effects

`impl/effects` and the classifier packs for TypeScript/Deno, Go and shell,
with per-language fixtures. `specctl policy effects --worktree P` prints the
effects. Calibrated on atproto-market and on hydradb itself:

- every `ssh`, `fetch`/`callService`, `Deno.Command`, route and
  `createRepoRecord` site in atproto-market is classified correctly;
- the misses and false positives are recorded and fixed.

### G2. Model and bindings

The ArchitectureModel type (`abc/policy`), the builder, `roles` and
`vocabulary` in `policies.yaml`, the role labels on SystemContext, and the
`lib.specd` model helpers with unit tests.

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
