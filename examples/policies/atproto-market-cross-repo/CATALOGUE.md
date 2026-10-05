# Policy catalogue

repository: atproto-market

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| rfp-guest-reports-network | the guest reports its network information and that report drives the host's emission | MUST | error | - | rfp-guest-reports-network (deny) |
| rfp-host-reach-in | the host never reaches into the guest for network discovery | MUST | error | - | rfp-host-reach-in (deny) |
| rfp-relay-only-guest-ssh | ssh to a guest goes through the relay | MUST | error | - | rfp-relay-only-guest-ssh (deny) |
