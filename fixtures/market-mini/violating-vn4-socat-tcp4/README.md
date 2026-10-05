# market-mini (violating-vn4-socat-tcp4)

The compliant fixture with a requester whose `ProxyCommand` is `socat - TCP4:<guest>:22`. A rule that knows only the bare `socat ... TCP:` spelling reports nothing; the proxy dials the guest address, so it must deny (review 0006 N4).
