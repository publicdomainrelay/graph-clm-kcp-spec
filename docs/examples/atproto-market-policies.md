# Example: the two policies against atproto-market

This records the phase B calibration run of plan 0008: evaluate
`examples/policies/atproto-market` against a fresh clone of
`publicdomainrelay/atproto-market` at three refs. The clone is read-only; no
branch is edited and nothing is pushed.

## How it was run

```bash
git clone https://github.com/publicdomainrelay/atproto-market \
  /home/johnandersen777/policy-b2-work/atproto-market
cd /home/johnandersen777/src/publicdomainrelay-kcp/hydradb-policy-b2
go build -o bin/specctl ./cmd/specctl
```

Then either the script:

```bash
WORK=/home/johnandersen777/policy-b2-work OUT=/tmp/policy-b2-out scripts/example-policies.sh
```

or, one ref at a time (what the script does):

```bash
bin/specctl policy eval --repo atproto-market --commit 7a2e9d9 \
  --path /home/johnandersen777/policy-b2-work/atproto-market \
  --library examples/policies/atproto-market
```

| ref | commit | command |
| --- | --- | --- |
| `master` | `7a2e9d9` | `--commit 7a2e9d9` |
| `pre-iroh` | `d20070c` | `--commit d20070c` |
| `spec/iroh-dumbpipe-20261004141803` | `ffac22e` | `--commit ffac22e` |

`--commit` exports the commit to a temporary directory, indexes it with
`codegraph`, and evaluates; the clone is not modified. Each run takes about a
second.

### Results

| ref | commit | violations |
| --- | --- | --- |
| `master` | `7a2e9d9` | **3 deny** -- `guest-report-reach-in`, `guest-report-driven-emission`, `guest-report-driven-onnetwork` |
| `pre-iroh` | `d20070c` | **1 deny** -- `guest-report-driven-onnetwork` |
| `spec/iroh-dumbpipe-20261004141803` | `ffac22e` | **1 deny** -- `guest-report-driven-onnetwork` |

`relay-only-ssh` and `guest-report-cloud-init` are clean at all three refs.
All three refs violate P-guest-reports under the strict reading (below); none
of them is compliant.

This library now imports the portable pack `rfp-guest-isolation` (plan 0009
G4), whose `RfpHostReachIn`, `RfpGuestReportsNetwork` and
`RfpRelayOnlyGuestSsh` restate the same two rules over the ArchitectureModel.
The table above is the concrete templates, which is what the output blocks
below record; a run of the library as it stands today prints twice as many
violations, the pack naming the same sites (`6 deny` at `master`, `2` at
`pre-iroh` and at the spec branch, all reported at the same `file:line`). Plan
0009 G4 has the pack's verdicts next to these and the table of every
difference.

## The strict reading

P-guest-reports says the bidder and the compute provider MUST NEVER reach into
the guest for the `vm.onNetwork` event, and the guest MUST reach out and
report its address, routing, iroh or fedproxy information. The strict reading
is about **who produced the event**, not about what it carries: the
`vm.onNetwork` record must be produced in response to the guest's outbound
report, so its emission must be reachable from the inbound guest-report
handler. An emission from the host's provisioning lifecycle
(`providerIdPromise.then(...)` after `computeProvider.provision`) fails even
when the record carries no address at all, or carries only a provider-assigned
container IP. The host decided the guest was on the network; the guest did
not report it.

`guest-report-driven-emission` covers the guest *identity*
(`vm.registerIdentity` / `computeIdentity`); `guest-report-driven-onnetwork`
covers the `vm.onNetwork` record itself. They are two constraints of the same
`GuestReportDrivenEmission` template, so a repository can fix one and still be
told about the other.

## master (7a2e9d9)

