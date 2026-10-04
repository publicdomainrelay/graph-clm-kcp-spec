# Review 0002 - atproto-market iroh/dumbpipe: architecture branch and PR #1

Reviewer: an independent Claude Opus 5.5 subagent, read-only; plan 0006 implements the recommendations. Subjects: the orphan
branch `open-architecture/atproto-market--spec-iroh-dumbpipe-20261004141803` (232 commits; no default `open-architecture/atproto-market`
on the remote) and PR #1 (`pre-iroh` <- `spec/iroh-dumbpipe-...`, OPEN, 17 files +509/-70). Populated base = `78519d1` (14:33:18, parent of first `origin=clm` edit `9ce4793`):
144 commits before, 88 after. All numbers below were computed by running code.

## Verdict
The spec delta is precise and mostly approvable. The realized transport does not work: the dumbpipe binary
is never installed (guest or host), and the ticket never reaches the requester with the pinned siblings.
The offline gate (`deno check` + string snapshot) passed both. The review flow found 3 bugs. It missed these.

## 1. Populated architecture: usefulness and accuracy
- I spot-checked 9 contexts (claims compared with the code). 8 are accurate: hono-plc (port 2587/PORT, HOSTNAME, config.json 0.0.0.0, fallback
  127.0.0.1, SIGINT/SIGTERM: `hono-plc/mod.ts:20,32-33`), lib-market-settlement-free (30000 ms, `assertSafeEgressUrl`
  before fetch, `FreeGrantError`: `client.ts:28,30`, `server.ts:26`), lib-trust-graph-bsky-mutuals (fail-soft: `mod.ts:17-28`),
  lib-cocore-api (default appview, trailing-slash strip: `mod.ts:36`), hono-bidder (`--skip-qr`, qr.fedfork.com, createRecord
  not putRecord: `mod.ts:300,373-375`), lib-secrets-oidc (`clockToleranceSec ?? 5`, decodeJwt, jwks_uri: `mod.ts:38,54-65,89`),
  lib-common-market-lexicons-com-atproto (`repo.ts` namespace re-export), lib-common-cloud-init-common r.builtin-modules.
  1 is stale after the feature: compute-events-vm r.on-network-record still says "hostname or IP", and so does `lexicons/.../vm/onNetwork.json`.
- Partition does not match the workspace. 81 contexts. All 44 `deno.json` workspace members are covered. 37 further contexts are
  not members. 27 of those are `@atproto/lex` codegen subdirectories (`lib-common-market-lexicons-com-...`, 28 lexicon
  contexts = 34.6%). They have 3-7 MUSTs each about generated one-line barrels. This is noise.
- Graph: `upstream: self` on 81/81. `depends_on` = only `sc.atproto-market` on 80/81 (none on the root). No
  overlay/orchestrator/arch node. The package import graph (the thing a reader wants) is absent. See 5.3.
- Requirements: 812 at base, 830 at tip (729 MUST / 80 SHOULD / 21 MAY at tip). The example doc says 807 -> 825 (wrong).
- CodeSynced=False: base 1 (`lib-cocore-api` InterfacesMissing "undeclared: call", already red before the feature). Tip 3: + `lib-common-cloud-init-common`, `lib-market-bidder-compute` CodeRefsUnresolved
  (for example, r.deprecated-wrappers refs `function:40149c..`/`46c9fd..` are missing from the 12-entry codeRefIndex). Cause: line-shifted ids.
  The run's binary predates `fba6e7b` "a code ref follows its symbol" (see 5.1).

## 2. Structure and layout
- Tree: README.md, arch.yaml (705 KB, `GeneratedArchitecture`, `metadata.branch` correct), repository.yaml,
  specs/ status/ context/ changes/ (81 each at base), graph/ (2 files, 1.26 MB), CHANGES.md (332 KB at base!). 3.5 MB total.
- **No `.gitattributes`.** The README "Specs on an orphan branch" promises one. The run used hydradb `d427ec1`+scripts.
  The plan5-iroh merge-base predates `2f5619e`/`ff4b3ea`/`c93b87d`/`fba6e7b` (verified with `merge-base --is-ancestor`).
- **CHANGES.md is useless here.** No default branch exists, so it diffs against nothing: 830 "added", 0 changed, 0 removed.
  The real delta (18 added, 10 changed, in 10 contexts) cannot be found. Edits such as `~r.ssh-session-provider` appear as "added".
