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

These three commits and their counts are re-run after plan 0010 track R (the
call-site attribution, the relay-term rule, the reach-in default, the model
and diff fixes, the one glob dialect and the fresh-index rule): 6 deny at
`master`, 2 at `pre-iroh`, 2 at the spec branch, the same constraints at the
same sites. The one visible change the stricter rules could have caused -- the
host's own xrpc calls counted as reach-ins, because their target does not
resolve -- is what the binding now answers: the requester role names
`getPdsEndpoint`, `serviceEndpoint`, `com.atproto.repo` and `callService` as
its target symbols, so those calls are flows to the requester and not
reach-ins. `market-mini` answers the same question for its loopback agent port
through `vocabulary.reachInExceptions`.

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
actually reaches the ssh invocations. Since plan 0010 U2 the rule denies only a
*direct* connection: an ssh with no `ProxyCommand`, or one whose `ProxyCommand`
only dials the guest (`nc`/`ncat`/`netcat`, `socat ... TCP:`, `/dev/tcp/`,
`ssh -W`/`-J`). Any other proxy command is a relay, named or not, so the
allowlist of transports is gone -- these sshs pass because they *have* a
`ProxyCommand`, whether it carries `websocat`, `dumbpipe connect`, `iroh
connect` or a name the binding never heard of.

The reachability proof is therefore to call every proxy command a direct dial:

```bash
cp -r examples/policies/atproto-market /tmp/atpm-debug
python3 - <<'PY'
import pathlib
p = pathlib.Path("/tmp/atpm-debug/constraints/relay-only-ssh.yaml")
p.write_text(p.read_text().replace(
    "    - '\\b(nc|ncat|netcat)\\b'\n", "    - 'ProxyCommand'\n"))
PY
bin/specctl policy eval --repo atproto-market \
  --worktree /home/johnandersen777/policy-u-work/atproto-market-master \
  --library /tmp/atpm-debug
```

With `directProxyPatterns: ['ProxyCommand']` every ssh outside the guest role
is reported. `master` reports 39 deny violations, 33 of them `relay-only-ssh`
at `lib/requester-xrpc/mod.ts:691` (22) and `:734` (11) (`new
Deno.Command("ssh", ...)`), reached from 11 driven tests
(`bidder_container_integration_test.ts`, `bidder_ssh_relay_test.ts`,
`gateway_ssh_integration_test.ts`, ...). At `spec/iroh-dumbpipe` (`ffac22e`)
it reports 29, 27 of them `relay-only-ssh` at `lib/requester-xrpc/mod.ts:979`
(18) and `:1012` (9), reached from 9 driven tests. With the real library both
refs report 2 violations and none of them `relay-only-ssh`: at master the ssh
args come from `sshTunnelArgs` with `ProxyCommand=...websocat...`, and at the
spec branch from `defaultProxyCommand` (`dumbpipe connect <ticket>`) or
`createIrohSshSessionProvider` (`iroh connect --bridge ...`). U2's rule reaches
the same sites R2's did; it just no longer cares which transport is named.

`guest-report-driven-onnetwork` is not vacuous by construction: it reports a
violation at every ref, at the `createRepoRecord(COMPUTE_EVENTS_VM_ONNETWORK_NSID`
line of the provisioning-lifecycle emitter.

## What each violation means

| policy | what it checks | why it matters |
| --- | --- | --- |
| `relay-only-ssh` | a test that drives a bidder and a requester reaches ssh only through a `ProxyCommand` -- any relay counts, a direct dial does not -- and never dials a guest address | the relay is the registry; nothing talks to a guest except through it |
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
bin/specctl policy accept atproto-market-relay-only-guest-ssh   # --kubeconfig/--workspace when no session is recorded

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

## The bind, measured again (plan 0010 D1)

That run does not support the claim above, and the review said why. The harness
was given the binding in the prompt, and the branch it read already carried the
hand-written `policies.yaml`, so the import resolved. It copied rather than
derived: the run's transcript shows it reading
`/home/johnandersen777/src/publicdomainrelay-kcp/hydradb/examples/policies/atproto-market/policies.yaml`
and diffing its own output against it.

Three things changed for the re-run:

- **The prompt carries no binding.** Bind mode builds the model with an empty
  binding and strips the roles, the globs and the vocabulary from the rendered
  model (`renderBindModel`), so the harness sees components, effects, flows and
  triggers only.
