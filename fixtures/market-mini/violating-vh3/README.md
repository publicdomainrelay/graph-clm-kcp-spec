# market-mini (violating-vh3)

The compliant fixture with a bidder that sshes into the running guest over the
relay and reads `ip -j addr` instead of waiting for the guest's own report.
The tunnel is the relay, so the relay rule is satisfied, and the target stays
unknown, so a rule that needs a resolved guest target reports nothing (review
0003 B3, run vH3). The host reaches into the guest, so it must deny.
