# market-mini (violating-ve)

The compliant fixture with a requester whose `ProxyCommand` is `nc` to the
guest's ssh port. The proxy command is not empty, so a rule that accepts any
proxy passes it, and the vocabulary never names `nc` as a relay (review 0003
B2, run vE). The tunnel is not a relay, so it must deny.
