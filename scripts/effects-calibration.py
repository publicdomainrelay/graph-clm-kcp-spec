#!/usr/bin/env python3
"""Measure the effect classifier against a repository.

The truth is a set of grep rules per effect kind (the constructs plan 0009 G1
names). The classifier output comes from `specctl policy effects -o json`.
Truth rules are language scoped and `not_with` removes a match already claimed
by a more specific kind at the same site. `skip` lists files the codegraph
indexer does not see, so a truth match there is not a classifier miss.

The script prints true positives, misses and false positives per effect kind
and exits non-zero when a miss or a false positive remains.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

DENO_RULES = [
    {"kind": "ssh.connect", "pattern": r'new\s+Deno\.Command\(\s*"ssh"'},
    {"kind": "container.exec", "pattern": r'new\s+Deno\.Command\(\s*"(?:docker|podman|nerdctl|container|virsh|lxc|incus)"'},
    {"kind": "proc.exec", "pattern": r'new\s+Deno\.Command\s*\(',
     "not_with": ["ssh.connect", "container.exec"]},
    {"kind": "proc.exec", "pattern": r'(?<![\w.])(?:spawn|execFile|execFileSync|execSync)\s*\('},
    {"kind": "http.request", "pattern": r'(?<![\w.])fetch\s*\('},
    {"kind": "http.request", "pattern": r'\.callService\s*\('},
    {"kind": "event.emit", "pattern": r'\.(?:createRepoRecord|createSignedRepoRecord|putRecord)\s*\('},
    {"kind": "event.receive", "pattern": r'\.getRecord\s*\('},
    {"kind": "net.listen", "pattern": r'Deno\.(?:serve|listen)\s*\('},
    {"kind": "net.dial", "pattern": r'new\s+(?:WebSocket|WebSocketStream)\s*\(|Deno\.connect\s*\('},
    {"kind": "http.handle", "pattern": r'\.(?:get|post|put|patch|delete|options|head|all|route)\(\s*["\'`]/'},
]

GO_RULES = [
    {"kind": "ssh.connect", "pattern": r'\bexec\.Command(?:Context)?\(\s*"ssh"'},
    {"kind": "ssh.connect", "pattern": r'\bssh\.Dial\s*\('},
    {"kind": "container.exec", "pattern": r'\bexec\.Command(?:Context)?\(\s*"(?:docker|podman|nerdctl|container|virsh|lxc|incus)"'},
    {"kind": "proc.exec", "pattern": r'\bexec\.Command(?:Context)?\s*\(',
     "not_with": ["ssh.connect", "container.exec"]},
    {"kind": "http.handle", "pattern": r'\b(?:mux|http)\.(?:HandleFunc|Handle)\s*\('},
    {"kind": "http.request", "pattern": r'\bhttp\.(?:Get|Post|Head|NewRequest|NewRequestWithContext)\s*\('},
    {"kind": "net.listen", "pattern": r'\bnet\.Listen\s*\('},
    {"kind": "net.dial", "pattern": r'\bnet\.Dial(?:Timeout)?\s*\('},
    {"kind": "file.write", "pattern": r'\bos\.(?:WriteFile|Create)\s*\('},
]

PROFILES = {
    "atproto-market": {
        "typescript": {"skip": [], "rules": DENO_RULES},
    },
    "hydradb": {
        "go": {"skip": [r'^impl/coverage/'], "rules": GO_RULES},
        "typescript": {"skip": [], "rules": DENO_RULES},
    },
}

SUFFIXES = {"typescript": {".ts", ".tsx"}, "go": {".go"}}
SKIP_DIRS = {".git", ".codegraph", "node_modules", "dist", "vendor"}


REGEX_KEYWORDS = {"return", "typeof", "case", "in", "of", "do", "else", "void",
                  "delete", "instanceof", "new", "yield", "await"}
REGEX_PREVIOUS = set("(,=:[!&|?{};+-*%~^<>")


def skip_regex(text: str, open_index: int) -> int:
    in_class = False
    index = open_index + 1
    while index < len(text):
        char = text[index]
        if char == "\\":
            index += 2
            continue
        if char == "\n":
            return open_index
        if char == "[":
            in_class = True
        elif char == "]":
            in_class = False
        elif char == "/" and not in_class:
            return index
        index += 1
    return open_index


def regex_can_start(text: str, mask: list[bool], index: int) -> bool:
    previous = index - 1
    while previous >= 0 and text[previous] in " \t\r\n":
        previous -= 1
    if previous < 0:
        return True
    if not mask[previous]:
        return False
    if text[previous] in REGEX_PREVIOUS:
        return True
    word = ""
    while previous >= 0 and (text[previous].isalnum() or text[previous] in "_$"):
        word = text[previous] + word
        previous -= 1
    return word in REGEX_KEYWORDS


def code_mask(text: str) -> list[bool]:
    mask = [False] * len(text)
    index = 0
    length = len(text)
    while index < length:
        char = text[index]
        if char in "\"'`":
            index += 1
            while index < length:
                if text[index] == "\\":
                    index += 2
                    continue
                if text[index] == char:
                    index += 1
                    break
                index += 1
            continue
        if char == "/" and index + 1 < length and text[index + 1] not in "/*" and regex_can_start(text, mask, index):
            close = skip_regex(text, index)
            if close > index:
                index = close + 1
                continue
        if char == "/" and index + 1 < length and text[index + 1] == "/":
            while index < length and text[index] != "\n":
                index += 1
            continue
        if char == "/" and index + 1 < length and text[index + 1] == "*":
            index += 2
            while index < length and not (text[index] == "*" and index + 1 < length and text[index + 1] == "/"):
                index += 1
            index += 2
            continue
        mask[index] = True
        index += 1
    return mask


def sources(worktree: Path, language: str) -> list[Path]:
    out = []
    for path in sorted(worktree.rglob("*")):
        if not path.is_file() or path.suffix not in SUFFIXES[language]:
            continue
        if SKIP_DIRS & set(path.relative_to(worktree).parts):
            continue
        out.append(path)
    return out


def truth(worktree: Path, profile: dict) -> set[tuple[str, str, int]]:
    found: set[tuple[str, str, int]] = set()
    for language, rules in profile.items():
        skips = [re.compile(pattern) for pattern in rules["skip"]]
        for path in sources(worktree, language):
            relative = path.relative_to(worktree).as_posix()
            if any(skip.search(relative) for skip in skips):
                continue
            text = path.read_text(errors="replace")
            mask = code_mask(text)
            claimed: dict[tuple[str, int], set[str]] = {}
            for rule in rules["rules"]:
                regex = re.compile(rule["pattern"])
                for match in regex.finditer(text):
                    if not mask[match.start()]:
                        continue
                    line = text.count("\n", 0, match.start()) + 1
                    site = (relative, line)
                    if any(kind in claimed.get(site, set()) for kind in rule.get("not_with", [])):
                        continue
                    claimed.setdefault(site, set()).add(rule["kind"])
                    found.add((rule["kind"], relative, line))
    return found


def classified(specctl: list[str], worktree: Path, classifiers: str | None) -> list[dict]:
    command = specctl + ["policy", "effects", "--worktree", str(worktree), "-o", "json"]
    if classifiers:
        command += ["--classifiers", classifiers]
    output = subprocess.run(command, capture_output=True, text=True)
    if output.returncode != 0:
        sys.stderr.write(output.stderr)
        raise SystemExit(f"specctl policy effects failed with {output.returncode}")
    return json.loads(output.stdout)


def profile_kinds(profile: dict, language: str) -> set[str]:
    return {rule["kind"] for rule in profile[language]["rules"]}


def language_of(path: str) -> str | None:
    suffix = Path(path).suffix
    for language, suffixes in SUFFIXES.items():
        if suffix in suffixes:
            return language
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True, choices=sorted(PROFILES))
    parser.add_argument("--worktree", required=True, type=Path)
    parser.add_argument("--specctl", default=None)
    parser.add_argument("--classifiers", default=None)
    parser.add_argument("--list", action="store_true", help="list every missed and extra site")
    args = parser.parse_args()

    profile = PROFILES[args.repo]
    specctl = args.specctl.split() if args.specctl else ["go", "run", "./cmd/specctl"]
    expected = truth(args.worktree, profile)

    actual = set()
    for item in classified(specctl, args.worktree, args.classifiers):
        language = language_of(item["file"])
        if language is None or language not in profile:
            continue
        if item["kind"] not in profile_kinds(profile, language):
            continue
        actual.add((item["kind"], item["file"], item["line"]))

    kinds = sorted({kind for kind, _, _ in expected} | {kind for kind, _, _ in actual})
    rows = []
    failures = 0
    for kind in kinds:
        want = {item for item in expected if item[0] == kind}
        got = {item for item in actual if item[0] == kind}
        missed = sorted(want - got)
        extra = sorted(got - want)
        rows.append((kind, len(want), len(want & got), len(missed), len(extra)))
        failures += len(missed) + len(extra)
        if args.list:
            for item in missed:
                print(f"  MISSED {item[0]} {item[1]}:{item[2]}")
            for item in extra:
                print(f"  EXTRA  {item[0]} {item[1]}:{item[2]}")

    width = max(len(kind) for kind in kinds) if kinds else 8
    print(f"{'kind'.ljust(width)}  truth  found  missed  extra")
    for kind, want, found, missed, extra in rows:
        print(f"{kind.ljust(width)}  {want:5d}  {found:5d}  {missed:6d}  {extra:5d}")
    print(f"{'total'.ljust(width)}  {sum(row[1] for row in rows):5d}")
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
