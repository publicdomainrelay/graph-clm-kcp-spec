# Review 0006: policies follow-up (Opus)

Follow-up of review 0003 after plan 0010 (tracks R, S, D) and the user corrections U1-U4, on main at 1805860. Plan 0010 round 2 implements the recommendations.

# Review 2: policies after plan 0010 (R, S, D, U1-U4)

Reviewer: Opus, read-only. Repo `hydradb` at `1805860` (main, contains
`policy-u`). Nothing in any repository was edited. Binary built from the tree
into `<tmp>/specctl`; every experiment ran
on copies under `<tmp>/` (`fx/`, `u2/`,
`u2nr/`, `h/`, `gitx/`, `gw/`, `am/`, libraries `packonly/`, `libx/`, `libw/`).
`go test -short ./abc/policy ./impl/policyeval ./impl/codegraphfacts` green;
`policy test --gator` green for both rfp packs, conformance, market-mini and
atproto-market.

`packonly` = the market-mini binding with only the `rfp-guest-isolation@v2`
import (the portable pack, no concrete market-mini templates). "full" = the
`examples/policies/market-mini` library (pack + 4 concrete templates).

## Verdict

The tracks closed most of 0003/0004 mechanically (globs, diff parser, carries
union, same-node triggers, sync scoping, spec-gate cap, `policy test` read-only,
pack freshness, git/OCI tests, conformance `pack.yaml`). The review's own
variants vB-vH3 deny. But the two user rules still do not hold against small,
realistic variations, and the U3/U4 machinery has a key design flaw:

1. **Critical.** The pack relay rule is skipped for any ssh whose *function or
   callees* mention a relay term, and for any test ssh whose *previous line*
   does (a comment is enough). A direct `ssh root@<guest>` with no ProxyCommand
   passes. B1 is only fixed for file-level nodes.
2. **Critical.** The baseline and waiver key is `(constraint, object, file,
   line)`. One added line above the accepted `vm.onNetwork` emission in the real
   atproto-market clone turns both inherited violations into **new** ones: the
   realize gate would block and tell the agent to "fix" exactly what U3 says must
   not be fixed.
3. **High.** A waiver can hide a different (new) violation: after a line shift a
   waiver written for violation A covers violation B; a waiver file with a
   misspelt `key` silently becomes constraint-wide.
4. **High.** U2's direct-connection detection is a substring list with no
   target check: many direct dialers pass, many real indirections deny, and the
   "test dials the guest" clause never fires on real code.

## (1) 0003 findings, status on current code

