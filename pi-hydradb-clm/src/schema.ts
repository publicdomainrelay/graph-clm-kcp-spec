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
  // A concept the model remembered to the requirement it speaks about. The
  // vocabulary is the spec graph's (abc/graph in the Go repository), so a
  // concept joins the spec instead of living beside it.
  specifies: "SPECIFIES",
} as const;

// The spec graph the controllers keep. Its labels are the Go writer's managed
// labels, and its CodeRef vertices are keyed by the same FNV-1a id, so a
// remembered concept can be tied to the requirement its code answers to.
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
