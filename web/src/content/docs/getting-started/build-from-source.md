---
title: Build from source
description: Clone the repository, install the toolchain with mise, and build or run the app yourself. macOS today; Linux builds in a container.
group: Getting started
order: 4
---

The [install script](/install) is the short path. This page is for building
the app yourself, from a clone. If you want to change the code rather than
just build it, the repository's `CONTRIBUTING.md` and `docs/development.md`
cover the quality gates, hooks, and layout; this page stops at a running app.

## Platforms

- **macOS** (Apple silicon and Intel) is the platform Hive ships on and the
  one a native build targets.
- **Linux** compiles, in a container, with the same task the release pipeline
  runs. There are no published Linux builds yet, so this is the way to get
  one.
- **Windows** is not supported.

## Prerequisites

- **Git**.
- **[mise](https://mise.jdx.dev/)**. It installs and pins everything else the
  Go side needs: Go, the Wails v3 CLI, sqlc, golangci-lint, lefthook, and the
  code generators. You do not install Go yourself.
- **Node.js 22** and npm, for the frontend. mise does not manage Node here.
- **tmux 3.2 or newer**, optional. The app runs without it; terminal mode and
  agent chats need it. See [Terminal mode](/docs/concepts/terminal-mode).
- **Docker**, only for the Linux build and the end-to-end suite.

## Set up the clone

```sh
git clone https://github.com/hay-kot/hive-desktop.git
cd hive-desktop
mise trust                      # once per clone, before mise reads mise.toml
mise install                    # toolchain, git hooks, and this clone's dev instance
cd desktop/frontend && npm ci && cd ../..
```

`mise install` also installs the git hooks and prepares an isolated
development instance for this clone, so two checkouts never share state.
`mise tasks` lists every task.

## Build

```sh
mise run build
```

The binary lands at `desktop/bin/hive-desktop`. On macOS that is the app
itself and runs from a terminal; the signed `.app` bundle and DMG that the
installer ships come from the release tooling, which needs Apple credentials.
To wrap a local binary in an ad-hoc-signed bundle:

```sh
cd desktop && wails3 package
```

That writes `desktop/bin/Hive.app`, signed with an ad-hoc identity, which is
enough to run it from Finder on the machine that built it.

### Linux

```sh
mise run build:linux            # this machine's architecture
ARCH=amd64 mise run build:linux # or arm64
```

Builds the image on first use and overwrites `desktop/bin/hive-desktop` with a
Linux binary. Docker has to be running.

## Run it in development

```sh
mise run dev
```

Starts Wails with hot reload against this clone's own development instance:
its own config directory, data directory, and log, generated into
`launch.env` by `mise install`. Your real Hive install is untouched. Two
things to know before you connect an account in it:

- By default a development run routes GitHub traffic through a local caching
  proxy so concurrent checkouts share one rate-limit budget. Start it with
  `mise run devserver` in another terminal, or opt out by putting
  `HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE=""` in a `overrides.env` file at
  the repository root.
- To replay first run without touching state, or work offline against
  fixtures, set a mock mode in that same file, for example
  `HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE=onboarding`. The modes are listed under
  `development` on the [settings reference](/docs/configuration/settings#development).

`mise run dev:fresh` recreates the instance from scratch, and `mise run
dev:reset` deletes it.

## Test

```sh
mise run test              # Go unit tests
mise run test:desktop      # plus the frontend's vitest suite
mise run check             # what the pre-push hook runs: generate, lint, test
```

`mise run ci` is the superset, including the end-to-end suite that builds a
container image and takes minutes. Nothing in GitHub CI runs that suite, so it
only runs when someone runs it locally.

## Where a source build keeps its files

A built app uses the same locations an installed one does, described on the
[settings reference](/docs/configuration/settings#where-the-files-live). Only
`mise run dev` redirects them, through `launch.env`, to keep a development
instance apart from your own.