| id | status | evidence |
| --- | --- | --- |
| B1 file-level leak | **partly fixed** (see N1) | vB denies (pack). But `abc/policy/modelbuild.go:709-711` `hintText = siteText + neighborhood`; `effectSiteText` (`:726-732`) returns the whole enclosing function text; `neighborhood` (`:742-780`) walks 3 hops of callees; `channel()` (`:820-835`) labels the flow `relay` from any of it; file-level sites use `siteWindow` = previous line + call (`abc/policy/effectmatch.go:1337-1354`). Run: `u2/direct_relay_callee` pass, `u2/test_ssh_direct_comment` pass |
| B2 any ProxyCommand = relay | **superseded by U2**; see N4 | `rfp-relay-only-guest-ssh/src.rego:28-59` |
| B3 reach-in only `getNodeId` | **fixed for vH1-vH3** (deny); evasions remain (N6) | `rfp-host-reach-in/src.rego:43-59`; runs `h/*` |
| B4 alias / test dial | **partly**: vC (top-level const argv0) denies; vD (test `Deno.connect` to guest) **still passes**, not in R9's fixtures (`fixtures/market-mini/violating-v{b,c,e,h1,h2,h3}` only); `` `ssh root@${host}` `` in a string still missed (`impl/effects/packs/typescript.yaml:113-117` needs `@[A-Za-z0-9]`); a function-local `const P = "nc"` unresolved | runs `u2/test_deno_connect*`, `u2nr/sh_c_ssh`, `u2nr/const_proxy_nc` |
| B5 carries dropped | fixed | `modelbuild.go:945-969` unions `Carries` |
| B6 same-node trigger | fixed | `modelbuild.go:1082-1087` `sameNodeDrives` needs a declaration span |
| B7 event class spelling | claimed fixed (R5, R4); not re-run | - |
| B8 generic payload terms | token match fixed (`containsFold`); the require stays an existence check (N8) | `modelbuild.go:876-906`, `rfp-guest-reports-network/src.rego:30-36` |
| B9 no spec-time clause for rule 1 | **open** | relay rego reads only `model_effects` (`src.rego:11-20,67-77`); a declared `test -> guest, channel: direct` passes |
| A1 glob dialects | fixed | `common/glob`; `lib/specd.rego:427,490,560` use `["/"]` |
| A2 Gatekeeper never sees kinds | fixed (docs + derived CRDs) | `deploy/crds/derived/*` |
| A3 stale inlined lib | **open** (low) | `impl/policyeval/engine.go:75`, `suite.go:113,133-140` unchanged |
| S1 two-way sync | fixed per test `TestPolicySyncActionPicksTheSideThatMoved`; `exceptions/` is not `managed` so `Stale` never deletes it (`impl/policykcp/policykcp.go:438-479`) | - |
| S2 per-repo label | fixed (test) | - |
| S3 imports frozen | fixed (`Files` skips `ImportedFrom`, `policykcp.go:408-436`) | - |
| S4 transient errors | fixed (`apierrors.IsNotFound`, `policykcp.go:227`) | - |
| S5 swallowed errors | claimed fixed; not re-run | - |
| G1 spec gate cap | fixed (`impl/policyeval/specgate.go:141-164`) | - |
| G2 whole-repo gates | fixed in shape (S6), **broken by the line-keyed baseline** (N2) | `abc/policy/baseline.go:3-43` |
| G3 deltas/members | fixed (test names in plan) | - |
| G4 diff parser | fixed | scratch test: `--- comment` inside a hunk is content, added lines kept |
| G5 stale index | fixed | `impl/codegraphfacts/codegraphfacts.go:72-127`; a checkout is never written; mtime-skewed edits on a plain tree still evaluated correctly in my runs |
| G6 small items | **open**: `gateLock` still duplicated (`impl/realize/policy.go:210`, `factory/specd/policy.go:181`); `inventory[:1]` unguarded (`factory/specd/policychange.go:320,347`); `repositorySource` still builds without `TestGlobs` (`policychange.go:713-720`) | - |
| D1 bind sees answer | prompt/branch fixed; the honesty claim is weak (N10) | `docs/examples/atproto-market-policies.md:579-600` |
| D2 harness not sandboxed | recorded limit, unchanged | - |
| D3 weak generated checks | **open**: `mutationCheck` passes if any one mutation denies (`impl/policyeval/generated.go:548-555`) | - |
| e `policy test` writes / gator path | fixed (S8, `a39ac48`) | - |
| f comments | **regressed**: `abc/policy` 0 comment lines at `a5a6c86`, 55 now (`exceptions.go` 9, `gate.go` 7, `fix.go` 4); `factory/specd` 170, `cmd/specctl` 80 | `git show a5a6c86:...` count |

## (1b) 0004 bugs and next steps

