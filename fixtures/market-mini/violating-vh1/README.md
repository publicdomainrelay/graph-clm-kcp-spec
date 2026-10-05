# market-mini (violating-vh1)

The compliant fixture with a bidder that reads the guest's address with
`container inspect` instead of waiting for the guest's own report. The verb is
not the one the binding names, so the target stays unknown and a rule that
needs a resolved guest target reports nothing (review 0003 B3, run vH1). The
host reaches into the guest, so it must deny.
