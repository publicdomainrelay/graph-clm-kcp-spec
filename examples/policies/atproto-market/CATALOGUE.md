# Policy catalogue

repository: atproto-market

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| guest-report-cloud-init | cloud-init makes the guest publish its address outbound | MUST | error | - | guest-report-cloud-init (deny) |
| guest-report-driven-emission | the guest network identity and the vm.onNetwork event are emitted from an inbound guest report | MUST | error | - | guest-report-driven-emission (deny), guest-report-driven-onnetwork (deny) |
| guest-report-reach-in | the network emitter never reaches into the guest | MUST | error | - | guest-report-reach-in (deny) |
| relay-only-ssh | integration tests ssh to a guest only over the relay | MUST | error | - | relay-only-ssh (deny) |
| rfp-guest-reports-network | the guest reports its network information and that report drives the host's emission | MUST | error | - | rfp-guest-reports-network (deny) |
| rfp-host-reach-in | the host never reaches into the guest for network discovery | MUST | error | - | rfp-host-reach-in (deny) |
| rfp-relay-only-guest-ssh | ssh to a guest goes through the relay | MUST | error | - | rfp-relay-only-guest-ssh (deny) |
