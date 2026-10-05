# market-mini (violating)

A small, realistic stand-in for `publicdomainrelay/atproto-market`: a bidder
that registers providers, a compute provider, a requester, a cloud-init module
registry, and an integration test that drives the bidder and the requester
together. This variant breaks both example policies of plan 0008: the test and
the requester ssh and `Deno.connect` a guest address directly, bypassing the
relay, and the bidder emits `vm.onNetwork` and `vm.registerIdentity` from the
provisioning lifecycle by calling `computeProvider.getNodeId(providerId)`,
reaching into the guest instead of letting the guest report.
