#!/usr/bin/env bash
#
# One request becomes a pull request against publicdomainrelay/atproto-market:
# switch the guest transport from did-key-ingress-proxy (websocat over the xrpc
# relay) to iroh / dumbpipe. A thin wrapper around scripts/example-pr.sh; the
# general script's header documents every knob.
#
# atproto-market's deno.json pulls eight sibling repositories through relative
# `../<dir>/...` imports. Three of them live on GitHub under another directory
# name (`hono-jsr` is hono-package-registry, `hono-compute-provider` is
# compute-provider-digitalocean, `deno-worker-sandbox` is deno-hono-sandbox);
# the rest keep their name. A directory that exists in the org root is cloned
# from there, because that is the working set the sibling revisions were
# developed against.
#
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)

REPO=atproto-market
BASE=${BASE:-pre-iroh}
SIBLINGS=${SIBLINGS:-"atproto-relay deno-macos-runner-desktop deno-worker-sandbox=deno-hono-sandbox did-key-ingress-proxy hono-compute-provider=compute-provider-digitalocean hono-jsr=hono-package-registry hono-pds policy-engine typescript-helpers"}
PROMPT=${PROMPT:-"switch from using did-key-ingress-proxy to iroh - use dumbpipe.dev to do this, you will need to switch out the onNetwork stuff and the ssh proxycommand as well and other things like that"}
BRANCH=${BRANCH:-spec/iroh-dumbpipe-$(date +%Y%m%d%H%M%S)}
VERIFY=${VERIFY:-'deno check && deno test -A test/cloud_init_snapshot_test.ts'}
BRIEF=${BRIEF:-"
Extra guidance for this repository:

- The org rule is \"the RFP flow is the spine\": a guest is born from the cloud-init user_data the winning bidder applies, and nothing else may reach a guest. The guest-side transport must therefore be installed by a cloud-init UserDataModule registered in lib/common/cloud-init-common/mod.ts (registerUserDataModule), exactly like the existing tunnel and fedproxy-ssh modules. No test may hand-provision a guest or mount a binary: test/bidder_container_integration_test.ts drives runComputeContract through RFP, bid, accept and cloud-init, and must keep doing so.
- Research iroh and dumbpipe before writing requirements: https://dumbpipe.dev and https://github.com/n0-computer/dumbpipe. The iroh project's own releases ship no 'iroh' CLI (only iroh-relay and iroh-dns-server); dumbpipe is the tool the request names. dumbpipe listen-tcp --host 127.0.0.1:22 on the guest prints a ticket on stdout; dumbpipe connect-tcp --addr 127.0.0.1:<port> <ticket> on the host listens on a local TCP port and forwards every connection to the guest. A ticket is a 256-bit iroh endpoint id, not an address, and it is stable for the lifetime of the listener. dumbpipe is installed from its release archives (dumbpipe-v<version>-linux-x86_64.tar.gz) the same way the existing modules install their binaries.
- The change must replace, concretely: lib/did-key-ingress-proxy (the SSH transport it carries), the websocat ProxyCommand in lib/requester-xrpc/mod.ts, and the vm.onNetwork node-id event path. Name every file to create or change.
- Say how the guest's ticket reaches the requester, and who reads it and when: the code agent sees only this repository and the spec, so leave no ambiguity. Keep the requirement at the behaviour level; let the code agent choose the mechanism.
- ssh must still reach the guest as root with the requester's own key, through a ProxyCommand over the new transport. Requests that must keep working: skipSsh, keepVm, the fedproxy and tunnel-subscriber transports if they stay, and the transport validation errors.
- Verify gate: deno check over the whole workspace plus deno test -A test/cloud_init_snapshot_test.ts. A requirement that changes cloud-init output must say to regenerate test/fixtures/cloud-init/ and to keep the existing snapshot tests green.
"}

export REPO BASE SIBLINGS PROMPT BRANCH VERIFY BRIEF
exec "$HERE/example-pr.sh"
