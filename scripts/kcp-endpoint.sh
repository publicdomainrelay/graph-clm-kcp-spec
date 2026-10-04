#!/usr/bin/env bash
#
# Resolve ROOT, KUBECONFIG_PATH and the bound ports from <root>/endpoint.json.
#
# A script sources this and calls specd_resolve_endpoint "$REPO" right after it
# has set REPO; environment values set by the caller always win, and a root
# with no endpoint.json (a fixed-port cluster started by hand) falls back to
# <root>/admin.kubeconfig exactly as before.
#
# shellcheck shell=bash

specd_resolve_endpoint() {
  local repo=$1
  ROOT=${ROOT:-$repo/.kcp-specd}
  SPECCTL=${SPECCTL:-$repo/bin/specctl}

  if [ -f "$ROOT/endpoint.json" ] && [ -x "$SPECCTL" ]; then
    # shellcheck disable=SC2046
    eval "$("$SPECCTL" kcp endpoint --root "$ROOT" -o sh 2>/dev/null || true)"
    ROOT=${KCP_ROOT:-$ROOT}
    if [ -n "${KUBECONFIG:-}" ]; then
      KUBECONFIG_PATH=${KUBECONFIG_PATH:-$KUBECONFIG}
    fi
  fi
  KUBECONFIG_PATH=${KUBECONFIG_PATH:-$ROOT/admin.kubeconfig}
}
