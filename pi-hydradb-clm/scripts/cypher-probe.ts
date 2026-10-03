import { writeFileSync } from "node:fs";
import { GraphClient } from "../src/graph.ts";
import { PROBE_CASES, type ProbeStatement } from "../src/conformance.ts";
import { resolveGraphTarget } from "../src/target.ts";

interface Outcome {
  id: string;
  group: string;
  portable: boolean;
  status: "pass" | "fail";
  error?: string;
}

function message(error: unknown): string {
  const text = error instanceof Error ? error.message : String(error);
  return text.replace(/\s+/g, " ").trim().slice(0, 200);
}

async function runStatements(
  client: GraphClient,
  statements: ProbeStatement[] | undefined,
): Promise<string | undefined> {
  for (const statement of statements ?? []) {
    try {
      await client.run(statement.cypher, statement.params ?? {});
    } catch (error) {
      return message(error);
    }
  }
  return undefined;
}

async function main(): Promise<void> {
  const target = resolveGraphTarget();
  const client = await GraphClient.connect({
    boltUrl: target.boltUrl,
    user: target.user,
    token: target.token,
    database: target.database,
  });

  const outcomes: Outcome[] = [];
  for (const probe of PROBE_CASES) {
    const setupError = await runStatements(client, probe.setup);
    if (setupError) {
      outcomes.push({
        id: probe.id,
        group: probe.group,
        portable: probe.portable,
        status: "fail",
        error: `setup: ${setupError}`,
      });
      continue;
    }
    try {
      await client.run(probe.cypher, probe.params ?? {});
      outcomes.push({
        id: probe.id,
        group: probe.group,
        portable: probe.portable,
        status: "pass",
      });
    } catch (error) {
      outcomes.push({
        id: probe.id,
        group: probe.group,
        portable: probe.portable,
        status: "fail",
        error: message(error),
      });
    }
  }

  await client.close();

  const name = target.backend;
  console.log(`backend: ${name}  ${target.boltUrl}${target.database ? ` db=${target.database}` : ""}\n`);
  let group = "";
  for (const outcome of outcomes) {
    if (outcome.group !== group) {
      group = outcome.group;
      console.log(`[${group}]`);
    }
    const mark = outcome.status === "pass" ? "PASS" : "FAIL";
    const portable = outcome.portable ? "core" : "    ";
    const detail = outcome.error ? `  ${outcome.error}` : "";
    console.log(`  ${mark} ${portable} ${outcome.id}${detail}`);
  }

  const passed = outcomes.filter((o) => o.status === "pass").length;
  const portableFailed = outcomes.filter((o) => o.portable && o.status === "fail");
  console.log(`\n${passed}/${outcomes.length} cases passed`);
  if (portableFailed.length > 0) {
    console.log(`core subset failures: ${portableFailed.map((o) => o.id).join(", ")}`);
  }

  const outPath = process.env.HYDRA_PROBE_OUT ?? `/tmp/cypher-probe-${name}.json`;
  writeFileSync(outPath, `${JSON.stringify({ backend: name, outcomes }, null, 2)}\n`);
  console.log(`report: ${outPath}`);
}

await main();
