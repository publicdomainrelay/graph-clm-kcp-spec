# Example: the two policies against atproto-market

This records the phase B calibration run of plan 0008: evaluate
`examples/policies/atproto-market` against a fresh clone of
`publicdomainrelay/atproto-market` at three refs. The clone is read-only; no
branch is edited and nothing is pushed.

## How it was run

```bash
git clone https://github.com/publicdomainrelay/atproto-market /home/johnandersen777/policy-b-work/atproto-market
cd /home/johnandersen777/src/publicdomainrelay-kcp/hydradb-policy-b
go build -o bin/specctl ./cmd/specctl
```

Then either the script:

```bash
WORK=/home/johnandersen777/policy-b-work OUT=/tmp/policy-run scripts/example-policies.sh
```

or, one ref at a time (what the script does):

```bash
bin/specctl policy eval --repo atproto-market --commit 7a2e9d9 \
  --path /home/johnandersen777/policy-b-work/atproto-market \
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

## Results

| ref | commit | violations |
| --- | --- | --- |
| `master` | `7a2e9d9` | **2 deny** -- `guest-report-reach-in`, `guest-report-driven-emission` |
| `pre-iroh` | `d20070c` | 0 -- clean |
| `spec/iroh-dumbpipe-20261004141803` | `ffac22e` | 0 -- clean |

`relay-only-ssh` and `guest-report-cloud-init` are clean at all three refs.

## master (7a2e9d9)

```
repository: atproto-market  commit: 7a2e9d9
templates: 4  constraints: 4
violations: 2 (deny 2, warn 0, dryrun 0)

deny     error      guest-report-driven-emission  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:284
         createVmBidderCallbacks emits the guest network identity from the provisioning lifecycle, not from an inbound guest report
deny     error      guest-report-reach-in  CodeGraph default/atproto-market
         lib/market-bidder-compute/mod.ts:282
         createVmBidderCallbacks reaches into the guest from the network emitter createVmBidderCallbacks: "\\.getNodeId\\s*\\("
```

Both violations are real. `lib/market-bidder-compute/mod.ts` at master:

```ts
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
  `providerIdPromise.then(...)` provisioning lifecycle. There is no inbound
  handler that receives the guest's report anywhere at master -- the guest
  writes its node id to `/root/secrets/iroh-node-id` in `buildIrohUserData`
  and the host must pull it. The policy's emitter patterns are
  `computeIdentity|REGISTER_IDENTITY|VM_REGISTER_IDENTITY`; the handler
  patterns (`guest.onNetwork`, `mountOnNetworkReport`, `OnNetworkReport`,
  `/v1/on-network`) match nothing at master.

`guest-report-cloud-init` is clean at master only because master still carries
the fedproxy/tunnel `UserDataModule`s (`fedproxy-client`, `tunnel-subscriber`),
which publish the guest's routing outbound. The iroh module it adds does not,
which is what forces the `getNodeId` pull in the first place.

## pre-iroh (d20070c)

```
violations: 0 (deny 0, warn 0, dryrun 0)
clean
```

`getNodeId` does not exist on this ref, and the bidder emits the provisioned
container IP as `vm.onNetwork` while the guest reports its own fedproxy FQDN
through the inbound `POST /v1/on-network` handler in `lib/market-bidder/mod.ts`.
The container IP is explicitly skipped by the requester ("only dispatcher
FQDNs are usable as SSH ProxyCommand targets"), so the guest's reachability is
guest-reported. No reach-in, no host-emitted identity, an outbound guest
report: clean. `relay-only-ssh` finds the same ssh invocations it finds at
master and every one carries a `ProxyCommand` (see the calibration note below).

## spec/iroh-dumbpipe-20261004141803 (ffac22e)

```
violations: 0 (deny 0, warn 0, dryrun 0)
clean
```

This is the branch that moves the transport to iroh/dumbpipe. The bidder no
longer calls `getNodeId` at all (the comment at
`lib/market-bidder-compute/mod.ts:308` says why), and the guest reports its
dumbpipe ticket to the requester's own per-contract endpoint -- the inbound
handler `mountOnNetworkReportHandler` / `REPORT_PATH = "/v1/on-network"` in
`lib/requester-xrpc/mod.ts`, reached from a guest `curl` the cloud-init
`buildIrohUserData` module starts (`iroh-report-ticket.sh`, `ctx.irohReportUrl`).
The bidder's `vm.onNetwork` record carries only a non-routable container IP.

## `relay-only-ssh` is not vacuous

`relay-only-ssh` reports zero at all three refs, which is only meaningful if it
actually reaches the ssh invocations. To prove the reachability, run with the
allowed transports replaced by a name that cannot occur:

```bash
cp -r examples/policies/atproto-market /tmp/atpm-debug
sed -i 's/^    - websocat$/    - __no_such_transport__/' /tmp/atpm-debug/constraints/relay-only-ssh.yaml
bin/specctl policy eval --repo atproto-market --commit 7a2e9d9 \
  --path /home/johnandersen777/policy-b-work/atproto-market --library /tmp/atpm-debug
```

At `master` this reports the ssh nodes in `lib/requester-xrpc/mod.ts:691`
(`new Deno.Command("ssh", ...)`), reached from 8 driven tests
(`bidder_container_integration_test.ts`, `bidder_ssh_relay_test.ts`,
`gateway_ssh_integration_test.ts`, ...). At `spec/iroh-dumbpipe` it reports
`lib/requester-xrpc/mod.ts:979` and `:1012` reached from 9 driven tests. With
the real allowlist (`websocat`, `fedproxy`, `relay-subscriber`,
`dumbpipe connect`, `iroh connect`) all of them pass: at master the ssh args
come from `sshTunnelArgs` with `ProxyCommand=...websocat...`, and at the spec
branch from `defaultProxyCommand` (`dumbpipe connect <ticket>`) or
`createIrohSshSessionProvider` (`iroh connect --bridge ...`).

The same proof is what fixed the calibration: the fixture's compliant
requester builds `ProxyCommand` in one function and the transport name
(`relay-subscriber`) in another, so the rule accepts a `ProxyCommand` and an
allowed transport that are both reachable from the ssh invocation, rather than
requiring both in one node.

## What each violation means

| policy | what it checks | why it matters |
| --- | --- | --- |
| `relay-only-ssh` | a test that drives a bidder and a requester reaches ssh only through a `ProxyCommand` whose transport the RFP cloud-init deploys, and never dials a guest address | the relay is the registry; nothing talks to a guest except through it |
| `guest-report-reach-in` | nothing reachable from the network emitter (or the emitter itself) queries the guest -- `getNodeId`, container exec/inspect, an agent query, a raw ssh | the host must not reach into a guest it provisioned |
| `guest-report-driven-emission` | the network identity is emitted from an inbound handler that receives the guest's report, not from the provisioning lifecycle | the guest reports itself; the host does not derive its address |
| `guest-report-cloud-init` | a cloud-init `UserDataModule` publishes the guest's address or routing outbound | the guest transport is born from cloud-init and reports out |
