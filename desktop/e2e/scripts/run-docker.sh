#!/usr/bin/env bash
set -euo pipefail

# E2E includes server builds, tmux-aware Hive services, and Playwright browser
# binaries. Keep all of that inside the pinned container: there is deliberately
# no host Playwright fallback.
cd "$(dirname "$0")/../../.."

if ! docker info >/dev/null 2>&1; then
  echo "error: Docker is required for desktop:e2e; host Playwright is intentionally unsupported" >&2
  exit 1
fi

# The per-run token is intentionally not a fixed truthy flag: the Docker CMD,
# web-server launcher, and action-smoke route all require this 256-bit marker.
# A direct host Playwright invocation therefore cannot start the e2e servers.
E2E_HARNESS_MARKER="$(openssl rand -hex 32)"
docker build --file desktop/e2e/Dockerfile --tag hive-desktop-e2e:local .

# Mount the results dirs out of the --rm container so failure traces and
# screenshots survive the run (CI uploads test-results as an artifact).
mkdir -p desktop/e2e/test-results desktop/e2e/screenshots
# --ipc=host: Chromium can exhaust Docker's default 64MB /dev/shm and crash
# mid-suite (playwright.dev/docs/docker); host IPC is the documented fix.
docker run --rm --init --network none --ipc=host \
  --env HIVE_DESKTOP_E2E_HARNESS="${E2E_HARNESS_MARKER}" \
  --volume "$(pwd)/desktop/e2e/test-results:/workspace/desktop/e2e/test-results" \
  --volume "$(pwd)/desktop/e2e/screenshots:/workspace/desktop/e2e/screenshots" \
  hive-desktop-e2e:local
