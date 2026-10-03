// The bridge from a remembered concept to the spec graph the controllers keep.
//
// The spec graph anchors a requirement to a `CodeRef` vertex whose id is the
// FNV-1a of `coderef:<codegraph id>` (the Go writer and every CLM host share
// that id scheme). So when `hydradb_remember` is given code references, the
// requirements those references answer to can be found and the concept tied to
// them, which is what makes `specctl graph neighbors <context>` show the why
// beside the what.
import { GraphClient } from "./graph.ts";
import { stableNodeId } from "./ids.ts";
import { EDGES, LABELS, SPEC_LABELS } from "./schema.ts";

/** The spec graph's CodeRef id for one CodeGraph id. */
export function specCodeRefId(codegraphId: string): number {
  return stableNodeId(`coderef:${codegraphId}`);
}

/**
 * Writes `(PiMemory)-[:SPECIFIES]->(SpecRequirement)` for every requirement the
 * given code references anchor to, and answers how many edges it wrote. An
 * empty spec graph writes nothing, so a pi host works with or without one.
 */
export async function linkSpecifies(
  client: GraphClient,
  memoryId: number,
  codegraphIds: Iterable<string>,
): Promise<number> {
  // The edge writer creates an edge, it does not merge one, so what is already
  // there is read first: remembering the same concept twice must not leave two
  // SPECIFIES edges behind.
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
