import type { Interface, ObservedFacts, ObservedInterface, Requirement, SystemContextSpec } from "./types.ts";

export function canonicalSet(values: readonly string[]): string[] {
  return [...new Set(values)].sort();
}

export function canonicalRequirements(requirements: readonly Requirement[]): Requirement[] {
  return [...requirements]
    .map((requirement) => ({ ...requirement, codeRefs: canonicalSet(requirement.codeRefs ?? []) }))
    .sort((left, right) => (left.id < right.id ? -1 : 1));
}

export function canonicalInterfaces(interfaces: readonly Interface[]): Interface[] {
  return [...interfaces].sort((left, right) => (left.name < right.name ? -1 : 1));
}

export function canonicalSpec(inSpec: SystemContextSpec): SystemContextSpec {
  const out: SystemContextSpec = { ...inSpec };
  out.requirements = canonicalRequirements(inSpec.requirements ?? []);
  out.interfaces = canonicalInterfaces(inSpec.interfaces ?? []);
  out.codeRefs = canonicalSet(inSpec.codeRefs ?? []);
  out.overlay = canonicalSet(inSpec.overlay ?? []);
  out.dependsOn = canonicalSet(inSpec.dependsOn ?? []);
  out.introduces = canonicalSet(inSpec.introduces ?? []);
  return out;
}

export function canonicalObservedInterfaces(interfaces: readonly ObservedInterface[]): ObservedInterface[] {
  return [...interfaces].sort((left, right) => (left.name < right.name ? -1 : 1));
}

export function canonicalObserved(observed: ObservedFacts): ObservedFacts {
  const out: ObservedFacts = { ...observed };
  out.files = canonicalSet(observed.files ?? []);
  out.interfaces = canonicalObservedInterfaces(observed.interfaces ?? []);
  return out;
}
