# orgroot fixtures

Sample codebases for an org root: a superproject plus three member
repositories, shaped like the publicdomainrelay polyrepo (the RFP flow split
across a market, a compute provider and a relay).

| directory | plays |
| --- | --- |
| `root/` | the org root's own files: its CLAUDE.md, docs and scripts |
| `market/` | requester and the guest's cloud-init: the guest side |
| `provider/` | the compute provider: the host side |
| `relay/` | the websocket relay every guest connection goes through |

`test/orgfixture` turns these into real git repositories: one bare remote per
repository, orphan `open-architecture/*` and `open-policy/*` branches on the
members, and a superproject that pins them as submodules.

Like every directory under `fixtures/`, these stand in for other people's code:
their comments are input to the code to spec measurement.
