export interface ProbeStatement {
  cypher: string;
  params?: Record<string, unknown>;
}

export interface ProbeCase extends ProbeStatement {
  id: string;
  group: "read" | "traversal" | "aggregation" | "write" | "schema";
  setup?: ProbeStatement[];
  portable: boolean;
  /** Minimum rows the probe must return, so a silently-empty result cannot pass. */
  minRows?: number;
  /** Exact row count, when the engine's semantics make it deterministic. */
  expectRows?: number;
  note?: string;
}

export const LABEL = "ClmProbe";
export const OTHER_LABEL = "ClmProbeOther";

const SEED_ROWS: ProbeStatement = {
  cypher: `UNWIND $rows AS row MERGE (n {id: row.id}) SET n:${LABEL}, n.name = row.name, n.weight = row.weight, n.kind = row.kind`,
  params: {
    rows: [
      { id: 900001, name: "alpha", weight: 1, kind: "seed" },
      { id: 900002, name: "beta", weight: 2, kind: "seed" },
      { id: 900003, name: "gamma", weight: 3, kind: "other" },
    ],
  },
};

const SEED_OTHER: ProbeStatement = {
  cypher: `UNWIND $rows AS row MERGE (n {id: row.id}) SET n:${OTHER_LABEL}, n.name = row.name`,
  params: { rows: [{ id: 910001, name: "delta" }] },
};

const SEED: ProbeStatement[] = [SEED_ROWS, SEED_OTHER];

const KNOWN_EDGE: ProbeStatement = {
  cypher: `UNWIND $rows AS row MATCH (a:${LABEL} {id: row.src}), (b:${LABEL} {id: row.dst}) CREATE (a)-[:CLM_KNOWS]->(b)`,
  params: {
    rows: [
      { src: 900001, dst: 900002 },
      { src: 900002, dst: 900003 },
    ],
  },
};

const SEED_WITH_EDGE: ProbeStatement[] = [...SEED, KNOWN_EDGE];

const ANTI_LABEL = "ClmAntiJoin";

const ANTI_JOIN_SEED: ProbeStatement[] = [
  {
    cypher: `UNWIND $rows AS row MERGE (n {id: row.id}) SET n:${ANTI_LABEL}, n.name = row.name`,
    params: {
      rows: [
        { id: 960001, name: "source" },
        { id: 960002, name: "sink" },
        { id: 960003, name: "isolated" },
      ],
    },
  },
  {
    cypher: `UNWIND $rows AS row MATCH (a:${ANTI_LABEL} {id: row.src}), (b:${ANTI_LABEL} {id: row.dst}) CREATE (a)-[:CLM_KNOWS]->(b)`,
    params: { rows: [{ src: 960001, dst: 960002 }] },
  },
];

