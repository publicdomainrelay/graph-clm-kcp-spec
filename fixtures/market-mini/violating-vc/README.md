# market-mini (violating-vc)

The compliant fixture with a requester that holds the ssh binary in a
constant: `const SSH = "ssh"` as the argv0 of the command that reaches the
guest directly. A classifier that reads only a literal argv0 sees a proc.exec
and reports nothing (review 0003 B4, run vC). The call is an ssh with no
tunnel, so it must deny.
