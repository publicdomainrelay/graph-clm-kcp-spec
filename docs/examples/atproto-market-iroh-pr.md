# One sentence to a pull request: atproto-market, iroh / dumbpipe

This is the second worked example, run for real against a repository this tool
did not write, [publicdomainrelay/atproto-market](https://github.com/publicdomainrelay/atproto-market)
(the AT Protocol compute marketplace: bidder, requester, PLC, relays, ~81
directories and 30 workspace packages). The only human input was one sentence
plus a short brief naming the external project and the repository's own rules:

> switch from using did-key-ingress-proxy to iroh - use dumbpipe.dev to do this,
> you will need to switch out the onNetwork stuff and the ssh proxycommand as
> well and other things like that

The result is **[publicdomainrelay/atproto-market#1](https://github.com/publicdomainrelay/atproto-market/pull/1)**:
33 files, +2021 / -193, `deno check` over the whole workspace plus the cloud-init
snapshot test green. The first round landed 17 files, +509 / -70 (the table
below); round 2 after the review and round 3's live ssh proof grew the same
branch, so the headline is the current pull request, not the first round. The
spec it realizes is on the orphan branch
[`open-architecture/atproto-market--spec-iroh-dumbpipe-20261004141803`](https://github.com/publicdomainrelay/atproto-market/tree/open-architecture/atproto-market--spec-iroh-dumbpipe-20261004141803).

The first worked example is [deno-kcp](deno-kcp-pr.md); the script below is the
same one, generalized (`scripts/example-pr.sh`).

```mermaid
sequenceDiagram
    participant U as you
    participant C as specctl up
    participant K as kcp (this clone)
    participant H as harness: claude + cc-clm-mod
    participant S as specd
    participant R as realize agent (contained)
    participant G as git
    U->>C: clone atproto-market + 9 siblings, git switch -c BRANCH origin/pre-iroh
    C->>K: kcp + kine on kernel ports, Repository
    S->>K: 81 SystemContexts, each summarized by DeepSeek
    S->>G: open-architecture/atproto-market--BRANCH (orphan)
    U->>H: "switch from using did-key-ingress-proxy to iroh ... dumbpipe.dev ..."
    H->>K: arch_outline, arch_context
    H->>H: read lib/common/cloud-init-common, the requester, the siblings
    H->>K: arch_edit: 9 contexts, 10 SpecToCode changes
    S->>S: batches the pending changes of the repository
    S->>R: one worktree, one agent run per batch
    R->>R: deno check + cloud_init_snapshot_test.ts gate, then commit
    U->>K: review; 4 more spec requirements through clm apply
    S->>R: realizes those too
    U->>G: git push, gh pr create --base pre-iroh
```

## Before you start

| need | check |
| --- | --- |
| Go 1.26+, git | `go version && git --version` |
| kcp v0.33, kine, kubectl | `kcp --version && kine --version && kubectl version --client` |
| CodeGraph | `codegraph --version` |
| Deno 2.x | `deno --version` |
| the DeepSeek launcher (a `claude` that talks to DeepSeek) | `echo pong \| deepseek-claude -p` |
| gh, only to open the pull request | `gh auth status` |
| a graph DB, optional | ArcadeDB on `bolt://127.0.0.1:7688`, or `export SPECD_BOLT_URL=` to run without one |

Build the tools from this worktree (the binaries must be this checkout's):

```bash
cd /home/johnandersen777/src/publicdomainrelay-kcp/hydradb-plan5
make build
export HYDRA=$PWD
export PATH=$HYDRA/bin:$PATH
export SPECD_SPECCTL=$HYDRA/bin/specctl
```

## The short way: one command

```bash
PUSH=0 $HYDRA/scripts/example-atproto-market-iroh-pr.sh
```

It clones atproto-market and the nine sibling repositories its relative imports
need, checks out `pre-iroh`, runs `specctl up`, gives the harness the sentence
and the brief, waits for specd, prints the commits, the gate, the orphan branch
and the clean tree. `PUSH=1` publishes and opens the pull request.

## The long way, step by step

Everything below is what was run, in order, with the environment the script sets.

### 0. Which base branch, and why

`pre-iroh`, not `master`. `master` (at `7a2e9d9`) predates the cloud-init
`UserDataModule` registry (`buildUserData` + `registerUserDataModule`) that the
repository's own rules require the guest-side transport to use; `pre-iroh` (at
`d20070c`) carries it and is 79 commits past the fork point. `master` also
already contains a *fictional* iroh transport whose `buildIrohUserData` shells
out to `iroh endpoint --bind ... --bridge ...`, a CLI the iroh project does not
ship - `n0-computer/iroh`'s releases contain `iroh-relay` and `iroh-dns-server`
only. The real tool is dumbpipe.

### 1. Clone, side by side

atproto-market's `deno.json` pulls nine sibling repositories through relative
`../<dir>/...` imports. Three live on GitHub under a different directory name,
so the script's `SIBLINGS` entries are `dir=repo`:

| directory the imports need | repository |
| --- | --- |
| `hono-jsr` | `hono-package-registry` |
| `hono-compute-provider` | `compute-provider-digitalocean` |
| `deno-worker-sandbox` | `deno-hono-sandbox` |
| `atproto-relay`, `deno-macos-runner-desktop`, `did-key-ingress-proxy`, `hono-pds`, `policy-engine`, `typescript-helpers` | same name |

```bash
export WORK=$(mktemp -d /tmp/specd-atproto-iroh.XXXXXX)
export ORG_ROOT=/home/johnandersen777/src/publicdomainrelay-kcp   # siblings live here
export SIBLINGS="atproto-relay deno-macos-runner-desktop deno-worker-sandbox=deno-hono-sandbox did-key-ingress-proxy hono-compute-provider=compute-provider-digitalocean hono-jsr=hono-package-registry hono-pds policy-engine typescript-helpers"
cd $WORK
git clone -q https://github.com/publicdomainrelay/atproto-market atproto-market
for entry in $SIBLINGS; do d=${entry%%=*}; git clone -q $ORG_ROOT/$d $d; done
cd atproto-market
git switch -c spec/iroh-dumbpipe-20261004141803 origin/pre-iroh
git log --oneline -1        # d20070c fix(requester): key onNetwork resolvers by receipt
deno check >/dev/null && echo "pre-iroh typechecks against these siblings"
```

`deno check` here is the whole workspace, `test/*.ts` included - 4.6 s. Note the
`$ORG_ROOT` clones: the sibling revisions matter. Cloning the three renamed ones
from GitHub instead leaves `deno check` red (8 errors, including
`Object literal may only specify known properties, and 'tls' does not exist in
type 'SubscriberOptions'`), because the org root carries the revisions
`pre-iroh` was developed against.

### 2. Build the architecture in kcp

```bash
specctl up --remote "" --out $WORK/session.json
```

`--remote ""` indexes from scratch. Populate: **81 SystemContexts**, each
summarized by DeepSeek, about **15 minutes** (14:18:03 to 14:33:18).

```bash
until [[ "$(specctl status)" == *"Populated ("* ]]; do specctl status | grep '^phase'; sleep 10; done
specctl status
# phase       Populated (81 contexts, 81 summarized, 0 failed)
```

### 3. Gate the Repository on an honest verify command

`specctl up` sets `spec.verify` from the repository's build system, and for a
`deno.json` repository that is `deno test` - which runs the integration suites
and needs live services. The command the repository actually uses and that is
green at baseline is:

```bash
export VERIFY='deno check && deno test -A test/cloud_init_snapshot_test.ts'
specctl get repository atproto-market -o json > $WORK/repository.json
# patch spec.verify to ["bash","-lc","<VERIFY>"] and apply it back
specctl apply -f $WORK/repository.json
# repository/atproto-market applied
```

Why this gate: `deno check` with no arguments type-checks every workspace member
and every test file (4.6 s, green at `pre-iroh`), and
`test/cloud_init_snapshot_test.ts` is the repository's own cloud-init
verification surface - 16 tests at baseline (41 ms), byte-exact fixtures for
each module composition plus semantic assertions. The transport change is
exactly what that file gates.

**Live acceptance is not added**, with a reason: the canonical harness
`test/bidder_container_integration_test.ts` is red *before* this change. Docker
is available on this machine (`docker info` -> 29.8.2), but the harness fails in
0.25 s in the relay registration path, before any transport code runs:

```
relay ... POST /xrpc/com.fedproxy.temp.xrpc.getRegistrationNonce -> 401
  {"error":"AuthenticationRequired","message":"Error: cannot resolve signing key for did:plc:..."}
--- FAILED | 0 passed | 1 failed (0.26s)
```

The same failure occurs at `origin/pre-iroh`, at the org-root `pre-iroh` HEAD,
and at the realized branch HEAD. A gating acceptance step would therefore gate
every realization on a pre-existing failure.

### 4. Ask for the change, through the spec

The harness is headless and reads the architecture from kcp; it never touches a
file.

```bash
export PROMPT='switch from using did-key-ingress-proxy to iroh - use dumbpipe.dev to do this, you will need to switch out the onNetwork stuff and the ssh proxycommand as well and other things like that'
cat > $WORK/harness-prompt.txt <<EOF
$PROMPT

<a brief: the RFP flow is the spine, the guest transport must be a registered
 cloud-init UserDataModule, iroh's releases ship no `iroh` CLI so dumbpipe
 (`listen-tcp --host 127.0.0.1:22` prints the ticket; `connect-tcp --addr ...`
 or stdio `connect <ticket>` on the host) is the tool, name every file, say how
 the ticket reaches the requester, keep ssh as root with the requester's key,
 and the gate is `deno check` + the snapshot test>

How to do it: this repository's architecture lives in kcp, not in files. ...
Change the SPEC only. Never create or edit files in this repository yourself ...
EOF
deepseek-claude -p --output-format text --plugin-dir $HYDRA/cc-clm-mod < $WORK/harness-prompt.txt | tee $WORK/harness.out
```

It returned in **5 minutes 11 seconds** (14:33:40 to 14:38:51) with:

```
Spec recorded. 9 contexts edited, 10 SpecToCode changes opened ...
- lib-common-cloud-init-common (+3 ~1) — new `iroh` UserDataModule registered beside tunnel/fedproxy-ssh ...
- lib-requester-xrpc (+4 ~4) — default transport `iroh`; `ensureDumbpipe` replaces websocat bootstrap ...
- lib-abc-requester (+1 ~3) — default documented as `iroh`; SshSessionProvider target is transport-neutral ...
- lib-market-bidder-compute (+1) — call `provider.getNodeId(providerId)` and publish the ticket ...
- lib-market-bidder (+1) — `/v1/on-network` passes the ticket through verbatim ...
- request-vm-ssh (+1 ~3) — `--user-data-transport` defaults to `iroh` ...
- lib-did-key-ingress-proxy (+1) — keeps XRPC ingress role only ...
- lib-compute-contract-gateway-xrpc (+1 ~2) — no fabricated `wss://` under iroh ...
- test (+1) / test-fixtures-cloud-init (+1) — snapshot asserts iroh.yaml ...
```

It also researched dumbpipe itself and wrote the real CLI into the requirements.

### 5. Watch specd realize

The first round failed every change three times, and the reason was not the
code:

```
TS2307 [ERROR]: Cannot find module '.../typescript-helpers/lib/tls-localhost/mod.ts'.
TS2353 [ERROR]: Object literal may only specify known properties, and 'insecureHTTP'
  does not exist in type 'RelayFactoryOptions'.
```

The realize worktrees symlink the clone's siblings, and the clone's siblings were
the wrong revisions: the script tested `-d "$ORG_ROOT/$dir/.git"` to decide
whether to clone from the org root, but the org-root checkouts are git
**worktrees**, where `.git` is a file. Every sibling silently came from GitHub.
Fixed in `scripts/example-pr.sh` (`git -C "$ORG_ROOT/$dir" rev-parse --git-dir`),
the nine clones replaced, and the contexts retried:

```bash
for c in lib-common-cloud-init-common lib-abc-requester lib-requester-xrpc \
         lib-did-key-ingress-proxy lib-compute-contract-gateway-xrpc \
         lib-market-bidder-compute lib-market-bidder request-vm-ssh test \
         test-fixtures-cloud-init; do specctl retry "$c"; done
# lib-common-cloud-init-common: cleared 3 failed change(s); specd raises a fresh attempt ...
```

17 minutes later, at 15:18:04, all ten were `Succeeded` with `verifyExitCode=0`,
landed as four commits - specd batches the pending changes of a repository into
one realization, so one commit carries up to six `Spec-Change:` trailers.

### 6. Review the diff, and iterate the spec where it is wrong

Reading the realized code found three things the requirements had got wrong or
left open. All three went back through the spec (never a file edit):

| finding | requirement | commit |
| --- | --- | --- |
| the guest extracts the ticket with `grep 'dumbpipe connect <ticket>'`, but `dumbpipe listen-tcp` prints `dumbpipe connect-tcp <ticket>` - the ticket file is never written | `r.iroh-ticket-file` amended, `r.iroh-ticket-extraction-tested` added | `4f1174f` |
| the `iroh` module writes an sshd drop-in and enables sshd but installs only `curl` | `r.iroh-module-installs-sshd` added; the snapshot test's "a module that configures an sshd installs openssh-server" rule now covers `iroh` | `4f1174f` |
| `usesDumbpipe = transport !== "fedproxy-ssh"` - selecting the legacy `tunnel` transport would dial a relay FQDN with `dumbpipe connect` | `r.proxycommand-follows-transport-id` added | `b807bd4`, `4e520f5` |

The amend-apply-realize cycle cost about **8 minutes** for all three, through
the operator's CLM bridge:

```bash
specctl clm render --context lib-common-cloud-init-common > ctx.md
# edit ctx.md: amend r.iroh-ticket-file, add r.iroh-module-installs-sshd
specctl clm apply --context lib-common-cloud-init-common < ctx.md
# specctl clm apply: lib-common-cloud-init-common applied (+1 ~1)
```

The second finding is the test rule biting on its own output: the first
realization of `test-fixtures-cloud-init` had dropped `packages:
["openssh-server"]` from the pre-existing `tunnel` and `fedproxy-ssh` modules,
and the new rule caught it and restored both.

### 7. Look at the result

```bash
git log --format='%h %s' origin/pre-iroh..HEAD
# 4e520f5 realize request-vm-ssh: ~1
# b807bd4 realize lib-requester-xrpc: +1
# 4f1174f realize lib-common-cloud-init-common: +2 ~2
# b9c1b77 realize lib-did-key-ingress-proxy: +6 ~3
# b296a7e realize lib-abc-requester: +5 ~5
# 857b94a realize lib-common-cloud-init-common: +3
# 89b584d realize test-fixtures-cloud-init: +1
git diff --stat origin/pre-iroh...HEAD | tail -1
# 17 files changed, 509 insertions(+), 70 deletions(-)
deno check && echo green
deno test -A test/cloud_init_snapshot_test.ts
# ok | 19 passed | 0 failed (43ms)
deno task check && echo green          # the repository's own task
git status --porcelain                 # empty
```

### 8. Publish, and stop

```bash
git push -u origin spec/iroh-dumbpipe-20261004141803
git push origin 'refs/heads/open-architecture/*:refs/heads/open-architecture/*'
gh pr create --repo publicdomainrelay/atproto-market --base pre-iroh \
  --head spec/iroh-dumbpipe-20261004141803 --title "..." --body-file pr-body.md
specctl down
```

## What happened, measured

| step | value |
| --- | --- |
| clone + sibling clones | 15 s |
| hydradb commit | not recorded at the time; review 0002 found `d427ec1`, four commits behind HEAD, so `fba6e7b` was missing (plan 0006 F1 makes the script record and check it) |
| `specctl up` to `Populated` (81 contexts summarized) | 15 min 15 s |
| harness: research + spec edit (9 contexts, 10 changes) | 5 min 11 s |
| first realize round (all failed: wrong sibling revisions) | 23 min, 9 contexts x 3 attempts |
| sibling fix + retries to all-`Succeeded` | 17 min, 10/10 changes, `verifyExitCode=0` |
| review + 4 spec amendments realized | 8 min, 4 changes |
| verify (`deno check` + snapshot test + `deno task check`) | 4.6 s / 43 ms / green |
| commits / diff | 7 commits, 17 files, +509 / -70 |
| project tree after | clean |

Requirements: the architecture held **807** requirement entries before the
harness and **825** after (+18 across ten contexts).

| context | requirements before | after |
| --- | --- | --- |
| `lib-common-cloud-init-common` | 11 | 15 |
| `lib-requester-xrpc` | 15 | 20 |
| `lib-abc-requester` | 24 | 25 |
| `lib-market-bidder-compute` | 15 | 16 |
| `lib-market-bidder` | 21 | 22 |
| `request-vm-ssh` | 18 | 19 |
| `lib-did-key-ingress-proxy` | 10 | 11 |
| `lib-compute-contract-gateway-xrpc` | 14 | 15 |
| `test` | 7 | 9 |
| `test-fixtures-cloud-init` | 5 | 6 |

## What the change actually does

Established by reading the diff, not by trusting the harness summary:

- **Guest side.** `lib/common/cloud-init-common/mod.ts` gains an `iroh`
  `UserDataModule`, registered beside the existing five. It installs
  `openssh-server` and the pinned `dumbpipe` v0.39.0 release archive, writes
  `authorized_keys` and `sshd_config.d/10-iroh.conf`, runs
  `dumbpipe listen-tcp --host 127.0.0.1:22` under systemd with its stderr
  appended to `/root/secrets/iroh-dumbpipe.log`, extracts the ticket from the
  `dumbpipe connect-tcp <ticket>` line into `/root/secrets/iroh-node-id`, and
  leaves the listener running. No sshd `ListenAddress`, so the provider can
  still TCP-probe `:22`.
- **Ticket path.** Guest writes the file -> the provider's `getNodeId(
  providerId)` reads it -> `lib/market-bidder-compute` polls that hook with
  bounded backoff and publishes the ticket as `vm.onNetwork.address` -> the
  requester resolves it and settles the SSH wait, feeding the same value into
  `pds.irohNodeId`/`resolveIrohNodeId`.
- **Host side.** `runComputeContract` defaults `userData.transport` to `iroh`,
  calls `ensureDumbpipe` (a sibling of `ensureWebsocat`, pinned to the same
  release), and builds `ProxyCommand=dumbpipe connect <ticket>`; `tunnel` and
  `fedproxy-ssh` keep `websocat`, and an explicit `sshProxyCommandFn` still
  overrides everything.
- **`lib/did-key-ingress-proxy` stays**, for the XRPC/repo ingress plane the
  market runs on (bids, accepts, `submitEvent`, PDS hosting). The request asks
  for the *guest SSH transport* to move; removing the relay entirely would break
  the market, which is the one thing the org rules say not to do.

## Analysis

**What worked.**

- **The spec carried the research.** The harness had no web access assumed: it
  knew from the brief that iroh ships no CLI and that dumbpipe is the tool, and
  it wrote requirements naming the pinned release, the listener command, the
  ticket file path and the cross-repo contract (`getNodeId` reads
  `/root/secrets/iroh-node-id`). A code agent contained to a worktree could
  implement all of it.
- **The realized code is honest about the RFP flow.** The guest transport is a
  registered `UserDataModule`; no test hand-provisions a guest; ssh still
  reaches the guest as root with the requester's key over a ProxyCommand. The
  provider-side `getNodeId` hook and the accept-bundle path are untouched.
- **`specctl retry` made iteration cheap.** After the sibling fix, ten failed
  changes were re-run without redoing populate, the harness or the indexing.
- **Reviewing found real bugs, and the flow fixed them.** All three findings are
  the kind an offline gate cannot see by itself - one is a string that does not
  match what the tool prints, one is a package that was never installed, one is
  a predicate that is false for a transport the repository still registers. Each
  became a requirement, a realized commit and a test.
- **The test rule bit its own author.** The "a module that configures an sshd
  installs openssh-server" rule caught a regression the first realization had
  introduced in two other modules.

**What did not work, and what it cost.**

- **Sibling resolution picked the wrong revisions and cost 23 minutes.** The
  org-root checkouts are worktrees (`.git` is a file); the script tested for a
  directory and fell through to GitHub. Nine contexts failed three times each
  before the cause was visible. Fixed in `scripts/example-pr.sh`; the general
  lesson is that "clone the siblings beside the repo" must test for a git
  repository, not for a directory.
- **Cross-context ordering is still not guaranteed.** `test-fixtures-cloud-init`
  realized before `lib-common-cloud-init-common`, i.e. before the module it
  renders existed; it coped by implementing the module itself and then the other
  context amended it. Two commits instead of one, and a scope overreach, but the
  batch window did eventually collapse six changes into a single commit.
- **The code -> spec direction does not converge after a batch realization.**
  Three of 81 contexts are left `CodeSynced=False`:
  `lib-common-cloud-init-common` and `lib-market-bidder-compute` with
  `CodeRefsUnresolved` (their requirement `codeRefs` point at CodeGraph function
  ids that shifted when the file was rewritten - plan 0004 E2), and
  `lib-cocore-api` with `InterfacesMissing` (unrelated to this change; its
  CodeToSpec succeeded and dropped three interfaces). The realized code is
  unaffected, but the drift markers are the honest state of a run whose code
  moved under the spec.
- **A code agent will overreach to make its own gate pass.** The
  `test-fixtures-cloud-init` change was scoped to a fixture; it wrote the whole
  module because otherwise `deno test` could not pass. Reasonable, but it means
  the commit-to-context mapping is looser than the spec suggests.

**What is not demonstrated.**

- **No green end-to-end SSH over dumbpipe.** The live harness that would prove
  it is red at baseline for reasons outside the change (the relay's
  registration nonce returns 401 against the local fake PLC). What is proven is
  the composed cloud-init, the ticket extraction against the line dumbpipe
  v0.39.0 actually prints (checked against upstream source), and the host-side
  command construction. That gap is stated in the pull request.

## Round 2: after review 0002

The run above was reviewed independently (`docs/reviews/0002-atproto-market-iroh.md`)
and the verdict was blunt: the spec delta was approvable, the **realized
transport did not work**. The dumbpipe binary was never installed (the release
archive stores `./dumbpipe`; `tar -xzf dp.tgz dumbpipe` is `tar: dumbpipe: Not
found in archive`, exit 2), the iroh identity was not persisted so every restart
invalidated the published ticket, the ticket came from a provider `getNodeId`
hook that does not exist at the pinned sibling revisions, and the ticket was
published in the world-readable `vm.onNetwork` record. The offline gate could
not see any of it.

Round 2 is plan 0006 F2: the same branch, the same pull request, driven this
time by an operator (a human-level reviewer) instead of the harness, with every
change still going through `specctl clm apply` - no file in atproto-market was
edited by hand.

### Setup, reproduced

```bash
export HYDRA=/home/johnandersen777/src/publicdomainrelay-kcp/hydradb   # e9f129e
export PATH=$HYDRA/bin:$PATH SPECD_SPECCTL=$HYDRA/bin/specctl
export TMPDIR=/home/johnandersen777/e2e-tmp
W=/home/johnandersen777/specd-atproto-iroh-f2 && mkdir -p $W && cd $W
git clone -q https://github.com/publicdomainrelay/atproto-market atproto-market
for entry in atproto-relay deno-macos-runner-desktop \
             deno-worker-sandbox=deno-hono-sandbox did-key-ingress-proxy \
             hono-compute-provider=compute-provider-digitalocean \
             hono-jsr=hono-package-registry hono-pds policy-engine \
             typescript-helpers; do
  d=${entry%%=*}; git clone -q "$ORG/$d" "$d"
done
cd atproto-market
git switch -q -c spec/iroh-dumbpipe-20261004141803 origin/spec/iroh-dumbpipe-20261004141803
specctl up --out $W/session.json      # restores the architecture from the branch
# repository atproto-market ... on branch spec/iroh-dumbpipe-20261004141803
# restored 81 context(s) from the branch on origin (open-architecture/... at 88dfb2ef9a54)
# phase       Populated (81 contexts, 81 summarized, 0 failed)
```

The sibling revisions are the org-root checkouts this branch was developed
against: `atproto-relay 1fab82b`, `deno-macos-runner-desktop dc7319f`,
`deno-worker-sandbox dc0f847`, `did-key-ingress-proxy cb78c70`,
`hono-compute-provider fb11e74` (**no `getNodeId`**), `hono-jsr 2d5d20e`,
`hono-pds bcab50f`, `policy-engine 3bdcb08`, `typescript-helpers 356308e`.

### The gate, strengthened

`spec.verify` grew the three new unit suites, and `spec.acceptance` was added
for the first time - the harness the first run declared unrunnable:

```bash
specctl get repository atproto-market -o json > $W/repository.json
# unwrap items[0], then:
#   spec.verify     = bash -lc 'deno check && deno test -A test/cloud_init_snapshot_test.ts \
#                     test/iroh_dumbpipe_install_test.ts test/iroh_transport_test.ts \
#                     test/iroh_private_report_test.ts'
#   spec.acceptance = [{name: acceptance, gate: true,
#                       command: bash -lc 'deno test --allow-all test/bidder_container_integration_test.ts'}]
specctl apply -f $W/repository.json
```

The 401 is gone: the dispatcher in `test/bidder_container_integration_test.ts`
(and the cross-platform, gateway SSH/request-VM and OAuth suites) is now built
with `resolveDidKey`, which reads the fake PLC's DID document and returns
`did:key:<publicKeyMultibase>`. Verified before writing the requirement by
patching the test by hand and reverting: `1 passed | 0 failed` in 2 s where the
baseline was `0 passed | 1 failed` in 0.25 s. The full cause is in the
`r.container-harness-relay-resolves-did-keys` requirement.

### Round 1: twelve changes, one commit

```bash
specctl clm render --context <ctx> > ctx.md   # for each of 12 contexts
$EDITOR ctx.md
specctl clm apply --context <ctx> < ctx.md
specctl get specchanges                        # 12 SpecToCode, all Pending
```

specd batched all twelve into one agent run at 16:09 and committed at 16:17:58:

```
1e1cd3c realize lib-abc-requester: +10 ~17
Spec-Change: lib-abc-requester-s2c-... / lib-common-cloud-init-common-s2c-...
             lib-market-bidder-compute-s2c-... / lib-requester-xrpc-s2c-...
             test-s2c-... / compute-contract-full-flow-s2c-... (+6 more)
Open-Architecture: open-architecture/atproto-market--spec-iroh-dumbpipe-20261004141803
23 files changed, 1065 insertions(+), 234 deletions(-)
```

### The review this run did by hand

Reading the diff found two defects the new unit suites could not see, and both
went back through the spec as amendments (round 2 below):

1. **Hono copies a child app's routes at `route()` time.** `createRequesterPDS`
   runs `serve.app.route("/", repoApp)`; the report endpoint was registered on
   `repoApp` afterwards, so the guest's POST would have 404ed and the SSH wait
   would have timed out - silently, because the unit test mounted on a fresh app
   and fetched it directly. Verified with Hono itself:

   ```bash
   deno eval 'import {Hono} from "@hono/hono"; const p=new Hono(), c=new Hono();
     c.get("/a",(x)=>x.text("a")); p.route("/",c); c.get("/b",(x)=>x.text("b"));
     console.log((await p.fetch(new Request("http://x/a"))).status,
                 (await p.fetch(new Request("http://x/b"))).status)'
   # 200 404
   ```

2. **A bearer token in cloud-init is not a secret.** `runComputeContract`
   publishes the composed `user_data` inside the `compute.vm` record, so the
   token the first round added to the guest's report config was world-readable.
   The endpoint is now keyed by the per-contract accept ref with a one-shot
   rule, and the residual race is stated in the requirement and the PR.

Outside the flow, the transport itself was exercised for real:

```bash
# the exact extraction the guest unit runs, against the real archive
tar -xzf dumbpipe-v0.39.0-linux-x86_64.tar.gz ./dumbpipe        # ./dumbpipe, not dumbpipe
./dumbpipe listen-tcp --host 127.0.0.1:2222 2>listen.log &      # ticket on stderr
TICKET=$(grep -m1 -oE 'dumbpipe connect-tcp [^[:space:]]+' listen.log | awk '{print $3}')
echo ${#TICKET}                                                  # 266
./dumbpipe connect-tcp --addr 127.0.0.1:3333 "$TICKET" &         # the host-side transport
python3 -c "import socket;s=socket.create_connection(('127.0.0.1',3333));s.sendall(b'hello-over-iroh');print(s.recv(200))"
# b'echo:hello-over-iroh'
```

with `IROH_SECRET` set on the listener, the ticket's endpoint id is stable
across restarts (the direct addresses inside it change, which is why the ticket
is re-extracted and re-reported on every start).

### Rounds 2 and 3: five changes

```bash
# round 2 - the report endpoint must be reachable through the ingress app,
# no credential in cloud-init, and the report test must drive the served app
specctl clm apply --context lib-requester-xrpc < requester-xrpc.md
specctl clm apply --context lib-common-cloud-init-common < cloud-init-common.md
specctl clm apply --context test < test.md
# round 3 - the first attempt to fix (1) swapped Hono's own router after the
# app had served; Hono throws on a late app.post(), so the requirement moved
# the mount to createRequesterPDS, before the app is mounted under the serve,
# and the test must prove the route survives an app that has already served
specctl clm apply --context lib-requester-xrpc < requester-xrpc.md
specctl clm apply --context test < test.md
```

The first shape of fix (1) worked but reached into Hono's private route
registry (`app.routes.push`, `app.router = new Hono().router`) because Hono
refuses to add a route once an app has served a request. The review rejected
that as the requirement; the accepted shape mounts the route once, in
`createRequesterPDS`, before `serve.app.route("/", app)` copies it in.

### Verification, run by hand on the landed branch

```bash
git log --oneline origin/pre-iroh..HEAD
# 0a87f24 realize lib-requester-xrpc: +5 ~2          <- round 3, 2 changes
# a15d9cd realize lib-common-cloud-init-common: -5 ~4 <- round 2, 3 changes
# 1e1cd3c realize lib-abc-requester: +10 ~17          <- round 1, 12 changes
deno check                                     # green, whole workspace
deno test -A test/cloud_init_snapshot_test.ts test/iroh_dumbpipe_install_test.ts \
             test/iroh_transport_test.ts test/iroh_private_report_test.ts
# ok | 33 passed | 0 failed
deno test --allow-all test/bidder_container_integration_test.ts
# ok | 1 passed | 0 failed          <- the acceptance step, green
deno task check && echo green      # the repository's own task
git status --porcelain             # empty
git diff --shortstat origin/pre-iroh...HEAD
# 32 files changed, 1447 insertions(+), 136 deletions(-)
```

| step | value |
| --- | --- |
| `specctl up`: restore 81 contexts from the branch to `Populated` | 3 min (15:55:55 to 15:58:55) |
| round 1: 12 changes applied, realized, committed (`1e1cd3c`) | 10 min 42 s (16:07:12 to 16:17:54) |
| round 2: 3 changes (`a15d9cd`) | 5 min 24 s (16:20:07 to 16:25:31) |
| round 3: 2 changes (`0a87f24`) | 3 min (16:26:20 to 16:29:25) |
| verification by hand (`deno check`, 33 tests, acceptance, `deno task check`) | 2 s / 2 s / 1 s / 1 s |
| unpushed work at the end | 3 realize commits, 32 files, +1447 / -136 |

The branch and its orphan architecture branch were pushed
(`spec/iroh-dumbpipe-20261004141803` at `0a87f24`,
`open-architecture/atproto-market--spec-iroh-dumbpipe-20261004141803`) and
[PR #1](https://github.com/publicdomainrelay/atproto-market/pull/1) rewritten
with the requirement-by-requirement account above.

### What round 2 found about the tool

| finding | state |
| --- | --- |
| an operator `clm apply` that drops requirements is accepted silently: a mis-sliced document removed five `test`-context requirements, the change showed `-5 ~1`, and specd realized it. Only the rendered requirement count revealed it. | fixed by plan 0006 F4.1: apply refuses a removal unless the document lists the id under `removed:` (or `--allow-remove` is passed) and prints the delta by id before it lands |
| `specctl clm apply` folds a second apply of a *running* change into that change ("folded into the running change ..."), so the corrected document could not be re-applied until the batch settled | fixed by plan 0006 F4.2: the edit is always written and becomes its own Pending change queued behind the running one |
| the realize commit subject still names one context (`realize lib-abc-requester: +10 ~17`) for a 12-context batch | plan 0006 F1.4, unchanged |

## hydradb defects this run found

| defect | state |
| --- | --- |
| CodeGraph ids are line-sensitive, so a realize that rewrites a file leaves other requirements' `codeRefs` unresolved and the context pinned at `CodeSynced=False` (`CodeRefsUnresolved` on `lib-common-cloud-init-common` and `lib-market-bidder-compute`) | already recorded as plan 0004 E2; this run is fresh evidence, recorded in [plan 0005](../plans/0005-atproto-market-iroh.md) |
| `specctl get <kind> -o json` returns a `List` wrapper, so an operator patching a Repository has to unwrap `items[0]` before `specctl apply` accepts it (`specapi: unknown kind "List"`) | recorded in plan 0005 |
| the sibling-view heuristic in the example script (not hydradb proper) fell through to GitHub for worktree siblings; fixed in `scripts/example-pr.sh` | fixed here |
| a `clm apply` while that context's change is `Running` is answered `no change: folded into the running change ...` and the edit is silently dropped from kcp, so an operator amendment can vanish without an error; render-and-grep the context after every apply | fixed by plan 0006 F4 (the apply is now written and gets its own queued change); this run still used the pre-F4 binary, and [plan 0006](../plans/0006-atproto-market-review-fixes.md) records the live evidence |

## Round 3: F3 closes the two gaps Round 2 left

Round 2 ended with two gaps written into the pull request: no live ssh over
dumbpipe in the harness, and a report endpoint whose only authorisation was the
public accept ref. Round 3 is plan 0006 F3, and it started by trying to reproduce
the first gap instead of trusting its description.

The run below is the third pass over these two gaps: the previous session had
diagnosed them and written the requirement edits, but died before the edits were
applied (the scratch documents under the run's `TMPDIR` hold its work, and the
rendered requirement ids showed exactly what had and had not landed). Round 3
picked the diagnosis up, corrected two of its conclusions, applied the amended
requirements, and drove the flow to a green acceptance.

### The first gap was not the environment

`docker info` is green (29.8.2) and the container-mode provider provisions. Four
defects stood between the harness and a green run, and the live container names
each of them.

**1. The harness tore down during provisioning.** `test/bidder_container_integration_test.ts`
raced the contract against a 40 s timer and then ran its `finally` cleanup. The
contract promise resolves when the receipt verifies; the bidder provisions
*after* it writes that receipt, in the background. The teardown therefore aborted
the dispatcher while provisioning was in flight, and the bidder's payload resolve
rejected. The stack says so exactly:

```
TypeError: fetch failed
    at async fetch
    at async Object.resolve (lib/market-atproto/records.ts:170:19)
    at async lib/market-bidder-compute/mod.ts:185:27
```

That is a record resolve, not container code. The realized harness awaits the
whole contract with no cap and asserts `sshReady === true` and an
`execProgram` exit code that can only succeed inside the guest.

**2. The guest could not mint its report token, and that was the decisive
blocker.** The report script read the `sub` claim from the provider-provisioned
JWT with a fixed `==` pad:

```
_sub=$(printf '%s' "$_wid_token" | cut -d. -f2 | tr '_-' '/+' | sed -e 's/$/==/' | base64 -d ...)
```

The provider mints tokens whose payload length varies; this guest's provisioning
token payload is 435 base64url characters, i.e. three short of a multiple of
four, so the fixed pad failed `base64 -d`, `_sub` came out empty, the script
minted no token and never sent the report -- five attempts, all counted as
failures, while the listener itself was healthy. It is the same fixed pad the
secrets module uses, where the token length happens to work out. The fix pads to
a multiple of four (`case $((${#_payload} % 4))`: 2 -> `==`, 3 -> `=`, 1 ->
`===`). Found by reading the live guest, not the offline gate.

**3. The listener unit could not run under the container-mode `systemctl` shim.**
The shim runs `ExecStart` through bash without re-quoting it, so a quoted
`/bin/sh -c '...'` died with `unexpected EOF while looking for matching quote`;
it also implements neither `ExecStartPost` nor a `StandardOutput=append:`
redirect, so the log the ticket is extracted from stayed empty. `ExecStart` is
now one bare path to `iroh-listen.sh`, which prepares and sources the identity,
owns the log through its own redirect and starts the reporter -- one unit that
satisfies real systemd and the shim.

**4. The harness needed the gateway-reachable wiring the OAuth suite already
had, in one specific shape.** The dispatcher serves one app on two `0.0.0.0`
listeners -- plain HTTP for the in-process subscribers and the other in-process
components, TLS for the guest -- with a certificate whose SANs are
`relay.localhost` and `*.relay.localhost` (a single-label `*.localhost` wildcard
is rejected by TLS stacks). `ingressProxyHost` is `relay.localhost:<plainPort>`,
which `hostnameOnly` reduces to the portless requester ingress URL. The shared
fetch interceptor must be installed with the **TLS** port and the CA, not the
plain port: the requester verifies the reporter token by fetching the provider's
discovery document and JWKS at the portless `issuer_uri`, and a plain-port
interceptor downgrades that fetch to `http` against the TLS listener. The
provider receives `guestTlsPort`, `caCertPem` and the OIDC provisioning enricher,
so it rewrites the guest's `https://*.localhost` URLs to that port, resolves the
names to the container gateway and installs the CA.

The pass before this one had a fifth symptom: the harness set `tls: true` (or
mixed the plain and TLS ports) so the in-process subscriber spoke HTTPS to the
plain listener and died in the first 0.3 s with `invalid HTTP version parsed`.
The submitted wiring above is the one that survives.

One claim from the earlier diagnosis did not survive: the guest's `/root/.curlrc`
rule is port-scoped (`resolve = *:<tlsPort>:<gateway>`), so it does not touch the
`github.com:443` archive download. The live guest extracted `./dumbpipe` on the
run that still had no `-q`, and the unit started. The `-q` flag was applied
anyway (it is harmless and the requirement asks for it), but it was not the
cause of anything.

Result: `specctl accept` on the realized branch, `ok | 1 passed | 0 failed` in
32 s (and 26 s on a second run): the guest booted from the RFP flow's cloud-init,
reported its ticket to `POST /v1/on-network` (200), ssh came ready on the first
poll with `ProxyCommand=dumbpipe connect <ticket>`, the exec program printed
`SSH_OK_VIA_IROH` from inside the guest, and ssh exited 0.

### The second gap: the report carries the guest's workload identity

The accept ref cannot authenticate the reporter -- it is public, and so is the
requester's ingress URL. Every piece needed to bind the report instead already
exists in the repository, so no new service was needed: the guest mints the
exchanged workload-identity token the winning provider issued it (read
`bid_config` out of the accept bundle, read the provisioning token at its
`token_path`, echo its `sub`, exchange at the provider's `url_route`, exactly as
the secrets module does), and the requester verifies it against the provider's
published JWKS through the injected fetch (`jose`, the same injected-fetch
discipline as `createSecretsAuthorizer` in `lib/secrets-oidc`). The requester
fails closed with 401 unless issuer == `bid_config.issuer_uri`, audience ==
`api://ATProto?actx=<requester DID>` and subject == the provider tag-derived
subject -- all three from the same `deriveGrantVars` the secrets grant uses. No
credential goes into the cloud-config, which is published inside the `compute.vm`
record. The private-report suite now also proves the 401 case and the JWKS
verification.

### Through the spec, as before

Every change is a requirement applied with `specctl clm apply` on the orphan
branch, with the full document rendered first and the requirement ids diffed
before applying (a document missing requirements silently deletes them). No file
in atproto-market was edited by hand.

| requirement | context | what it asks for |
| --- | --- | --- |
| `r.container-harness-proves-live-ssh-over-iroh` | test | the harness awaits the whole contract with `skipSsh: false`, keeps every service alive, asserts a guest-side `execProgram` exit code, and carries the two-listener TLS + gateway wiring with the interceptor on the TLS port |
| `r.iroh-unit-runs-under-container-shim` | lib-common-cloud-init-common | the listener unit starts and reports under the container-mode shim: bare-path ExecStart, the script owns its log, the script launches the reporter |
| `r.iroh-install-ignores-guest-curlrc` | lib-common-cloud-init-common | the archive download ignores `/root/.curlrc` |
| `r.iroh-report-carries-workload-token` | lib-common-cloud-init-common | the report POST mints and sends the guest's workload-identity token, padding the JWT payload to a multiple of four before decoding |
| `r.iroh-report-workload-identity` | lib-requester-xrpc | the report endpoint verifies that token (issuer, audience, subject, JWKS) and fails closed |
| `r.iroh-fixture` | test-fixtures-cloud-init | the fixture is regenerated byte-exact with the module |

Three realize attempts failed before the amendments landed: two on the
plain/TLS port mixup (`invalid HTTP version parsed` at the subscriber's nonce
fetch) and one on the 15-minute agent cap after it had provisioned a container.
The amended requirements -- the concrete wiring and the dynamic pad -- then
realized in one batch as `ffac22e` with `Acceptance: acceptance passed (gate)`,
eight files. The fixture (`2630773`) realized on its own first, because its own
requirement text already described the new module.

The apply defect from Round 2 bit again, in a sharper form: applying a
cloud-init-common amendment while that context's change was `Running` was
answered `no change: folded into the running change ...`, and the amendment was
silently **not** written to kcp -- the rendered document still had no
`r.iroh-install-ignores-guest-curlrc` and no pad text. Only a render-and-grep
after the apply revealed it. The amendment had to wait for every attempt of that
context to settle before it could land.

## How this run was kicked off

A coordinating Claude session on this machine dispatched the task to a headless
agent. The prompt it wrote is `.kcp-specd/coord/iroh.txt` (in the main
checkout's state directory): the change request verbatim, plus six steps - read
the deno-kcp example first, generalize `scripts/example-pr.sh` so
`example-deno-kcp-pr.sh` becomes a thin wrapper, choose atproto-market's verify
gate honestly and record why if a live acceptance cannot run, run the flow on a
fresh clone, review the realized diff as a human reviewer would, push and open
the PR, and record everything as a human-followable example, and finally
`gofmt`/`go vet`/`SPECD_REQUIRE_LIVE=1 go test ./...` green in the worktree,
commit and push `plan5-iroh`, and tear down everything started.

The dispatch pattern is the one in `.kcp-specd/coord/launch.sh`: the prompt is
written to a file, and the agent is started detached with its own session and
log, with a done-file marking completion so a waiter never has to poll a
process:

```bash
C=/home/johnandersen777/src/publicdomainrelay-kcp/hydradb/.kcp-specd/coord
rm -f "$C/done-$1"
setsid nohup env WT="${WT:-}" bash -c \
  "$C/run.sh $1 > $C/log-$1.txt 2>&1; touch $C/done-$1" >/dev/null 2>&1 < /dev/null &
```

The agent works in this repository's `plan5-iroh` worktree and pushes only that
branch. Its own long run is started the same way (a `setsid` script plus a
done-file), which is why the example above can be watched from outside without
ever attaching to it.
