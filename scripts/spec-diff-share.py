#!/usr/bin/env python3
"""Measure what a feature architecture branch's diff is made of.

Plan 0003's measure: the share of the diff that is `specs/` and `CHANGES.md`,
the part a person reviews, against everything the branch writes. The diff runs
from the commit before the harness's first spec edit - the commit whose message
names a `origin=clm` object, which is where the feature begins - to the branch
tip.

Usage: spec-diff-share.py <repo> <tip>

Prints a bucket table and the share twice: with every file counted, and with
the files .gitattributes marks linguist-generated dropped.
"""

import subprocess
import sys

GENERATED = (
    "arch.yaml",
    "repository.yaml",
    "changes/",
    "context/",
    "graph/",
    "status/",
)

REVIEWABLE = ("specs/", "CHANGES.md")


def git(repo, *args):
    return subprocess.run(
        ["git", "-C", repo, *args], capture_output=True, text=True, check=True
    ).stdout


def first_feature_commit(repo, tip):
    for commit in git(repo, "log", "--reverse", "--format=%H", tip).split():
        message = git(repo, "log", "-1", "--format=%B", commit)
        if "origin=clm" in message:
            return commit
    return None


def bucket(path):
    for prefix in GENERATED:
        if path == prefix or path.startswith(prefix):
            return prefix
    for prefix in REVIEWABLE:
        if path == prefix or path.startswith(prefix):
            return prefix
    return "other"


def numstat(repo, base, tip):
    counts = {}
    for line in git(repo, "diff", "--numstat", base, tip).splitlines():
        added, removed, path = line.split("\t", 2)
        if added == "-":
            continue
        counts[path] = counts.get(path, 0) + int(added) + int(removed)
    return counts


def main():
    if len(sys.argv) != 3:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    repo, tip = sys.argv[1:3]
    start = first_feature_commit(repo, tip)
    if start is None:
        print("no commit on this branch carries a clm-origin spec edit", file=sys.stderr)
        return 1
    before = git(repo, "rev-parse", f"{start}^").strip()
    print(f"first feature commit: {start[:8]}, measuring {before[:8]}..{tip[:8]}")

    counts = numstat(repo, before, tip)
    buckets = {}
    for path, lines in counts.items():
        buckets[bucket(path)] = buckets.get(bucket(path), 0) + lines
    total = sum(buckets.values())
    for name in sorted(buckets, key=lambda key: -buckets[key]):
        print(f"  {name:16} {buckets[name]:5}  {buckets[name] * 100 // total:3}%")
    print(f"  {'total':16} {total:5}")

    reviewable = sum(buckets.get(name, 0) for name in REVIEWABLE)
    generated = sum(buckets.get(name, 0) for name in GENERATED)
    other = total - reviewable - generated
    print(f"share (every file counted):          {reviewable * 100 / total:.1f}%"
          f"  ({reviewable} of {total})")
    if reviewable + other > 0:
        print(f"share (linguist-generated dropped):  {reviewable * 100 / (reviewable + other):.1f}%"
              f"  ({reviewable} of {reviewable + other}; {generated} generated lines dropped)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
