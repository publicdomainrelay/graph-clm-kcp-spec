import { writeFileSync } from "node:fs";
import { GraphClient } from "../src/graph.ts";
import {
  CODE_REF_PROPS,
  EDGES,
  FILE_PROPS,
  LABELS,
  MEMORY_PROPS,
  SESSION_PROPS,
  TURN_PROPS,
} from "../src/schema.ts";
import { resolveGraphTarget } from "../src/target.ts";

const LABEL_PROPS: Record<string, readonly string[]> = {
  [LABELS.session]: ["id", ...SESSION_PROPS],
  [LABELS.memory]: ["id", ...MEMORY_PROPS],
  [LABELS.file]: ["id", ...FILE_PROPS],
  [LABELS.turn]: ["id", ...TURN_PROPS],
  [LABELS.codeRef]: ["id", ...CODE_REF_PROPS],
};

const EDGE_SHAPES: Array<[string, string, string]> = [
  [EDGES.remembered, LABELS.session, LABELS.memory],
  [EDGES.touched, LABELS.session, LABELS.file],
  [EDGES.occurred, LABELS.session, LABELS.turn],
  [EDGES.references, LABELS.memory, LABELS.codeRef],
];

function truncate(value: unknown, max = 160): string {
  const text = String(value ?? "").replace(/\s+/g, " ");
  return text.length > max ? `${text.slice(0, max)}...` : text;
}

async function main(): Promise<void> {
  const target = resolveGraphTarget();
  const client = await GraphClient.connect({
    boltUrl: target.boltUrl,
    user: target.user,
    token: target.token,
    database: target.database,
  });

  const sessionFilter = process.env.HYDRA_DUMP_SESSION;
  const dump: Record<string, unknown> = { backend: target.backend, labels: {}, edges: {} };

  console.log(
    `backend: ${target.backend}  ${target.boltUrl}  db=${target.database ?? "-"}` +
      (sessionFilter ? `  session=${sessionFilter}` : "") +
      "\n",
  );

  const sessionId = sessionFilter
    ? Number(
        (
          await client.selectVertices(LABELS.session, ["id"], { key: sessionFilter })
        )[0]?.id,
      )
    : undefined;

  for (const [label, props] of Object.entries(LABEL_PROPS)) {
    const rows = await client.selectVertices(
      label,
      [...props],
      sessionId ? { session: sessionId } : {},
    );
    (dump.labels as Record<string, unknown>)[label] = rows;
    console.log(`== ${label} (${rows.length})`);
    for (const row of rows) {
      const id = row.id;
      if (label === LABELS.memory) {
        console.log(`  [${id}] ${row.kind} | ${row.title}`);
        console.log(`      ${truncate(row.body, 300)}`);
      } else if (label === LABELS.file) {
        console.log(`  [${id}] ${row.path}  touches=${row.touches}`);
      } else if (label === LABELS.turn) {
        console.log(`  [${id}] turn ${row.turnIndex}: ${truncate(row.summary, 160)}`);
      } else if (label === LABELS.codeRef) {
        console.log(`  [${id}] ${row.codegraph_id}  ${row.kind} ${row.name}`);
      } else {
        console.log(`  [${id}] ${row.key} cwd=${row.cwd} revision=${row.revision}`);
      }
    }
    console.log();
  }

  for (const [type, from, to] of EDGE_SHAPES) {
    const rows = await client.run(
      `MATCH (a:${from})-[:${type}]->(b:${to}) RETURN a.id AS from_id, b.id AS to_id`,
    );
    (dump.edges as Record<string, unknown>)[type] = rows;
    console.log(`== -[:${type}]-> ${rows.length}`);
  }

  await client.close();

  const out = process.env.HYDRA_DUMP_OUT ?? "/tmp/graph-dump.json";
  writeFileSync(out, `${JSON.stringify(dump, null, 2)}\n`);
  console.log(`\njson: ${out}`);
}

await main();