| item | status |
| --- | --- |
| bug 1 stale `--worktree` index | fixed (R7) |
| bug 2 restore prunes | fixed: `cmd/specctl/policy_kcp.go:294` default `false` |
| bug 3 dead `draft.summary` | fixed: read at `factory/specd/policychange.go:818-822` |
| bug 4 CWD repo inference | fixed: `findExampleLibrary` walks up from the worktree (`cmd/specctl/policy.go:1305-1335`) |
| bug 5 duplicate onNetwork / `peer: unknown` | fixed (`examples/policies/hono-compute-provider/policies.yaml:45`, `cmd/specctl/policy_model.go:253`) |
| next 1 index refresh | fixed |
| next 2 pack freshness | fixed: `impl/policyeval/conformance_test.go:206` walks `policies/packs/*` |
| next 3 phase I retry under policies | **open**: `docs/examples/atproto-market-iroh-pr.md` has no policies round; U3 says it "will be re-run" |
| next 4 git/OCI test | fixed: `impl/policyeval/packs_test.go:93,134` |
| next 5 conformance `pack.yaml` | fixed |
| next 6 restore prune | fixed |
| next 7 portability check | fixed: `ForbiddenIdentifiers` bans target attrs and classifier names (`abc/policy/generate.go:79-110`) |
| next 8 observed flows in drafting prompt | not verified |
| next 9 propose-interactions | fixed |
| next 10 reach-in kinds | host side fixed (`net.dial`, `http.request` in `default_reach_in_kinds`); test-side dial still never resolves to the guest (N5) |
| portability 1-7 | 1 fixed by deny-by-default + `reachInExceptions` (new FP cost, recorded); 4 `channels/relay` optional now; 5 "role not here" still absent; 6 superseded by U2; 7 still needs a member |

## (2) U1-U4 against the user's spec

**U1: done.** `rfp-guest-isolation` v2 holds `rfp-relay-only-guest-ssh` (rule 1)
and `rfp-host-reach-in` + `rfp-guest-reports-network` (rule 2); provenance in
`policies/packs/rfp-provisioning-provenance`; `--with-library` defaults false
(`cmd/specctl/policy.go:309`); no script or e2e seeds the library. Nit:
`pack.yaml:9` says "the user's three rules"; the user gave two.
`examples/policies/deno-kcp` still carries 7 library templates, which is an
explicit opt-in example, acceptable.

**U2: implemented, unsound.** See N1, N4, N5.

**U3: shape done, key unsound.** See N2. The baseline also only runs when the
head is blocked (`impl/realize/policy.go:117-125`), fine.

**U4: partly.** `findings`, `waive`, `eval --inherited` work. Gaps: N2, N3, N7,
N9.

## (3) Findings (new or remaining), with runs

### N1. critical: relay rule exempts direct ssh by neighbourhood text
- `rfp-relay-only-guest-ssh/src.rego:15` `not relay_carried(effect.id)` runs
  before `direct_ssh`; `relay_carried` (`:61-65`) trusts `flow.channel == relay`,
  and the channel is guessed from the whole function, 3 hops of callees, or the
  previous source line (`modelbuild.go:709-835`, `effectmatch.go:1337-1354`).
- Runs (packonly): requester `ssh -p 22 root@${guestHost}` with no proxy plus an
  unused `const transport = createRelayTransport();` in the same function:
  **pass**; same without that line: deny. Test file direct
  `ssh root@${guestHost}` preceded by `// TODO switch to websocat`: **pass**.
  With the relay callee present, every direct dialer passes too (`socat TCP:`,
  `/dev/tcp`, `nc`): 0 pack denies across the 23 requester variants in `u2/`
  that keep the callee.
- The market-mini concrete `relay-only-ssh` catches some of them (nc, socat
  `TCP:`, `/dev/tcp`, no-proxy) but is repo-specific and misses the N4 list;
  the portable pack is what other projects get.
- Fix: never let a channel hint exempt an effect whose own `proxyCommand` is
  empty or a direct dialer. Derive `relay_carried` only from the effect's own
  args (call text, not function or callees). Add both runs as pack suite cases
  and `fixtures/market-mini` variants.

### N2. critical: baseline and waiver key is line-based
- `abc/policy/exceptions.go:112-115` `Key = ViolationID(constraint, object,
  file, line)`; `baseline.go:3-5` reuses it; `findingsOf`, `DecideBaseline`,
  `Override.Matches` (`exceptions.go:89-107`, line check `:103`) all compare it.
