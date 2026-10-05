# Policy catalogue

repository: atproto-market

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| change-succeeded-with-failed-acceptance | a change is Succeeded while a gating acceptance step failed | MUST | error | - | change-succeeded-with-failed-acceptance (warn) |
| guest-report-cloud-init | cloud-init makes the guest publish its address outbound | MUST | error | - | guest-report-cloud-init (deny) |
| guest-report-driven-emission | the guest network identity and the vm.onNetwork event are emitted from an inbound guest report | MUST | error | - | guest-report-driven-emission (deny), guest-report-driven-onnetwork (deny) |
| guest-report-reach-in | the network emitter never reaches into the guest | MUST | error | - | guest-report-reach-in (deny) |
| provisioning-cloud-init-bypass | the cloud-init user_data path is skipped | MUST | error | - | provisioning-cloud-init-bypass (warn) |
| provisioning-container-in-test | a test stands up its own container instead of driving runComputeContract | MUST | error | - | provisioning-container-in-test (warn) |
| provisioning-manual-key-material | ssh key material is made by hand instead of by the RFP cloud-init | MUST | error | - | provisioning-manual-key-material (warn) |
| provisioning-new-guest-transport | a guest transport is added outside cloud-init-common | MUST | error | - | provisioning-new-guest-transport (warn) |
| relay-only-ssh | integration tests ssh to a guest only over the relay | MUST | error | - | relay-only-ssh (deny) |
| requirement-text-has-machine-path | a requirement text names a path that exists on one machine only | MUST | error | - | requirement-text-has-machine-path (warn) |
| rfp-guest-reports-network | the guest reports its network information and that report drives the host's emission | MUST | error | - | rfp-guest-reports-network (deny) |
| rfp-guest-transport-provenance | a guest transport is installed by the guest's user_data, not hand-assembled elsewhere | MUST | error | - | rfp-guest-transport-provenance (deny) |
| rfp-host-reach-in | the host never reaches into the guest for network discovery | MUST | error | - | rfp-host-reach-in (deny) |
| rfp-key-material-provenance | ssh key material is placed by the guest's user_data, never assembled by hand | MUST | error | - | rfp-key-material-provenance (deny) |
| rfp-relay-only-guest-ssh | ssh to a guest goes through the relay | MUST | error | - | rfp-relay-only-guest-ssh (deny) |
| security-disabled-verification | an added line turns off certificate verification | MUST | error | - | security-disabled-verification (deny) |
