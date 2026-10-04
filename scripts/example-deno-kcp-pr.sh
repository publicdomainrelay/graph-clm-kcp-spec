#!/usr/bin/env bash
#
# One natural-language request becomes a pull request against a real repository
# this tool did not write: publicdomainrelay/deno-kcp. A thin wrapper around
# scripts/example-pr.sh; the general script's header documents every knob.
#
# deno-kcp's go.mod replaces kcp-libs with ../kcp-libs, so it is cloned beside
# it; the bidder and the PDS live in two more repositories the harness reads to
# learn their flags.
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)

REPO=deno-kcp
SIBLINGS="kcp-libs atproto-market hono-pds"
PROMPT="add a running bidder instance to the example and a PDS for bob under his own namespace"
BRANCH=${BRANCH:-spec/bidder-and-bob-pds-$(date +%Y%m%d%H%M%S)}
BRIEF=${BRIEF:-"
Extra guidance for this repository:

- The contexts the request concerns are deploy-examples-atproto-market (\"the example\") and test-integration (its examples registry lists the example files the tests check).
- Read (do not edit) the existing manifests in deploy/examples/atproto/market and, beside this clone, \$WORK/atproto-market/hono-bidder (mod.ts, cli-args-env.ts, config.json) and \$WORK/hono-pds. Find the bidder's entry module, the flags and env it needs to run here (PLC directory, relay, keys, serve port, a compute provider that needs no cloud credentials) and which workspace it belongs in. The example's manifests name sibling repositories under /home/johnandersen777/src/publicdomainrelay-kcp, which apply.sh rewrites to the real org root; keep that convention.
- The requirements must state precisely: a workspace root:bob with its OpenBao object; a DenoPod pds in root:bob namespace default, built like alice's, serving pds.default.bob.svc.kcp.local on a port no other service uses; a running bidder DenoPod (name it bidder, say which workspace and why) with the exact entry path, argv, env and port; apply.sh, rbac and the README table updated; the test-side examples registry updated so the tests cover the new files. Name the files to create or change.
"}

export REPO SIBLINGS PROMPT BRANCH BRIEF
exec "$HERE/example-pr.sh"
