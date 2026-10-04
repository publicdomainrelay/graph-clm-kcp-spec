import { GraphClient } from "./graph.ts";
import { stableNodeId } from "./ids.ts";
import { EDGES, LABELS, SPEC_LABELS } from "./schema.ts";

export function specCodeRefId(codegraphId: string): number {
  return stableNodeId(`coderef:${codegraphId}`);
}

export async function linkSpecifies(
  client: GraphClient,
  memoryId: number,
  codegraphIds: Iterable<string>,
): Promise<number> {
  const linked = new Set(
    (
      await client.selectNeighbors(EDGES.specifies, LABELS.memory, SPEC_LABELS.requirement, ["id"], memoryId)
    ).map((row) => Number(row.id)),
  );
  let created = 0;
  for (const codegraphId of new Set(codegraphIds)) {
    const rows = await client.run(
      `MATCH (r:${SPEC_LABELS.requirement})-[:${EDGES.references}]->` +
        `(c:${SPEC_LABELS.codeRef} {id: $id}) RETURN r.id AS id`,
      { id: specCodeRefId(codegraphId) },
    );
    const edges = rows
      .map((row) => ({ src: memoryId, dst: Number(row.id) }))
      .filter((edge) => Number.isFinite(edge.dst) && !linked.has(edge.dst));
    await client.upsertEdges(EDGES.specifies, LABELS.memory, SPEC_LABELS.requirement, edges);
    for (const edge of edges) linked.add(edge.dst);
    created += edges.length;
  }
  return created;
}
