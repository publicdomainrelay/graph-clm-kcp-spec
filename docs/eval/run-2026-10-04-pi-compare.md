# specctl eval comparison

- baseline agent: `scripted`
- live agent: `pi`
- not discriminating: no
- 5 of 11 shared measure(s) differ

| measure | scripted | live | equal |
| --- | --- | --- | --- |
| spec -> code pass rate (verify + acceptance) | 100.0% | 93.3% | no |
| spec -> code via CLM | not measured | 100.0% | no |
| delta precision (entries as intended) | 100.0% | 100.0% | yes |
| code -> spec facts stated (judged) | 100.0% | 93.3% | no |
| spec sufficiency (rebuild from spec, original tests) | not measured | not measured | no |
| drift (code -> spec from a human edit) | 100.0% | 75.0% | no |
| interface recall | 100.0% | 100.0% | yes |
| interface precision | 100.0% | 90.0% | no |
| interface F1 | 100.0% | 90.0% | no |
| requirement anchoring | 100.0% | 100.0% | yes |
| validator pass | 100.0% | 100.0% | yes |
| round trip interface Jaccard | 100.0% | 100.0% | yes |
| fixtures reaching Populated | 100.0% | 100.0% | yes |

