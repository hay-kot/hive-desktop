# Browser image version matches desktop/e2e/package-lock.json's @playwright/test.
# Index digests retain the upstream multi-architecture manifests.
FROM golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS go

FROM mcr.microsoft.com/playwright:v1.61.1-noble@sha256:5b8f294aff9041b7191c34a4bab3ac270157a28774d4b0660e9743297b697e48
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:${PATH} \
    CI=1

# The server-build action runtime uses git and tmux; bash, curl, and coreutils
# are used by the isolated e2e harness. The Playwright base image already has
# the matching browser system dependencies installed.
RUN apt-get update && apt-get install -y --no-install-recommends \
      git tmux bash curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY desktop/frontend/package.json desktop/frontend/package-lock.json desktop/frontend/.npmrc ./desktop/frontend/
RUN cd desktop/frontend && npm ci
COPY desktop/e2e/package.json desktop/e2e/package-lock.json ./desktop/e2e/
RUN cd desktop/e2e && npm ci
COPY . .

# Build the frontend and the headless server at image-build time rather than
# inside Playwright's webServer launcher: compile failures surface here with
# clean attribution instead of as a mysterious "webServer was not able to
# start", and the webServer timeout then covers server startup alone.
RUN cd desktop/frontend && npm run build
RUN CGO_ENABLED=0 go build -tags server -o desktop/bin/hive-desktop-server ./desktop

# Entrypoint validates the per-run harness marker minted by run-docker.sh,
# then execs its arguments from desktop/e2e. The default CMD is the full
# Playwright suite; run-docker.sh appends its own arguments after the image
# name, which replace the CMD. Docker-entrypoint convention: a first argument
# that is a flag is treated as Playwright options, so
# `run-docker.sh --grep foo --repeat-each 5` runs
# `npx playwright test --grep foo --repeat-each 5`.
COPY <<'ENTRYPOINT_EOF' /usr/local/bin/e2e-entrypoint
#!/usr/bin/env bash
set -euo pipefail
if [[ ! "${HIVE_DESKTOP_E2E_HARNESS:-}" =~ ^[[:xdigit:]]{64}$ ]]; then
  echo 'error: Docker e2e harness marker missing' >&2
  exit 1
fi
cd /workspace/desktop/e2e
if [[ $# -eq 0 || $1 == -* ]]; then
  set -- npx playwright test "$@"
fi
exec "$@"
ENTRYPOINT_EOF
RUN chmod +x /usr/local/bin/e2e-entrypoint
ENTRYPOINT ["/usr/local/bin/e2e-entrypoint"]
CMD ["npx", "playwright", "test"]
