#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-ghcr.io/hydra-db/hydradb}"
TAG="${TAG:-0.2.0}"
PLATFORM="${PLATFORM:-linux/amd64}"
PREFIX="${PREFIX:-/usr/local}"
FORCE="${FORCE:-0}"

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="$ROOT/bin"
CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"
LAYOUT_DIR="${LAYOUT_DIR:-$CACHE_HOME/hydradb-binaries-$TAG}"

NODE_SRC="graph-node"
INDEXER_SRC="graph-indexer"
GRAPHBLAS_REAL="libgraphblas.so.7.4.0"
GRAPHBLAS_SONAME="libgraphblas.so.7"
GRAPHBLAS_SRC="lib/$GRAPHBLAS_REAL"

TARGETS=("$NODE_SRC" "$INDEXER_SRC" "$GRAPHBLAS_SRC")

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

for tool in go jq tar install; do
  require "$tool"
done

if [ "$(id -u)" -eq 0 ]; then
  SUDO=()
else
  require sudo
  SUDO=(sudo)
fi

LDCONFIG="$(command -v ldconfig || printf '/sbin/ldconfig')"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

STAGE_DIR="$WORK_DIR/stage"
mkdir -p "$STAGE_DIR"

if [ "$FORCE" = "1" ] || [ ! -f "$LAYOUT_DIR/index.json" ]; then
  (cd "$ROOT" && go build -o "$WORK_DIR/hydradb-bins" ./cmd/hydradb-bins)
  "$WORK_DIR/hydradb-bins" \
    -image "$IMAGE" \
    -tag "$TAG" \
    -platform "$PLATFORM" \
    -out "$LAYOUT_DIR"
fi

MANIFEST="$LAYOUT_DIR/blobs/sha256/$(jq -r '.manifests[0].digest' "$LAYOUT_DIR/index.json" | cut -d: -f2)"
[ -f "$MANIFEST" ] || die "manifest not found in layout: $LAYOUT_DIR"

while read -r digest; do
  layer="$LAYOUT_DIR/blobs/sha256/${digest#sha256:}"
  tar -xzf "$layer" -C "$STAGE_DIR" --wildcards "${TARGETS[@]}" 2>/dev/null || true
done < <(jq -r '.layers[].digest' "$MANIFEST")

for target in "${TARGETS[@]}"; do
  [ -f "$STAGE_DIR/$target" ] || die "payload missing: $target"
done

mkdir -p "$BIN_DIR"

install -m755 "$STAGE_DIR/$NODE_SRC" "$BIN_DIR/graph-node"
install -m755 "$STAGE_DIR/$INDEXER_SRC" "$BIN_DIR/graph-indexer"

"${SUDO[@]}" install -d "$PREFIX/lib"
"${SUDO[@]}" install -m644 "$STAGE_DIR/$GRAPHBLAS_SRC" "$PREFIX/lib/$GRAPHBLAS_REAL"
"${SUDO[@]}" ln -sf "$GRAPHBLAS_REAL" "$PREFIX/lib/$GRAPHBLAS_SONAME"

if [ "$PREFIX" = "/usr/local" ]; then
  printf '%s\n' "$PREFIX/lib" | "${SUDO[@]}" tee /etc/ld.so.conf.d/usr-local.conf >/dev/null
fi

"${SUDO[@]}" "$LDCONFIG"

"$LDCONFIG" -p | grep -q "$GRAPHBLAS_SONAME" || die "loader still cannot resolve $GRAPHBLAS_SONAME"

printf 'layout:  %s\n' "$LAYOUT_DIR"
printf 'binary:  %s\n' "$BIN_DIR/graph-node"
printf 'binary:  %s\n' "$BIN_DIR/graph-indexer"
printf 'library: %s/lib/%s\n' "$PREFIX" "$GRAPHBLAS_SONAME"

"$BIN_DIR/graph-node" --version
"$BIN_DIR/graph-indexer" --version