```
repository: atproto-market  commit: 7a2e9d9
templates: 4  constraints: 5
violations: 3 (deny 3, warn 0, dryrun 0)

deny     error      guest-report-driven-emission  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:284
         createVmBidderCallbacks emits the guest network identity from the provisioning lifecycle, not from an inbound guest report
deny     error      guest-report-driven-onnetwork  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:257
         createVmBidderCallbacks emits the vm.onNetwork event from the provisioning lifecycle, not from an inbound guest report
deny     error      guest-report-reach-in  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:282
         createVmBidderCallbacks reaches into the guest from the network emitter createVmBidderCallbacks: "\\.getNodeId\\s*\\("
```

All three violations are real. `lib/market-bidder-compute/mod.ts` at master:

```ts
251      // Push vm.onNetwork event back to requester when provision succeeds
...
257          createRepoRecord(COMPUTE_EVENTS_VM_ONNETWORK_NSID, {
258            $type: COMPUTE_EVENTS_VM_ONNETWORK_NSID,
259            createdAt: nowIso,
260          })...
...
280      // Submit registerIdentity with iroh nodeId if available.
281      if (providerId && computeProvider.getNodeId) {
282        computeProvider.getNodeId(String(providerId)).then((nodeId) => {
283          if (!nodeId) return;
284          return createRepoRecord(COMPUTE_EVENTS_VM_REGISTER_IDENTITY_NSID, {
285            $type: COMPUTE_EVENTS_VM_REGISTER_IDENTITY_NSID,
286            computeIdentity: { nodeId },
```

- `guest-report-reach-in` at 282: the bidder asks the compute provider for the
  guest's iroh node id (an agent query into the running guest) to build the
  network identity. That is the reach-in the requirement forbids.
- `guest-report-driven-emission` at 284: the identity is written from the
  `providerIdPromise.then(...)` provisioning lifecycle.
- `guest-report-driven-onnetwork` at 257: the `vm.onNetwork` record is written
  from the same lifecycle. At master the record carries only `$type` and
  `createdAt` -- no address at all -- and it still fails: the host announced
  the guest on the network by itself. There is no inbound handler that
  receives the guest's report anywhere at master; the handler patterns
  (`guest.onNetwork`, `mountOnNetworkReport`, `OnNetworkReport`,
  `/v1/on-network`) match no definition node that reaches the emitters.

`guest-report-cloud-init` is clean at master only because master still carries
the fedproxy/tunnel `UserDataModule`s (`fedproxy-client`, `tunnel-subscriber`),
which publish the guest's routing outbound. The iroh module it adds does not,
which is what forces the `getNodeId` pull in the first place.

## pre-iroh (d20070c)

```
violations: 1 (deny 1, warn 0, dryrun 0)

deny     error      guest-report-driven-onnetwork  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:304
         createVmBidderCallbacks emits the vm.onNetwork event from the provisioning lifecycle, not from an inbound guest report
```

