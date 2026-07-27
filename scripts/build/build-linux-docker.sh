#!/usr/bin/env bash
# build-linux-docker.sh — build the Linux desktop binary in a container.
#
# Usage:
#   ./scripts/build/build-linux-docker.sh [--arch=amd64|arm64] [--image-only]
#
# Exists so a non-Linux host (or a Linux host targeting the other architecture)
# can produce the same binary the release runner does. It runs the *same*
# `wails3 task linux:build`, in an image mirroring the ubuntu-24.04 runner, so a
# locally driven release cannot diverge from CI's.
#
# Output: desktop/bin/hive-desktop, owned by the invoking user.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

ARCH="" IMAGE_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --arch=*) ARCH="${arg#*=}" ;;
    --image-only) IMAGE_ONLY=1 ;;
    -*) echo "unknown flag: $arg" >&2; exit 2 ;;
    *) echo "unexpected argument: $arg" >&2; exit 2 ;;
  esac
done

HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  x86_64 | amd64) HOST_ARCH="amd64" ;;
  aarch64 | arm64) HOST_ARCH="arm64" ;;
esac
ARCH="${ARCH:-${ARCH_ENV:-$HOST_ARCH}}"
case "$ARCH" in
  amd64 | arm64) ;;
  *) echo "invalid --arch '$ARCH': expected amd64 or arm64" >&2; exit 2 ;;
esac

command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }
docker info >/dev/null 2>&1 || { echo "docker is installed but not running" >&2; exit 2; }

# Tagged per architecture on purpose: the image carries a native toolchain, so
# an arm64 image cannot serve --platform linux/amd64 — docker would go looking
# for an amd64 variant in a registry and fail.
IMAGE="${LINUX_BUILDER_IMAGE:-hive-linux-builder}:$ARCH"
# node_modules holds platform-native binaries (esbuild, rollup) and is kept in a
# per-arch named volume. That also shadows the host's macOS-native copy, which
# would otherwise be mounted in and fail to execute.
NODE_VOLUME="${LINUX_BUILDER_NODE_VOLUME:-hive-linux-builder-node-modules}-$ARCH"

emulated_note() {
  [[ "$ARCH" == "$HOST_ARCH" ]] && return 0
  echo "note: linux/$ARCH on $HOST_ARCH runs under emulation — expect this to be slow" >&2
}

if ! docker image inspect "$IMAGE" >/dev/null 2>&1 || [[ $IMAGE_ONLY = 1 ]]; then
  # Read the wails3 pin from mise.toml rather than repeating it, so the
  # container runs the same Taskfile logic as the release runner.
  wails_version="$(sed -n 's/.*cmd\/wails3" = "\(.*\)"/\1/p' mise.toml)"
  [[ -n "$wails_version" ]] || { echo "could not read the pinned wails3 version from mise.toml" >&2; exit 1; }
  emulated_note
  echo "==> building $IMAGE (wails3 $wails_version)"
  docker build --platform "linux/$ARCH" -t "$IMAGE" \
    -f desktop/build/docker/Dockerfile.linux \
    --build-arg "WAILS3_VERSION=$wails_version" \
    desktop/build/docker/
fi
[[ $IMAGE_ONLY = 0 ]] || exit 0

# The wails scaffold's frontend task runs `npm install`, which rewrites
# package-lock.json whenever the builder's npm differs from whichever npm last
# wrote it on the host (it drops the newer `libc` fields, for one). That is an
# artifact of the build environment, not a dependency change, and a build must
# not leave the tree dirty. Restore it — but only if it was clean to begin with,
# so a genuine in-progress lockfile edit is never discarded.
LOCKFILE="desktop/frontend/package-lock.json"
lock_was_clean=0
git diff --quiet -- "$LOCKFILE" 2>/dev/null && lock_was_clean=1

emulated_note
echo "==> building linux/$ARCH binary"
docker run --rm \
  --platform "linux/$ARCH" \
  -v "$PWD:/src" -w /src/desktop \
  -v "$(go env GOMODCACHE):/gomod" \
  -v "$NODE_VOLUME:/src/desktop/frontend/node_modules" \
  -e GOMODCACHE=/gomod -e GOFLAGS=-mod=mod \
  -e GOPROXY=off \
  -e GOPRIVATE="${GOPRIVATE:-}" \
  -e HIVE_DESKTOP_VERSION="${HIVE_DESKTOP_VERSION:-dev}" \
  -e HIVE_DESKTOP_COMMIT="${HIVE_DESKTOP_COMMIT:-$(git rev-parse HEAD)}" \
  -e HIVE_DESKTOP_DATE="${HIVE_DESKTOP_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" \
  -e HIVE_DESKTOP_REPORT_TOKEN="${HIVE_DESKTOP_REPORT_TOKEN:-}" \
  "$IMAGE" wails3 task linux:build

if [[ $lock_was_clean = 1 ]] && ! git diff --quiet -- "$LOCKFILE" 2>/dev/null; then
  echo "==> restoring $LOCKFILE (the builder's npm rewrote it)"
  git checkout -q -- "$LOCKFILE"
fi

echo "==> built desktop/bin/hive-desktop for linux/$ARCH"
