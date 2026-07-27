#!/usr/bin/env bash
#
# Hive desktop installer
#
#     curl -fsSL https://hivedesktop.com/install/a1c6d523f7a3d06eed1e7b43/install.sh | bash
#
# The path token keeps this out of casual discovery while the beta is private;
# it is obscurity, not authentication (anyone with the link can fetch it). Drop
# the `| bash` to read this first.
#
# Detects your OS + CPU, pulls the channel's latest build, verifies its SHA-256
# against the published manifest, installs the app, and symlinks `hive` onto
# your PATH. macOS is the only platform during the private beta; Linux is wired
# up but only installs once linux builds are published.
#
# Channel defaults to stable. Track another by passing the flag through bash:
#
#     curl -fsSL <url> | bash -s -- --channel dev
#
# or set `HIVE_CHANNEL`. `HIVE_BIN_DIR` sets the `hive` symlink directory.
#
set -euo pipefail

DL_BASE="${HIVE_DL_BASE:-https://dl.hivedesktop.com}"
CHANNEL="${HIVE_CHANNEL:-stable}"
APP_NAME="Hive.app"
APP_EXE="hive-desktop" # CFBundleExecutable inside Hive.app / linux binary name

if [ -t 1 ]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; RED=$'\033[31m'; GRN=$'\033[32m'; RST=$'\033[0m'
else
  BOLD=; DIM=; RED=; GRN=; RST=
fi