`getNodeId` does not exist at this ref, so the identity rule and the reach-in
rule are clean. The container IP path is guest-reported on the requester side:
`lib/market-bidder/mod.ts:491` emits `vm.onNetwork` with `address:
body.address` from the inbound `POST /v1/on-network` handler, and the
requester skips the container IP ("only dispatcher FQDNs are usable as SSH
ProxyCommand targets"). That handler emission passes the policy.

The bidder-compute emitter does not. `lib/market-bidder-compute/mod.ts:292`:

```ts
292    providerIdPromise.then(async (providerId) => {
...
298      // Always emit vm.onNetwork via firehose (record on bidder PDS, firehose distributes).
299      // Previously gated on submitEventUrl which is empty with --no-ingress-proxy.
300      if (providerId && !registered.has(rk)) {
301        registered.add(rk);
302        const nowIso = new Date().toISOString();
303
304        createRepoRecord(COMPUTE_EVENTS_VM_ONNETWORK_NSID, {
305          $type: COMPUTE_EVENTS_VM_ONNETWORK_NSID,
306          address: provisionIp,
307          createdAt: nowIso,
```

`provisionIp` is `result.metadata?.ip` from `computeProvider.provision(...)`,
and this record is emitted the moment provisioning resolves -- before, and
independently of, any guest report. The guest's own report is a *second*,
unrelated path. The host still decides the guest is on the network, so the
branch fails the strict reading even though the guest also reports.

## spec/iroh-dumbpipe-20261004141803 (ffac22e)

```
violations: 1 (deny 1, warn 0, dryrun 0)

deny     error      guest-report-driven-onnetwork  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:313
         createVmBidderCallbacks emits the vm.onNetwork event from the provisioning lifecycle, not from an inbound guest report
```

This is the branch of atproto-market#1 that moves the transport to
iroh/dumbpipe. The bidder no longer calls `getNodeId` (the comment at
`lib/market-bidder-compute/mod.ts:308` says why), and the guest reports its
dumbpipe ticket to the requester's own per-contract endpoint -- the inbound
handler `mountOnNetworkReportHandler` / `REPORT_PATH = "/v1/on-network"` in
`lib/requester-xrpc/mod.ts`, reached from a guest `curl` the cloud-init
`buildIrohUserData` module starts (`iroh-report-ticket.sh`, `ctx.irohReportUrl`).

The bidder still emits its own `vm.onNetwork` from the provisioning lifecycle
with the non-routable container IP, and the comment at :304 says so:

```ts
304        // Informational only: the provider's provisioned address (a
305        // non-routable container/droplet IP), never the guest's iroh ticket or
...
311        const address = provisionIp;
312
313        createRepoRecord(COMPUTE_EVENTS_VM_ONNETWORK_NSID, {
314          $type: COMPUTE_EVENTS_VM_ONNETWORK_NSID,
315          address,
```

"I never carried anything secret" is not the rule. The event itself is
host-produced, and it is produced before the guest reports. Under the strict
reading the branch fails.

## What makes a branch compliant

The change is the same at all three refs: **emit `vm.onNetwork` only from the
inbound guest-report handler**, and delete the provisioning-lifecycle
emission.

- The handler already exists at `pre-iroh` and `spec/iroh-dumbpipe`:
  `lib/market-bidder/mod.ts` `serve.app.post("/v1/on-network", ...)` records
  the event with the guest's `body.address`. The bidder-compute emitter is
  redundant; removing the `if (providerId && !registered.has(rk)) { ... }`
  block at `lib/market-bidder-compute/mod.ts:300-331` (pre-iroh) /
  `:300-340` (spec) makes the branch pass.
- A record whose only job is "provisioning finished" is not a `vm.onNetwork`
  event. If the requester needs to know provisioning resolved, that belongs in
  the receipt/contract status, not in a network event.
- The same applies to `vm.registerIdentity` at master: `getNodeId` is an agent
  query, so the identity has to come from the guest's report (or not exist).

At `master` there is no inbound handler at all: the guest writes its node id
to `/root/secrets/iroh-node-id` in `buildIrohUserData` and the host pulls it.
Being compliant there means adding the guest-side report (a cloud-init
`UserDataModule` that posts outbound) and emitting from the handler that
receives it -- which is what the spec branch did for the ticket, and what it
still has to do for `vm.onNetwork`.

## Where the container IP comes from

The policy is evaluated over the atproto-market `CodeGraph`, so what it can
see is the *call* `computeProvider.provision(...)`; the address it puts in the
record is `result.metadata.ip`. The provider that fills `metadata.ip` is not
in atproto-market -- it is the sibling `publicdomainrelay/hono-compute-provider`
(at `pre-iroh`, `fb11e74`, the revision read here). There,
`lib/compute-provider-local/mod.ts` reads the IP out of the container runtime:

```ts
381    const ip = await backend.inspectIp(containerName);
...
395    const ready = await pollSshExec(backend, containerName, 22);
```

`inspectIp` is `container inspect <name>` into the guest runtime (the backend
is `lib/container-backend-container/mod.ts:27`, `docker inspect` in the docker
backend), and `pollSshExec` execs into the container to wait for sshd. Both are
reach-ins of exactly the kind `guest-report-reach-in` forbids. They are not
caught by the policy as calibrated, because the rule only sees the repository
under evaluation and the inspect lives in another repository: atproto-market
holds a *value* returned by an interface, not a call into the guest. Widening
`hostGlobs` would not help -- the node is not in the graph. Enforcing the rule
across repositories is plan 0009's problem (policies over roles, effects and
flows), not this calibration's. In-repo, the emission itself is the
observable symptom, and that is what `guest-report-driven-onnetwork` flags.