- Run on the real repository (copy of `~/policy-u-work/atproto-market`
  at `1a46108`): add one comment line at the top of
  `lib/market-bidder-compute/mod.ts`, commit, `policy findings --base HEAD~1`:
  `findings: 2 new, 0 inherited` (`guest-report-driven-onnetwork`,
  `rfp-guest-reports-network` at `:305`). Without the shift the docs show
  `0 new, 2 inherited`. Same on market-mini: one header line in two files flips
  11 of 15 inherited to new.
- Effect: any realize that edits above the accepted `vm.onNetwork` emission is
  denied, and the agent is told to move the emission into the report handler,
  which U3 forbids. The demo waiver (`exceptions/d3dac3b0fbc49c88.yaml`,
  `docs/policies.md:458-465`) stops matching the same way. Not recorded in
  `docs/policies.md` limits; `:386-389` claims the key is stable.
- Fix: key on line-independent identity: constraint, object, file, enclosing
  declaration qualified name (CodeGraph node), effect kind and normalized call
  text (or effect id built from those). Match base to head via the CodeDiff
  line map as a fallback. Waivers: match on that identity, never on `line`.
  Add a test: shift lines, assert inherited and waived survive.

### N3. high: a waiver can hide a new violation
- Line collision: in `gitx` (market-mini violating) I waived key
  `14b472c7b699beec` = `relay-only-ssh` "test dials a guest address directly"
  at `lib/requester/mod.ts:18` on the base commit. After a one-line shift the
  same key belongs to a **different** clause ("ssh invocation ... reaches the
  guest directly") now at `:18`; findings report that one `waived` and the
  originally waived one `new`. The same mechanism made base `:18` mark a
  different head violation `inherited`.
- Fail-open on typos: `impl/policyeval/library.go:122-142` accepts an exception
  with only `constraint` + `reason`; `sigs.k8s.io/yaml` ignores unknown fields.
  `exceptions/typo.yaml` with `kye: e8543cb5a9ecdcbc` waived **every**
  `rfp-relay-only-guest-ssh` violation, including the test-side ssh (rule 1),
  reported as waived with no warning.
- Siteless violations (declared flows: `rfp-host-reach-in` `site_file = ""`,
  `src.rego:147-153`; `rfp-guest-reports-network` clause 1) share one key per
  constraint and object: one waiver covers every future declared reach-in.
- Same key repeats within one site (`9992b197c6d61b4d` twice) - recorded at
  `docs/policies.md:1586-1593`.
- Fix: strict decode (`yaml.UnmarshalStrict` / `DisallowUnknownFields`); require
  `key` for files under `exceptions/` (constraint-wide waivers only through an
  explicit `scope: constraint` field); include the clause identity in the key
  (N2); refuse to waive a siteless violation without an explicit scope.

### N4. high: U2 direct-dial detection is a substring list without a target
Runs, packonly, requester function without a relay term (`u2nr/`):

| variant | U2 expects | result |
| --- | --- | --- |
| `ProxyCommand=socat - TCP4:<guest>:22` | deny | **pass** (`src.rego:48-51` needs `tcp:`) |
| `socat STDIO TCP-CONNECT:<guest>:22` | deny | **pass** |
| `nc.openbsd <guest> 22` | deny | **pass** (`:45` needs `nc` + space) |
| `openssl s_client -connect <guest>:22`, `python3 -c socket...` | deny | **pass** |
| local `const P = "nc"; ProxyCommand=${P} <guest> 22` | deny | **pass** (unresolved = relay) |
| `autossh`, `/usr/bin/ssh`, `scp`, `sshpass ssh`, `sh -c "ssh root@<guest>"` | deny | **pass** (no `ssh.connect`: `typescript.yaml:4-10` argv0 `[ssh]` only) |
| `ProxyCommand=ssh -W %h:%p jumphost` | pass (indirect) | **deny** (`:57-59` any `-W`/`-J`) |
| `-J relay` / `-o ProxyJump=relay` | pass | **deny** (ProxyJump never extracted) |
| `nc -X 5 -x 127.0.0.1:1080 %h %p` (SOCKS), `nc relay 9000` | pass | **deny** |
| `ssh -F cfg guest-vm` (config holds relay ProxyCommand) | pass | **deny** |
| `ssh -T git@github.com` | not a guest | **deny** (no target check, `:28-31`) |
| `ProxyCommand=bash -c "exec 3<>/dev/tcp/<guest>/22..."`, `socat - TCP:<guest>:22` | deny | deny |

- Fix: decide "direct" by target, not by tool name: a ProxyCommand is direct
  when its own destination (parsed host after `-W`, `nc`/`socat`/`openssl`
  argument, `/dev/tcp/H/`) is the ssh's own target, `%h`, or resolves to the
  guest role; a jump/proxy host that is not the guest is a relay. Extract
  `ProxyJump`/`-J` and the `ProxyCommand <space>` form. Classify `scp`, `sftp`,
  `rsync -e ssh`, `autossh`, `sshpass`, absolute `ssh` paths and `sh -c` strings
  as `ssh.connect`. Treat an unresolved proxy as `warn` (0003 B2's advice), not
  as relay. Require clause 1's target to be the guest or unknown, not any host.

### N5. high: "a net.dial from a test to a guest" never fires on real code
- `src.rego:67-77` needs `acted_on_in_role(flow, guest)`; the test's
  `Deno.connect({hostname: contract.guestHost, port: 22})` and even the literal
  `{hostname: "10.0.0.7", port: 2222}` resolve to `to: unknown` (model dump of
  `u2/test_deno_connect*`). Both pass, full library too. vD from 0003 is still
  open; the suite case `model-denied-test-dial.yaml` hand-writes a flow the
  model builder never produces.
- Fix: in the test role treat `unknown` targets like the host rule does (deny
  unless a `reachInExceptions` term matches), and port 22/2222 as guest ssh;
  add vD to `fixtures/market-mini` and `TestViolatingVariantsDeny`.

### N6. medium: host reach-in evasions
Runs `h/*`, both libraries: `sh -c "ssh root@<guest> ip -j addr"`,
`sh -c "container inspect <vm>"`, `bash -c "exec 3<>/dev/tcp/<guest>/7000"`,
`node:child_process execSync("container inspect ...")` all **pass**;
`Deno.connect`, `fetch(url var)`, `WebSocket`, `virsh domifaddr` deny.
Fix: classify shell strings and `child_process` by their embedded argv0 (reuse
the shell pack on string args).

### N7. medium: plain `policy eval` ignores durable waivers
- `cmd/specctl/policy.go:1031-1032` `--strict` uses `Decide(report, {}, nil)`;
  `printReport` (`:1431`) has no waived status. Run (`gw` + `libw` with one
  exception): `eval` prints `deny`, `--strict` exits **1**. Only
  `eval --inherited` and `findings` honour exceptions. `--specs-only --strict`
  same. Contradicts `docs/policies.md:466-468` "Every path that decides honours
  it -- the offline evaluation".
- Fix: pass `policyeval.Waivers(library, nil, now)` and print `waived` in both
  modes.

### N8. medium: rule 2's "the guest must reach out" is an existence check
- `rfp-guest-reports-network/src.rego:18-36` passes when any guest flow carries
  network-info; clause 2 checks only that the emitter has a trigger from a
  report handler. Run `h/dhcp_leases`: the bidder reads the address from the
  host's DHCP lease file and passes it through `handleOnNetworkReport`:
  0 violations. Data provenance (the emitted address comes from the report
  body) is not checked.
- Fix: require the emitted event's payload to flow from the handler's request
  body (a def-use edge, or at least that the emitter is reachable only from the
  handler), and flag a host-sourced address feeding the emitter.

### N9. medium: `policy fix` does not produce a SpecChange
- `cmd/specctl/policy_findings.go:524-575` prints a `FixRequest`
  (`abc/policy/fix.go:13-33`); nothing converts it into a `SpecChange` object or
  applies it (only `impl/realize/policy_fix_test.go` uses it). U4 says "fix it
  (turn it into a SpecChange)".
- Fix: `policy fix <key> --apply` that writes a SpecChange (instruction =
  prompt, contexts = the site's context) to kcp or the spec branch.

### N10. medium: bind honesty claim
- `docs/examples/atproto-market-policies.md:579-600`: a blind first-attempt bind
  byte-identical to the hand-written file, 14 host globs and
  `targets.symbols [reportUrl, _url_path, requester, REPORT_PATH]` in the same
  order, is itself evidence of a remaining copy path; the doc admits no trace.
- Fix: re-run on a perturbed clone (renamed symbols and directories) with a
  transcript captured outside the sandbox; score field by field.

### N11. low
- B9 open (no spec-time clause for rule 1); A3, G6, D3 open (table above).
- Comments regressed in `abc/policy` (0 -> 55) and new files in `cmd/specctl`.
- `pack.yaml:9` "three rules".
- `policy build` leaves `dist/<slug>.yaml` of a deleted template (seen in
  `packonly/dist`); eval reads templates, so only gator suites and the catalogue
  can go stale.
- Pack suites test the Rego on hand-written models; N1/N5 show the model
  builder never produces some of those shapes. Add fixture-driven cases
  (`TestViolatingVariantsDeny`) for every pack suite deny case.

## (4) Does the system meet the goal?

- **First-class over spec-driven development:** yes in structure: orphan
  `open-policy/<repo>` branch, kcp ConstraintTemplates, audit + spec gate +
  realize gate, PolicyChange generation, exceptions on the branch.
- **Gatekeeper-based, extensible:** yes: real ConstraintTemplates, gator parity,
  derived CRDs for CodeGraph/CodeDiff/ArchitectureModel, packs with pins
  (embedded/git/OCI), `policy new` scaffolds.
- **Easy to write and generate, runnable examples:** mostly: `policy new`,
  examples, `scripts/example-policy-findings.sh` runs. Generation checks stay
  weak (D3) and the bind evidence is not convincing (N10).
- **Portable, greenfield:** partly: the pack carries no repo identifiers, but the
  relay rule is bypassable by neighbourhood text (N1), the direct-dial check is
  a tool list (N4), host reach-in needs per-repo exception lists, greenfield
  specs are not checked for rule 1 (B9).
- **The two rules themselves:** not reliably enforced (N1, N4, N5, N6, N8), and
  the delta enforcement the user asked for (U3) breaks on any line shift (N2).

## Prioritized list

1. N2 (critical): line-independent violation identity for baseline and waivers;
   regression test with shifted lines, on the atproto-market onNetwork case.
2. N1 (critical): relay exemption only from the effect's own args; never exempt
   an empty or direct ProxyCommand; add the two runs as fixtures.
3. N3 (high): strict exception decoding, `key` required unless an explicit
   constraint scope, clause identity in the key, no siteless waiver by default.
4. N4 (high): target-based direct-dial detection; ProxyJump/-J; ssh family
   argv0 and shell strings; unresolved proxy = warn; guest-or-unknown target
   for clause 1.
5. N5 (high): test-role unknown-target dial denies; add vD fixture.
6. N7 (medium): waivers in plain `eval` and `--specs-only`.
7. N8 (medium): payload provenance from the report handler to the emitter.
8. N6 (medium): shell-string and `child_process` classification.
9. N9 (medium): `policy fix --apply` creates the SpecChange.
10. Phase I re-run with change-scoped gates (after N2), and the N10 bind re-run.
11. Low: B9, A3, G6, D3, comments, pack wording, stale `dist/` files.
