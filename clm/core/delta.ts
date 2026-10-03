// The delta is the contract between the two directions, and Go computes it in
// abc/delta. This is the same algebra, validated against the same golden files
// (testdata/delta), so a host can show a model exactly what changed without
// guessing: `Diff(old, new)`, its inverse `Apply`, and the compact `Summary`.

import {
  canonicalInterfaces,
  canonicalObserved,
  canonicalSet,
  canonicalRequirements,
  canonicalSpec,
} from "./canonical.ts";
import {
  OP_ADDED,
  OP_CHANGED,
  OP_REMOVED,
  type Delta,
  type FieldDelta,
  type Interface,
  type InterfaceDelta,
  type ObservedDelta,
  type ObservedFacts,
  type ObservedInterface,
  type ObservedInterfaceDelta,
  type Requirement,
  type RequirementDelta,
  type StringSetDelta,
  type SystemContextSpec,
} from "./types.ts";

export function diff(oldSpec: SystemContextSpec, newSpec: SystemContextSpec): Delta {
  const out: Delta = {};
  if (oldSpec.intent !== newSpec.intent) out.intent = field(oldSpec.intent ?? "", newSpec.intent ?? "");
  if (oldSpec.upstream !== newSpec.upstream) out.upstream = field(oldSpec.upstream ?? "", newSpec.upstream ?? "");
  if (oldSpec.orchestrator !== newSpec.orchestrator) {
    out.orchestrator = field(oldSpec.orchestrator ?? "", newSpec.orchestrator ?? "");
  }
  assignSet(out, "overlay", diffSet(oldSpec.overlay ?? [], newSpec.overlay ?? []));
  assignSet(out, "dependsOn", diffSet(oldSpec.dependsOn ?? [], newSpec.dependsOn ?? []));
  assignSet(out, "introduces", diffSet(oldSpec.introduces ?? [], newSpec.introduces ?? []));
  assignSet(out, "codeRefs", diffSet(oldSpec.codeRefs ?? [], newSpec.codeRefs ?? []));
  const requirements = diffRequirements(oldSpec.requirements ?? [], newSpec.requirements ?? []);
  if (requirements.length) out.requirements = requirements;
  const interfaces = diffInterfaces(oldSpec.interfaces ?? [], newSpec.interfaces ?? []);
  if (interfaces.length) out.interfaces = interfaces;
  return out;
}

export function diffObserved(oldFacts: ObservedFacts, newFacts: ObservedFacts): Delta {
  const files = diffSet(oldFacts.files ?? [], newFacts.files ?? []);
  const interfaces = diffObservedInterfaces(oldFacts.interfaces ?? [], newFacts.interfaces ?? []);
  const fingerprint =
    oldFacts.fingerprint !== newFacts.fingerprint
      ? field(oldFacts.fingerprint ?? "", newFacts.fingerprint ?? "")
      : undefined;
  if (!files && !interfaces.length && !fingerprint) return {};
  const observed: ObservedDelta = {};
  if (files) observed.files = files;
  if (interfaces.length) observed.interfaces = interfaces;
  if (fingerprint) observed.fingerprint = fingerprint;
  return { observed };
}

export function apply(base: SystemContextSpec, change: Delta): SystemContextSpec {
  const out: SystemContextSpec = { ...base };
  if (change.intent) out.intent = change.intent.to;
  if (change.upstream) out.upstream = change.upstream.to;
  if (change.orchestrator) out.orchestrator = change.orchestrator.to;
  if (change.overlay) out.overlay = applySet(base.overlay ?? [], change.overlay);
  if (change.dependsOn) out.dependsOn = applySet(base.dependsOn ?? [], change.dependsOn);
  if (change.introduces) out.introduces = applySet(base.introduces ?? [], change.introduces);
  if (change.codeRefs) out.codeRefs = applySet(base.codeRefs ?? [], change.codeRefs);
  if (change.requirements?.length) out.requirements = applyRequirements(base.requirements ?? [], change.requirements);
  if (change.interfaces?.length) out.interfaces = applyInterfaces(base.interfaces ?? [], change.interfaces);
  return canonicalSpec(out);
}

export function applyObserved(base: ObservedFacts, change: Delta): ObservedFacts {
  if (!change.observed) return canonicalObserved(base);
  const out: ObservedFacts = { ...base };
  if (change.observed.files) out.files = applySet(base.files ?? [], change.observed.files);
  if (change.observed.interfaces?.length) {
    out.interfaces = applyObservedInterfaces(base.interfaces ?? [], change.observed.interfaces);
  }
  if (change.observed.fingerprint) out.fingerprint = change.observed.fingerprint.to;
  return canonicalObserved(out);
}

