# org root

All TypeScript work in this directory tree uses the Deno + Hono + JSR layering
style. This directory is the org root: every repository of the project is a git
submodule below it.

Run `./scripts/find-all-package.ts` at session start to see where everything
lives.

## The RFP flow is the spine

A requester posts a compute request, bidders bid, the winner provisions a guest
through cloud-init `user_data`, and the requester reaches the guest only through
the relay. Nothing talks to a guest except through that path.