The same asymmetry is why `lib/market-bidder-compute`'s emitter is clean at
`master` for the reach-in rule but dirty for the emission rule: `getNodeId` is
in-repo, `inspectIp` is not.

## `relay-only-ssh` is not vacuous

`relay-only-ssh` reports zero at all three refs, which is only meaningful if it
actually reaches the ssh invocations. To prove the reachability, run with the
allowed transports replaced by a name that cannot occur:

```bash
cp -r examples/policies/atproto-market /tmp/atpm-debug
sed -i -E 's/^    - (websocat|fedproxy|relay-subscriber|dumbpipe connect|iroh connect)$/    - __no_such_transport__/' \
  /tmp/atpm-debug/constraints/relay-only-ssh.yaml
bin/specctl policy eval --repo atproto-market --commit 7a2e9d9 \
  --path /home/johnandersen777/policy-b2-work/atproto-market --library /tmp/atpm-debug
```

Every allowed transport has to go: replacing only `websocat` leaves the spec
branch passing, because its `ProxyCommand` carries `dumbpipe connect` /
`iroh connect`. With all five replaced, `master` reports 36 deny violations,
33 of them `relay-only-ssh` at `lib/requester-xrpc/mod.ts:691` (22) and
`:734` (11) (`new Deno.Command("ssh", ...)`), reached from 11 driven tests
(`bidder_container_integration_test.ts`, `bidder_ssh_relay_test.ts`,
`gateway_ssh_integration_test.ts`, ...). At `spec/iroh-dumbpipe` it reports
27 `relay-only-ssh` violations at `lib/requester-xrpc/mod.ts:979` (18) and
`:1012` (9), reached from 9 driven tests. With the real allowlist
(`websocat`, `fedproxy`, `relay-subscriber`,
`dumbpipe connect`, `iroh connect`) all of them pass: at master the ssh args
come from `sshTunnelArgs` with `ProxyCommand=...websocat...`, and at the spec
branch from `defaultProxyCommand` (`dumbpipe connect <ticket>`) or
`createIrohSshSessionProvider` (`iroh connect --bridge ...`).

The same proof is what fixed the calibration: the fixture's compliant
requester builds `ProxyCommand` in one function and the transport name
(`relay-subscriber`) in another, so the rule accepts a `ProxyCommand` and an
allowed transport that are both reachable from the ssh invocation, rather than
requiring both in one node.

`guest-report-driven-onnetwork` is not vacuous by construction: it reports a
violation at every ref, at the `createRepoRecord(COMPUTE_EVENTS_VM_ONNETWORK_NSID`
line of the provisioning-lifecycle emitter.

## What each violation means

