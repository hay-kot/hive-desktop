#!/usr/bin/env bash
# release-desktop.sh — build, sign, notarize, and publish the desktop app to R2.
#
# Usage:
#   ./scripts/release/release-desktop.sh <version> [--skip-notarize] [--skip-upload] [--force]
#
#   <version>   full semver. The prerelease identifier routes the channel:
#                 1.4.0        -> stable (manifests: stable, beta, dev)
#                 1.4.0-beta.1 -> beta   (manifests: beta, dev)
#                 1.4.0-dev.1  -> dev    (manifests: dev)
#               anything other than dev/beta is rejected (docs/decisions/0004).
#
# Secrets come from the environment; locally mise loads the gitignored
# repo-root .env (run via `mise run release:desktop`). Required variables:
#   MACOS_CERTIFICATE       base64-encoded Developer ID Application .p12
#   MACOS_CERTIFICATE_PWD   password for that .p12
#   MACOS_SIGN_IDENTITY     e.g. "Developer ID Application: Acme (TEAMID)"
#   AC_API_KEY              base64-encoded App Store Connect API .p8 key
#   AC_API_KEY_ID           App Store Connect API key id
#   AC_API_ISSUER_ID        App Store Connect API issuer id
#   R2_ACCESS_KEY_ID        R2 S3 credential (Object Read & Write on the bucket)
#   R2_SECRET_ACCESS_KEY    R2 S3 credential secret
# Optional (defaults for this project):
#   R2_BUCKET       (hive-desktop-releases)
#   R2_ACCOUNT_ID   (bce6b95e4e84d92b1972d3b55b6cfaf6)
#   DL_BASE_URL     (https://dl.hivedesktop.com)
#
# The script never prints secret values. Artifacts under releases/<version>/
# are immutable: re-publishing an existing version requires --force.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# ---- arguments --------------------------------------------------------------
VERSION="" SKIP_NOTARIZE=0 SKIP_UPLOAD=0 FORCE=0
for arg in "$@"; do
  case "$arg" in
    --skip-notarize) SKIP_NOTARIZE=1 ;;
    --skip-upload) SKIP_UPLOAD=1 ;;
    --force) FORCE=1 ;;
    -*) echo "unknown flag: $arg" >&2; exit 2 ;;
    *) VERSION="$arg" ;;
  esac
done
[[ -n "$VERSION" ]] || { echo "usage: $0 <version> [--skip-notarize] [--skip-upload] [--force]" >&2; exit 2; }

