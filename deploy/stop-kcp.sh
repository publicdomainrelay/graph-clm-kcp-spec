#!/usr/bin/env bash
#
# Stop the local kcp and kine that deploy/start-kcp.sh started.
#
# Two ways to find them, because the pid files live inside the root directory
# and that directory can be removed while the cluster is still running - which
# left the cluster unstoppable by the one script meant to stop it:
#
#   1. the pid files start-kcp.sh writes;
#   2. failing that, any process whose cmdline names this root directory.
#
# Every candidate is checked against /proc before it is signalled, so a
# recycled pid, or an unrelated process, cannot be killed by accident.
# Idempotent: nothing to stop is reported, not an error.
#
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${ROOT:-$REPO/.kcp-specd}

pids_for_root() {
  local want=$1 p argv0 cl
  for p in $(pgrep -f "$ROOT" 2>/dev/null || true); do
    [ "$p" = "$$" ] && continue
    [ -r "/proc/$p/cmdline" ] || continue
    argv0=$(tr '\0' '\n' <"/proc/$p/cmdline" 2>/dev/null | head -1)
    [ "$(basename "$argv0")" = "$want" ] || continue
    cl=$(tr '\0' ' ' <"/proc/$p/cmdline" 2>/dev/null) || continue
    case "$cl" in
      *"$ROOT"*) echo "$p" ;;
    esac
  done
}

candidates() {
  local name=$1 pidfile=$ROOT/$2.pid pid
  if [ -f "$pidfile" ]; then
    pid=$(cat "$pidfile")
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      echo "$pid"
    fi
  fi
  pids_for_root "$name"
}

stop_one() {
  local name=$1 pidfile=$ROOT/$2.pid found=0 pid cmdline

  for pid in $(candidates "$name" "$2" | sort -u); do
    cmdline=$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null || true)
    case "$cmdline" in
      *"$ROOT"*) ;;
      *)
        echo "$name: pid $pid does not name $ROOT; leaving it alone" >&2
        continue
        ;;
    esac
    found=1
    echo "$name: stopping pid $pid"
    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 40); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.25
    done
    if kill -0 "$pid" 2>/dev/null; then
      echo "$name: still running after 10s, sending SIGKILL"
      kill -9 "$pid" 2>/dev/null || true
    fi
  done

  [ "$found" = "0" ] && echo "$name: nothing running for $ROOT"
  rm -f "$pidfile"
}

stop_one kcp kcp
stop_one kine kine

exit 0
