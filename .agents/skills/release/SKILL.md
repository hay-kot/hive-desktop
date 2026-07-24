---
name: release
description: Cut the next Hive Desktop dev, beta, or stable release. Use when asked to release or publish the desktop app, or when explicitly invoked with a release channel such as `/release dev`.
compatibility: Requires git, Go, mise, GitHub CLI authentication, and repository release secrets configured in GitHub Actions.
disable-model-invocation: true
---

# Release Hive Desktop

Cut a release through the tag-triggered CI publisher. Never upload locally and
then push the same tag: the tag would trigger CI to republish an immutable
version and fail.

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
4. If no explicit version was supplied, run:

   ```bash
   go run .agents/skills/release/scripts/next_version.go <channel>
   ```

5. Read all existing channel manifests from
   `https://dl.hivedesktop.com/desktop/channels/<channel>/latest.json`, treating
   404 as an empty channel. Validate the candidate against the publish format:
   - `dev` must be `X.Y.Z-dev.N`;
   - `beta` must be `X.Y.Z-beta.N`;
   - `stable` must be bare `X.Y.Z`;
   - `desktop-v<version>` must not already exist locally or on `origin`;
   - the candidate must advance the selected channel under SemVer precedence.

   Stop if manifests are unreachable or malformed. Never choose from repository
   tags alone: the R2 history predates this repository and may contain a newer
   version than any local tag.
6. Show the candidate version, channel, exact commit SHA and subject, and
   affected manifests (`dev`; `beta+dev`; or `stable+beta+dev`). Ask for an
   explicit confirmation before doing anything that publishes. Prefer the
   harness's structured confirmation UI when available; otherwise ask in plain
   text and wait. The confirmation must make clear that it will push the tag
   and start a signed/notarized public release. A typed alternate version is
   acceptable; revalidate it. Stop on cancellation.
7. After confirmation, run the same local gates used before pushes:

   ```bash
   mi check:generate
   mi check:tidy
   mi lint
   mi test
   mi desktop:test:frontend
   ```

   Stop on the first failure. Verify the worktree is still clean and `HEAD`
   still equals `origin/main` afterward.
8. Create and push the lightweight release tag:

   ```bash
   git tag "desktop-v<version>" HEAD
   git push origin "desktop-v<version>"
   ```

   This push triggers `.github/workflows/desktop-publish.yml`, which runs
   `scripts/release/release-desktop.sh` with the selected version. Do not call
   that script locally in the normal flow.
9. Locate the triggered `Publish Desktop` GitHub Actions run with `gh run list`,
   then watch it to completion with `gh run watch --exit-status`. On success,
   report the version, tag, workflow URL, channel manifests, and artifact URL
   prefix (`https://dl.hivedesktop.com/desktop/releases/<version>/`).
10. On failure, report the failed step and workflow URL. Do not delete or move
    the tag, rerun with `--force`, overwrite artifacts, or invent a replacement
    version without explicit user approval.

## Version selection rules

The helper uses the highest numeric base version found across both
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
