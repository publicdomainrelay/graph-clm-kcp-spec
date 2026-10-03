# specctl eval report

- agent: `scripted`
- fixtures: `fixtures`
- started: 2026-10-03T23:49:11Z
- finished: 2026-10-03T23:49:43Z
- scenarios: 10

## Measures

| measure | value |
| --- | --- |
| spec -> code pass rate (verify + acceptance) | 100.0% |
| delta precision (entries as intended) | 100.0% |
| interface recall | 100.0% |
| interface precision | 100.0% |
| interface F1 | 100.0% |
| requirement anchoring | 100.0% |
| validator pass | 100.0% |
| round trip interface Jaccard | 100.0% |
| fixtures reaching Populated | 100.0% |

## Populate (one Repository manifest per fixture)

| fixture | phase | contexts | summarized | failed | wall time |
| --- | --- | --- | --- | --- | --- |
| calc | Populated | 2 | 2 | 0 | 1.77s |
| greet | Populated | 2 | 2 | 0 | 1.76s |
| shared | Populated | 1 | 1 | 0 | 1.76s |
| todo | Populated | 3 | 3 | 0 | 2.27s |

## Code -> spec

| fixture | context | observed | declared | recall | precision | anchoring | validator | round trip | req delta |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| calc | calc | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| calc | cmd-calc | 0 | 0 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| greet | format | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| greet | greet | 6 | 6 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| shared | shared | 8 | 8 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| todo | cmd-todo | 0 | 0 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| todo | httpapi | 1 | 1 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| todo | todo | 7 | 7 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |

## Spec -> code (hidden acceptance tests)

| fixture | scenario | level | context | pass | verify | acceptance | delta | attempts | outside | progress | wall time |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| calc | add-divide-with-an-error | 3 | calc | yes | yes | yes | 2/2 | 1 | none | 0 | 1.07s |
| calc | add-subtract | 1 | calc | yes | yes | yes | 2/2 | 1 | none | 0 | 1.19s |
| calc | change-add-to-variadic | 2 | calc | yes | yes | yes | 2/2 | 1 | none | 0 | 1.19s |
| greet | add-a-person-module | 3 | greet | yes | yes | yes | 6/6 | 1 | none | 0 | 1.80s |
| greet | add-farewell | 1 | greet | yes | yes | yes | 2/2 | 1 | none | 0 | 1.72s |
| greet | change-the-greeting-text | 2 | greet | yes | yes | yes | 1/1 | 1 | none | 0 | 1.65s |
| shared | add-delete-to-both | 2 | shared | yes | yes | yes | 3/3 | 1 | none | 0 | 1.30s |
| todo | add-delete | 1 | todo | yes | yes | yes | 2/2 | 1 | none | 0 | 1.55s |
| todo | add-get-one-task-endpoint | 3 | httpapi | yes | yes | yes | 2/2 | 1 | none | 0 | 1.13s |
| todo | list-newest-first | 2 | todo | yes | yes | yes | 1/1 | 1 | none | 0 | 1.41s |

## Notes

- working trees under /tmp/specd-eval.1114978005

