export const LABELS = {
  session: "PiSession",
  memory: "PiMemory",
  file: "PiFile",
  turn: "PiTurn",
  codeRef: "PiCodeRef",
} as const;

export const EDGES = {
  remembered: "REMEMBERED",
  touched: "TOUCHED",
  occurred: "OCCURRED",
  references: "REFERENCES",
  specifies: "SPECIFIES",
} as const;

export const SPEC_LABELS = {
  requirement: "SpecRequirement",
  codeRef: "CodeRef",
} as const;

export const SESSION_PROPS = ["key", "started", "cwd", "revision"] as const;
export const MEMORY_PROPS = ["session", "kind", "title", "body", "created"] as const;
export const FILE_PROPS = ["session", "path", "touches"] as const;
export const TURN_PROPS = ["session", "turnIndex", "summary", "created"] as const;
export const CODE_REF_PROPS = [
  "session",
  "codegraph_id",
  "kind",
  "name",
  "file_path",
] as const;
