# specctl eval report

- agent: `claude-mod`
- fixtures: `fixtures/external`
- started: 2026-10-03T22:46:27Z
- finished: 2026-10-03T22:50:50Z
- scenarios: 0

## Measures

| measure | value |
| --- | --- |
| spec -> code pass rate (verify + acceptance) | 100.0% |
| delta precision (entries as intended) | 100.0% |
| interface recall | 85.7% |
| interface precision | 89.1% |
| interface F1 | 85.5% |
| requirement anchoring | 100.0% |
| validator pass | 100.0% |
| round trip interface Jaccard | 100.0% |
| fixtures reaching Populated | 0.0% |

## Populate (one Repository manifest per fixture)

| fixture | phase | contexts | summarized | failed | wall time |
| --- | --- | --- | --- | --- | --- |
| kcp-libs | Failed (Failed; Populated: PopulateFailed: 1 context(s) could not be summarized) | 34 | 33 | 1 | 260.98s |

- kcp-libs could not be summarized: abc-cache: ...or)","file":"abc/cache/cache.go"},{"name":"ByIndex","kind":"method","signature":"ByIndex(indexName, indexedValue string) ([]any, error)","file":"abc/cache/cache.go"},{"name":"List","kind":"method","signature":"List() []any","file":"abc/cache/cache.go"},{"name":"IndexFunc","kind":"type_alias","signature":"type IndexFunc ()","file":"abc/cache/cache.go"},{"name":"Indexers","kind":"type_alias","signature":"type Indexers ()","file":"abc/cache/cache.go"},{"name":"Set","kind":"struct","signature":"type Set struct","file":"abc/cache/cache.go"},{"name":"NewSet","kind":"function","signature":"func NewSet() *Set","file":"abc/cache/cache.go"},{"name":"Add","kind":"method","signature":"func (s *Set) Add(kind string, indexer Indexer)","file":"abc/cache/cache.go"},{"name":"Get","kind":"method","signature":"func (s *Set) Get(kind string, r ref.Ref) (any, bool)","file":"abc/cache/cache.go"},{"name":"ByIndex","kind":"method","signature":"func (s *Set) ByIndex(kind, index, value string) []any","file":"abc/cache/cache.go"},{"name":"Names","kind":"method","signature":"func (s *Set) Names(kind, index, value string) []string","file":"abc/cache/cache.go"},{"name":"Decode","kind":"function","signature":"func Decode[T any](obj any) (*T, error)","file":"abc/cache/cache.go"},{"name":"ClusterOf","kind":"function","signature":"func ClusterOf(obj any) string","file":"abc/cache/cache.go"},{"name":"RefOf","kind":"function","signature":"func RefOf(obj any) (ref.Ref, bool)","file":"abc/cache/cache.go"},{"name":"NameOf","kind":"function","signature":"func NameOf(obj any) string","file":"abc/cache/cache.go"},{"name":"PhaseOf","kind":"function","signature":"func PhaseOf(obj any) string","file":"abc/cache/cache.go"},{"name":"NestedString","kind":"function","signature":"func NestedString(obj any, fields ...string) string","file":"abc/cache/cache.go"},{"name":"IndexersFor","kind":"function","signature":"func IndexersFor(parentLabel, jobLabel, triggerPodField string) Indexers","file":"abc/cache/cache.go"}]} ...or)","file":"abc/cache/cache.go"},{"name":"ByIndex","kind":"method","signature":"ByIndex(indexName, indexedValue string) ([]any, error)","file":"abc/cache/cache.go"},{"name":"List","kind":"method","signature":"List() []any","file":"abc/cache/cache.go"},{"name":"IndexFunc","kind":"type_alias","signature":"type IndexFunc ()","file":"abc/cache/cache.go"},{"name":"Indexers","kind":"type_alias","signature":"type Indexers ()","file":"abc/cache/cache.go"},{"name":"Set","kind":"struct","signature":"type Set struct","file":"abc/cache/cache.go"},{"name":"NewSet","kind":"function","signature":"func NewSet() *Set","file":"abc/cache/cache.go"},{"name":"Add","kind":"method","signature":"func (s *Set) Add(kind string, indexer Indexer)","file":"abc/cache/cache.go"},{"name":"Get","kind":"method","signature":"func (s *Set) Get(kind string, r ref.Ref) (any, bool)","file":"abc/cache/cache.go"},{"name":"ByIndex","kind":"method","signature":"func (s *Set) ByIndex(kind, index, value string) []any","file":"abc/cache/cache.go"},{"name":"Names","kind":"method","signature":"func (s *Set) Names(kind, index, value string) []string","file":"abc/cache/cache.go"},{"name":"Decode","kind":"function","signature":"func Decode[T any](obj any) (*T, error)","file":"abc/cache/cache.go"},{"name":"ClusterOf","kind":"function","signature":"func ClusterOf(obj any) string","file":"abc/cache/cache.go"},{"name":"RefOf","kind":"function","signature":"func RefOf(obj any) (ref.Ref, bool)","file":"abc/cache/cache.go"},{"name":"NameOf","kind":"function","signature":"func NameOf(obj any) string","file":"abc/cache/cache.go"},{"name":"PhaseOf","kind":"function","signature":"func PhaseOf(obj any) string","file":"abc/cache/cache.go"},{"name":"NestedString","kind":"function","signature":"func NestedString(obj any, fields ...string) string","file":"abc/cache/cache.go"},{"name":"IndexersFor","kind":"function","signature":"func IndexersFor(parentLabel, jobLabel, triggerPodField string) Indexers","file":"abc/cache/cache.go"}]}

## Code -> spec

| fixture | context | observed | declared | recall | precision | anchoring | validator | round trip | req delta |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| kcp-libs | abc-cache | 18 | 0 | 0.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-joballoc | 9 | 9 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-pki | 24 | 9 | 37.5% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-policy | 4 | 4 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-probe | 7 | 8 | 100.0% | 87.5% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-queue | 21 | 21 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-reconcile | 24 | 24 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-runner | 11 | 11 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-runref | 9 | 10 | 55.6% | 50.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | abc-store | 12 | 12 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-clientlimit | 1 | 1 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-condition | 7 | 7 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-denocomputer | 7 | 7 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-denospec | 10 | 10 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-expiringmap | 11 | 11 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-kcp | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-logging | 3 | 3 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-outputs | 2 | 2 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-ref | 7 | 8 | 71.4% | 62.5% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-statuspatch | 6 | 6 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | common-ttl | 4 | 4 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | factory-admission | 10 | 10 | 40.0% | 40.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | factory-controller | 11 | 11 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | factory-servicenames | 14 | 14 | 57.1% | 57.1% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-assets | 5 | 5 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-execrunner | 10 | 10 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-exportwatch | 9 | 9 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-informerwatch | 8 | 8 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-kcpstore | 27 | 27 | 29.6% | 29.6% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-memoryrunner | 10 | 10 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-metrics | 13 | 14 | 23.1% | 21.4% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-openbaoclient | 19 | 23 | 100.0% | 82.6% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-pkiprovisioner | 9 | 9 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |
| kcp-libs | impl-policyclient | 6 | 6 | 100.0% | 100.0% | 100.0% | yes | 100.0% | +0 |

## Spec -> code (hidden acceptance tests)

not measured

## Notes

- working trees under /tmp/specd-eval.3824919250

