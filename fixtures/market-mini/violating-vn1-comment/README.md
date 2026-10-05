# market-mini (violating-vn1-comment)

The compliant fixture with a test that sshes to the guest directly under a
`// TODO switch to websocat` comment. A rule that exempts an ssh because the
line above it mentions a relay reads the comment and reports nothing (review
0006 N1, run `u2/test_ssh_direct_comment`). A comment is not a tunnel, so this
one must deny.
