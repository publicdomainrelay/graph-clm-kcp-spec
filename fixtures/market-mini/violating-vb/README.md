# market-mini (violating-vb)

The compliant fixture with a test file that holds two ssh calls: the first test
sshes to the guest address directly, the second one goes through the relay.
A classifier that reads the enclosing file instead of the call site lends the
first test the second test's `ProxyCommand` and reports nothing (review 0003
B1, run vB). The direct ssh has no tunnel, so it must deny.
