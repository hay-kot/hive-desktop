# One-line install script behind an obscure path

- **Status:** accepted
- **Date:** 2026-07-27

## Context

Alpha testers need to go from nothing to a running app without hunting through
R2 for the right artifact. The in-app updater already resolves builds from
channel manifests ([r2-manifest-distribution](2026-07-23-r2-manifest-distribution.md),
[release-channels](2026-07-23-release-channels.md)); first install should resolve the same way so
the two never disagree on version or artifact. We want no new infrastructure,
and the link kept out of casual discovery while the beta — and the private repo
that holds the link — stay private. macOS is the only published platform; the
Linux installer half is left to the Linux publishing work (#36).

## Decision

1. **A `curl | bash` one-liner, served as a static asset by the existing
   landing-page worker ([web-workers-static-assets](2026-07-23-web-workers-static-assets.md))** and shipped
   by `deploy-web.yml`. No new worker, bucket, or domain.

2. **Resolve from the same channel manifest the updater reads**, verify the
   downloaded artifact's sha256 against the manifest before touching disk, and
   abort on mismatch. Install and auto-update agree by construction, and a
   corrupted or swapped artifact never installs.

3. **Behind an obscure path token, nested under the existing invite-page token**
   (`/install/<token>/install.sh`). `robots.txt` disallows the whole `/install/`
   prefix, so the token never appears in a public file — a top-level token
   directory would have to be named in robots.txt or left crawlable. This is
   **obscurity, not authentication**: anyone with the link can fetch it, and it
   holds only while the repo is private. Re-evaluate before the repo goes public.

4. **macOS installs to `/Applications`** (falling back to `~/Applications`
   without sudo) from a Developer ID-signed, notarized, stapled build, so
   Gatekeeper passes cleanly with no `xattr` workaround — and a `curl` download
   carries no quarantine bit to strip in the first place. Re-running upgrades in
   place, so the script doubles as a manual update path when the in-app updater
   is unavailable.

## Consequences

- Rotating the URL means renaming both the invite page
  (`web/src/pages/install/<token>.astro`) and the script directory
  (`web/public/install/<token>/`) to a new token.
- The script itself is unsigned and unversioned; its integrity rests on HTTPS
  from a domain we control, the in-manifest checksum of the artifact it
  downloads, and being short enough to read (`| less`) before running.
- Linux is out of scope here: platform detection and an install branch are
  stubbed and documented in the script, but making them real — matching the
  tarball format, keys, and user-owned prefix — belongs to the Linux publishing
  work (#36).
