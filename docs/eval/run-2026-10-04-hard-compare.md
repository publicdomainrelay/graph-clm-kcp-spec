# specctl eval comparison

- baseline agent: `scripted`
- live agent: `claude-mod`
- not discriminating: no
- 6 of 11 shared measure(s) differ

| measure | scripted | live | equal |
| --- | --- | --- | --- |
| spec -> code pass rate (verify + acceptance) | 100.0% | 93.3% | no |
| spec -> code via CLM | not measured | 100.0% | no |
| delta precision (entries as intended) | 100.0% | 100.0% | yes |
| code -> spec facts stated (judged) | 100.0% | 95.9% | no |
| spec sufficiency (rebuild from spec, original tests) | not measured | 100.0% | no |
| drift (code -> spec from a human edit) | 100.0% | 50.0% | no |
| interface recall | 100.0% | 100.0% | yes |
| interface precision | 100.0% | 81.8% | no |
| interface F1 | 100.0% | 81.8% | no |
| requirement anchoring | 100.0% | 95.0% | no |
| validator pass | 100.0% | 100.0% | yes |
| round trip interface Jaccard | 100.0% | 100.0% | yes |
| fixtures reaching Populated | 100.0% | 100.0% | yes |

