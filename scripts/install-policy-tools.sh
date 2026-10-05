#!/usr/bin/env bash
# Install the pinned policy tools into bin/: opa (Open Policy Agent) and gator
# (Gatekeeper's suite runner). Both are release binaries, both are checked by
# sha256, and a rerun with the same versions and the same hashes is a no-op.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${BIN:-$ROOT/bin}"

OPA_VERSION="${OPA_VERSION:-v1.21.0}"
GATOR_VERSION="${GATOR_VERSION:-v3.23.1}"

case "$(uname -s)" in
  Linux) os=linux ;;
  *)
    echo "install-policy-tools: only linux release binaries are pinned" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *)
    echo "install-policy-tools: only amd64 and arm64 are pinned" >&2
    exit 1
    ;;
esac

opa_sha=""
gator_archive_sha=""
gator_binary_sha=""
case "$arch" in
  amd64)
    opa_sha=5eef70644868bb04d0556bcc795ee42f2ab379e73f51d1bfa30f83e1305bc9b9
    gator_archive_sha=c268d7b809c9fe59a110ab0bf7c296ab00b0b6c894c209cd15e94194590291bb
    gator_binary_sha=fe5b69787ae65e2582c08ef734269b02b79365906a7eb8b2d637ec2344b64fb8
    ;;
  arm64)
    opa_sha=0ec34027c15b4d969c21d01ed570fe14fbebd508a08157043ab09f9a0dccbee6
    gator_archive_sha=2d4bd0e28e708c2893a66fdca652f1d775b23fc5a3c07571fd314ba06d19ff2b
    gator_binary_sha=e3d4dc204f72f3216b4f97681eb5b185cffd386d9db5f5be43b7af2cdd54f216
    ;;
esac

opa_url="https://github.com/open-policy-agent/opa/releases/download/${OPA_VERSION}/opa_${os}_${arch}_static"
gator_url="https://github.com/open-policy-agent/gatekeeper/releases/download/${GATOR_VERSION}/gator-${GATOR_VERSION}-${os}-${arch}.tar.gz"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir -p "$BIN"

sha_of() {
  sha256sum "$1" | awk '{print $1}'
}

verify() {
  local path="$1" want="$2" got
  got="$(sha_of "$path")"
  if [ "$got" != "$want" ]; then
    echo "install-policy-tools: sha256 mismatch for $path: want $want, got $got" >&2
    exit 1
  fi
}

install_opa() {
  local target="$BIN/opa"
  if [ -x "$target" ] && [ "$(sha_of "$target")" = "$opa_sha" ]; then
    echo "opa ${OPA_VERSION} already installed"
    return
  fi
  echo "downloading opa ${OPA_VERSION} (${os}/${arch})"
  curl -fsSL -o "$work/opa" "$opa_url"
  verify "$work/opa" "$opa_sha"
  install -m 0755 "$work/opa" "$target"
  echo "installed $target"
}

install_gator() {
  local target="$BIN/gator"
  if [ -x "$target" ] && [ "$(sha_of "$target")" = "$gator_binary_sha" ]; then
    echo "gator ${GATOR_VERSION} already installed"
    return
  fi
  echo "downloading gator ${GATOR_VERSION} (${os}/${arch})"
  curl -fsSL -o "$work/gator.tar.gz" "$gator_url"
  verify "$work/gator.tar.gz" "$gator_archive_sha"
  tar -xzf "$work/gator.tar.gz" -C "$work" gator
  install -m 0755 "$work/gator" "$target"
  echo "installed $target"
}

install_opa
install_gator

"$BIN/opa" version
"$BIN/gator" version
