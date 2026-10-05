# Policy catalogue

repository: deno-kcp

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| change-succeeded-with-failed-acceptance | a change is Succeeded while a gating acceptance step failed | MUST | error | - | change-succeeded-with-failed-acceptance (deny) |
| provisioning-cloud-init-bypass | the cloud-init user_data path is skipped | MUST | error | - | provisioning-cloud-init-bypass (deny) |
| provisioning-container-in-test | a test stands up its own container instead of driving runComputeContract | MUST | error | - | provisioning-container-in-test (deny) |
| provisioning-manual-key-material | ssh key material is made by hand instead of by the RFP cloud-init | MUST | error | - | provisioning-manual-key-material (deny) |
| provisioning-new-guest-transport | a guest transport is added outside cloud-init-common | MUST | error | - | provisioning-new-guest-transport (deny) |
| relay-only-ssh | integration tests ssh to a guest only over the relay | MUST | error | - | relay-only-ssh (deny) |
| requirement-text-has-machine-path | a requirement text names a path that exists on one machine only | MUST | error | - | requirement-text-has-machine-path (deny) |
| security-disabled-verification | an added line turns off certificate verification | MUST | error | - | security-disabled-verification (deny) |
