export type Level = "MUST" | "SHOULD" | "MAY";

export const LEVELS: readonly Level[] = ["MUST", "SHOULD", "MAY"];

export interface Requirement {
  id: string;
  level: Level;
  text: string;
  codeRefs?: string[];
}

export interface Interface {
  name: string;
  kind?: string;
  signature?: string;
  file?: string;
}

export interface Interaction {
  id: string;
  peer: string;
  initiator: "self" | "peer";
  channel?: string;
  carries?: string[];
  purpose?: string;
  level?: Level;
  forbidden?: boolean;
}

export interface ObservedInterface {
  name: string;
  kind?: string;
  signature?: string;
  file?: string;
  line?: number;
  codegraphId?: string;
}

export interface ObservedFacts {
  files?: string[];
  interfaces?: ObservedInterface[];
  fingerprint?: string;
}

export interface SystemContextSpec {
  repository?: string;
  upstream?: string;
  overlay?: string[];
  orchestrator?: string;
  dependsOn?: string[];
  introduces?: string[];
  intent?: string;
  requirements?: Requirement[];
  interfaces?: Interface[];
  interactions?: Interaction[];
  codeRefs?: string[];
}

export interface SystemContextStatus {
  observedCommit?: string;
  observed?: ObservedFacts;
  syncedCommit?: string;
  syncedFingerprint?: string;
  syncedObserved?: ObservedFacts;
  realizedSpecHash?: string;
  conditions?: Condition[];
}

export interface Condition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
}

export interface SystemContext {
  apiVersion?: string;
  kind?: string;
  metadata: { name: string; namespace?: string; generation?: number; annotations?: Record<string, string> };
  spec: SystemContextSpec;
  status?: SystemContextStatus;
}

export interface ProgressRecord {
  turn?: number;
  tool?: string;
  files?: string[];
  note?: string;
  at?: string;
}

export interface SpecChange {
  apiVersion?: string;
  kind?: string;
  metadata: { name: string; namespace?: string };
  spec: {
    systemContext: string;
    direction: "SpecToCode" | "CodeToSpec";
    fromSpecHash?: string;
    toSpecHash?: string;
    fromCommit?: string;
    toCommit?: string;
    delta?: Delta;
  };
  status?: {
    phase?: string;
    branch?: string;
    commit?: string;
    verifyExitCode?: number;
    filesTouched?: string[];
    agentLog?: string;
    message?: string;
    progress?: ProgressRecord[];
  };
}

export const OP_ADDED = "added";
export const OP_REMOVED = "removed";
export const OP_CHANGED = "changed";

export type Op = typeof OP_ADDED | typeof OP_REMOVED | typeof OP_CHANGED;

export interface FieldDelta {
  from: string;
  to: string;
}

export interface StringSetDelta {
  added?: string[];
  removed?: string[];
}

export interface RequirementDelta {
  op: Op;
  id: string;
  from?: Requirement;
  to?: Requirement;
  fields?: string[];
}

export interface InterfaceDelta {
  op: Op;
  name: string;
  from?: Interface;
  to?: Interface;
  fields?: string[];
}

export interface InteractionDelta {
  op: Op;
  id: string;
  from?: Interaction;
  to?: Interaction;
  fields?: string[];
}

export interface ObservedInterfaceDelta {
  op: Op;
  name: string;
  from?: ObservedInterface;
  to?: ObservedInterface;
  fields?: string[];
}

export interface ObservedDelta {
  files?: StringSetDelta;
  interfaces?: ObservedInterfaceDelta[];
  fingerprint?: FieldDelta;
}

export interface Delta {
  intent?: FieldDelta;
  upstream?: FieldDelta;
  orchestrator?: FieldDelta;
  overlay?: StringSetDelta;
  dependsOn?: StringSetDelta;
  introduces?: StringSetDelta;
  codeRefs?: StringSetDelta;
  requirements?: RequirementDelta[];
  interfaces?: InterfaceDelta[];
  interactions?: InteractionDelta[];
  observed?: ObservedDelta;
}
