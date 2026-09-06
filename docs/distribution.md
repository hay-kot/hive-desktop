# Distribution Reference

Concrete infrastructure and runbook for shipping the desktop app. Decisions behind this: [r2-manifest-distribution](decisions/2026-07-23-r2-manifest-distribution.md) (R2 + manifests), [release-channels](decisions/2026-07-23-release-channels.md) (channels), [in-app-problem-reporting](decisions/2026-07-27-in-app-problem-reporting.md) (problem reporting).

## Infrastructure

| Thing | Value |
| ----- | ----- |
| Cloudflare account | `bce6b95e4e84d92b1972d3b55b6cfaf6` |
| Zone | `hivedesktop.com` (`654b5078db773efcbf7c73b7c67eae89`) |
| Landing page worker | `hive-desktop-web` → https://hivedesktop.com (config: `web/wrangler.jsonc`) |
| Artifact bucket | R2 `hive-desktop-releases` (ENAM, Standard) |
| Download domain | https://dl.hivedesktop.com (bucket custom domain, public, TLS ≥ 1.2) |
| Liveness probe | https://dl.hivedesktop.com/healthcheck.txt |
| Reports bucket | R2 `hive-desktop-reports` (private, no custom domain, no public access) |
| Report endpoint | `POST https://hivedesktop.com/api/report` (worker `REPORTS` binding) |

## Bucket layout

```
desktop/
├── releases/<full-semver>/                  # immutable
│   ├── Hive-<ver>-darwin-universal.dmg
│   ├── Hive-<ver>-darwin-universal.zip
│   ├── Hive-<ver>-linux-amd64.tar.gz
│   ├── Hive-<ver>-linux-arm64.tar.gz
│   └── SHA256SUMS
└── channels/{stable,beta,dev}/latest.json   # mutable channel pointers
```

One `SHA256SUMS` lists every artifact in the release. A logical publish writes each object under the prefix once. Recovery may fill objects a failed attempt did not reach, but reuses an existing object only after proving its bytes match, so nothing under the prefix is rewritten. That matters because those objects carry a one-year immutable `Cache-Control`.

## Artifact formats

| Platform | Artifact | Notes |
| -------- | -------- | ----- |
| `darwin-universal` | `.dmg` holding `Hive.app` + an `/Applications` symlink | **The human download.** Universal binary, Developer ID signed, notarized + stapled — the image itself, not just the app inside (decision [macos-dmg-installer](decisions/2026-08-03-macos-dmg-installer.md)) |
| `darwin-universal` | `.zip` of `Hive.app` | **The update artifact.** Same signed, notarized, stapled app; the updater cannot consume a disk image |
| `linux-amd64` | `.tar.gz` of a single `hive-desktop` binary | Unsigned; integrity comes from the manifest sha256 |
| `linux-arm64` | `.tar.gz` of a single `hive-desktop` binary | Same, built for aarch64 |

macOS is the one platform that publishes two artifacts, because the two jobs conflict: the Wails updater declares its artifact filetype as `zip` and unpacks it in place, while a first-time user needs an install affordance a bare `.app` in `~/Downloads` does not give them. Both are cut from the same signed, stapled app in the same publish, so they cannot drift.

The Linux tarballs contain **exactly one entry, and that entry is the binary**. This is a hard requirement of the updater, not a style choice: it rejects archives with more than one top-level entry, and its helper renames whatever it extracts directly over the running executable — so a wrapper directory would replace the binary with a directory. `cmd/release` builds the archive itself and re-reads it to assert entry count, name, type, and the executable bit before uploading.

## Manifest schema (`latest.json`)

