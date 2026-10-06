# Example: specctl org on the real socialweb-computer

`scripts/example-org-root.sh` repeats this run. It clones
`publicdomainrelay/socialweb-computer` (the org root, 18 submodules) shallow and
reads it with `specctl org`. Nothing is edited and nothing is pushed. The run is
from 2026-10-06 against `main` at `d49877f`.

It shows, in order: the members come from the gitlinks; a shallow clone has no
orphan branches until they are fetched; the real atproto-market architecture
(the 16-context spec of plans 0005 and 0006) is read in place and resolves to the
pinned commit; the root's history shows how each pointer moved, which member
commits each move brought in and which member spec each pin corresponds to.

## 1. A recursive clone

```
$ specctl org clone --depth 1 https://github.com/publicdomainrelay/socialweb-computer
cloned socialweb-computer: 18 member(s)
  .reference/compute-contract-reference-implementation-poc 1bf6249  spec: unspecced
  atproto-market                   05fe296  spec: resolved @d851f04
  atproto-relay                    8f6da6f  spec: unspecced
  ...
  hono-pds                         cf12a44  spec: stale
  ...
```

`org clone` is `git clone --recurse-submodules --shallow-submodules` followed by
`org fetch`, which fetched, by name, the architecture and policy branches that
belong to each member (not every branch): three for atproto-market, one for
hono-pds. The other 16 members have none, so they are `unspecced`.

`atproto-market` is `resolved @d851f04`. Its default branch has no architecture;
`open-architecture/atproto-market--spec-iroh-dumbpipe-policy2-20261005` indexed
exactly the pinned commit `05fe296`, so that is the architecture the root
references. `hono-pds` has an architecture branch none of whose commits indexed
its pin or an ancestor of it: `stale`, which is a prompt to run `specctl up`
there, not a failure.

## 2. The manifest: references, no content

```
$ specctl org manifest
...
- name: atproto-market
  path: atproto-market
  url: https://github.com/publicdomainrelay/atproto-market
  codeCommit: 05fe29612a62a645d70273560238ecc3180414d2
  arch:
    branch: open-architecture/atproto-market--spec-iroh-dumbpipe-policy2-20261005
    commit: d851f0479dd6c793cd7382c8e960e327e3b6d916
    indexedCommit: 05fe29612a62a645d70273560238ecc3180414d2
  state: resolved
```

That is all the root's architecture branch holds about a member. `--write`
commits it as `members.yaml`, with plumbing, without touching the tree.

## 3. The member's own spec, read in place

```
$ specctl org outline --member atproto-market
atproto-market  (open-architecture/atproto-market--spec-iroh-dumbpipe-policy2-20261005@d851f04)
  atproto-market  requirements=16
    intent: The tests that drive the market, the gateway, the bidder and the PLC must not touch the real plc.directory ...
  hono-bidder  requirements=23
  hono-compute-contract-gateway  requirements=14
  hono-market  requirements=3
  lib-abc-compute-contract-gateway  requirements=9
  lib-abc-guest-capability  requirements=11
  ...
```

46 contexts, read from the member repository's own object database at the commit
the root recorded. Nothing was copied into the root and no branch was checked out.

## 4. How the pointers moved

```
$ specctl org fetch --depth 200
atproto-market: 3 branch(es), pin fetched  [deepened to 200]
hono-pds: 1 branch(es), pin fetched  [deepened to 200]
$ specctl org history --member atproto-market -n 3
0031c6b  2026-10-05T02:52:25-07:00  feat(cli): every webserver takes TLS files and writes the port it bound
  ~ atproto-market 2f34793..05fe296  1 commit(s)  spec @d851f04
      05fe296 feat(cli): serve TLS from files, and write the bound port to a file
5a0fb51  2026-10-05T02:25:06-07:00  bunch of random fixes
  ~ atproto-market d20070c..2f34793  4 commit(s)
      2f34793 feat(cli): serve TLS when given a certificate and key
      417ad36 feat: a k3s UserDataModule
      6738077 fix: the tunnel and fedproxy-ssh modules did not install the sshd they configure
      994f53d fix: the gateway trusted the body's issuer, and restore burned live sessions
3c6d250  2026-09-19T18:17:08-07:00  chore: bump atproto-market + socialweb-computer-ssh
  ~ atproto-market 0c685be..d20070c  1 commit(s)  spec @6ba57e0
      d20070c fix(requester): key onNetwork resolvers by receipt
```

Three root commits moved the atproto-market pointer. Each line shows the member
commits behind the move (the real log of the member repository, not the root's)
and, where one exists, the member architecture commit the new pin resolves to:
`spec @6ba57e0` for the older pin `d20070c` and `spec @d851f04` for `05fe296`.
The middle move, `bunch of random fixes`, has no spec: no architecture commit
indexed `2f34793` or an ancestor that the branch still holds.

`org fetch --depth` needed one thing a plain `git fetch --depth` does not: the
root's fetch must not recurse into submodules (`--no-recurse-submodules`).
Without it git tried to fetch every submodule at the commits older root commits
name, and failed on commits that no longer exist.

## 5. What an agent is told

`specctl org brief` prints the member table above, where each spec and policy
lives, and the order of work: change the member in its own checkout, push the
member, then `specctl org bump --change C <member>` so the root's commit carries
`Member: <name> <path> <from>..<to>` trailers. `specctl org status` says what is
wrong before that: on this clone every pin of a shallow or single branch member is
reported as `info` (a shallow clone cannot tell whether a pin is on a remote
branch), and the 18 members that have no `open-architecture/<repo>` branch say
`specctl up --repo <path>`.

## What this run found

Running the first version against the real repository found four mistakes the
fixtures had not:

1. a shallow member made every pin look unpublished (an error); it is now
   unknown, for shallow and for single branch clones;
2. atproto-market's only architecture is on code branches' own architecture
   branches; the default-branch rule alone reported it `unspecced`;
3. `git fetch --depth` in the root recursed into submodules and failed;
4. `org history -o json` used Go field names for the commit fields.

Each is now a test on the fixture polyrepo
(`TestASingleBranchCloneCannotTellWhetherAPinIsPublished`,
`TestAPinDescribedOnlyByACodeBranchsArchitectureResolvesWhenItIndexedThePin`,
`TestFetchDepthDeepensAShallowCloneSoHistoryListsMemberCommits`).
