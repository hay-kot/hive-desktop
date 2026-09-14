# Problem reports are GitHub issues

- **Status:** accepted
- **Date:** 2026-09-14

Supersedes ADR [in-app-problem-reporting](2026-07-27-in-app-problem-reporting.md).

## Context

The app uploaded a redacted diagnostic bundle to a private R2 bucket behind a
shared bearer token stamped into release builds (ADR in-app-problem-reporting).
That existed because the repository was private and had no issue tracker. It is
public now, and `.github/ISSUE_TEMPLATE/bug.yml` asks for the same facts the
bundle carries.

The upload path also cost a whole branch of the release flow: a worker route, an
R2 bucket, a secret in two places, and a preflight in `cmd/release` that verified
the release token before shipping. Source builds had no token and failed closed,
so the feature behaved differently in development and in a release.

## Decision

1. **The bundle is written to disk, never transmitted.** `ReportService.Save`
   assembles the same redacted bundle, gzips it, and writes
   `<DataDir>/reports/hive-report-<id>.json.gz`. `report.Uploader`, the stamped
   token, the `/api/report` route, and the `hive-desktop-reports` bucket are
   removed.

2. **Only build identity is prefilled into the issue.** The app opens
   `issues/new?template=bug.yml` with the version and the OS/arch/commit line
   filled in. Redaction removes credentials, not identity: a log tail names the
   user's home directory, repositories and branches, and flows name the orgs and
   hosts they poll. That is acceptable in a bucket only the maintainer reads and
   not acceptable in a public issue, so it travels as a file the user reviews and
   attaches, or does not.

3. **Every surface beyond build info is opt-in and defaults off.** Logs,
   settings, flows and actions each have their own switch, labelled with what it
   exposes rather than with what it is. The previous "basic info" group bundled
   safe build facts with the log tail, which cannot survive a default-off rule.

4. **`ErrorDialog` hands off instead of filing.** It opened a report with every
   surface attached in one click, which is the one thing a public tracker
   forbids. It now dismisses itself and opens the report dialog.

## Consequences

- The release pipeline has no report-token preflight. `verifyWeb` proves the
  worker is live with `GET /api/latest?channel=__probe__`, which stops at the
  worker's own channel validation (400) without reading the manifest bucket.
- Source builds and release builds no longer differ here.
- The bundle cap is GitHub's 25 MB attachment ceiling rather than the worker's
  5 MB body limit.
- The connected-account list left the bundle. It was the most identifying field
  in it (self-hosted hostnames and usernames) and the least diagnostic.
- The maintainer no longer receives a report the user did not choose to send.
  A user who builds a bundle and never attaches it is invisible, which is the
  intended trade.
- #88 (list and pull down problem reports) has no subject.