export function deltaEmpty(change: Delta): boolean {
  if (change.intent || change.upstream || change.orchestrator) return false;
  if (!emptySet(change.overlay) || !emptySet(change.dependsOn) || !emptySet(change.introduces) || !emptySet(change.codeRefs)) {
    return false;
  }
  if (change.requirements?.length || change.interfaces?.length) return false;
  if (change.observed) {
    if (!emptySet(change.observed.files) || change.observed.interfaces?.length || change.observed.fingerprint) return false;
  }
  return true;
}

export interface Counts {
  added: number;
  removed: number;
  changed: number;
}

export function count(change: Delta): Counts {
  const counts: Counts = { added: 0, removed: 0, changed: 0 };
  for (const entry of [change.intent, change.upstream, change.orchestrator]) if (entry) counts.changed++;
  for (const set of [change.overlay, change.dependsOn, change.introduces, change.codeRefs]) {
    if (!set) continue;
    counts.added += set.added?.length ?? 0;
    counts.removed += set.removed?.length ?? 0;
  }
  for (const entry of change.requirements ?? []) tally(counts, entry.op);
  for (const entry of change.interfaces ?? []) tally(counts, entry.op);
  if (change.observed) {
    counts.added += change.observed.files?.added?.length ?? 0;
    counts.removed += change.observed.files?.removed?.length ?? 0;
    for (const entry of change.observed.interfaces ?? []) tally(counts, entry.op);
    if (change.observed.fingerprint) counts.changed++;
  }
  return counts;
}

/** The compact form a reader sees: `+2 ~1 -1`, or `-` for no change at all. */
export function summary(change: Delta): string {
  const counts = count(change);
  const parts: string[] = [];
  if (counts.added) parts.push(`+${counts.added}`);
  if (counts.removed) parts.push(`-${counts.removed}`);
  if (counts.changed) parts.push(`~${counts.changed}`);
  return parts.length ? parts.join(" ") : "-";
}

function tally(counts: Counts, op: string): void {
  if (op === OP_ADDED) counts.added++;
  else if (op === OP_REMOVED) counts.removed++;
  else if (op === OP_CHANGED) counts.changed++;
}

function field(from: string, to: string): FieldDelta {
  return { from, to };
}

function emptySet(set: StringSetDelta | undefined): boolean {
  return !set || ((set.added?.length ?? 0) === 0 && (set.removed?.length ?? 0) === 0);
}

function assignSet<K extends "overlay" | "dependsOn" | "introduces" | "codeRefs">(
  out: Delta,
  key: K,
  value: StringSetDelta | undefined,
): void {
  if (value) out[key] = value;
}

function diffSet(oldValues: readonly string[], newValues: readonly string[]): StringSetDelta | undefined {
  const before = canonicalSet(oldValues);
  const after = canonicalSet(newValues);
  if (same(before, after)) return undefined;
  const have = new Set(before);
  const want = new Set(after);
  const out: StringSetDelta = {};
  const added = after.filter((value) => !have.has(value));
  const removed = before.filter((value) => !want.has(value));
  if (added.length) out.added = added;
  if (removed.length) out.removed = removed;
  return out;
}

function applySet(base: readonly string[], change: StringSetDelta): string[] {
  const values = new Set(base);
  for (const value of change.removed ?? []) values.delete(value);
  for (const value of change.added ?? []) values.add(value);
  return canonicalSet([...values]);
}

function diffRequirements(oldList: readonly Requirement[], newList: readonly Requirement[]): RequirementDelta[] {
  const before = index(oldList, (entry) => entry.id);
  const after = index(newList, (entry) => entry.id);
  const out: RequirementDelta[] = [];
  for (const id of unionKeys(before, after)) {
    const from = before.get(id);
    const to = after.get(id);
    if (!from) {
      out.push({ op: OP_ADDED, id, to: canonicalRequirements([to as Requirement])[0] });
    } else if (!to) {
      out.push({ op: OP_REMOVED, id, from: canonicalRequirements([from])[0] });
    } else {
      const canonicalFrom = canonicalRequirements([from])[0] as Requirement;
      const canonicalTo = canonicalRequirements([to])[0] as Requirement;
      if (same(canonicalFrom, canonicalTo)) continue;
      out.push({
        op: OP_CHANGED,
        id,
        from: canonicalFrom,
        to: canonicalTo,
        fields: changedRequirementFields(canonicalFrom, canonicalTo),
      });
    }
  }
  return out;
}