if [[ "$VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  CHANNEL="stable"
elif [[ "$VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+)-(dev|beta)\.[0-9A-Za-z.]+$ ]]; then
  CHANNEL="${BASH_REMATCH[2]}"
else
  echo "invalid version '$VERSION': expected X.Y.Z or X.Y.Z-(dev|beta).N" >&2
  exit 2
fi
BASE_VERSION="${VERSION%%-*}"
case "$CHANNEL" in
  stable) CHANNELS=(stable beta dev) ;;
  beta) CHANNELS=(beta dev) ;;
  dev) CHANNELS=(dev) ;;
esac

# ---- environment ------------------------------------------------------------
# Secrets arrive via the environment. Locally, mise loads the gitignored
# repo-root .env (mise.toml: `_.file`); run this through `mise run
# release:desktop`. CI provides the same variables as workflow secrets.

R2_BUCKET="${R2_BUCKET:-hive-desktop-releases}"
R2_ACCOUNT_ID="${R2_ACCOUNT_ID:-bce6b95e4e84d92b1972d3b55b6cfaf6}"
DL_BASE_URL="${DL_BASE_URL:-https://dl.hivedesktop.com}"
R2_ENDPOINT="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"

missing=()
require() { for v in "$@"; do [[ -n "${!v:-}" ]] || missing+=("$v"); done; }
require MACOS_CERTIFICATE MACOS_CERTIFICATE_PWD MACOS_SIGN_IDENTITY
[[ $SKIP_NOTARIZE = 1 ]] || require AC_API_KEY AC_API_KEY_ID AC_API_ISSUER_ID
[[ $SKIP_UPLOAD = 1 ]] || require R2_ACCESS_KEY_ID R2_SECRET_ACCESS_KEY
if ((${#missing[@]})); then
  printf 'missing required environment variables:\n' >&2
  printf '  %s\n' "${missing[@]}" >&2
  exit 2
fi

command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
if [[ $SKIP_UPLOAD = 0 ]]; then
  curl --help all 2>/dev/null | grep -q "aws-sigv4" || { echo "curl with --aws-sigv4 support is required (curl >= 7.86)" >&2; exit 2; }
fi

echo "==> releasing $VERSION (channel: $CHANNEL -> manifests: ${CHANNELS[*]})"

# ---- cleanup ----------------------------------------------------------------
WORK="$(mktemp -d /tmp/hive-desktop-release.XXXXXX)"
KEYCHAIN_PATH="" ORIGINAL_KEYCHAINS=""
cleanup() {
  if [[ -n "$KEYCHAIN_PATH" ]]; then
    # shellcheck disable=SC2086
    security list-keychains -d user -s $ORIGINAL_KEYCHAINS 2>/dev/null || true
    security delete-keychain "$KEYCHAIN_PATH" 2>/dev/null || true
  fi
  rm -rf "$WORK"
  git checkout -q -- desktop/build/darwin/Info.plist 2>/dev/null || true
}
trap cleanup EXIT

# ---- build ------------------------------------------------------------------
# Info.plist carries the bare X.Y.Z (Apple's expected format); the full semver
# including the channel identifier is stamped into the binary and manifest.
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $BASE_VERSION" desktop/build/darwin/Info.plist
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BASE_VERSION" desktop/build/darwin/Info.plist

echo "==> building universal .app"
(
  cd desktop
  HIVE_DESKTOP_VERSION="$VERSION" \
  HIVE_DESKTOP_COMMIT="$(git rev-parse HEAD)" \
  HIVE_DESKTOP_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    mise x -- wails3 task darwin:package:universal
)

APP="desktop/bin/hive-desktop.app"
[[ -d "$APP" ]] || { echo "build did not produce $APP" >&2; exit 1; }

# ---- sign -------------------------------------------------------------------
echo "==> importing Developer ID certificate into an ephemeral keychain"
KEYCHAIN_PATH="$WORK/signing.keychain-db"
KEYCHAIN_PWD="$(openssl rand -base64 24)"
ORIGINAL_KEYCHAINS="$(security list-keychains -d user | tr -d '"')"

printf '%s' "$MACOS_CERTIFICATE" | base64 --decode > "$WORK/cert.p12"
security create-keychain -p "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 3600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"
security import "$WORK/cert.p12" -P "$MACOS_CERTIFICATE_PWD" -k "$KEYCHAIN_PATH" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$KEYCHAIN_PWD" "$KEYCHAIN_PATH" >/dev/null
# shellcheck disable=SC2086
security list-keychains -d user -s "$KEYCHAIN_PATH" $ORIGINAL_KEYCHAINS
rm -f "$WORK/cert.p12"

echo "==> codesigning (Developer ID, hardened runtime)"
codesign --force --deep --timestamp --options runtime \
  --entitlements desktop/build/darwin/entitlements.plist \
  --sign "$MACOS_SIGN_IDENTITY" \
  "$APP"
codesign --verify --strict --verbose=2 "$APP"

# ---- notarize ---------------------------------------------------------------
if [[ $SKIP_NOTARIZE = 1 ]]; then
  echo "==> skipping notarization (--skip-notarize)"
else
  echo "==> notarizing"
  KEY_PATH="$WORK/ac_api_key.p8"
  printf '%s' "$AC_API_KEY" | base64 --decode > "$KEY_PATH"
  ditto -c -k --keepParent "$APP" "$WORK/notarize.zip"

  submit_json=$(xcrun notarytool submit "$WORK/notarize.zip" \
    --key "$KEY_PATH" --key-id "$AC_API_KEY_ID" --issuer "$AC_API_ISSUER_ID" \
    --output-format json)
  submission_id=$(jq -er '.id' <<<"$submit_json")
  echo "    submission: $submission_id"

  deadline=$((SECONDS + 7200))
  consecutive_errors=0
  status=""
  while ((SECONDS < deadline)); do
    if info_json=$(xcrun notarytool info "$submission_id" \
      --key "$KEY_PATH" --key-id "$AC_API_KEY_ID" --issuer "$AC_API_ISSUER_ID" \
      --output-format json 2>&1); then
      consecutive_errors=0
      status=$(jq -er '.status' <<<"$info_json")
      echo "    status: $status"
      case "$status" in
        Accepted) break ;;
        "In Progress") ;;
        Invalid | Rejected)
          xcrun notarytool log "$submission_id" \
            --key "$KEY_PATH" --key-id "$AC_API_KEY_ID" --issuer "$AC_API_ISSUER_ID" || true
          echo "notarization failed: $status" >&2
          exit 1
          ;;
        *)
          echo "unexpected notarization status: $status" >&2
          exit 1
          ;;
      esac
    else
      consecutive_errors=$((consecutive_errors + 1))
      echo "    status check failed ($consecutive_errors/10)" >&2
      ((consecutive_errors < 10)) || { echo "too many notarytool errors" >&2; exit 1; }
    fi
    sleep 30
  done
  [[ "$status" == Accepted ]] || { echo "timed out waiting for notarization" >&2; exit 1; }

  xcrun stapler staple "$APP"
  xcrun stapler validate "$APP"
