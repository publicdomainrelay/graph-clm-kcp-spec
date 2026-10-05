# market-mini (violating-vn5-test-dial)

The compliant fixture with a test that opens a socket to a literal guest address and port. Review 0003 vD: the test's dial resolves to an unknown target in the model, so a rule that only reads the flow's target role reports nothing. It must deny (review 0006 N5).
