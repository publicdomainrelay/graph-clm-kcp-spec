# Cypher conformance: hydradb vs arcadedb

Each row is one query from `src/conformance.ts`, run over Bolt against both engines. `core` marks the subset `src/cypher.ts` emits, which the pi extension depends on.

| case | group | core | hydradb | arcadedb | failing engine says |
| --- | --- | --- | --- | --- | --- |
| `read.count_all` | read |  | no | yes | OpenCypher query is not supported yet: node-only MATCH requires an id, label, or property predicate |
| `read.bare_node` | read |  | no | yes | OpenCypher query is not supported yet: RETURN currently supports <binding>.<property> or count(*) |
| `read.labels_function` | read |  | no | yes | OpenCypher query is not supported yet: RETURN currently supports <binding>.<property> or count(*) |
| `read.count_by_label` | read | core | yes | yes |  |
| `read.property_by_id` | read | core | yes | yes |  |
| `read.two_properties` | read | core | yes | yes |  |
| `read.where_equality` | read | core | yes | yes |  |
| `read.where_is_not_null` | read |  | yes | yes |  |
| `read.order_by_alias` | read | core | yes | yes |  |
| `read.order_by_desc` | read |  | yes | yes |  |
| `read.limit` | read | core | yes | yes |  |
| `read.skip` | read |  | yes | yes |  |
| `read.distinct` | read |  | yes | yes |  |
| `read.parameter_predicate` | read |  | yes | yes |  |
| `read.regex_match` | read |  | no | yes | OpenCypher query is not supported yet: WHERE currently supports boolean combinations of property comparisons |
| `read.starts_with` | read |  | yes | yes |  |
| `read.union` | read |  | yes | yes |  |
| `read.optional_match` | read |  | yes | yes |  |
| `read.optional_match_was_null` | read |  | no | no | returned 3 rows, expected exactly 2 |
| `read.with_clause` | read |  | no | yes | OpenCypher query is not supported yet: WITH currently supports only pass-through identifiers without DISTINCT, WHERE, or ORDER BY |
| `agg.sum` | aggregation |  | yes | yes |  |
| `agg.collect` | aggregation |  | yes | yes |  |
| `agg.group_by` | aggregation |  | yes | yes |  |
| `traversal.one_hop_typed` | traversal | core | yes | yes |  |
| `traversal.one_hop_count` | traversal | core | yes | yes |  |
| `traversal.multi_hop` | traversal |  | yes | yes |  |
| `traversal.variable_length` | traversal |  | yes | yes |  |
| `traversal.relationship_type_function` | traversal |  | no | yes | OpenCypher query is not supported yet: relationship pattern must have exactly one type in Query engine |
| `write.create_literal` | write |  | no | yes | OpenCypher query is not supported yet: only one-hop edge patterns are executable in Query engine CREATE |
| `write.create_with_return` | write |  | no | yes | OpenCypher query is not supported yet: CREATE with following clauses is not executable in Query engine |
| `write.unwind_vertex_upsert` | write | core | yes | yes |  |
| `write.merge_literal` | write |  | no | yes | OpenCypher query is not supported yet: MERGE with following clauses is not executable in Query engine |
| `write.unwind_edge_create` | write | core | yes | yes |  |
| `write.set_property` | write |  | yes | yes |  |
| `write.detach_delete_match` | write | core | yes | yes |  |
| `write.detach_delete_unwind` | write | core | yes | yes |  |
| `write.delete_connected_vertex` | write |  | no | no | Graph query is not supported yet: DELETE vertex batch requires DETACH because it has 1 incident edge(s) |
| `write.string_node_id` | write |  | no | yes | OpenCypher query is not supported yet: only one-hop edge patterns are executable in Query engine CREATE |
| `schema.create_index` | schema |  | no | yes | OpenCypher parse error: Invalid input 'c': expected '=' or CREATE INDEX ON |
| `schema.show_indexes` | schema |  | no | yes | OpenCypher parse error: Invalid input 'H': expected SET or START |

**Totals:** hydradb 26/40, arcadedb 38/40. Core subset: 12/12 and 12/12.

## Divergences

- `read.count_all`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: node-only MATCH requires an id, label, or property predicate`
- `read.bare_node`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: RETURN currently supports <binding>.<property> or count(*)`
- `read.labels_function`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: RETURN currently supports <binding>.<property> or count(*)`
- `read.regex_match`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: WHERE currently supports boolean combinations of property comparisons`
- `read.with_clause`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: WITH currently supports only pass-through identifiers without DISTINCT, WHERE, or ORDER BY`
- `traversal.relationship_type_function`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: relationship pattern must have exactly one type in Query engine`
- `write.create_literal`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: only one-hop edge patterns are executable in Query engine CREATE`
- `write.create_with_return`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: CREATE with following clauses is not executable in Query engine`
- `write.merge_literal`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: MERGE with following clauses is not executable in Query engine`
- `write.string_node_id`: arcadedb accepts, hydradb rejects — `OpenCypher query is not supported yet: only one-hop edge patterns are executable in Query engine CREATE`
- `schema.create_index`: arcadedb accepts, hydradb rejects — `OpenCypher parse error: Invalid input 'c': expected '=' or CREATE INDEX ON`
- `schema.show_indexes`: arcadedb accepts, hydradb rejects — `OpenCypher parse error: Invalid input 'H': expected SET or START`