- Feature diff base..tip: 123 files, +3689/-623. specs/ 531 lines + CHANGES.md 77 = **14.1%** of 4312. changes/ 55.9%,
  arch.yaml 12.3%, status 8.6%, graph 6.9%, context 2.0%. 76 of 81 status files changed only commit-id lines.
- Commits after base: 19 spec / 35 change / 34 status. 4 are `spec(...): no declared change`. 101 Spec-Change and 7 Code-Commit
  trailers. 4 `architecture(atproto-market): 1 modified` ticks. 10 attempt files (`-a2/-a3`) were deleted (the README says
  changes/ never shrinks). That failure history is lost from the tip.

## 3. Feature spec delta vs the request
- Good: names files, the pinned release, `listen-tcp --host 127.0.0.1:22`, the ticket path `/root/secrets/iroh-node-id`, root
  ssh with the requester key. The guest transport is a registered UserDataModule. Review fixes added 3 requirements.
- Wrong or inconsistent:
  - r.iroh-ticket-file and r.iroh-ticket-emission depend on `getNodeId`. That hook exists only on hono-compute-provider
    `main` (`989c006`, local provider only). The org-root `pre-iroh` (`fb11e74`) lacks it, so the code casts around it
    (`lib/market-bidder-compute/mod.ts:99`). The PR body says the hook "already reads" the file. That is false for the pinned revision.
    DigitalOcean cannot implement it, because it has no exec.
  - r.guest-on-network-iroh-ticket (lib-market-bidder) specifies the guest POSTing its ticket to `/v1/on-network`. The iroh module
    never POSTs. The change Succeeded with zero files changed in lib/market-bidder.
  - r.xrpc-ingress-not-ssh says the relay SSH tunnel "is no longer part of this package's consumers". This contradicts
    r.proxycommand-follows-transport-id: `tunnel` still uses the relay websocket. The change also Succeeded with no file change.
- Coverage gaps: the "onNetwork stuff" was only half switched. The guest-push report (provider `/v1/on-network`) was replaced by a
  provider pull that nothing implements. The secrets capability still reaches the requester through the ingress proxy
  (request-vm-ssh r.capabilities). Not updated: the requestComputeVM lexicon output (`types.ts` gained transport/ticket, but the JSON and
  `.defs.ts:51-54` did not), the onNetwork lexicon text, READMEs (`request-vm-ssh/README.md:10,92`,
  `hono-compute-contract-gateway/README.md:170-181`), and `compute-contract-full-flow`. No requirements cover ticket
  confidentiality or a stable iroh identity.
- Keeping lib/did-key-ingress-proxy: defensible. It carries submitBid/submitEvent/associateConfirm, and atproto service
  endpoints must be https. But the harness brief made this decision, not the user. It needs an explicit question to the user
  or a spec section headed "out of scope: XRPC plane, secrets path".

## 4. Realized code
- **Install broken (blocker).** The release archive stores `./dumbpipe`. `tar -xzf dp.tgz dumbpipe` gives "tar: dumbpipe: Not found in
  archive", exit 2 (GNU tar 1.35, run here). Affected: guest `cloud-init-common/mod.ts:378`, host `requester-xrpc/mod.ts:914`.
  The guest unit restarts forever, and the ticket loop exits 1 after 60 s. The host logs `dumbpipe_extract_failed`, and ssh
  then fails because no dumbpipe binary is on PATH. Fix: extract `./dumbpipe` or `--strip-components`/wildcards.
- **Ticket not stable.** dumbpipe makes a new key on each start unless `IROH_SECRET` is set (it prints "using secret key <hex>"
  into the 0700 log). The unit has `Restart=always` (`:360`) and appends to the log (`:366`). `grep -m1` (`:386`) keeps the first
  (stale) ticket, and runcmd runs once per instance. After any restart or reboot, the published ticket is dead.
- Verified: listen-tcp prints `dumbpipe connect-tcp <ticket>` (v0.39.0 `main.rs:562`). The extraction works on a real
  listener (ticket 266 chars, no `.`/`:`). `dumbpipe connect` (stdio) and listen-tcp share ALPN and handshake.
  `ssh -G root@<ticket>` accepts the host. Asset names match all four triples.
- **Ticket path dead with pinned siblings.** getNodeId is absent, so resolveIrohTicket returns undefined. The bidder publishes the IP
  (`market-bidder-compute/mod.ts:347-352`). The requester skips addresses without letters, and SSH times out. If a provider has getNodeId but the
  transport is legacy, onNetwork is delayed up to 600 s (`:83`). `createdAt` is taken before the wait (`:341`).
