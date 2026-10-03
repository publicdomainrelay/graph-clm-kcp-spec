export const LABELS = {
  session: "PiSession",
  memory: "PiMemory",
  file: "PiFile",
  turn: "PiTurn",
} as const;

export const EDGES = {
  remembered: "REMEMBERED",
  touched: "TOUCHED",
  occurred: "OCCURRED",
  mentions: "MENTIONS",
} as const;

export const SESSION_PROPS = ["key", "started", "cwd", "revision"] as const;
export const MEMORY_PROPS = ["session", "kind", "title", "body", "created"] as const;
export const FILE_PROPS = ["session", "path", "touches"] as const;
export const TURN_PROPS = ["session", "turnIndex", "summary", "created"] as const;
