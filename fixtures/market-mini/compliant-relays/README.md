# market-mini (compliant-relays)

The compliant fixture with the ssh forms that are not direct connections: a
`-J`/`ProxyJump` hop, a SOCKS proxy, an inner `ssh -W` through a jumphost, an
ssh config file that carries the ProxyCommand, and an ssh to a host that is not
the guest. Every one of them connects to a hop rather than to the guest, so the
rule must report nothing (review 0006 N4).