- **The branch carries no binding.** The re-run seeds
  `open-policy/atproto-market` from a manifest that holds the pack import and
  nothing else. Before this change that branch could not even be read -- the
  pack requires roles and vocabulary, so the resolution failed -- which is why
  the only runnable bind was one against the answer. `policygit.ReadRaw` reads
  a draft's branch unresolved; the roles the harness writes are the declaration
  the resolution expects.
- **The harness cannot reach a copy.** specd does not confine the harness (see
  the limit in `docs/policies.md`), so the run wraps it: the repository's
  `spec.agent.command` is a `bwrap` wrapper that hides every hydradb checkout,
  `/tmp`, the kcp store and the agent's own state, and binds only the scratch
  directory. The first re-run, without that masking, found the hand-written
  file again -- through a copy of it in the agent's own earlier transcript.

The masked re-run, on the same fresh clone at `7a2e9d9`:

```bash
bin/specctl policy bind --repo atproto-market --pack rfp-guest-isolation --wait
# policychange/atproto-market-bind-rfp-guest-isolation  Evaluated  mode=bind
# attempts: 1
#   ok   manifest
#   ok   binding: 5 role(s), 4/5 classes matched; no effect and no spec term
#        speaks these vocabulary classes yet: routes/report
#   ok   suites: 14 case(s)
#   ok   mutation: denies host-reaches-in, guest-report-dropped, unrelayed-ssh,
#        test-dials-the-guest
#   ok   head evaluation: 3 violation(s) against 7a2e9d98
```

Field by field against `examples/policies/atproto-market/policies.yaml`:

| field | generated | hand-written | equal |
| --- | --- | --- | --- |
| `repository` | `atproto-market` | `atproto-market` | yes |
| `imports` | `rfp-guest-isolation@v1 embedded` | same | yes |
| `defaultEnforcement` | `deny` | `deny` | yes |
| `version`, `testGlobs` | `"1"`, `[test/**]` | same | yes |
| `roles.guest` | `declared`, `globs [lib/common/cloud-init-common/**]`, `targets.attrs [getNodeId]`, `targets.routes [/v1/on-network]` | same | yes |
| `roles.host` | 14 globs (`hono-bidder/**` … `lib/trust-graph-tangled-graph/**`), `symbols [createMarketBidder]`, `targets.symbols [registerIdentity, vm.onNetwork]` | same | yes |
| `roles.relay` | `globs [lib/did-key-ingress-proxy/**, lib/hono-factory-did-plc-directory/**, lib/did-plc/**]` | same | yes |
| `roles.requester` | `globs [lib/requester-xrpc/**, request-vm-ssh/**]`, `symbols [createRequesterPDS]`, `targets.routes [/v1/on-network]`, `targets.symbols [reportUrl, _url_path, requester, REPORT_PATH]` | same | yes |
| `roles.test` | `globs [test/**]` | same | yes |
| `vocabulary` | all five classes, term for term | same | yes |

The run above resolved `rfp-guest-isolation@v1`, which is what the branch
carried then. The pack is `v2` now (plan 0010 U1 moved the two provisioning
provenance templates to the opt-in `rfp-provisioning-provenance`), so a re-run
of the same bind reports `@v2`; the binding's roles and vocabulary are
unchanged, and `channels/relay` is optional in v2 rather than required.

The whole file is byte-identical to the hand-written binding -- as that file
stood at the run. Plan 0010 track R landed afterwards and extended it: the
reach-in rule now denies an unknown target by default, so the requester role
names four more target symbols (`getPdsEndpoint`, `serviceEndpoint`,
`com.atproto.repo`, `callService`) that keep the host's own xrpc calls out of
the report, and `market-mini` names `vocabulary.reachInExceptions`. The
generated binding predates that reading and does not carry them, so the two
files differ now, in the requester role only. A bind re-run after the merge is
what would judge the merged rules; the plan's Order section puts that with the
other post-merge re-runs.

Honest reading: the harness wrote the identical manifest on the first attempt
with no access to it. It read the repository's own code -- the module cache of
the checkout holds the compiled sources, and the prompt's model names every
component and every effect site -- and the hand-written binding is the reading
of that code. Two caveats keep this from being a stronger claim than it is:
the masking is the run's, not specd's, so a harness that is not sandboxed can
still copy (the first re-run proves it); and the derivation was not observed
directly, because the CLI cannot write a transcript inside the sandbox, so
"could not have copied" rests on the masks, not on a trace. The masks were
checked from inside: with them, no file on the machine holds the manifest text.

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

