#!/usr/bin/env python3
"""Recall and precision of the effect classifier against hand labels.

The labels in testdata/effects-recall/ were read from the source, not from the
classifier, so this is not a mirror of the rules. It runs specctl over a
checkout and compares the reported effects with the labels per file and kind.

    python3 scripts/effects-recall.py --worktree /path/to/atproto-market \\
        --labels testdata/effects-recall/atproto-market-master.yaml \\
        [--specctl bin/specctl] [-o json]

A label counts as found when the classifier reports the same kind within one
line of it. Reported effects in a labelled file that match no label are false
positives. Labels with no report are false negatives; a file may also carry
bulk gaps (a kind and a count) for sites the pack does not attempt, and those
count as false negatives without a line.
"""

import argparse
import json
import subprocess
import sys
from collections import defaultdict


def load_yaml(path):
    try:
        import yaml
    except ImportError:
        sys.exit("pyyaml is required: python3 -m pip install pyyaml")
    with open(path) as handle:
        return yaml.safe_load(handle)


def classifier_effects(specctl, worktree, repository):
    command = [specctl, "policy", "effects", "--worktree", worktree, "-o", "json"]
    if repository:
        command += ["--repo", repository]
    completed = subprocess.run(command, capture_output=True, text=True)
    if completed.returncode != 0:
        sys.exit(completed.stderr.strip() or "specctl policy effects failed")
    return json.loads(completed.stdout)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--worktree", required=True)
    parser.add_argument("--labels", required=True)
    parser.add_argument("--specctl", default="bin/specctl")
    parser.add_argument("--repository", default="")
    parser.add_argument("-o", "--output", default="text", choices=["text", "json"])
    args = parser.parse_args()

    labels = load_yaml(args.labels)
    reported = classifier_effects(args.specctl, args.worktree, args.repository)
    by_file = defaultdict(list)
    for effect in reported:
        by_file[effect["file"]].append(effect)

    stats = defaultdict(lambda: {"tp": 0, "fp": 0, "fn": 0})
    detail = []

    for entry in labels.get("files", []):
        path = entry["path"]
        found = by_file.get(path, [])
        used = set()
        for label in entry.get("sites", []):
            hit = None
            for index, effect in enumerate(found):
                if index in used:
                    continue
                if effect["kind"] == label["kind"] and abs(effect.get("line", 0) - label["line"]) <= 1:
                    hit = index
                    break
            if hit is None:
                stats[label["kind"]]["fn"] += 1
                detail.append({"file": path, "line": label["line"], "kind": label["kind"], "result": "missed"})
                continue
            used.add(hit)
            stats[label["kind"]]["tp"] += 1
        for index, effect in enumerate(found):
            if index in used:
                continue
            stats[effect["kind"]]["fp"] += 1
            detail.append({"file": path, "line": effect.get("line", 0), "kind": effect["kind"], "result": "false-positive"})
        for label in entry.get("missed", []):
            stats[label["kind"]]["fn"] += 1
            detail.append({"file": path, "line": label["line"], "kind": label["kind"], "result": "missed-indirect"})
        for gap in entry.get("gaps", []):
            stats[gap["kind"]]["fn"] += gap["count"]
            detail.append({"file": path, "line": 0, "kind": gap["kind"], "result": f"gap x{gap['count']}"})

    if args.output == "json":
        print(json.dumps({"stats": stats, "detail": detail}, indent=2, sort_keys=True))
        return 0

    print(f"{'kind':<16}{'tp':>4}{'fp':>4}{'fn':>4}{'precision':>11}{'recall':>9}")
    for kind in sorted(stats):
        row = stats[kind]
        precision = row["tp"] / (row["tp"] + row["fp"]) if row["tp"] + row["fp"] else 1.0
        recall = row["tp"] / (row["tp"] + row["fn"]) if row["tp"] + row["fn"] else 1.0
        print(f"{kind:<16}{row['tp']:>4}{row['fp']:>4}{row['fn']:>4}{precision:>11.3f}{recall:>9.3f}")
    total = {key: sum(row[key] for row in stats.values()) for key in ("tp", "fp", "fn")}
    precision = total["tp"] / (total["tp"] + total["fp"]) if total["tp"] + total["fp"] else 1.0
    recall = total["tp"] / (total["tp"] + total["fn"]) if total["tp"] + total["fn"] else 1.0
    print(f"{'total':<16}{total['tp']:>4}{total['fp']:>4}{total['fn']:>4}{precision:>11.3f}{recall:>9.3f}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
