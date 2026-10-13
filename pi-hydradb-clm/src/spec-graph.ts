import { GraphClient } from "./graph.ts";
import { codeRefIdIn } from "../../clm/core/ids.ts";
import { EDGES, LABELS, SPEC_LABELS } from "./schema.ts";

// specCodeRefId is graph.Key of the shared core: the id carries the instance's
// graph namespace, so a SPECIFIES match finds the CodeRef row the rebuild
// wrote. Matching the unnamespaced id found nothing in a namespaced graph.
export function specCodeRefId(codegraphId: string, namespace = ""): number {
  return codeRefIdIn(namespace, codegraphId);
}

export async function linkSpecifies(
  client: GraphClient,
  memoryId: number,
  codegraphIds: Iterable<string>,
  namespace = "",
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
      { id: specCodeRefId(codegraphId, namespace) },
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
