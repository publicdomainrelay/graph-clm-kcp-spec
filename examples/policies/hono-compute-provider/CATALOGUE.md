# Policy catalogue

repository: hono-compute-provider

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| rfp-guest-reports-network | the guest reports its network information and that report drives the host's emission | MUST | error | - | rfp-guest-reports-network (deny) |
| rfp-guest-transport-provenance | a guest transport is installed by the guest's user_data, not hand-assembled elsewhere | MUST | error | - | rfp-guest-transport-provenance (deny) |
| rfp-host-reach-in | the host never reaches into the guest for network discovery | MUST | error | - | rfp-host-reach-in (deny) |
| rfp-key-material-provenance | ssh key material is placed by the guest's user_data, never assembled by hand | MUST | error | - | rfp-key-material-provenance (deny) |
| rfp-relay-only-guest-ssh | ssh to a guest goes through the relay | MUST | error | - | rfp-relay-only-guest-ssh (deny) |
