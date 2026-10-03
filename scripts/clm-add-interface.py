#!/usr/bin/env python3
"""Add interfaces to the spec block of a rendered CLM model zone.

The example uses this to stand in for the model editing its context document:
the spec block is YAML, so the interfaces list is replaced whole rather than
appended into, and the indentation the renderer chose does not matter.
"""

import sys

FIXTURE_FILE = "calc/calc.go"


def main() -> int:
    path, *added = sys.argv[1:]
    if not path or not added:
        print("usage: clm-add-interface.py <model-zone> <name>...", file=sys.stderr)
        return 2
    text = open(path, encoding="utf-8").read()
    fence = "```yaml spec"
    open_at = text.index(fence) + len(fence)
    close_at = text.index("```", open_at)
    lines = text[open_at:close_at].split("\n")

    kept, names, index = [], [], 0
    while index < len(lines):
        line = lines[index]
        if line.strip() == "interfaces:":
            index += 1
            while index < len(lines) and (lines[index].startswith("- ") or lines[index].startswith("  ")):
                stripped = lines[index].strip()
                if stripped.startswith("- name: "):
                    names.append(stripped[len("- name: ") :])
                elif stripped.startswith("name: "):
                    names.append(stripped[len("name: ") :])
                index += 1
            continue
        kept.append(line)
        index += 1

    kept.append("interfaces:")
    for name in [*names, *(name for name in added if name not in names)]:
        kept.append("- kind: function")
        kept.append(f"  name: {name}")
        kept.append(f"  signature: func {name}(a, b int) int")
        kept.append(f"  file: {FIXTURE_FILE}")

    block = "\n".join(kept)
    open(path, "w", encoding="utf-8").write(text[:open_at] + block + text[close_at:])
    return 0


if __name__ == "__main__":
    sys.exit(main())
