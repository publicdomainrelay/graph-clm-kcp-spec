# One pack, three repositories

`policies/packs/rfp-guest-isolation` is written once, over roles, effects and
flows, and bound three times: to atproto-market with a second repository beside
it, to that second repository alone, and to a repository that is only specs.
Only `policies.yaml` changes between the three. This is the run plan 0009 phase
G5 records; `scripts/example-portable-policies.sh` repeats it.

One correction to "only `policies.yaml` changes": two of the three bindings --
`atproto-market-cross-repo` and `hono-compute-provider` -- also need a
`classifiers/compute-provider.yaml` file, because the shared effect packs do not
know that repository's `inspectIp` and `backend.exec` verbs. The repository
data file changes too, not only the binding. Plan 0010 R3 and R4 make the
reach-in rule deny by default for unknown targets and resolve aliases, which is
meant to make that classifier file unnecessary for this pack.

The pack's rules, in one line each:

| template | fires when |
| --- | --- |
| `RfpHostReachIn` | a `host`-initiated flow acts on the `guest`, carrying `network-info` or for `network-discovery`, or a `container.exec`/`ssh.connect` in `host` reaches the guest |
| `RfpGuestReportsNetwork` | no flow has the guest initiating toward another role carrying `network-info`, or an `event.emit` of the network report in `host` is not triggered by the `http.handle` of the report route |
| `RfpRelayOnlyGuestSsh` | an `ssh.connect` outside the guest role is a direct connection -- no `proxyCommand`, or one that only dials the guest -- and no relay-channel flow carries it, or a test dials the guest directly |

The last two rules of `v1`, `RfpGuestTransportProvenance` and
`RfpKeyMaterialProvenance`, moved to the opt-in pack
`policies/packs/rfp-provisioning-provenance` in `v2` (plan 0010 U1). None of
the three bindings here imports it, so none of them carries those rules.

## What G5 added to the model

`policies.yaml` gained `members:`: another repository, a ref, its own role
binding and the classifier packs its code is read with.

```yaml
members:
- name: hono-compute-provider
  url: https://github.com/publicdomainrelay/hono-compute-provider
  ref: fb11e74
  classifiers:
  - compute-provider.yaml
  roles:
    host:
      globs:
      - lib/compute-provider-local/**
      - lib/container-backend-container/**
    guest:
      targets:
        attrs:
        - inspectIp
        - backend.exec
```

Evaluation clones each member into `$SPECD_CACHE_DIR/policy-members/<name>`
(default `.kcp-specd/cache/policy-members`), checks out the ref, and pins the
commit it resolved to. `policy eval` and `policy model` print the pins;
`policy build` writes them into `policies.lock`:

```yaml
imports:
- files: 35
  pack: rfp-guest-isolation
  sha256: 1c28a6a6c4c0120281534a024df8a06d26713d2a53ce1cbff0226f6c3d7607f3
  source: embedded
  version: v2
members:
- commit: fb11e740185ef27e870b361c414f1e4aee9a11cd
  name: hono-compute-provider
  ref: fb11e74
  url: https://github.com/publicdomainrelay/hono-compute-provider
```

The ArchitectureModel is built over every member and merged: a member's
components are named `<member>/<context>`, its effects and flows carry the
prefix, and its roles merge with the importing library's, so a pack rule never
learns a repository name. The model is now built wherever a library is
evaluated -- `specctl policy eval`, `specctl policy model`, the realize gate
and the kcp audit -- so the pack's model rules gate a realize and an audit, not
only an offline run.

A library can name `classifiers:` for its own code, the symmetry of a member's:
a repository whose effect sites the shared packs do not know names them from
the policy branch instead of editing code.

The one case the model cannot see is a member whose ref is not on its public
remote. `--member name=path` clones that member from a local checkout and still
records the declared url in the lock; the runs below use it, because
`fb11e74` is not on the provider's public remote.

## A. atproto-market plus the provider repository

The provider is not in atproto-market's checkout. At `pre-iroh` (`d20070c`)
atproto-market has no `host -> guest` flow at all, and the reach-in this phase
is about -- `compute-provider-local` reading the guest's address with
`container inspect` (`inspectIp`) and probing into it with `backend.exec`
(`pollSshExec`) -- lives in `hono-compute-provider`. One CodeGraph cannot see
it.

