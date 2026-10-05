# market-mini (violating-vn6-sh-container)

The compliant fixture with a bidder that inspects the guest's container inside a `sh -c` string. A classifier that reads only the argv0 of the command reports a proc.exec and nothing denies; the string's own argv0 is a container command, so it must deny (review 0006 N6).
