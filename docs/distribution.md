# Distribution Reference

Concrete infrastructure and runbook for shipping the desktop app. Decisions behind this: [0003](decisions/0003-r2-manifest-distribution.md) (R2 + manifests), [0004](decisions/0004-release-channels.md) (channels).

## Infrastructure

| Thing | Value |
| ----- | ----- |
| Cloudflare account | `bce6b95e4e84d92b1972d3b55b6cfaf6` |
| Zone | `hivedesktop.com` (`654b5078db773efcbf7c73b7c67eae89`) |
| Landing page worker | `hive-desktop-web` → https://hivedesktop.com (config: `web/wrangler.jsonc`) |
| Artifact bucket | R2 `hive-desktop-releases` (ENAM, Standard) |
| Download domain | https://dl.hivedesktop.com (bucket custom domain, public, TLS ≥ 1.2) |
| Liveness probe | https://dl.hivedesktop.com/healthcheck.txt |

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

## Cache-Control (set per object at upload)

- `releases/**`: `public, max-age=31536000, immutable`
- `channels/**/latest.json`: `no-cache`

## Publish flow

The pipeline is the Go CLI in `cmd/release`. Its `publish` command builds the universal .app, Developer ID signs it with an ephemeral keychain, notarizes + staples it, packages without macOS AppleDouble metadata, verifies the extracted archive's signature and stapled ticket, writes `SHA256SUMS`, uploads to `releases/<semver>/`, and writes channel manifests. `next`, `prepare`, and `verify` handle version selection, preflight validation, and live artifact verification without separate scripts. Channel routing and cascade follow the rules below.

**CI release** (the normal path): push a `desktop-v<semver>` tag; `.github/workflows/desktop-publish.yml` wraps the same script on a macOS runner using the repo secrets.

```bash
git tag desktop-v1.4.0-dev.1 && git push origin desktop-v1.4.0-dev.1
```

**Local release** (secrets from the gitignored repo-root `.env`, loaded by mise; tag afterwards):

```bash
go run ./cmd/release prepare dev 1.4.0-dev.1
mise run release:desktop -- 1.4.0-dev.1   # flags: --skip-upload, --skip-notarize (requires --skip-upload), --force
go run ./cmd/release verify 1.4.0-dev.1
git tag desktop-v1.4.0-dev.1             # local only; do not push after a local upload
```

Rules enforced by the script:
1. The first prerelease identifier routes the channel (`-dev.N` → dev, `-beta.N` → beta, none → stable; any other identifier is rejected).
2. `latest.json` is written for the target channel **and cascades to less-stable channels** (stable → stable+beta+dev; beta → beta+dev; dev → dev only).
3. `releases/<semver>/` is immutable — re-publishing an existing version requires `--force`.

## Auto-update

The in-app updater (`desktop/updater_provider.go`) polls `https://dl.hivedesktop.com/desktop/channels/<channel>/latest.json`, compares semver against the running version, and downloads the manifest's artifact URL with the manifest's sha256 verified by the Wails updater. A published build follows its own channel — the version's prerelease identifier a release was built with also selects the channel it tracks — and `update_channel: stable|beta|dev` in the desktop `settings.yaml` overrides that default. Source builds (version `dev`) never self-update.

## Rollback

Rewrite the channel's `latest.json` to point at a prior `releases/<semver>/` directory. Never modify or delete release artifacts as part of a rollback.

## Retention

Dev builds are pruned by a scheduled job (delete `-dev.` versions older than N days). R2 lifecycle rules are prefix-only and cannot match `-dev.` mid-key.

## Credentials

- `CLOUDFLARE_API_TOKEN` (repo secret) — web deploys; Workers edit on the account + `hivedesktop.com` zone. Dashboard-created (OAuth sessions cannot mint API tokens).
- `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` (repo secrets + local `.env`) — S3 credentials for `hive-desktop-releases`.
- Signing/notary set (repo secrets + local `.env`, same names in both): `MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PWD`, `MACOS_SIGN_IDENTITY`, `AC_API_KEY`, `AC_API_KEY_ID`, `AC_API_ISSUER_ID`.
