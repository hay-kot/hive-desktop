# Distribution Reference

Concrete infrastructure and runbook for shipping the desktop app. Decisions behind this: [0003](decisions/0003-r2-manifest-distribution.md) (R2 + manifests), [0004](decisions/0004-release-channels.md) (channels), [0024](decisions/0024-in-app-problem-reporting.md) (problem reporting).

## Infrastructure

| Thing | Value |
| ----- | ----- |
| Cloudflare account | `bce6b95e4e84d92b1972d3b55b6cfaf6` |
| Zone | `hivedesktop.com` (`654b5078db773efcbf7c73b7c67eae89`) |
| Landing page worker | `hive-desktop-web` → https://hivedesktop.com (config: `web/wrangler.jsonc`) |
| Beta signup list | listmonk at https://listmonk.haybytes.com, list `ae24f0b5-c230-4d2e-9fc0-747e9270636e` (Hive Desktop) |
| Artifact bucket | R2 `hive-desktop-releases` (ENAM, Standard) |
| Download domain | https://dl.hivedesktop.com (bucket custom domain, public, TLS ≥ 1.2) |
| Liveness probe | https://dl.hivedesktop.com/healthcheck.txt |
| Reports bucket | R2 `hive-desktop-reports` (private, no custom domain, no public access) |
| Report endpoint | `POST https://hivedesktop.com/api/report` (worker `REPORTS` binding) |

## Bucket layout

```
desktop/
├── releases/<full-semver>/                  # immutable
│   ├── Hive-<ver>-<platform>.zip            # e.g. Hive-1.4.0-darwin-universal.zip
│   └── SHA256SUMS
└── channels/{stable,beta,dev}/latest.json   # mutable channel pointers
```

## Manifest schema (`latest.json`)

```json
{
  "channel": "stable",
  "version": "1.4.0",
  "pub_date": "2026-08-01T00:00:00Z",
  "platforms": {
    "darwin-universal": {
      "url": "https://dl.hivedesktop.com/desktop/releases/1.4.0/Hive-1.4.0-darwin-universal.zip",
      "sha256": "<hex>",
      "size": 48123456
    }
  }
}
```

## Landing page

The download CTA on hivedesktop.com resolves through the stable manifest at runtime, so shipping a release does not require redeploying the site. `dl.hivedesktop.com` sends no CORS headers, so the page fetches the same-origin `/api/latest` route on the worker, which proxies the manifest and caches it at the edge for 5 minutes. If that fetch fails the button keeps its static fallback (`#beta`) rather than breaking.

