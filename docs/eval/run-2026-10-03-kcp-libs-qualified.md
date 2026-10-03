# specctl eval report

- agent: `claude-mod`
- fixtures: `fixtures/external`
- started: 2026-10-03T23:32:36Z
- finished: 2026-10-03T23:36:28Z
- scenarios: 0

## Measures

| measure | value |
| --- | --- |
| spec -> code pass rate (verify + acceptance) | 100.0% |
| delta precision (entries as intended) | 100.0% |
| interface recall | 98.8% |
| interface precision | 98.2% |
| interface F1 | 98.4% |
| requirement anchoring | 100.0% |
| validator pass | 100.0% |
| round trip interface Jaccard | 100.0% |
| fixtures reaching Populated | 100.0% |

## Populate (one Repository manifest per fixture)

| fixture | phase | contexts | summarized | failed | wall time |
| --- | --- | --- | --- | --- | --- |
| kcp-libs | Populated | 34 | 34 | 0 | 231.31s |

## Code -> spec

| fixture | context | observed | declared | recall | precision | anchoring | validator | round trip | req delta |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| kcp-libs | abc-cache | 20 | 20 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-joballoc | 9 | 9 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-pki | 25 | 25 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-policy | 4 | 4 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-probe | 7 | 8 | 100.0% | 87.5% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-queue | 21 | 21 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-reconcile | 27 | 27 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-runner | 15 | 15 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-runref | 10 | 10 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-store | 12 | 12 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-clientlimit | 1 | 1 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-condition | 7 | 7 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-denocomputer | 7 | 7 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-denospec | 10 | 10 | 70.0% | 70.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-expiringmap | 11 | 11 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-kcp | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-logging | 3 | 3 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-outputs | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-ref | 8 | 8 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-statuspatch | 6 | 6 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-ttl | 4 | 4 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | factory-admission | 10 | 10 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | factory-controller | 11 | 11 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | factory-servicenames | 16 | 14 | 87.5% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-assets | 5 | 5 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-execrunner | 14 | 14 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-exportwatch | 9 | 9 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-informerwatch | 8 | 8 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-kcpstore | 27 | 27 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-memoryrunner | 14 | 14 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-metrics | 14 | 14 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-openbaoclient | 19 | 23 | 100.0% | 82.6% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-pkiprovisioner | 9 | 9 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-policyclient | 6 | 6 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |

## Spec -> code (hidden acceptance tests)

not measured

## Notes

- working trees under /tmp/specd-eval.2083225794

