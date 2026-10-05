# Policy catalogue

repository: market-mini

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| guest-report-cloud-init | cloud-init makes the guest publish its address outbound | MUST | error | - | guest-report-cloud-init (deny) |
| guest-report-driven-emission | the guest network identity is emitted from an inbound guest report | MUST | error | - | guest-report-driven-emission (deny) |
| guest-report-reach-in | the network emitter never reaches into the guest | MUST | error | - | guest-report-reach-in (deny) |
| relay-only-ssh | integration tests ssh to a guest only over the relay | MUST | error | - | relay-only-ssh (deny) |