- **Security of the ticket as a capability.** It is written to `vm.onNetwork.address`, a public repo record on the bidder PDS (firehose).
  It is also in the gateway response and in both logs. Any reader can dial the guest sshd over iroh. The full ticket also carries the
  guest's direct IP addresses. Only key-only sshd protects the guest (with `StrictHostKeyChecking=no`). sshd also listens on 0.0.0.0:22 (no ListenAddress).
  Deliver it privately (submitEvent to the requester only, or encrypted to the requester key). Use a short ticket or `--custom-alpn` as a second secret.
- RFP compliance: OK. Provisioning stays requester -> RFP -> cloud-init. No hand-provisioned guest. No second transport outside the registry.
- Smells: `proxyCmdOverride("t").includes("websocat")` probe (`requester-xrpc/mod.ts:1873`). Shape heuristic
  `defaultProxyCommand` (`:711`). `pds.irohNodeId` is resolved but never awaited. `vmFqdn` and log key `fqdn` now hold tickets.
- Tests: the snapshot is 19/19 green here. It checks substrings only (`!y.includes("dumbpipe connect [^")` is near-vacuous). There are no
  tests for resolveIrohTicket, ensureDumbpipe, defaultProxyCommand, or the CLI helper map. The two gateway integration tests were edited but not run (red baseline).

## 5. hydradb defects
1. Stale binary: the run used `d427ec1`. `ff4b3ea`, `2f5619e`, `c93b87d` and `fba6e7b` were not in it. The example doc does not
   record the hydradb commit, and it attributes CodeRefsUnresolved to an open item that main had already fixed.
2. No baseline: `impl/persist/persist.go:428` returns an empty Baseline when the default branch is missing. `branchOffDefault`
   (`:465`) silently starts the feature branch orphan. Populating on a feature checkout never writes the default branch.
3. No TS dependencies: `abc/sync/sync.go:564` `importDirectory` gives up when `modulePath==""` (no go.mod), so only the forced root
   edge (`:523-527`) remains. No resolution of the deno.json import map or workspace.
4. `specctl up` defaults to directory partition (`cmd/specctl/up.go:250`) for a deno workspace. Result: 27 codegen contexts.
5. Realize commit subject uses `members[0].Context` (`impl/realize/realize.go:522`). Example: "realize lib-did-key-ingress-proxy: +6 ~3" for a
   6-context batch that touched no lib/did-key-ingress-proxy file.
6. SpecToCode Succeeded with no file changed under the context's own directory (lib-market-bidder, lib-did-key-ingress-proxy). There is no
   "realized nothing" signal.
7. Noise: 4 `no declared change` spec commits and 76/81 status files rewritten only for commit ids. The gate cannot see runtime defects (tar member, unit restart).

## 6. Recommendations (ranked)
1. Fix the tar extraction on guest and host, and add a test that extracts the real v0.39.0 archive. atproto-market spec. Small.
2. Persist the iroh identity: generate `IROH_SECRET` once (EnvironmentFile 0600), truncate the log per start, re-extract the ticket. atproto-market spec. Small.
3. Make the guest push its ticket via `/v1/on-network` (accept bundle) instead of the getNodeId pull. Alternatively, pin hono-compute-provider with the hook and
   state that DigitalOcean is unsupported. atproto-market spec. Medium.
4. Do not publish the ticket in a public record. Send it privately or encrypt it to the requester. Drop direct addresses. atproto-market spec. Medium.
5. CHANGES.md baseline: when the default branch is missing, use the feature branch's own last pre-`origin=clm` commit, or
   populate `open-architecture/<repo>` from the default code branch first. hydradb (`persist.go:424-466`). Medium.
6. Record and enforce the hydradb commit in example runs. Rerun with main to get .gitattributes, append-only changes/ and ref re-anchoring.
   hydradb scripts/docs. Small.
7. Resolve TS imports through the deno.json import map and workspace into `depends_on`. Pick package partition for deno workspaces, and
   fold `@atproto/lex` codegen directories. hydradb (`abc/sync/sync.go:544-570`, `up.go:250`). Medium.
8. Realize commit subject lists all member contexts. Flag a SpecToCode that touched no file of its context. Suppress no-op spec
   commits and commit-id-only status rewrites. hydradb (`realize.go:522`, oabranch). Small.
9. Close spec gaps: the onNetwork and requestComputeVM lexicons, READMEs, the r.xrpc-ingress-not-ssh contradiction, an out-of-scope
   note for the XRPC and secrets ingress, and unit tests for the transport selection. atproto-market spec. Medium.
