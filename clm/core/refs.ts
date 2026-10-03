// Reference extraction: any backticked token in the model's prose is a claim
// about the code, and a CodeGraph id spelled outright is a claim too. Both are
// collected here so a host can resolve them against the index and report the
// ones that do not answer, instead of silently keeping a guess.

const CODEGRAPH_KINDS =
  "file|function|method|class|struct|interface|constant|type_alias|variable|import";

const CODEGRAPH_ID = new RegExp(`^(?:${CODEGRAPH_KINDS}):[A-Za-z0-9_./\\\\-]+$`);

const REFERENCE_TOKEN = /^[A-Za-z0-9_][A-Za-z0-9_./\\:#-]*$/;

export function isCodegraphId(value: string): boolean {
  return CODEGRAPH_ID.test(value.trim());
}

export function isReferenceCandidate(token: string): boolean {
  const trimmed = token.trim();
  if (trimmed.length === 0 || trimmed.length > 200) return false;
  if (!REFERENCE_TOKEN.test(trimmed)) return false;
  if (trimmed.startsWith("/")) return false;
  return true;
}

export function extractReferences(text: string): string[] {
  const found: string[] = [];
  const seen = new Set<string>();
  const push = (value: string) => {
    const trimmed = value.trim();
    if (!isReferenceCandidate(trimmed) || seen.has(trimmed)) return;
    seen.add(trimmed);
    found.push(trimmed);
  };
  for (const match of text.matchAll(/`([^`\n]+)`/g)) push(match[1] ?? "");
  for (const match of text.matchAll(new RegExp(CODEGRAPH_ID.source.slice(1, -1), "g"))) push(match[0]);
  return found;
}
