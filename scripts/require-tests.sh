#!/usr/bin/env bash
# require-tests.sh <run regexp> <package>...
#
# A plan item's acceptance step: run the named tests and fail when they fail or
# when none of them ran. "go test -run X" alone passes when X matches nothing,
# so an acceptance step written before its tests exist would pass on an empty
# match; this one does not.
set -euo pipefail
if [ "$#" -lt 2 ]; then
  echo "usage: $0 <run regexp> <package>..." >&2
  exit 2
fi
pattern=$1
shift
output=$(mktemp)
trap 'rm -f "$output"' EXIT
status=0
go test -count=1 -v -timeout "${REQUIRE_TESTS_TIMEOUT:-45m}" -run "$pattern" "$@" >"$output" 2>&1 || status=$?
grep -E '^(--- (PASS|FAIL|SKIP)|FAIL|ok)' "$output" || true
if [ "$status" -ne 0 ]; then
  tail -n 40 "$output"
  exit "$status"
fi
passed=$(grep -c '^--- PASS' "$output" || true)
if [ "$passed" -eq 0 ]; then
  echo "require-tests: no test matching $pattern ran in $*" >&2
  exit 1
fi
echo "require-tests: $passed test(s) passed"