say() { printf '%s %s\n' "${DIM}hive${RST}" "$*"; }
ok()  { printf '%s %s\n' "${GRN}✓${RST}" "$*"; }
die() { printf '%s %s\n' "${RED}✗${RST}" "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "this installer needs \`$1\` on your PATH"; }

# `bash -s -- --channel dev` passes the flag through to here; it wins over the
# HIVE_CHANNEL env var.
parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --channel) [ $# -ge 2 ] || die "--channel needs a value"; CHANNEL="$2"; shift 2 ;;
      --channel=*) CHANNEL="${1#*=}"; shift ;;
      -h | --help)
        printf 'usage: install.sh [--channel stable|beta|dev]\n'
        exit 0 ;;
      *) die "unknown option: $1 (try --help)" ;;
    esac
  done
}

detect_platform() {
  case "$(uname -s)" in
    Darwin) OS=darwin ;;
    Linux)  OS=linux ;;
    *) die "unsupported OS: $(uname -s). Hive ships for macOS and Linux" ;;
  esac
  case "$(uname -m)" in
    arm64 | aarch64) ARCH=arm64 ;;
    x86_64 | amd64)  ARCH=amd64 ;;
    *) die "unsupported CPU: $(uname -m)" ;;
  esac
}

# Candidate manifest platform keys, best match first.
platform_keys() {
  if [ "$OS" = darwin ]; then
    printf '%s\n' "darwin-universal" "darwin-$ARCH"
  else
    printf '%s\n' "linux-$ARCH"
    [ "$ARCH" = amd64 ] && printf '%s\n' "linux-x86_64"
  fi
}

# Reads $MANIFEST (indented JSON) and prints one field of one platform block.
json_field() { # $1=platform-key  $2=field
  printf '%s\n' "$MANIFEST" | awk -F'"' -v key="$1" -v field="$2" '
    $2 == "platforms" { in_p = 1 }
    in_p && $2 == key { in_b = 1; next }
    in_b && $2 == field { print $4; exit }
    in_b && /}/ { in_b = 0 }'
}

resolve_from_channel() {
  local url="$DL_BASE/desktop/channels/$CHANNEL/latest.json"
  MANIFEST="$(curl -fsSL --proto '=https' --tlsv1.2 "$url")" \
    || die "couldn't fetch the $CHANNEL manifest ($url)"
  VERSION="$(printf '%s\n' "$MANIFEST" | awk -F'"' '/"version":/ { print $4; exit }')"
  local key
  for key in $(platform_keys); do
    ART_URL="$(json_field "$key" url)"
    if [ -n "$ART_URL" ]; then
      ART_SHA="$(json_field "$key" sha256)"
      PLATFORM="$key"
      return
    fi
  done
  die "the $CHANNEL channel has no ${OS}-${ARCH} build yet. macOS is the only platform during the private beta"
}

verify_sha() {
  [ -n "$ART_SHA" ] || die "the manifest has no checksum for $PLATFORM"
  local actual
  if command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$PKG" | awk '{ print $1 }')"
  elif command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$PKG" | awk '{ print $1 }')"
  else
    die "need \`shasum\` or \`sha256sum\` to verify the download"
  fi
  [ "$actual" = "$ART_SHA" ] || die "checksum mismatch: expected $ART_SHA, got $actual"
}

install_darwin() {
  need unzip
  unzip -q "$PKG" -d "$WORK/x" || die "failed to unzip the download"
  [ -d "$WORK/x/$APP_NAME" ] || die "archive did not contain $APP_NAME"
  local dest="/Applications"
  [ -w "$dest" ] || { dest="$HOME/Applications"; mkdir -p "$dest"; }
  say "installing to ${BOLD}$dest/$APP_NAME${RST}…"
  rm -rf "${dest:?}/${APP_NAME:?}"
  if command -v ditto >/dev/null 2>&1; then
    ditto "$WORK/x/$APP_NAME" "$dest/$APP_NAME"
  else
    cp -R "$WORK/x/$APP_NAME" "$dest/$APP_NAME"
  fi
  APP_PATH="$dest/$APP_NAME"
  EXE_PATH="$APP_PATH/Contents/MacOS/$APP_EXE"
}

install_linux() {
  need tar
  local dir="${HIVE_HOME:-$HOME/.local/share/hive}"
  mkdir -p "$dir"
  tar -xzf "$PKG" -C "$dir" || die "failed to extract the download"
  EXE_PATH="$dir/$APP_EXE"
  [ -x "$EXE_PATH" ] || EXE_PATH="$(find "$dir" -maxdepth 2 -type f -name "$APP_EXE" | head -1)"
  [ -n "$EXE_PATH" ] || die "couldn't find the $APP_EXE binary in the archive"
  chmod +x "$EXE_PATH"
  APP_PATH="$dir"
}

link_cli() {
  local bindir="${HIVE_BIN_DIR:-}"
  if [ -z "$bindir" ]; then
    if [ -w /usr/local/bin ]; then bindir="/usr/local/bin"; else bindir="$HOME/.local/bin"; fi
  fi
  mkdir -p "$bindir"
  ln -sf "$EXE_PATH" "$bindir/hive"
  BIN_DIR="$bindir"
  case ":$PATH:" in
    *":$bindir:"*) ON_PATH=1 ;;
    *) ON_PATH=0 ;;
  esac
}

print_next() {
  echo
  ok "installed ${BOLD}Hive $VERSION${RST} → $APP_PATH"
  ok "linked ${BOLD}$BIN_DIR/hive${RST}"
  echo
  if [ "$ON_PATH" != 1 ]; then
    say "add it to your PATH: ${BOLD}export PATH=\"$BIN_DIR:\$PATH\"${RST}"
  fi
  if [ "$OS" = darwin ]; then
    say "launch it: ${BOLD}open -a Hive${RST}  ·  or run ${BOLD}hive${RST}"
  else
    say "launch it: ${BOLD}hive${RST}"
  fi
  say "next: connect GitHub, then build your first feed."
}

main() {
  parse_args "$@"
  need curl
  need awk
  detect_platform
  say "installing Hive for ${BOLD}${OS}-${ARCH}${RST}…"
  resolve_from_channel
  say "release ${BOLD}$VERSION${RST} · $PLATFORM · $CHANNEL"
  WORK="$(mktemp -d)"
  trap 'rm -rf "$WORK"' EXIT
  PKG="$WORK/pkg"
  say "downloading…"
  curl -fSL --proto '=https' --tlsv1.2 -o "$PKG" "$ART_URL" || die "download failed: $ART_URL"
  verify_sha
  ok "checksum verified"
  if [ "$OS" = darwin ]; then install_darwin; else install_linux; fi
  link_cli
  print_next
}

main "$@"
