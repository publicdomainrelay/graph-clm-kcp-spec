# Policy catalogue

repository: conformance

| policy | title | level | severity | requirements | constraints |
| --- | --- | --- | --- | --- | --- |
| conformance-forbidden-flow | no flow matches a declared must-never | MUST | error | - | conformance-forbidden-flow (deny) |
| conformance-must-unrealized | a declared MUST flow has evidence once code exists | SHOULD | warning | - | conformance-must-unrealized (warn) |
| conformance-observed-undeclared | every observed flow is declared by an interaction | SHOULD | warning | - | conformance-observed-undeclared (warn) |
