# market-mini (compliant)

A small, realistic stand-in for `publicdomainrelay/atproto-market`: a bidder
that registers providers, a compute provider, a requester, a cloud-init module
registry, and an integration test that drives the bidder and the requester
together. This variant satisfies both example policies of plan 0008: every ssh
in the test and the requester goes through the relay with a `ProxyCommand`
built from the transport a cloud-init `UserDataModule` deploys, and the
`vm.onNetwork` event is produced only from the handler that receives the
guest's inbound report, never by reaching into the guest.
