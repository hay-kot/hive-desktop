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

1. Tag `desktop-v<semver>` — the first prerelease identifier routes the channel (`-dev.N` → dev, `-beta.N` → beta, none → stable; any other identifier fails the workflow).
2. CI builds, signs, notarizes; uploads the zip + `SHA256SUMS` to `releases/<semver>/`.
3. CI writes `latest.json` for the target channel **and cascades to less-stable channels** (stable → stable+beta+dev; beta → beta+dev; dev → dev only).

## Rollback

Rewrite the channel's `latest.json` to point at a prior `releases/<semver>/` directory. Never modify or delete release artifacts as part of a rollback.

## Retention

Dev builds are pruned by a scheduled job (delete `-dev.` versions older than N days). R2 lifecycle rules are prefix-only and cannot match `-dev.` mid-key.

## Credentials

- `CLOUDFLARE_API_TOKEN` (repo secret) — web deploys; Workers edit on the account + `hivedesktop.com` zone. Dashboard-created (OAuth sessions cannot mint API tokens).
- Release CI additionally needs R2 write credentials for `hive-desktop-releases` — dashboard-created R2 API token; not yet provisioned.