```bash
WORK=/home/johnandersen777/policy-g5-work
git clone --quiet https://github.com/publicdomainrelay/atproto-market "$WORK/atproto-market"
git -C "$WORK/atproto-market" checkout --quiet --detach d20070c
git clone --quiet /home/johnandersen777/src/publicdomainrelay-kcp/hono-compute-provider \
  "$WORK/hono-compute-provider"
git -C "$WORK/hono-compute-provider" checkout --quiet --detach fb11e74

bin/specctl policy model --repo atproto-market --worktree "$WORK/atproto-market" \
  --library examples/policies/atproto-market-cross-repo \
  --member hono-compute-provider="$WORK/hono-compute-provider"
```

```
repository: atproto-market  commit: d20070c3
member: hono-compute-provider fb11e74 at fb11e740
components: 7
  guest                    roles=[guest] context=- source=observed
  hono-compute-provider/host roles=[host] context=- source=observed
  hono-compute-provider/test roles=[test] context=- source=observed
  host                     roles=[host] context=- source=observed
  relay                    roles=[relay] context=- source=observed
  requester                roles=[requester] context=- source=observed
  test                     roles=[test] context=- source=observed
effects: 347
flows: 19
```

The flow the pack reads:

```
  host -> guest  initiator=host channel=- carries=[network-info] purpose=- source=observed
      hono-compute-provider/33edbbd527089040
      hono-compute-provider/760ada5bf6168215
      hono-compute-provider/c9f8ecea5f48307d
      hono-compute-provider/ca1fee91e15f55e4
```

Those four evidence effects are, in the provider's checkout:

| effect id (short) | site | what it is |
| --- | --- | --- |
| `c9f8ecea` | `lib/compute-provider-local/mod.ts:186` | `backend.exec(containerName, ["bash","-c",probe])` inside `pollSshExec` |
| `ca1fee91` | `lib/compute-provider-local/mod.ts:381` | `backend.inspectIp(containerName)` in `provisionContainer` |
| `33edbbd5` | `lib/compute-provider-local/mod.ts:455` | `docker.inspectIp(containerName)` in `provisionVM` |
| `760ada5b` | `lib/compute-provider-local/mod.ts:539` | `docker.inspectIp(containerName)` in the local VM path |

```bash
bin/specctl policy eval --repo atproto-market --worktree "$WORK/atproto-market" \
  --library examples/policies/atproto-market-cross-repo \
  --member hono-compute-provider="$WORK/hono-compute-provider"
```

```
repository: atproto-market  commit: d20070c3
templates: 5  constraints: 5
member: hono-compute-provider fb11e74 at fb11e740
violations: 2 (deny 2, warn 0, dryrun 0)

deny     error      rfp-guest-reports-network  ArchitectureModel default/atproto-market
         lib/market-bidder-compute/mod.ts:304
         the host emits the network report "COMPUTE_EVENTS_VM_ONNETWORK_NSID" from the provisioning lifecycle, not from the http.handle of the guest's report: lib/market-bidder-compute/mod.ts:304
deny     error      rfp-host-reach-in  ArchitectureModel default/atproto-market
         hono-compute-provider/lib/compute-provider-local/mod.ts:455
         the host reaches into the guest: host -> guest (initiator host, channel , purpose , carries ["network-info"])
```

The second deny is the reach-in, and it is the provider's line, not
atproto-market's. The same evaluation without the member does not see it:

```bash
bin/specctl policy eval --repo atproto-market --worktree "$WORK/atproto-market" \
  --library examples/policies/atproto-market
```

```
repository: atproto-market  commit: d20070c3
templates: 9  constraints: 10
violations: 2 (deny 2, warn 0, dryrun 0)

deny     error      guest-report-driven-onnetwork  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:304
         createVmBidderCallbacks emits the vm.onNetwork event from the provisioning lifecycle, not from an inbound guest report
deny     error      rfp-guest-reports-network  ArchitectureModel default/atproto-market
         lib/market-bidder-compute/mod.ts:304
         the host emits the network report "COMPUTE_EVENTS_VM_ONNETWORK_NSID" from the provisioning lifecycle, not from the http.handle of the guest's report: lib/market-bidder-compute/mod.ts:304
```