## Find the pre-existing violation and decide (plan 0010 U3, U4)

Rule 2 is enforced on the delta (track S6): the gates block only a violation
the change introduced, and a pre-existing one is `inherited`. The bidder's
host-emitted `vm.onNetwork` carrying the provisioned IP is exactly that: a
known, accepted violation the user decided **not** to fix, because the IP may
be a public IPv4 the client can judge.

This is the whole workflow on a fresh clone of the repository at pre-iroh
`d20070c`, with one unrelated commit on top (the clone is never edited or
pushed; the policy library is a copy):

```bash
git clone /path/to/atproto-market /home/johnandersen777/policy-u-work/atproto-market
cd /home/johnandersen777/policy-u-work/atproto-market
git checkout -b u3-check d20070c
printf '\n// unrelated note\n' >> lib/common/market-common/mod.ts
git commit -am "docs: an unrelated note"
cp -r examples/policies/atproto-market /tmp/atpm-demo
```

**1. List.** With `--inherited`, `policy eval` evaluates `--diff-base` too:

```bash
bin/specctl policy eval --repo atproto-market \
  --worktree /home/johnandersen777/policy-u-work/atproto-market \
  --diff-base d20070c --inherited --strict --library /tmp/atpm-demo
# key              status    constraint                 site
# d3dac3b0fbc49c88 inherited guest-report-driven-onnetwork lib/market-bidder-compute/mod.ts:304
# 39eb35808df2f09d inherited rfp-guest-reports-network    lib/market-bidder-compute/mod.ts:304
# findings: 0 new, 2 inherited, 0 waived
# exit 0
```

`--strict` exits 0 because neither violation is new. The emission site is the
`createRepoRecord(COMPUTE_EVENTS_VM_ONNETWORK_NSID, {address: provisionIp, ...})`
call in `createVmBidderCallbacks`: the host announces the guest's address from
the provisioning lifecycle instead of letting the guest report it.
`policy findings --base d20070c` prints the same list with the same keys.

**2. Decide not to fix it.** Record the reason on the policy branch:

```bash
bin/specctl policy waive d3dac3b0fbc49c88 \
  --reason "the emitted address may be a public IPv4 the client can judge, so the host may announce it" \
  --owner john --repo atproto-market \
  --worktree /home/johnandersen777/policy-u-work/atproto-market \
  --library /tmp/atpm-demo --dir /tmp/atpm-demo
# waived guest-report-driven-onnetwork at lib/market-bidder-compute/mod.ts
# wrote exceptions/d3dac3b0fbc49c88.yaml
```

The file it writes:

```yaml
constraint: guest-report-driven-onnetwork
file: lib/market-bidder-compute/mod.ts
key: d3dac3b0fbc49c88
line: 304
object: CodeGraph default/atproto-market
owner: john
reason: the emitted address may be a public IPv4 the client can judge, so the host
  may announce it
```

**3. Re-run.**

```bash
bin/specctl policy findings --repo atproto-market \
  --worktree /home/johnandersen777/policy-u-work/atproto-market \
  --base d20070c --library /tmp/atpm-demo
# d3dac3b0fbc49c88 waived    guest-report-driven-onnetwork lib/market-bidder-compute/mod.ts:304
#                            reason: the emitted address may be a public IPv4 the client can judge...
# 39eb35808df2f09d inherited rfp-guest-reports-network    lib/market-bidder-compute/mod.ts:304
# findings: 0 new, 1 inherited, 1 waived
```

The waived violation is still listed, with its reason and owner. Nothing was
dropped, and no gate blocks on it: the realize gate and the audit read the same
exceptions.

**The other decision is to fix it.** `specctl policy fix <key>` turns the
finding into a SpecChange request -- the violation message, the site and the
instruction the spec flow hands to the agent -- and the fix path itself is
shown on the fixtures in `docs/policies.md` and
`scripts/example-policy-findings.sh`.

`scripts/example-policy-findings.sh` runs both decisions end to end: the waiver
above on the real clone, and, on `fixtures/market-mini/violating`, a `policy
findings` listing, a `policy fix` request for the `relay-only-ssh` violation, a
`policy waive` for the host reach-in and the re-run that reports it waived.

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