fi

# ---- package ----------------------------------------------------------------
ZIP_NAME="Hive-${VERSION}-darwin-universal.zip"
echo "==> packaging $ZIP_NAME"
(
  cd desktop/bin
  ditto -c -k --keepParent hive-desktop.app "$ZIP_NAME"
  # SHA256SUMS is a manual-verification/audit sidecar; the in-app updater
  # verifies the sha256 published in the channel manifest.
  shasum -a 256 "$ZIP_NAME" > SHA256SUMS
  cat SHA256SUMS
)
ZIP_PATH="desktop/bin/$ZIP_NAME"
SHA256="$(awk '{print $1}' desktop/bin/SHA256SUMS)"
SIZE="$(stat -f%z "$ZIP_PATH")"

# ---- upload -----------------------------------------------------------------
if [[ $SKIP_UPLOAD = 1 ]]; then
  echo "==> skipping upload (--skip-upload); artifact at $ZIP_PATH"
  exit 0
fi

# S3-compatible calls via curl's native SigV4 signing; no AWS CLI needed.
r2_curl() {
  curl -fsS --aws-sigv4 "aws:amz:auto:s3" \
    --user "$R2_ACCESS_KEY_ID:$R2_SECRET_ACCESS_KEY" "$@"
}
r2_put() { # key file content-type cache-control
  r2_curl -X PUT -T "$2" -H "Content-Type: $3" -H "Cache-Control: $4" \
    "$R2_ENDPOINT/$R2_BUCKET/$1" >/dev/null
}
r2_exists() { r2_curl -o /dev/null -I "$R2_ENDPOINT/$R2_BUCKET/$1" 2>/dev/null; }

RELEASE_PREFIX="desktop/releases/$VERSION"
if [[ $FORCE = 0 ]] && r2_exists "$RELEASE_PREFIX/$ZIP_NAME"; then
  echo "release $VERSION already exists in the bucket (immutable); use --force to overwrite" >&2
  exit 1
fi

echo "==> uploading artifacts to r2://$R2_BUCKET/$RELEASE_PREFIX/"
r2_put "$RELEASE_PREFIX/$ZIP_NAME" "$ZIP_PATH" application/zip "public, max-age=31536000, immutable"
r2_put "$RELEASE_PREFIX/SHA256SUMS" desktop/bin/SHA256SUMS text/plain "public, max-age=31536000, immutable"

PUB_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
for ch in "${CHANNELS[@]}"; do
  echo "==> writing channel manifest: $ch"
  jq -n \
    --arg channel "$ch" \
    --arg version "$VERSION" \
    --arg pub_date "$PUB_DATE" \
    --arg url "$DL_BASE_URL/$RELEASE_PREFIX/$ZIP_NAME" \
    --arg sha256 "$SHA256" \
    --argjson size "$SIZE" \
    '{channel: $channel, version: $version, pub_date: $pub_date,
      platforms: {"darwin-universal": {url: $url, sha256: $sha256, size: $size}}}' \
    > "$WORK/latest-$ch.json"
  r2_put "desktop/channels/$ch/latest.json" "$WORK/latest-$ch.json" application/json "no-cache"
done

echo "==> verifying published manifest"
curl -fsS "$DL_BASE_URL/desktop/channels/$CHANNEL/latest.json" | jq .

cat <<DONE

Release $VERSION published to the $CHANNEL channel.
  artifact: $DL_BASE_URL/$RELEASE_PREFIX/$ZIP_NAME
  manifests updated: ${CHANNELS[*]}
DONE

# In CI the desktop-v tag already exists (it triggered the run); only suggest
# tagging for local releases that have not been tagged yet.
if ! git rev-parse -q --verify "refs/tags/desktop-v$VERSION" >/dev/null; then
  cat <<DONE

Tag the release commit:
  git tag desktop-v$VERSION && git push origin desktop-v$VERSION
DONE
fi
