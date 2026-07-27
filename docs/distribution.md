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
│   ├── Hive-<ver>-darwin-universal.zip
│   ├── Hive-<ver>-linux-amd64.tar.gz
│   ├── Hive-<ver>-linux-arm64.tar.gz
│   └── SHA256SUMS
└── channels/{stable,beta,dev}/latest.json   # mutable channel pointers
```

One `SHA256SUMS` lists every artifact in the release. A single publish writes the whole prefix once, so nothing under it is ever rewritten — which matters because those objects carry a one-year immutable `Cache-Control`.

## Artifact formats

| Platform | Artifact | Notes |
| -------- | -------- | ----- |
| `darwin-universal` | `.zip` of `Hive.app` | Universal binary, Developer ID signed, notarized + stapled |
| `linux-amd64` | `.tar.gz` of a single `hive-desktop` binary | Unsigned; integrity comes from the manifest sha256 |
| `linux-arm64` | `.tar.gz` of a single `hive-desktop` binary | Same, built for aarch64 |

The Linux tarballs contain **exactly one entry, and that entry is the binary**. This is a hard requirement of the updater, not a style choice: it rejects archives with more than one top-level entry, and its helper renames whatever it extracts directly over the running executable — so a wrapper directory would replace the binary with a directory. `cmd/release` builds the archive itself and re-reads it to assert entry count, name, type, and the executable bit before uploading.

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
    },
    "linux-amd64": {
      "url": "https://dl.hivedesktop.com/desktop/releases/1.4.0/Hive-1.4.0-linux-amd64.tar.gz",
      "sha256": "<hex>",
      "size": 41234567
    },
    "linux-arm64": {
      "url": "https://dl.hivedesktop.com/desktop/releases/1.4.0/Hive-1.4.0-linux-arm64.tar.gz",
      "sha256": "<hex>",
      "size": 40123456
    }
  }
}
```

Platform keys come from `platformKey` in `internal/adapter/wailsui/updater_provider.go`: macOS ships one universal build so both arches resolve to `darwin-universal`; everything else is `<os>-<arch>`.

## Landing page

The download CTA on hivedesktop.com resolves through the stable manifest at runtime, so shipping a release does not require redeploying the site. `dl.hivedesktop.com` sends no CORS headers, so the page fetches the same-origin `/api/latest` route on the worker, which proxies the manifest and caches it at the edge for 5 minutes. If that fetch fails the button keeps its static fallback (`#beta`) rather than breaking.