Private-beta signups POST to `/api/subscribe`; the worker validates the address, drops honeypot submissions (the form's hidden `company` field, answered with a fake success), and forwards the rest to listmonk's public form endpoint with the Hive Desktop list UUID. Subscribers are managed in the listmonk admin at https://listmonk.haybytes.com/admin.

## Problem reporting

The app's "Report a problem" dialog (System settings ▸ Diagnostics) gzips a redacted diagnostic bundle and POSTs it to `/api/report` on the same worker, which stores it in the private `hive-desktop-reports` bucket. The bundle carries build/system info, a bounded log tail, and a secret-scrubbed config snapshot (ADR 0024). The endpoint requires a shared bearer token, `Content-Encoding: gzip`, and a ≤5 MB body, and writes the object key from its own clock: `reports/YYYY/MM/DD/<report-id>.json.gz`.

**One-time setup to enable it:**

```bash
# 1. Create the private bucket (no public access, no custom domain).
wrangler r2 bucket create hive-desktop-reports

# 2. Set the shared token the worker checks (any long random string).
cd web && wrangler secret put REPORT_TOKEN

# 3. Expire reports after 90 days.
wrangler r2 bucket lifecycle add hive-desktop-reports --expire-days 90 --prefix reports/

# 4. Rate-limit the endpoint at the edge (dashboard): a WAF rate-limiting rule
#    on hostname hivedesktop.com + path /api/report, e.g. 10 requests / 10 min / IP.
```

The same token value must be stamped into the released app so its uploads authenticate: add `-X github.com/hay-kot/hive-desktop/internal/adapter/wailsui.reportToken=<token>` to the release build's ldflags. With no token stamped in, the app hides/disables reporting and the worker answers `503 reporting_disabled` — the feature fails closed.

**Reading reports** (no viewer UI yet):

```bash
wrangler r2 object get hive-desktop-reports/reports/2026/07/27/<id>.json.gz --file report.json.gz
gunzip -c report.json.gz | jq .
```

## Cache-Control (set per object at upload)

- `releases/**`: `public, max-age=31536000, immutable`
- `channels/**/latest.json`: `no-cache`

## Publish flow

The pipeline is the Go CLI in `cmd/release`. Its `publish` command builds the universal .app, Developer ID signs it with an ephemeral keychain, notarizes + staples it, packages without macOS AppleDouble metadata, verifies the extracted archive's signature and stapled ticket, writes `SHA256SUMS`, uploads to `releases/<semver>/`, writes channel manifests, and verifies the live artifact. `next`, `prepare`, and `verify` handle version selection, preflight validation, and standalone diagnostics without separate scripts. Channel routing and cascade follow the rules below.

**Local release** (the normal path; secrets from the gitignored repo-root `.env`, loaded by mise; tag afterwards):

```bash
go run ./cmd/release prepare dev 1.4.0-dev.1
mise run release:desktop -- 1.4.0-dev.1   # flags: --skip-upload, --skip-notarize (requires --skip-upload), --force
git tag desktop-v1.4.0-dev.1             # local only; do not push after a local upload
```

`publish` verifies every affected live manifest and downloads the public artifact to verify its size and SHA-256 before it succeeds. `verify` remains available for later diagnostics without rebuilding.

**CI release** (explicit alternative): create and push `desktop-v<semver>` at the current `main` commit. `.github/workflows/desktop-publish.yml` runs the same gates as the local release procedure, then wraps the publisher on a macOS runner using repository secrets.

```bash
git tag desktop-v1.4.0-dev.1 && git push origin desktop-v1.4.0-dev.1
```

Rules enforced by the script:
1. Release preparation requires a clean, current `main`. Publishing requires the same state locally, or a detached CI checkout whose exact version tag points at a commit on `origin/main`. Source state is checked again immediately before upload.
2. The first prerelease identifier routes the channel (`-dev.N` → dev, `-beta.N` → beta, none → stable; any other identifier is rejected).
3. `latest.json` is written for the target channel **and cascades to less-stable channels** (stable → stable+beta+dev; beta → beta+dev; dev → dev only).
4. `releases/<semver>/` is immutable — re-publishing an existing version requires `--force`.

## Auto-update

The in-app updater (`desktop/updater_provider.go`) polls `https://dl.hivedesktop.com/desktop/channels/<channel>/latest.json`, compares semver against the running version, and downloads the manifest's artifact URL with the manifest's sha256 verified by the Wails updater. A published build follows its own channel — the version's prerelease identifier a release was built with also selects the channel it tracks — and `updates.channel: stable|beta|dev` in the desktop `settings.yaml` overrides that default. Source builds (version `dev`) never self-update.

## Rollback

Rewrite the channel's `latest.json` to point at a prior `releases/<semver>/` directory. Never modify or delete release artifacts as part of a rollback.

## Retention

Dev builds are pruned by a scheduled job (delete `-dev.` versions older than N days). R2 lifecycle rules are prefix-only and cannot match `-dev.` mid-key.

## Credentials

- `CLOUDFLARE_API_TOKEN` (repo secret) — web deploys; Workers edit on the account + `hivedesktop.com` zone. Dashboard-created (OAuth sessions cannot mint API tokens).
- `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` (repo secrets + local `.env`) — S3 credentials for `hive-desktop-releases`.
- Signing/notary set (repo secrets + local `.env`, same names in both): `MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PWD`, `MACOS_SIGN_IDENTITY`, `AC_API_KEY`, `AC_API_KEY_ID`, `AC_API_ISSUER_ID`.