Two denies there too, and neither is the reach-in: without the member the model
has no `host -> guest` flow, because the code that makes one is in another
checkout. That is the whole point of `members:`.

### Reading the reach-in honestly

- `inspectIp` is the guest address read: `cli(["inspect", containerName])`
  parses `status.networks[0].ipv4Address` out of the container runtime. The
  effect kind is `container.exec` because the read is a runtime command against
  the guest's container, and the member's guest role carries
  `targets.attrs: [inspectIp, backend.exec]`, which is what resolves the flow's
  target role.
- `pollSshExec` (`lib/compute-provider-local/mod.ts:186`) is **not an ssh**. It
  runs `ss -H -tln | grep -q ':22 '` *inside* the guest through
  `backend.exec`. It is denied as a `container.exec` reach-in by the same rule;
  `RfpRelayOnlyGuestSsh` has nothing to say about it, and calling it an ssh
  would be wrong. Its site is one of the flow's evidence effects, above.
- `pollSsh` (`:159`) is the one reach-in the pack misses: it is
  `Deno.connect({hostname, port})`, a `net.dial` host to guest on port 22, and
  `RfpHostReachIn`'s kind list is `container.exec` and `ssh.connect`. A raw TCP
  probe is not covered. Recording it as a gap, not as a pass.
- The classifier rules for `inspectIp` and `exec` live in the library's
  `classifiers/compute-provider.yaml`, not in the shared TypeScript pack:
  `backend.exec` is a provider-local helper name, and a shared pack should not
  know it.

## B. The same pack on a second real repository

`examples/policies/hono-compute-provider` binds the pack to the provider alone:
its checkout is the host, and the guest is the container VM the provider runs.
The binding is the only difference from A.

```bash
bin/specctl policy eval --repo hono-compute-provider \
  --worktree "$WORK/hono-compute-provider" \
  --library examples/policies/hono-compute-provider
```

```
repository: hono-compute-provider  commit: fb11e740
templates: 5  constraints: 5
violations: 2 (deny 2, warn 0, dryrun 0)

deny     error      rfp-guest-reports-network  ArchitectureModel default/hono-compute-provider
         no declared or observed flow has the guest initiating toward another role carrying network-info: the guest must report its own network information outbound
deny     error      rfp-host-reach-in  ArchitectureModel default/hono-compute-provider
         lib/compute-provider-local/mod.ts:455
         the host reaches into the guest: host -> guest (initiator host, channel , purpose , carries ["network-info"])
```

Both are real, and only the second is a defect in this repository:

| violation | real or false | why |
| --- | --- | --- |
| `rfp-host-reach-in` | real | the provider reads the guest's address and execs into the guest's container from the host. The same site A reports through the member. |
| `rfp-guest-reports-network` | false for this checkout | the guest's report is not here: the guest is a cloud-init image the provider boots, and its `user_data` is written in another repository. The require cannot be satisfied from this one. |

The false one is the useful result. A provider-only repository can never
satisfy a rule about what the guest does, and the honest answer is not to
weaken the rule but to bind the repository that holds the guest -- which is
what A does. `RfpGuestReportsNetwork`'s require waits for `has_effects`, so it
never fires on a repository that has no code at all; it fires here because the
provider does have effects and the guest still does not report.

## C. A repository that is only specs

`fixtures/greenfield-market` is a README and a plan. `examples/policies/greenfield-market`
binds the same pack to it by context name, and the pack's `RfpHostReachIn` fires
on a *declared* flow with no evidence at all -- which is what the spec-time
gate needed and did not have: the rule read its site through the flow's
evidence, so a declared flow left it undefined and the rule could not fire. The
site now falls back to empty, with the suite case
`denied-a-declared-reach-in-before-any-code` and the opa unit test
`test_violation_when_a_declared_flow_has_no_evidence` pinning it.

Offline, on a clone whose `open-architecture/greenfield-market` branch carries
the specs:

```bash
bin/specctl policy eval --repo greenfield-market --specs-only \
  --path "$WORK/greenfield-market" \
  --library examples/policies/greenfield-market --strict
```

A host that plans to reach in:

