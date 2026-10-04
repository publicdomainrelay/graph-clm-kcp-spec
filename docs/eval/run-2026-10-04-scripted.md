# specctl eval report

- agent: `scripted`
- fixtures: `fixtures`
- started: 2026-10-04T00:28:49Z
- finished: 2026-10-04T00:29:42Z
- scenarios: 15

## Measures

| measure | value | samples |
| --- | --- | --- |
| spec -> code pass rate (verify + acceptance) | 100.0% | 14 (1 excluded) |
| spec -> code via CLM | not measured | 0 |
| delta precision (entries as intended) | 100.0% | 14 (1 excluded) |
| code -> spec facts stated (judged) | 100.0% | 9 (3 excluded) |
| spec sufficiency (rebuild from spec, original tests) | not measured | 0 |
| drift (code -> spec from a human edit) | 100.0% | 4 |
| interface recall | 100.0% | 9 (3 excluded) |
| interface precision | 100.0% | 9 (3 excluded) |
| interface F1 | 100.0% | 9 (3 excluded) |
| requirement anchoring | 100.0% | 12 |
| validator pass | 100.0% | 9 (3 excluded) |
| round trip interface Jaccard | 100.0% | 12 |
| fixtures reaching Populated | 100.0% | 5 |

## Populate (one Repository manifest per fixture)

| fixture | phase | contexts | summarized | failed | wall time |
| --- | --- | --- | --- | --- | --- |
| calc | Populated | 2 | 2 | 0 | 1.27s |
| greet | Populated | 2 | 2 | 0 | 1.77s |
| ledger | Populated | 4 | 4 | 0 | 1.76s |
| shared | Populated | 1 | 1 | 0 | 1.76s |
| todo | Populated | 3 | 3 | 0 | 2.27s |

## Code -> spec

| fixture | context | observed | declared | recall | precision | anchoring | validator | round trip | req delta |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| calc | calc | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| calc | cmd-calc | 0 | 0 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| greet | format | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| greet | greet | 6 | 6 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| ledger | cmd-ledger | 0 | 0 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| ledger | domain | 10 | 10 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| ledger | httpapi | 1 | 1 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| ledger | storage | 7 | 7 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| shared | shared | 8 | 8 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| todo | cmd-todo | 0 | 0 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| todo | httpapi | 1 | 1 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| todo | todo | 7 | 7 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |

## Facts stated (what the model was not handed)

| fixture | context | fact | stated | judge | evidence |
| --- | --- | --- | --- | --- | --- |
| calc | calc | f-add-sums | yes | keyword | add |
| calc | calc | f-multiply-products | yes | keyword | multiply |
| greet | format | f-title-case | yes | keyword | titlecase |
| greet | format | f-trim-all | yes | keyword | trimall |
| greet | greet | f-greet-text | yes | keyword | greet |
| greet | greet | f-greeter-prefix | yes | keyword | greeter |
| greet | greet | f-shout-uppercase | yes | keyword | shout |
| ledger | domain | f-balance-sums-account | yes | keyword | balance |
| ledger | domain | f-empty-account-error | yes | keyword | balance |
| ledger | domain | f-entries-oldest-first | yes | keyword | entries |
| ledger | domain | f-post-appends | yes | keyword | post |
| ledger | domain | f-sum-totals | yes | keyword | sum |
| ledger | domain | f-validate-empty-account | yes | keyword | validate |
| ledger | httpapi | f-entries-query | yes | keyword | entries |
| ledger | httpapi | f-handler-routes | yes | keyword | handler |
| ledger | storage | f-add-appends | yes | keyword | add |
| ledger | storage | f-all-copies | yes | keyword | all |
| ledger | storage | f-for-one-account | yes | keyword | for |
| ledger | storage | f-load-reads | yes | keyword | load |
| ledger | storage | f-save-json | yes | keyword | save |
| shared | shared | f-indexer-order | yes | keyword | indexer |
| shared | shared | f-set-sorted | yes | keyword | set |
| todo | httpapi | f-handler-routes | yes | keyword | handler |
| todo | todo | f-add-returns | yes | keyword | add |
| todo | todo | f-complete-marks-done | yes | keyword | complete |
| todo | todo | f-get-not-found | yes | keyword | get |
| todo | todo | f-list-oldest-first | yes | keyword | list |
| todo | todo | f-new-store-empty | yes | keyword | newstore |

## Drift (a human code edit, the spec specd wrote for it)

| fixture | scenario | context | phase | added | removed | delta | facts | pass |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| calc | human-adds-abs | calc | Succeeded | Abs |  | 1/1 | 1/1 | yes |
| ledger | human-adds-sign | domain | Succeeded | Sign |  | 1/1 | 1/1 | yes |
| ledger | human-rejects-negative | domain | Succeeded |  |  | 0/0 | 1/1 | yes |
| ledger | human-removes-save | storage | Succeeded |  | Store.Save | 1/1 | 1/1 | yes |

## Spec -> code (hidden acceptance tests)

| fixture | scenario | via | level | context | pass | verify | acceptance | delta | attempts | outside | progress | wall time |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| calc | add-divide-with-an-error | apply | 3 | calc | yes | yes | yes | 2/2 | 1 | none | 0 | 0.82s |
| calc | add-subtract | apply | 1 | calc | yes | yes | yes | 2/2 | 1 | none | 0 | 1.43s |
| calc | change-add-to-variadic | apply | 2 | calc | yes | yes | yes | 2/2 | 1 | none | 0 | 1.44s |
| greet | add-a-person-module | apply | 3 | greet | yes | yes | yes | 6/6 | 1 | none | 0 | 2.36s |
| greet | add-farewell | apply | 1 | greet | yes | yes | yes | 2/2 | 1 | none | 0 | 2.70s |
| greet | change-the-greeting-text | apply | 2 | greet | yes | yes | yes | 1/1 | 1 | none | 0 | 2.29s |
| ledger | clm-reject-negative-amounts | clm | 3 | domain | skipped | no | no | 0/0 | 0 | none | 0 | 0.00s |
| ledger | post-all-across-two-contexts | apply | 3 | domain+storage | yes | yes | yes | 4/4 | 2 | none | 0 | 2.92s |
| ledger | reject-negative-amounts | apply | 3 | domain | yes | yes | yes | 1/1 | 1 | none | 0 | 2.19s |
| ledger | remove-save | apply | 2 | storage | yes | yes | yes | 2/2 | 1 | none | 0 | 1.98s |
| ledger | rename-sum-to-total | apply | 3 | domain | yes | yes | yes | 3/3 | 1 | none | 0 | 1.70s |
| shared | add-delete-to-both | apply | 2 | shared | yes | yes | yes | 3/3 | 1 | none | 0 | 1.42s |
| todo | add-delete | apply | 1 | todo | yes | yes | yes | 2/2 | 1 | none | 0 | 1.79s |
| todo | add-get-one-task-endpoint | apply | 3 | httpapi | yes | yes | yes | 2/2 | 1 | none | 0 | 1.43s |
| todo | list-newest-first | apply | 2 | todo | yes | yes | yes | 1/1 | 1 | none | 0 | 1.67s |

## Notes

- working trees under /tmp/specd-eval.3078104291

