# market-mini (violating-vn8-dhcp)

The compliant fixture with a bidder that reads the guest's address from the
host's DHCP lease file and feeds it to `handleOnNetworkReport`, so the emitted
`vm.onNetwork` carries a host-sourced address while the emitter still sits
behind the report handler (review 0006 N8, run `h/dhcp_leases`). The guest must
report its own network information outbound, so this must deny.