| policy | what it checks | why it matters |
| --- | --- | --- |
| `relay-only-ssh` | a test that drives a bidder and a requester reaches ssh only through a `ProxyCommand` whose transport the RFP cloud-init deploys, and never dials a guest address | the relay is the registry; nothing talks to a guest except through it |
| `guest-report-reach-in` | nothing reachable from the network emitter (or the emitter itself) queries the guest -- `getNodeId`, container exec/inspect, an agent query, a raw ssh | the host must not reach into a guest it provisioned |
| `guest-report-driven-emission` | the guest identity is emitted from an inbound handler that receives the guest's report, not from the provisioning lifecycle | the guest reports itself; the host does not derive its identity |
| `guest-report-driven-onnetwork` | the `vm.onNetwork` record is emitted from an inbound handler that receives the guest's report, not from the provisioning lifecycle | the guest reports itself on the network; the host does not announce it |
| `guest-report-cloud-init` | a cloud-init `UserDataModule` publishes the guest's address or routing outbound | the guest transport is born from cloud-init and reports out |

# Generated: the same two invariants, authored from one sentence

Plan 0009 G6 generates the policies from the operator's sentence instead of
hand-writing them. This is the real run: a fresh clone,
`/home/johnandersen777/policy-g6-work/atproto-market`, never edited and never
pushed, with the policy branch seeded from `examples/policies/atproto-market`
(the G4 binding, so the harness reads the same roles and vocabulary the pack
does).

```bash
git clone https://github.com/publicdomainrelay/atproto-market \
  /home/johnandersen777/policy-g6-work/atproto-market
cd /home/johnandersen777/policy-g6-work/atproto-market
bin/specctl up --repo . --agent claude --summarize=false

bin/specctl policy generate --repo atproto-market --slug relay-only-guest-ssh \
  --prompt "integration tests with bidder and requester MUST always make ssh connections over the relay" \
  --requirement lib-requester-xrpc#r.relay --wait
bin/specctl policy accept atproto-market-relay-only-guest-ssh

bin/specctl policy generate --repo atproto-market --slug guest-reports-network \
  --prompt "the bidder and the compute provider MUST NEVER reach into the guest for the vm.onNetwork event, the guest MUST reach out to it to provide the address, routing / iroh / fedproxy info" \
  --requirement lib-market-bidder-compute#r.on-network --enforcement deny --wait
bin/specctl policy accept atproto-market-guest-reports-network

bin/specctl policy bind --repo atproto-market --pack rfp-guest-isolation --wait
```

The harness is `deepseek-claude` (the default agent kind), one model call per
attempt, about five minutes each.

## Generation 2: the guest reports out, the host never reaches in

The sentence:

> the bidder and the compute provider MUST NEVER reach into the guest for the
> vm.onNetwork event, the guest MUST reach out to it to provide the address,
> routing / iroh / fedproxy info

`status.checks` after the first attempt (8 checks, none refused):

| check | outcome |
| --- | --- |
| `annotations` | the requirements `lib-market-bidder-compute#r.on-network`, `generated-by` and the severity, written from the request |
| `shape` | one template, one constraint, slug `guest-reports-network`, reviewing the `ArchitectureModel` |
| `portable` | the source names no repository, context or glob |
| `compile` | the constraint client compiles it |
| `units` | 6 opa unit tests |
| `suite` | 2 gator cases (one allowed, one denied) |
| `mutation` | denies `host-reaches-in`, `guest-report-dropped`, `emission-from-the-lifecycle` |
| `head evaluation` | 4 violations against `7a2e9d9` |

The rule it wrote has the pack's three clauses:

```rego
violation[specd.violation(msg, details)] {
	flow := specd.model_flows[_]
	specd.initiator_in_role(flow, host_role)
	specd.acted_on_in_role(flow, guest_role)
	network_flow(flow)
	...
}

violation[specd.violation(msg, details)] {
	has_effects
	not guest_reports
	msg := sprintf("no flow has the %s role initiating toward the %s role carrying %s: ...", [...])
}

violation[specd.violation(msg, details)] {
	effect := specd.model_effects[_]
	specd.component_in_role(effect.component, host_role)
	specd.event_class(effect, network_event_class)
	not driven_by_report_handler(effect)
	...
}
```

The head violations at `master` (`7a2e9d9`), with the pack's own verdicts
beside them:

