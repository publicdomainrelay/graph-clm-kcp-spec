import { readFileSync, writeFileSync } from "node:fs";

interface Outcome {
  id: string;
  group: string;
  portable: boolean;
  status: "pass" | "fail";
  error?: string;
}

interface Report {
  backend: string;
  outcomes: Outcome[];
}

function load(path: string): Report {
  return JSON.parse(readFileSync(path, "utf8")) as Report;
}

const [, , leftPath, rightPath, outPath] = process.argv;
if (!leftPath || !rightPath || !outPath) {
  throw new Error("usage: cypher-diff <left.json> <right.json> <out.md>");
}

const left = load(leftPath);
const right = load(rightPath);
const rightById = new Map(right.outcomes.map((outcome) => [outcome.id, outcome]));

const mark = (outcome: Outcome | undefined) =>
  outcome === undefined ? "?" : outcome.status === "pass" ? "yes" : "no";

const lines: string[] = [];
lines.push(`# Cypher conformance: ${left.backend} vs ${right.backend}`);
lines.push("");
lines.push(
  `Each row is one query from \`src/conformance.ts\`, run over Bolt against both engines. ` +
    `\`core\` marks the subset \`src/cypher.ts\` emits, which the pi extension depends on.`,
);
lines.push("");
lines.push(`| case | group | core | ${left.backend} | ${right.backend} | failing engine says |`);
lines.push("| --- | --- | --- | --- | --- | --- |");

for (const outcome of left.outcomes) {
  const other = rightById.get(outcome.id);
  const detail = (outcome.status === "fail" ? outcome.error : other?.error) ?? "";
  lines.push(
    `| \`${outcome.id}\` | ${outcome.group} | ${outcome.portable ? "core" : ""} | ` +
      `${mark(outcome)} | ${mark(other)} | ${detail.replace(/\|/g, "\\|")} |`,
  );
}

const count = (report: Report) => report.outcomes.filter((o) => o.status === "pass").length;
const core = (report: Report) => report.outcomes.filter((o) => o.portable);
lines.push("");
lines.push(
  `**Totals:** ${left.backend} ${count(left)}/${left.outcomes.length}, ` +
    `${right.backend} ${count(right)}/${right.outcomes.length}. ` +
    `Core subset: ${core(left).filter((o) => o.status === "pass").length}/${core(left).length} ` +
    `and ${core(right).filter((o) => o.status === "pass").length}/${core(right).length}.`,
);
lines.push("");
lines.push("## Divergences");
lines.push("");
for (const outcome of left.outcomes) {
  const other = rightById.get(outcome.id);
  if (!other || other.status === outcome.status) continue;
  const winner = outcome.status === "pass" ? left.backend : right.backend;
  const loser = outcome.status === "pass" ? right.backend : left.backend;
  const error = outcome.status === "fail" ? outcome.error : other.error;
  lines.push(`- \`${outcome.id}\`: ${winner} accepts, ${loser} rejects — \`${error}\``);
}
lines.push("");

writeFileSync(outPath, `${lines.join("\n")}\n`);
console.log(`wrote ${outPath}`);