export const PROBE_CASES: ProbeCase[] = [
  {
    id: "read.count_all",
    group: "read",
    cypher: "MATCH (n) RETURN count(*) AS total",
    setup: SEED,
    portable: false,
    note: "HydraDB: node-only MATCH requires an id, label, or property predicate",
    minRows: 1,
  },
  {
    id: "read.bare_node",
    group: "read",
    cypher: "MATCH (n) RETURN n LIMIT 1",
    setup: SEED,
    portable: false,
    note: "HydraDB: RETURN takes only <binding>.<property> or count(*)",
    minRows: 1,
  },
  {
    id: "read.labels_function",
    group: "read",
    cypher: `MATCH (n:${LABEL}) RETURN labels(n) AS labels`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.count_by_label",
    group: "read",
    cypher: `MATCH (n:${LABEL}) RETURN count(*) AS total`,
    setup: SEED,
    portable: true,
    minRows: 1,
  },
  {
    id: "read.property_by_id",
    group: "read",
    cypher: `MATCH (n:${LABEL} {id: 900001}) RETURN n.name AS name`,
    setup: SEED,
    portable: true,
    minRows: 1,
  },
  {
    id: "read.two_properties",
    group: "read",
    cypher: `MATCH (n:${LABEL} {id: 900001}) RETURN n.name AS name, n.weight AS weight`,
    setup: SEED,
    portable: true,
    minRows: 1,
  },
  {
    id: "read.where_equality",
    group: "read",
    cypher: `MATCH (n:${LABEL}) WHERE n.kind = 'seed' RETURN n.name AS name`,
    setup: SEED,
    portable: true,
    minRows: 1,
  },
  {
    id: "read.where_is_not_null",
    group: "read",
    cypher: `MATCH (n:${LABEL}) WHERE n.name IS NOT NULL RETURN n.name AS name`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.order_by_alias",
    group: "read",
    cypher: `MATCH (n:${LABEL}) RETURN n.name AS name ORDER BY name`,
    setup: SEED,
    portable: true,
    minRows: 1,
  },
  {
    id: "read.order_by_desc",
    group: "read",
    cypher: `MATCH (n:${LABEL}) RETURN n.name AS name ORDER BY name DESC`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.limit",
    group: "read",
    cypher: `MATCH (n:${LABEL}) RETURN n.name AS name LIMIT 2`,
    setup: SEED,
    portable: true,
    minRows: 1,
  },
  {
    id: "read.skip",
    group: "read",
    cypher: `MATCH (n:${LABEL}) RETURN n.name AS name SKIP 1 LIMIT 1`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.distinct",
    group: "read",
    cypher: `MATCH (n:${LABEL}) RETURN DISTINCT n.kind AS kind`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.parameter_predicate",
    group: "read",
    cypher: `MATCH (n:${LABEL} {id: $id}) RETURN n.name AS name`,
    params: { id: 900001 },
    setup: SEED,
    portable: false,
    note: "HydraDB: property values take literals, not parameters",
    minRows: 1,
  },
  {
    id: "read.regex_match",
    group: "read",
    cypher: `MATCH (n:${LABEL}) WHERE n.name =~ '(?i)ALPHA' RETURN n.name AS name`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.starts_with",
    group: "read",
    cypher: `MATCH (n:${LABEL}) WHERE n.name STARTS WITH 'al' RETURN n.name AS name`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.union",
    group: "read",
    cypher: `MATCH (n:${LABEL} {id: 900001}) RETURN n.name AS name UNION MATCH (n:${OTHER_LABEL} {id: 910001}) RETURN n.name AS name`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.optional_match",
    group: "read",
    cypher: `MATCH (n:${LABEL} {id: 900003}) OPTIONAL MATCH (n)-[:CLM_KNOWS]->(m) RETURN m.name AS name`,
    setup: SEED_WITH_EDGE,
    portable: false,
    minRows: 1,
  },
  {
    id: "read.optional_match_was_null",
    group: "read",
    cypher: `MATCH (n:${ANTI_LABEL}) OPTIONAL MATCH (n)-[:CLM_KNOWS]->(m:${ANTI_LABEL}) WHERE m.id IS NULL RETURN n.id AS id`,
    setup: ANTI_JOIN_SEED,
    portable: false,
    expectRows: 2,
    note:
      "anti-join: 960002 and 960003 have no outgoing edge, so two rows. " +
      "ArcadeDB ignores the WHERE on an OPTIONAL MATCH and returns every left row",
  },
  {
    id: "read.with_clause",
    group: "read",
    cypher: `MATCH (n:${LABEL}) WITH n WHERE n.weight > 1 RETURN n.name AS name`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "agg.sum",
    group: "aggregation",
    cypher: `MATCH (n:${LABEL}) RETURN sum(n.weight) AS total`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "agg.collect",
    group: "aggregation",
    cypher: `MATCH (n:${LABEL}) RETURN collect(n.name) AS names`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "agg.group_by",
    group: "aggregation",
    cypher: `MATCH (n:${LABEL}) RETURN n.kind AS kind, count(*) AS total`,
    setup: SEED,
    portable: false,
    minRows: 1,
  },
  {
    id: "traversal.one_hop_typed",
    group: "traversal",
    cypher: `MATCH (a:${LABEL} {id: 900001})-[:CLM_KNOWS]->(b:${LABEL}) RETURN b.name AS name`,
    setup: SEED_WITH_EDGE,
    portable: true,
    minRows: 1,
  },
  {
    id: "traversal.one_hop_count",
    group: "traversal",
    cypher: `MATCH (a:${LABEL} {id: 900001})-[:CLM_KNOWS]->(b:${LABEL}) RETURN count(*) AS total`,
    setup: SEED_WITH_EDGE,
    portable: true,
    minRows: 1,
  },
  {
    id: "traversal.multi_hop",
    group: "traversal",
    cypher: `MATCH (a:${LABEL} {id: 900001})-[:CLM_KNOWS]->(b:${LABEL})-[:CLM_KNOWS]->(c:${LABEL}) RETURN c.name AS name`,
    setup: SEED_WITH_EDGE,
    portable: false,
    minRows: 1,
  },
  {
    id: "traversal.variable_length",
    group: "traversal",
    cypher: `MATCH (a:${LABEL} {id: 900001})-[:CLM_KNOWS*1..2]->(c:${LABEL}) RETURN c.name AS name`,
    setup: SEED_WITH_EDGE,
    portable: false,
    minRows: 1,
  },
  {
    id: "traversal.relationship_type_function",
    group: "traversal",
    cypher: `MATCH (a:${LABEL} {id: 900001})-[r]->(b:${LABEL}) RETURN type(r) AS rel`,
    setup: SEED_WITH_EDGE,
    portable: false,
    minRows: 1,
  },
  {
    id: "write.create_literal",
    group: "write",
    cypher: `CREATE (n:${LABEL} {id: 920001, name: 'created'})`,
    portable: false,
  },
  {
    id: "write.create_with_return",
    group: "write",
    cypher: `CREATE (n:${LABEL} {id: 920002, name: 'created2'}) RETURN n.id AS id`,
    portable: false,
    note: "HydraDB: CREATE followed by another clause is not executable",
  },
  {
    id: "write.unwind_vertex_upsert",
    group: "write",
    cypher: `UNWIND $rows AS row MERGE (n {id: row.id}) SET n:${LABEL}, n.name = row.name`,
    params: { rows: [{ id: 930001, name: "upserted" }] },
    portable: true,
    note: "the form src/cypher.ts emits",
  },
  {
    id: "write.merge_literal",
    group: "write",
    cypher: `MERGE (n:${LABEL} {id: 930002}) SET n.name = 'merged'`,
    portable: false,
  },
  {
    id: "write.unwind_edge_create",
    group: "write",
    cypher: `UNWIND $rows AS row MATCH (a:${LABEL} {id: row.src}), (b:${LABEL} {id: row.dst}) CREATE (a)-[:CLM_LINK]->(b)`,
    params: { rows: [{ src: 930001, dst: 900001 }] },
    setup: [SEED_ROWS],
    portable: true,
    note: "the form src/cypher.ts emits",
  },
  {
    id: "write.set_property",
    group: "write",
    cypher: `MATCH (n:${LABEL} {id: 900001}) SET n.touched = 'yes'`,
    setup: SEED,
    portable: false,
  },
  {
    id: "write.detach_delete_match",
    group: "write",
    cypher: `MATCH (n:${LABEL} {id: 940002}) DETACH DELETE n`,
    setup: [
      {
        cypher: `UNWIND $rows AS row MERGE (n {id: row.id}) SET n:${LABEL}, n.name = row.name`,
        params: { rows: [{ id: 940002, name: "doomed" }] },
      },
    ],
    portable: true,
  },
  {
    id: "write.detach_delete_unwind",
    group: "write",
    cypher: `UNWIND $rows AS row MATCH (n {id: row.id}) DETACH DELETE n`,
    params: { rows: [{ id: 940001 }] },
    setup: [
      {
        cypher: `UNWIND $rows AS row MERGE (n {id: row.id}) SET n:${LABEL}, n.name = row.name`,
        params: { rows: [{ id: 940001, name: "doomed2" }] },
      },
    ],
    portable: true,
    note: "the form src/cypher.ts emits",
  },
  {
    id: "write.delete_connected_vertex",
    group: "write",
    cypher: `MATCH (n:${LABEL} {id: 950002}) DELETE n`,
    setup: [
      {
        cypher: `UNWIND $rows AS row MERGE (n {id: row.id}) SET n:${LABEL}, n.name = row.name`,
        params: {
          rows: [
            { id: 950001, name: "keep" },
            { id: 950002, name: "linked" },
          ],
        },
      },
      {
        cypher: `UNWIND $rows AS row MATCH (a:${LABEL} {id: row.src}), (b:${LABEL} {id: row.dst}) CREATE (a)-[:CLM_LINK]->(b)`,
        params: { rows: [{ src: 950001, dst: 950002 }] },
      },
    ],
    portable: false,
    note: "HydraDB: deleting a vertex with edges requires DETACH",
  },
  {
    id: "write.string_node_id",
    group: "write",
    cypher: `CREATE (n:${LABEL} {id: 'not-an-integer', name: 'stringy'})`,
    portable: false,
    note: "HydraDB: the node id property must be an integer",
  },
  {
    id: "schema.create_index",
    group: "schema",
    cypher: `CREATE INDEX clm_probe_name_idx FOR (n:${LABEL}) ON (n.name)`,
    portable: false,
  },
  {
    id: "schema.show_indexes",
    group: "schema",
    cypher: "SHOW INDEXES",
    portable: false,
  },
];

export const PORTABLE_CASES = PROBE_CASES.filter((probe) => probe.portable);