| site | generated `guest-reports-network` | pack `rfp-guest-isolation` |
| --- | --- | --- |
| `lib/market-bidder-compute/mod.ts:282` | deny: the host role reaches into the guest role for network-discovery (`host -> guest` over `relay` carrying `network-info`) | `rfp-host-reach-in`, same site |
| `lib/market-bidder-compute/mod.ts:257` | deny: the host emits the `network-report` event `COMPUTE_EVENTS_VM_ONNETWORK_NSID` without the report route driving it | `rfp-guest-reports-network`, same site |
| `lib/market-bidder-compute/mod.ts:284` | deny: the host emits `COMPUTE_EVENTS_VM_REGISTER_IDENTITY_NSID` the same way | `rfp-guest-reports-network`, same site |
| (no site) | deny: no flow has the guest initiating toward the host carrying `network-info` | `rfp-guest-reports-network`'s require, same reading |

So the generated rule finds exactly the sites the hand-written pack finds at
`master`.

## Generation 1: the integration tests reach the guest over the relay

The sentence:

> integration tests with bidder and requester MUST always make ssh connections
> over the relay

`status.checks` after the first attempt: `annotations`, `shape`
(`relay-only-guest-ssh`), `portable`, `compile`, `units` (9 opa tests),
`suite` (2 cases), `mutation` (denies `unrelayed-ssh`), `head evaluation`
(0 violations against `7a2e9d9`).

The rule it wrote scopes itself to a model that carries an integration test
(a flow the `test` role initiates onto the `requester` or the `host`), then
denies any `ssh.connect` in the test, requester or host role that is not the
relay itself and whose model shows neither a flow carrying it over the relay
channel nor a relay transport in its own attributes:

```rego
violation[specd.violation(msg, details)] {
	integration_test
	effect := specd.model_effects[_]
	effect.kind == "ssh.connect"
	ssh_role(effect)
	not specd.component_in_role(effect.component, relay_role)
	not over_relay(effect)
	...
}
```

The first attempt at this sentence used the slug `relay-only-ssh`, which the
repository's own template already owns: the branch write replaced that
template's directory while the kcp apply skipped the generated one, so the two
halves of the apply disagreed. `specctl` now refuses a change whose slug, or
whose kind's name, is already taken, before the harness runs; the run above is
the re-run with the free slug `relay-only-guest-ssh`.

### Where the generated policies fire

The generated `guest-reports-network` alone (its own constraint, evaluated
against the same clone at the three refs), beside the pack's own verdicts for
the same three invariants:

| ref | generated `guest-reports-network` | pack `rfp-guest-isolation` |
| --- | --- | --- |
| `master` `7a2e9d9` | 4 deny: the reach-in at `:282`, the two emissions at `:257` and `:284`, and "no flow has the guest role initiating toward the host role carrying network-info" | 3 deny: `rfp-host-reach-in` (`:282`), `rfp-guest-reports-network` (`:257`, `:284`) |
| `pre-iroh` `d20070c` | 2 deny: the emission at `:304`, and the missing guest report | 1 deny: `rfp-guest-reports-network` (`:304`) |
| `spec/iroh-dumbpipe-20261004141803` `ffac22e` | 2 deny: the emission at `:313`, and the missing guest report | 1 deny: `rfp-guest-reports-network` (`:313`) |

Every site the pack names, the generated rule names too. The one difference is
the require clause, and it is the difference G4 already recorded: the pack's
`guest-reports-network` require accepts *any* role the guest reports to
("the guest initiates toward another role carrying `network-info`"), because at
all three refs the guest's report resolves to `guest -> requester` or
`guest -> unknown` while the handler that receives it lives in
`lib/market-bidder`; the generated rule reads the sentence -- "the guest MUST
reach out to it", the bidder -- as `guest -> host` and therefore fires at every
ref. The pack carries a `reportPeerRoles` parameter for a repository that wants
the strict reading; the generated rule is the strict reading by construction.

