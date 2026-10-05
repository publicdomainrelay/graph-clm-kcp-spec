# Policy catalogue

repository: rfp-provisioning-provenance

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| rfp-guest-transport-provenance | a guest transport is installed by the guest's user_data, not hand-assembled elsewhere | MUST | error | - | rfp-guest-transport-provenance (deny) |
| rfp-key-material-provenance | ssh key material is placed by the guest's user_data, never assembled by hand | MUST | error | - | rfp-key-material-provenance (deny) |