```json
{
  "channel": "stable",
  "version": "1.4.0",
  "pub_date": "2026-08-01T00:00:00Z",
  "summary": "Terminal paste fixes and installed-font support.",
  "notes": "## Added\n\n- ...",
  "platforms": {
    "darwin-universal": {
      "url": "https://dl.hivedesktop.com/desktop/releases/1.4.0/Hive-1.4.0-darwin-universal.zip",
      "sha256": "<hex>",
      "size": 48123456,
      "installer_url": "https://dl.hivedesktop.com/desktop/releases/1.4.0/Hive-1.4.0-darwin-universal.dmg",
      "installer_sha256": "<hex>",
      "installer_size": 47987654
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

`summary` and `notes` carry the version's release notes — a stable release's own changelog entry, or the accumulated draft for a prerelease. They are the *only* place a pending release's notes exist, since the app's own copy is embedded in the build it describes and that build is not installed yet (ADR release-notes-ship-inside-the-binary). Both are optional — manifests published before the changelog existed still parse. The updater reads them into `UpdateInfo.Notes`, but **no surface renders that field yet**: an update-available prompt that wants to say what the update contains is still to be built, and should read `summary` and `notes` separately rather than the flattened `UpdateInfo.Notes`.

`url`/`sha256`/`size` are the artifact the **updater** downloads. The `installer_*` fields are the artifact a **human** downloads, and appear only where the two differ — today, macOS. They are optional and must be read as a set: a consumer either has all three or treats the platform as having no separate installer, because a URL without its checksum would mean installing unverified bytes. Consumers written against the pre-installer schema keep working; the updater ignores unknown fields.

## Landing page

The download CTA on hivedesktop.com resolves through the stable manifest at runtime, so shipping a release does not require redeploying the site. `dl.hivedesktop.com` sends no CORS headers, so the page fetches the same-origin `/api/latest` route on the worker, which proxies the manifest and caches it at the edge for 5 minutes. If that fetch fails the button keeps its static fallback (`/install`) rather than breaking.

The worker route is live but **no page consumes it yet** — every CTA points at `/install`, and installs go through the one-liner there. When the CTA lands it reads `installer_url`, not `url`: the zip is the updater's artifact, and handing it to a first-time visitor is the problem the DMG exists to solve. The proxy passes the manifest through untouched, so that needs no worker change.

## Install script

The one-line installer ([ADR install-script](decisions/2026-07-27-install-script.md)) is a static asset served by the same worker and shipped by `deploy-web.yml`:

```
curl -fsSL https://hivedesktop.com/install.sh | bash
```

It detects OS+arch, resolves the channel's latest build from the **same manifest the updater reads** (`channels/<channel>/latest.json`), verifies the artifact's sha256 from the manifest before installing, and on macOS unzips `Hive.app` into `/Applications` (falling back to `~/Applications`) and symlinks `hive` onto the PATH. The channel defaults to stable; pass another with `… | bash -s -- --channel dev` or the `HIVE_CHANNEL` env var, and `HIVE_BIN_DIR` sets the symlink dir. It always installs the channel's latest — no version pin — and re-running upgrades in place.

- **macOS is the only published platform.** The Linux branch is wired but inert until Linux artifacts exist (#36).
- The script and its page are public and crawlable: `web/public/install.sh` and `web/src/pages/install.astro`, listed in `sitemap.xml.ts`. They sat behind a path token while the repo was private; that reversed when it went public (ADR [install-script](decisions/2026-07-27-install-script.md)). The URL is published in the README and the docs, so treat it as stable.

## Problem reporting

The app's "Report a problem" dialog (System settings ▸ Diagnostics) gzips a redacted diagnostic bundle and POSTs it to `/api/report` on the same worker, which stores it in the private `hive-desktop-reports` bucket. The reporter chooses what to attach: basic info (build/system info, a bounded log tail, connected accounts) as one group, and settings/flows/actions individually — each config surface is secret-scrubbed before it is included (ADR in-app-problem-reporting). The endpoint requires a shared bearer token, `Content-Encoding: gzip`, and a ≤5 MB body, and writes the object key from its own clock: `reports/YYYY/MM/DD/<report-id>.json.gz`.

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

The pipeline is the Go CLI in `cmd/release`. **A release publishes every platform at once, from one machine** — there is no CI publishing workflow (decision [linux-tarball-distribution](decisions/2026-07-27-linux-tarball-distribution.md)). `publish`:

1. builds the universal .app, Developer ID signs it with an ephemeral keychain, notarizes + staples it, packages without macOS AppleDouble metadata, and verifies the extracted archive's signature and stapled ticket;
2. builds the installer `.dmg` from that stapled app, signs it, notarizes and staples **the image** (a second Apple round trip), then mounts it and asserts the layout the user will see;
3. builds `linux-amd64` and `linux-arm64` in a container, asserting each binary carries the version stamp and each tarball still satisfies the updater's single-entry rule;
4. writes one `SHA256SUMS` covering all four, uploads them to `releases/<semver>/`, writes one channel manifest naming all of them, and verifies every published artifact — installer included — against the manifest it just wrote;
5. records the release on GitHub ([github-tags-and-releases](decisions/2026-07-29-github-tags-and-releases.md)) — pushes the lightweight `desktop-v<semver>` tag and creates a GitHub Release whose body is the version's committed release notes: a stable release's own changelog entry, or the draft for a prerelease. dev and beta are marked prerelease; only stable is the latest release. It attaches no artifacts — downloads stay in R2 (decision 0003) — and is idempotent, so `release github <version>` re-records a release whose GitHub step failed after the upload.

Publishing everything in one process is what keeps the manifest-advancement rule (below) usable: a second publish topping up another platform would be rejected for not advancing the version the first just set. It also means a release needs macOS, a running Docker, **and** an authenticated `gh` on the same machine. `next`, `prepare`, and `verify` handle version selection, preflight validation, and standalone diagnostics without separate scripts. Channel routing and cascade follow the rules below.

### Building Linux from macOS

The Linux binary is built in a container so a Mac can produce it: `desktop/build/docker/Dockerfile.linux` mirrors ubuntu-24.04 (GTK 4.14, the repo's pinned Go, the pinned wails3, all base images digest-pinned) and runs the **same** `wails3 task linux:build` a native Linux host would, so a locally driven release cannot diverge from a native build. The image is tagged per architecture — it carries a native toolchain, so an arm64 image cannot serve `--platform linux/amd64` — plus a fingerprint of the Dockerfile and the wails3 pin, so a pin bump or Dockerfile edit rebuilds it instead of silently reusing a stale image.

```bash
ARCH=arm64 mise run build:linux:image   # optional pre-build, ~5 min per arch
mise run build:linux                    # binary only → desktop/bin/hive-desktop
```

Building the non-host architecture (amd64 on Apple Silicon) works but runs the image build *and* the compile under emulation — budget considerably more time. The Go module cache is mounted **read-only** from the host and `GOPROXY=off` is set, so the container never needs credentials for the private `colonyops/hive` module and the third-party npm code it runs (with lifecycle scripts disabled) cannot poison the cache the host's own builds trust; `node_modules` lives in a per-arch named volume so the host's macOS-native copy is never mounted in.

The web landing page and worker are **not** independent of a release. Before the app build, `publish` deploys `web/` (`npm ci && npm run deploy`) and verifies the worker is live and — when `HIVE_DESKTOP_REPORT_TOKEN` is set — that the release token is accepted (an authenticated non-gzip `POST /api/report` must return `415`, past the `401`/`503` gates, so it never writes a report). This runs first because the R2 upload is the only irreversible step: a broken or misconfigured backend aborts the release before any immutable artifact ships, keeping the app and its backend in sync or failing loudly. `--skip-web` opts out. Pushing to `main` under `web/**` still deploys the site on its own (`.github/workflows/deploy-web.yml`) for web-only changes.

**Local release** (the normal path; secrets from the gitignored repo-root `.env`, loaded by mise):

```bash
mise release                         # select the channel and patch/minor/major increment
mise release dev                     # preselect the channel, then select the increment
mise release dev 1.4.0-dev.1         # preselect the channel and exact version
mise release --dry-run               # exercise the prompts without publishing
```

The interactive command refreshes release tags, checks the source and GitHub authentication, and computes the normal patch-oriented candidate from live manifests. It shows the exact resulting version for patch, minor, and major choices; patch preserves normal channel progression (for example, the next dev prerelease or a dev-to-beta promotion), while minor and major start a new base version at prerelease `.1` where applicable. Publishing requires an explicit confirmation that defaults to cancel. It then runs `mi check` and `mi frontend:test`, verifies that the confirmed commit is still current and clean, and publishes.

`--dry-run` is a prompt preview that also works from a dirty feature worktree. It reads live manifests and tags and validates the selected version, but skips the clean-main and GitHub-authentication requirements and stops after confirmation without running gates, building artifacts, uploading, tagging, or creating a GitHub release.

`mise run release:publish -- <version>` is the low-level publisher used for local build diagnostics and recovery (`--skip-upload`, `--skip-notarize` with `--skip-upload`, `--skip-web`, `--force`, `--resume`); do not use it to bypass the interactive confirmation for a normal public release. `publish` verifies every affected live manifest and downloads the public artifact to verify its size and SHA-256, then pushes the `desktop-v1.4.0-dev.1` tag and creates its GitHub Release — do not tag by hand. A local build (`--skip-upload`) records nothing on GitHub. `verify` remains available for later diagnostics without rebuilding, and `release github <version>` re-records the GitHub side alone.

Rules enforced by the publisher:
1. Release preparation requires a clean, current `main`. Publishing requires the same state. Source state is checked again immediately before upload.
2. A **stable** version must have a changelog entry at `internal/app/releasenotes/changelog/<version>.md`. It is checked before anything is built, because the notes are embedded in the binary and an entry written afterwards would describe a release that cannot display it (ADR release-notes-ship-inside-the-binary). Create it by promoting the accumulated draft — `mise run changelog:promote -- <stable|version>` — and commit the result before releasing. A prerelease is not gated: it publishes `next.md` as it stands, including nothing.
3. SQLite migrations must be contiguous, and every migration present in the latest reachable `desktop-v*` tag must remain at the same path with the same contents. Both `prepare` and `publish` run `scripts/check-migration-order.sh` before release work begins.
4. The first prerelease identifier routes the channel (`-dev.N` → dev, `-beta.N` → beta, none → stable; any other identifier is rejected).
5. `latest.json` is written for the target channel **and cascades to less-stable channels** (stable → stable+beta+dev; beta → beta+dev; dev → dev only).
6. `releases/<semver>/` is immutable. A normal re-publish rejects any existing object. Resume reuses one only after downloading it and proving it is byte-identical to the verified local artifact; `--force` remains a separate manual override and cannot be combined with `--resume`.
7. After the artifacts are live and verified, the `desktop-v<semver>` tag is pushed and its GitHub Release created; an existing tag or release pointing at another commit is a conflict, and one already at the release commit is left untouched.

### Recovering an interrupted publish

R2 HEAD, GET, and PUT calls retry bounded transient curl failures, including connection, TLS, and partial-transfer errors. If those retries are exhausted after the build finished, keep `desktop/bin` intact and resume the same version:

```bash
mise run release:publish -- <version> --resume
```

Resume does not deploy the web worker, rebuild, sign, or submit anything to Apple. It loads the four versioned artifacts already in `desktop/bin`, verifies both macOS signatures and stapled tickets, checks both Linux archive shapes, confirms every binary carries the current release commit, and reconstructs `SHA256SUMS`. It downloads every object already present under the release prefix and reuses it only when its bytes match the local artifact, uploads missing objects, and completes any channel manifests not already written. A manifest already on the version must have identical notes and artifact metadata; a conflicting or newer manifest stops recovery.

Do not cut a replacement version for a partial upload, and do not use `--force`: both discard the verified build that resume exists to preserve. If `desktop/bin` was deleted or any local artifact differs from an already-uploaded object, the interrupted version cannot be resumed safely. If R2 and the manifests are already live and only the final GitHub step failed, use `go run ./cmd/release github <version>` instead.

## Installing on macOS

Open `Hive-<ver>-darwin-universal.dmg` and drag Hive onto the `/Applications` shortcut in the window. Gatekeeper is satisfied without a right-click-to-open dance: the image carries its own notarization ticket, and so does the app inside it, so the copy in `/Applications` validates even offline.

Installing into `/Applications` is what the in-app updater expects — an app left in `~/Downloads` is subject to translocation, where the running bundle is a read-only mount the updater cannot replace. The [install script](#install-script) puts the app in the same place without the DMG, and is the path invites point at.

## Installing on Linux

Linux ships a plain tarball, deliberately — no `.deb`, `.rpm`, AppImage, or repository (decision [linux-tarball-distribution](decisions/2026-07-27-linux-tarball-distribution.md)). Download it, verify it, and put the binary somewhere on `PATH` **that your user owns**, which is what makes in-app updates work:

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
- `gh` authentication (`gh auth status`) — the maintainer's own GitHub login, used to create the GitHub Release; the tag push uses `git`'s configured push credentials. Not a repo secret. `publish` checks it in preflight so a missing login aborts before the upload.