`relay-only-guest-ssh` reports nothing at any of the three refs, which is what
the pack's own `rfp-relay-only-guest-ssh` reports there: at `master` and
`pre-iroh` the ssh is tunneled (`ProxyCommand=...websocat|dumbpipe...`), and a
tunneled ssh is what both rules accept.

## What the bind produced

`specctl policy bind --repo atproto-market --pack rfp-guest-isolation` asked
the same harness for the binding instead of a rule. `status.checks`:

| check | outcome |
| --- | --- |
| `manifest` | `policies.yaml` keeps the pack import and names the repository |
| `binding` | 5 roles select components, 4 of 5 vocabulary classes match an effect or a spec term; `routes/report` matches nothing yet, reported as a note |
| `suites` | the pack's own 13 gator cases pass |
| `mutation` | the pack denies `host-reaches-in`, `guest-report-dropped`, `unrelayed-ssh` and `test-dials-the-guest` on the repository's model |
| `head evaluation` | 3 violations at `7a2e9d9`, the pack's own sites |

The binding it wrote is the hand-written `examples/policies/atproto-market/policies.yaml`
of G4, role for role and class for class: the same five roles with the same
globs and target hints, and the same vocabulary (`channels/relay`
`[websocat, fedproxy, dumbpipe, iroh connect, tunnel-subscriber]`,
`events/network-report`, `payloads/network-info`, `purposes/network-discovery`,
`routes/report`). The generated binding differs from the hand-written one only
in that the harness also wrote `defaultEnforcement: deny` and the same
`testGlobs`.

## What landed on the branch

```
open-policy/atproto-market
  seed:  the G4 binding, its four concrete templates and the five pack templates
  + policy relay-only-guest-ssh          (changes/atproto-market-relay-only-guest-ssh.yaml)
  + policy guest-reports-network         (changes/atproto-market-guest-reports-network.yaml)
  + bind rfp-guest-isolation             (changes/atproto-market-bind-rfp-guest-isolation.yaml)
```

Both generated templates carry `specs.publicdomainrelay.dev/requirements`
(`lib-requester-xrpc#r.relay`, `lib-market-bidder-compute#r.on-network`) and
`.../generated-by` naming the PolicyChange, and the branch's `templates/` holds
their `src.rego`, `src_test.rego`, `template.yaml`, their suites under
`tests/` and their `dist/`, exactly as a hand-written template does.

The audit records what enforces what: `SystemContext/lib-requester-xrpc`
carries `status.enforcedBy: [relayonlyguestssh]` and
`SystemContext/lib-market-bidder-compute` carries
`[guestreportsnetwork]`; a context whose requirements no policy names carries
the empty list, not the last answer.

## Honest reading

- The two rules are the sentences, not the pack. `guest-reports-network`
  reproduces the pack's three clauses (reach-in, guest reports out, the
  emission driven by the report handler) and adds the strict peer reading
  described above; `relay-only-guest-ssh` accepts a relay-channel flow or a
  relay transport on the ssh itself, the same reading as
  `rfp-relay-only-guest-ssh`. Nothing here replaces the pack: a generated rule
  is a repository's own rule, and the pack stays what it is.
- The checks judge a generated rule by its own fixtures, the derived mutations
  and the head evaluation. None of them proves the fixture models the
  repository's real shape; the first audit on the branch is the honest check.
- The generation is one model call per attempt. Both policies and the binding
  were accepted on the first attempt here; the retry loop (the refused
  attempt's checks are the next attempt's instruction) is exercised by
  `test/e2e/policy_generate_live_test.go` with a scripted harness, not by this
  run.
- The run also found two real defects, both fixed here: the slug
  `relay-only-ssh` collides with the repository's own template (the apply
  replaced the branch file while kcp kept the old object), and a status that
  carried violations could be written but not read back, so `policy accept`
  failed on the second change.