```
repository: greenfield-market  commit: 3377a4c6
templates: 5  constraints: 5
violations: 1 (deny 1, warn 0, dryrun 0)

deny     error      rfp-host-reach-in  ArchitectureModel default/greenfield-market
         the host reaches into the guest: host -> guest (initiator host, channel relay, purpose network-discovery, carries ["network-info"])
spec gate: denied: rfp-host-reach-in: the host reaches into the guest: host -> guest (initiator host, channel relay, purpose network-discovery, carries ["network-info"])
spec gate exit: 1
```

The same host with the guest initiating the report:

```
repository: greenfield-market  commit: d9498e78
templates: 5  constraints: 5
violations: 0 (deny 0, warn 0, dryrun 0)
clean
spec gate: allowed (0 warned, 0 dryrun)
spec gate exit: 0
```

That is the pack, not the conformance pack: the message is
`rfp-host-reach-in`'s, and no declared must-never is involved.

### The live half: deny, realize, audit

`test/e2e/policy_portable_live_test.go` drives the same loop through kcp. It
writes the portable library to the repository's own `open-policy/greenfield-market`
branch, so the spec gate, the realize gate and the audit read one library with
no `--policy-library` override. The violating edit comes back
`PolicyValid=False/PolicyDeniedAtSpec`, HEAD does not move and no worktree
branch opens. The fixed spec realizes with the scripted agent
(`examples/policy-gate/scenario-greenfield-portable.yaml`), which writes the
guest's report and the host's report handler; the audit then reads the observed
model and finds the pack satisfied.

```bash
TMPDIR=/home/johnandersen777/e2e-tmp SPECD_REQUIRE_LIVE=1 \
  go test ./test/e2e -run TestPortablePackGatesGreenfieldSpecs -count=1 -v
```

```
e2e: private kcp on kernel ports at https://127.0.0.1:39495 (kine http://127.0.0.1:38709), state /home/johnandersen777/e2e-tmp/specd-e2e-kcp-2125857171
=== RUN   TestPortablePackGatesGreenfieldSpecs
--- PASS: TestPortablePackGatesGreenfieldSpecs (13.01s)
PASS
ok  	github.com/publicdomainrelay/graph-clm-kcp-spec/test/e2e	26.770s
```

The code the agent writes is what makes the audit meaningful rather than
vacuous:

```ts
// lib/guest/report.ts -- the guest's own outbound call, carrying address and
// nodeId, for purpose onNetwork
await fetch(reportUrl, {
  method: "POST",
  body: JSON.stringify({ purpose: "onNetwork", address: info.address, nodeId: info.nodeId }),
});

// lib/host/report.ts -- the emit is inside the handler that receives it
app.post("/v1/on-network", async (request: Request) => {
  const report = await request.json();
  await putRecord(agent, ON_NETWORK_EVENT, report);
  return Response.json({ ok: true });
});
```

Observed over that tree, the model is:

```
flows: 2
  guest -> host  initiator=guest channel=- carries=[network-info] purpose=network-discovery source=observed
  host -> unknown  initiator=host channel=- carries=[network-info] purpose=- source=observed
triggers: 1
  <http.handle POST /v1/on-network> -> <event.emit ON_NETWORK_EVENT>
```

The first flow satisfies `RfpGuestReportsNetwork`'s require, and the trigger
satisfies its second clause: the emit is reachable from the report handler, not
from a provisioning lifecycle. No `host -> guest` flow exists, so
`RfpHostReachIn` is silent.

## Reproducing all three

```bash
scripts/example-portable-policies.sh
```

It clones both repositories under `WORK` (default
`/home/johnandersen777/policy-g5-work`), runs A with and without the member, B,
and both halves of C offline, writing every report to
`$WORK/portable-policies-reports`. It then prints the live command above.

## What is still open

- `pollSsh`'s raw `net.dial` to the guest is not covered by the pack (above).
- A repository that holds only one side of a guest/host split must bind the
  other side as a member, or accept the false require B reports. There is no
  "this role is not in this repository" declaration yet.
- The spec-time gate builds the declared model only: a member's observed
  reach-in is not a fact at spec time, so a repository whose code is a member
  is guarded at realize and audit, not before drafting.
- `git:` and `oci:` pack sources still have no test that pulls a pack.
