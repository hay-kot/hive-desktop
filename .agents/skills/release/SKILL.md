---
name: release
description: Cut the next Hive Desktop dev, beta, or stable release. Use when asked to release or publish the desktop app, or when explicitly invoked with a release channel such as `/release dev`.
compatibility: Requires git, Go, mise, GitHub CLI authentication, and repository release secrets configured in GitHub Actions.
disable-model-invocation: true
---

# Release Hive Desktop

Cut a release through the local publisher by default. `mise` loads release
credentials automatically; never read `.env`, inspect secret values, or invoke
the release CLI's `publish` command outside `mise`. Use the tag-triggered GitHub
Actions publisher only when the user explicitly requests the CI workflow.

Never upload locally and then push the same tag: pushing the tag would trigger
CI to republish an immutable version and fail. In the default local flow, create
the release tag locally after publishing and do not push it.

## Arguments

Accept one of these channels:

- `dev`
- `beta`
- `stable`

An explicit version may follow the channel, for example `dev 0.2.0-dev.1`.
Reject missing or unknown channels instead of guessing.

## Procedure

1. Resolve the repository root with `git rev-parse --show-toplevel`, change to
   it, and run every command below from there. Read `docs/distribution.md`.
2. Run `git fetch origin main --tags --prune`.
3. Require all of the following:
   - the working tree is clean;
   - the current branch is `main`;
   - local `main` is identical to `origin/main` (neither ahead nor behind);
   - `gh auth status` succeeds.

   Stop and explain the mismatch if any check fails. Do not stash, reset, merge,
   pull, switch branches, or discard work automatically.
4. Select and validate the candidate with the Go release CLI. If no explicit
   version was supplied, run:

   ```bash
   go run ./cmd/release prepare <channel>
   ```

   Otherwise run:

   ```bash
   go run ./cmd/release prepare <channel> <version>
   ```

5. Use the candidate and manifest state printed by `prepare`. It reads all live
   stable, beta, and dev manifests (treating 404 as an empty channel), includes
   both manifests and `desktop-v*` tags when selecting the next version, checks
   the channel-specific format and dev → beta → stable advancement across every
   affected manifest, and rejects a tag
   that already exists locally or on `origin`. Stop if it reports any error.
   Never choose from repository tags alone: the R2 history predates this
   repository and may contain a newer version than any local tag.
6. Show the candidate version, channel, exact commit SHA and subject, and
   affected manifests (`dev`; `beta+dev`; or `stable+beta+dev`). Ask for an
   explicit confirmation before doing anything that publishes. Prefer the
   harness's structured confirmation UI when available; otherwise ask in plain
   text and wait. The confirmation must make clear that the local workflow will
   build, sign, notarize, and upload a public release. A typed alternate version
   is acceptable; revalidate it. Stop on cancellation.
7. After confirmation, run the same local gates used before pushes:

   ```bash
   mi check
   mi desktop:test:frontend
   ```

   Stop on the first failure. Verify the worktree is still clean and `HEAD`
   still equals `origin/main` afterward.
8. Publish through `mise`, which loads the credentials without exposing them:

   ```bash
   mise run release:desktop -- <version>
   ```

   Do not read `.env`, print credential environment variables, or call
   `go run ./cmd/release publish` directly. The publisher verifies every affected
   live manifest and downloads the public artifact to check its size and SHA-256
   before it succeeds. Stop on failure. Do not rerun with `--force`, overwrite
   artifacts, or invent a replacement version without explicit user approval.
9. After successful publishing and verification, create the lightweight release
   tag locally:

    ```bash
    git tag "desktop-v<version>" HEAD
    ```

    Do not push the tag: `.github/workflows/desktop-publish.yml` runs on the tag
    push and would attempt to republish the immutable release. Report the
    version, local tag, affected channel manifests, and artifact URL prefix
    (`https://dl.hivedesktop.com/desktop/releases/<version>/`).

## Explicit CI workflow

Only when the user explicitly asks to publish through GitHub Actions, replace
steps 8-9 with:

1. Create and push `desktop-v<version>` at `HEAD`. The workflow rechecks that
   the tagged commit belongs to `origin/main` and runs the same release gates
   before publishing.
2. Locate the triggered `Publish Desktop` run with `gh run list` and watch it
   with `gh run watch --exit-status`.
3. On success, report the version, tag, workflow URL, channel manifests, and
   artifact URL prefix. On failure, report the failed step and workflow URL; do
   not delete or move the tag, force a rerun, overwrite artifacts, or choose a
   replacement version without explicit user approval.

## Version selection rules

The Go release CLI uses the highest numeric base version found across both
`desktop-v*` tags and the live stable, beta, and dev manifests. Including the
manifests is essential because the R2 release history predates this repository;
for example, a `0.1.1-dev.1` tag cannot update clients already on
`0.1.8-dev.1`.

- another release in the same channel increments its prerelease number;
- beta promotes the current dev base to `-beta.1`;
- stable promotes the current prerelease base to the bare version;
- a dev release after beta/stable advances the patch and starts at `-dev.1`;
- a beta or stable release after stable advances the patch;
- when no release tags exist, the base is `0.1.0`.

The confirmation step is the opportunity to choose a minor or major bump
instead of the computed patch-oriented default.