function applyRequirements(base: readonly Requirement[], change: readonly RequirementDelta[]): Requirement[] {
  const entries = index(base, (entry) => entry.id);
  for (const entry of change) {
    if (entry.op === OP_REMOVED) entries.delete(entry.id);
    else if (entry.to) entries.set(entry.id, entry.to);
  }
  return canonicalRequirements([...entries.values()]);
}

function changedRequirementFields(from: Requirement, to: Requirement): string[] {
  const fields: string[] = [];
  if (from.level !== to.level) fields.push("level");
  if (from.text !== to.text) fields.push("text");
  if (!same(from.codeRefs ?? [], to.codeRefs ?? [])) fields.push("codeRefs");
  return fields.sort();
}

function diffInterfaces(oldList: readonly Interface[], newList: readonly Interface[]): InterfaceDelta[] {
  const before = index(oldList, (entry) => entry.name);
  const after = index(newList, (entry) => entry.name);
  const out: InterfaceDelta[] = [];
  for (const name of unionKeys(before, after)) {
    const from = before.get(name);
    const to = after.get(name);
    if (!from) out.push({ op: OP_ADDED, name, to: to as Interface });
    else if (!to) out.push({ op: OP_REMOVED, name, from });
    else if (!same(from, to)) {
      out.push({ op: OP_CHANGED, name, from, to, fields: changedInterfaceFields(from, to) });
    }
  }
  return out;
}

function applyInterfaces(base: readonly Interface[], change: readonly InterfaceDelta[]): Interface[] {
  const entries = index(base, (entry) => entry.name);
  for (const entry of change) {
    if (entry.op === OP_REMOVED) entries.delete(entry.name);
    else if (entry.to) entries.set(entry.name, entry.to);
  }
  return canonicalInterfaces([...entries.values()]);
}

function changedInterfaceFields(from: Interface, to: Interface): string[] {
  const fields: string[] = [];
  if (from.kind !== to.kind) fields.push("kind");
  if (from.signature !== to.signature) fields.push("signature");
  if (from.file !== to.file) fields.push("file");
  return fields.sort();
}

function diffObservedInterfaces(
  oldList: readonly ObservedInterface[],
  newList: readonly ObservedInterface[],
): ObservedInterfaceDelta[] {
  const before = index(oldList, (entry) => entry.name);
  const after = index(newList, (entry) => entry.name);
  const out: ObservedInterfaceDelta[] = [];
  for (const name of unionKeys(before, after)) {
    const from = before.get(name);
    const to = after.get(name);
    if (!from) out.push({ op: OP_ADDED, name, to: to as ObservedInterface });
    else if (!to) out.push({ op: OP_REMOVED, name, from });
    else if (!same(from, to)) {
      out.push({ op: OP_CHANGED, name, from, to, fields: changedObservedFields(from, to) });
    }
  }
  return out;
}

function applyObservedInterfaces(
  base: readonly ObservedInterface[],
  change: readonly ObservedInterfaceDelta[],
): ObservedInterface[] {
  const entries = index(base, (entry) => entry.name);
  for (const entry of change) {
    if (entry.op === OP_REMOVED) entries.delete(entry.name);
    else if (entry.to) entries.set(entry.name, entry.to);
  }
  return canonicalObserved({ interfaces: [...entries.values()] }).interfaces ?? [];
}

function changedObservedFields(from: ObservedInterface, to: ObservedInterface): string[] {
  const fields: string[] = [];
  if (from.kind !== to.kind) fields.push("kind");
  if (from.signature !== to.signature) fields.push("signature");
  if (from.file !== to.file) fields.push("file");
  if (from.line !== to.line) fields.push("line");
  if (from.codegraphId !== to.codegraphId) fields.push("codegraphId");
  return fields.sort();
}

function index<T>(values: readonly T[], keyOf: (value: T) => string): Map<string, T> {
  const out = new Map<string, T>();
  for (const value of values) out.set(keyOf(value), value);
  return out;
}

function unionKeys<T>(left: Map<string, T>, right: Map<string, T>): string[] {
  return [...new Set([...left.keys(), ...right.keys()])].sort();
}

// Go compares with reflect.DeepEqual, which has no opinion on key order, so the
// comparison here sorts keys first: two spellings of one entry are one entry.
function same(left: unknown, right: unknown): boolean {
  return stableKey(left) === stableKey(right);
}

function stableKey(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value) ?? "null";
  if (Array.isArray(value)) return `[${value.map(stableKey).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, entry]) => entry !== undefined)
    .sort(([left], [right]) => (left < right ? -1 : 1));
  return `{${entries.map(([key, entry]) => `${JSON.stringify(key)}:${stableKey(entry)}`).join(",")}}`;
}
