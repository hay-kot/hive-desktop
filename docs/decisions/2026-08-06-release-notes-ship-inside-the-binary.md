# Release notes ship inside the binary

- **Status:** accepted
- **Date:** 2026-08-06

## Context

After the app updates itself and relaunches, the version silently ticks up. The
user should be told what changed.

Three constraints decided the shape:

1. **The repository is private.** A user cannot read its GitHub releases, so
   GitHub is not a surface the app can link to or an API it can fetch from. The
   existing About ▸ Release link already 404s for anyone but the maintainer.
2. **Notes generated at publish time cannot describe the build that ships
   them.** The pipeline builds, signs, notarizes, uploads, then creates the
   GitHub release. Anything derived from the tag exists only after the binary
   is sealed.
3. **Publishes cascade** (ADR release-channels): stable reaches stable, beta and
   dev; beta reaches beta and dev; dev reaches dev alone. A stable user
   upgrading across twenty dev builds must not be shown twenty entries.

The install path also rules out an install-time trigger: `InstallUpdate` ends by
relaunching, so the process that would raise the surface is gone.

## Decision

Release notes are markdown files committed to
`internal/app/releasenotes/changelog/<version>.md` and embedded with `go:embed`.
An entry is authored **before** the release commit; `cmd/release` refuses to
publish a version that has none, reading the entries through the same package
the app serves them from so the gate validates the exact bytes the binary
carries.

The surface is triggered at **launch**, not at install: the running version is
compared against a last-acknowledged version recorded in
`<StateDir>/releasenotes.json`. Newer means show the releases crossed to get
here, narrowed to the channel the build follows. This also covers updates the
app did not perform — a package manager, a manual reinstall.

The marker lives in the state directory rather than `settings.yaml` because
`settings.yaml` is dotfiles-managed and shared between machines: having read the
notes is a fact about one installation, not a preference to carry.

Presentation follows the channel. Stable and beta get a modal; dev gets a toast,
because dev builds are cut close to daily and a modal on nearly every launch is
hostile. An upgrade whose range carries no entries degrades to the toast too — a
modal with an empty body says less than the line "Updated to X". The full
per-channel history is always readable in Settings ▸ About.

A *pending* update is the one case notes cannot come from the binary, since that
release is not installed. The channel manifest therefore carries `summary` and
`notes` for its version, which is what populates `UpdateInfo.Notes`.

## Consequences

- Notes are readable offline, at first launch, with no network call — and a
  private repository stays private without costing users their changelog.
- Every release now needs a written entry. `mise run changelog:new` scaffolds
  one from the commit subjects since the previous tag, so the cost is editing
  rather than authoring from nothing, but the release **will** fail without it.
- The GitHub release body is the committed entry rather than `--generate-notes`
  output, so the changelog and the release page cannot disagree.
- A build can only ever describe releases up to its own version. That is exactly
  what the What's New surface needs and is why the history list is not a
  substitute for the update check.
- A fresh install records the running version silently and shows nothing: there
  is no version it upgraded *from*.