Private-beta signups POST to `/api/subscribe`; the worker validates the address, drops honeypot submissions (the form's hidden `company` field, answered with a fake success), and forwards the rest to listmonk's public form endpoint with the Hive Desktop list UUID. Subscribers are managed in the listmonk admin at https://listmonk.haybytes.com/admin.

## Install script

The one-line installer ([ADR 0026](decisions/0026-install-script.md)) is a static asset served by the same worker and shipped by `deploy-web.yml`:

```
curl -fsSL https://hivedesktop.com/install/a1c6d523f7a3d06eed1e7b43/install.sh | bash
```

It detects OS+arch, resolves the channel's latest build from the **same manifest the updater reads** (`channels/<channel>/latest.json`), verifies the artifact's sha256 from the manifest before installing, and on macOS unzips `Hive.app` into `/Applications` (falling back to `~/Applications`) and symlinks `hive` onto the PATH. The channel defaults to stable; pass another with `… | bash -s -- --channel dev` or the `HIVE_CHANNEL` env var, and `HIVE_BIN_DIR` sets the symlink dir. It always installs the channel's latest — no version pin — and re-running upgrades in place.

- **macOS only during the beta.** The Linux branch is wired but inert until Linux artifacts exist (#36).
- The path token is **obscurity, not authentication** — it keeps the link out of casual discovery while the repo is private, nothing more. `robots.txt` disallows the whole `/install/` prefix, so the token never appears in a public file. Rotating it means renaming both `web/public/install/<token>/install.sh` and the invite page `web/src/pages/install/<token>.astro` to a new token. Re-evaluate before the repo goes public.

## Problem reporting

The app's "Report a problem" dialog (System settings ▸ Diagnostics) gzips a redacted diagnostic bundle and POSTs it to `/api/report` on the same worker, which stores it in the private `hive-desktop-reports` bucket. The reporter chooses what to attach: basic info (build/system info, a bounded log tail, connected accounts) as one group, and settings/flows/actions individually — each config surface is secret-scrubbed before it is included (ADR 0024). The endpoint requires a shared bearer token, `Content-Encoding: gzip`, and a ≤5 MB body, and writes the object key from its own clock: `reports/YYYY/MM/DD/<report-id>.json.gz`.

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

The same token value is stamped into the released app so its uploads pass the worker's bearer check. The release build reads `HIVE_DESKTOP_REPORT_TOKEN` from the environment (repo-root `.env` locally) and stamps it via `-X github.com/hay-kot/hive-desktop/internal/adapter/wailsui.reportToken=$HIVE_DESKTOP_REPORT_TOKEN` in both platform Taskfiles (`desktop/build/darwin/Taskfile.yml` and `desktop/build/linux/Taskfile.yml`); set its value to the worker's `REPORT_TOKEN` secret. With no token stamped in — every source and dev build — the app hides/disables reporting and the worker answers `503 reporting_disabled`, so the feature fails closed. The publisher warns when `HIVE_DESKTOP_REPORT_TOKEN` is empty so a release cannot silently ship with reporting off.

The stamped client token is not a secret — it ships in the binary and is extractable. It exists to gate the feature off in non-release builds and as a rotatable deterrent; the endpoint's real protection is edge rate-limiting plus the write-only private bucket, the server-chosen object key, and the size cap.

**Reading reports** (no viewer UI yet):

```bash
wrangler r2 object get hive-desktop-reports/reports/2026/07/27/<id>.json.gz --file report.json.gz
gunzip -c report.json.gz | jq .
```

## Cache-Control (set per object at upload)

- `releases/**`: `public, max-age=31536000, immutable`
- `channels/**/latest.json`: `no-cache`

## Publish flow

The pipeline is the Go CLI in `cmd/release`. **A release publishes every platform at once, from one machine** — there is no CI publishing workflow (decision [0028](decisions/0028-linux-tarball-distribution.md)). `publish`:

1. builds the universal .app, Developer ID signs it with an ephemeral keychain, notarizes + staples it, packages without macOS AppleDouble metadata, and verifies the extracted archive's signature and stapled ticket;
2. builds `linux-amd64` and `linux-arm64` in a container, asserting each binary carries the version stamp and each tarball still satisfies the updater's single-entry rule;
3. writes one `SHA256SUMS` covering all three, uploads them to `releases/<semver>/`, writes one channel manifest naming all three, and verifies every published artifact against the manifest it just wrote.

Publishing everything in one process is what keeps the manifest-advancement rule (below) usable: a second publish topping up another platform would be rejected for not advancing the version the first just set. It also means a release needs macOS **and** a running Docker on the same machine. `next`, `prepare`, and `verify` handle version selection, preflight validation, and standalone diagnostics without separate scripts. Channel routing and cascade follow the rules below.

### Building Linux from macOS

The Linux binary is built in a container so a Mac can produce it: `desktop/build/docker/Dockerfile.linux` mirrors the ubuntu-24.04 runner (GTK 4.14, the repo's pinned Go, the pinned wails3) and runs the **same** `wails3 task linux:build` a native Linux host would, so a locally driven release cannot diverge from a native build. The image is tagged per architecture because it carries a native toolchain — an arm64 image cannot serve `--platform linux/amd64`.

```bash
ARCH=arm64 mise run desktop:build:linux:image   # one-time per arch, ~5 min
mise run desktop:build:linux                    # binary only → desktop/bin/hive-desktop
```

Building the non-host architecture (amd64 on Apple Silicon) works but runs the image build *and* the compile under emulation — budget considerably more time. The Go module cache is mounted from the host and `GOPROXY=off` is set, so the container never needs credentials for the private `colonyops/hive` module; `node_modules` lives in a per-arch named volume so the host's macOS-native copy is never mounted in.

The web landing page and worker are **not** independent of a release. Before the app build, `publish` deploys `web/` (`npm ci && npm run deploy`) and verifies the worker is live and — when `HIVE_DESKTOP_REPORT_TOKEN` is set — that the release token is accepted (an authenticated non-gzip `POST /api/report` must return `415`, past the `401`/`503` gates, so it never writes a report). This runs first because the R2 upload is the only irreversible step: a broken or misconfigured backend aborts the release before any immutable artifact ships, keeping the app and its backend in sync or failing loudly. `--skip-web` opts out. Pushing to `main` under `web/**` still deploys the site on its own (`.github/workflows/deploy-web.yml`) for web-only changes.

**Local release** (the normal path; secrets from the gitignored repo-root `.env`, loaded by mise; tag afterwards):

```bash
go run ./cmd/release prepare dev 1.4.0-dev.1
mise run release:desktop -- 1.4.0-dev.1   # flags: --skip-upload, --skip-notarize (requires --skip-upload), --skip-web, --force
git tag desktop-v1.4.0-dev.1             # local only; do not push after a local upload
```

`publish` verifies every affected live manifest and downloads the public artifact to verify its size and SHA-256 before it succeeds. `verify` remains available for later diagnostics without rebuilding.

Rules enforced by the publisher:
1. Release preparation requires a clean, current `main`. Publishing requires the same state. Source state is checked again immediately before upload.
2. The first prerelease identifier routes the channel (`-dev.N` → dev, `-beta.N` → beta, none → stable; any other identifier is rejected).
3. `latest.json` is written for the target channel **and cascades to less-stable channels** (stable → stable+beta+dev; beta → beta+dev; dev → dev only).
4. `releases/<semver>/` is immutable — re-publishing an existing version requires `--force`.

## Installing on Linux

Linux ships a plain tarball, deliberately — no `.deb`, `.rpm`, AppImage, or repository (decision [0028](decisions/0028-linux-tarball-distribution.md)). Download it, verify it, and put the binary somewhere on `PATH` **that your user owns**, which is what makes in-app updates work:

```bash
VER=1.4.0
BASE=https://dl.hivedesktop.com/desktop/releases/$VER
ARCH=amd64                                  # or arm64
curl -fLO "$BASE/Hive-$VER-linux-$ARCH.tar.gz"
curl -fL "$BASE/SHA256SUMS" | grep "linux-$ARCH" | sha256sum -c -

mkdir -p ~/.local/bin
tar -xzf "Hive-$VER-linux-$ARCH.tar.gz" -C ~/.local/bin
~/.local/bin/hive-desktop
```

Runtime dependencies are GTK 4 and WebKitGTK 6.0 — `libgtk-4-1` + `libwebkitgtk-6.0-4` on Debian/Ubuntu, `gtk4` + `webkitgtk6.0` on Fedora. Ubuntu 24.04 / Debian 13 / Fedora 40 or newer satisfy these from the base repos.

**System tray.** Hive normally hides to the tray when you close its window, and the tray menu is where profiles and Quit live. That needs a StatusNotifier host: Ubuntu, KDE, XFCE, and Cinnamon have one; **vanilla GNOME (Fedora Workstation, Debian GNOME) does not** unless you install the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/). Hive checks the session bus at startup, and when no host owns `org.kde.StatusNotifierWatcher` it makes closing the window quit the app instead of hiding it — otherwise closing would leave it running with no window and no tray to restore it from.

To get a launcher entry, drop a `.desktop` file in place (optional):

```bash
cat > ~/.local/share/applications/hive-desktop.desktop <<EOF
[Desktop Entry]
Type=Application
Name=Hive
Exec=$HOME/.local/bin/hive-desktop
Icon=$HOME/.local/bin/hive-desktop
Categories=Development;
Terminal=false
EOF
update-desktop-database ~/.local/share/applications
```

## Auto-update

The in-app updater (`internal/adapter/wailsui/updater_provider.go`) polls `https://dl.hivedesktop.com/desktop/channels/<channel>/latest.json`, compares semver against the running version, and downloads the manifest's artifact URL with the manifest's sha256 verified by the Wails updater. A published build follows its own channel — the version's prerelease identifier a release was built with also selects the channel it tracks — and `updates.channel: stable|beta|dev` in the desktop `settings.yaml` overrides that default. Source builds (version `dev`) never self-update.

On both platforms the update is an in-place swap: the framework downloads and verifies the artifact, unpacks it, and a detached helper waits for the app to exit before renaming the new payload over the old one and relaunching.

**Linux specifics** (`internal/adapter/wailsui/updater_staging.go`) — two properties of that helper need handling, and neither shows up on macOS:

- **The swap is a bare rename with no cross-device fallback**, while the framework stages downloads under `$TMPDIR`. On every distro that mounts `/tmp` as tmpfs — Fedora, Arch, openSUSE, RHEL 9+, Debian 13+ — that rename fails with `EXDEV`, and the user watches the app relaunch on the old version. Hive works around it by pointing `$TMPDIR` at the running binary's own directory for the duration of the download, so staging lands on the filesystem the rename has to reach. The override is scoped to the download rather than set at startup, because `$TMPDIR` is inherited by the terminal and shell commands the output worker spawns.
- **An install the user cannot write can never self-update.** A tarball unpacked into a root-owned prefix like `/usr/local/bin` fails when the helper tries to write its backup. Hive probes for this *before* downloading and reports that the install must be updated the same way it was installed. Installing under `~/.local/bin` avoids it entirely.

## Rollback

Rewrite the channel's `latest.json` to point at a prior `releases/<semver>/` directory. Never modify or delete release artifacts as part of a rollback.

## Retention

Dev builds are pruned by a scheduled job (delete `-dev.` versions older than N days). R2 lifecycle rules are prefix-only and cannot match `-dev.` mid-key.

## Credentials

- `CLOUDFLARE_API_TOKEN` (repo secret) — web deploys; Workers edit on the account + `hivedesktop.com` zone. Dashboard-created (OAuth sessions cannot mint API tokens).
- `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` (repo secrets + local `.env`) — S3 credentials for `hive-desktop-releases`.
- `HIVE_DESKTOP_REPORT_TOKEN` (repo secret + local `.env`) — stamped into release builds so problem-report uploads pass the worker's bearer check; set it to the same value as the worker's `REPORT_TOKEN` secret. Extractable from the binary, so not a real secret (see [Problem reporting](#problem-reporting)).
- Signing/notary set (local `.env`): `MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PWD`, `MACOS_SIGN_IDENTITY`, `AC_API_KEY`, `AC_API_KEY_ID`, `AC_API_ISSUER_ID`. macOS only — Linux publishing needs nothing beyond the R2 pair.
