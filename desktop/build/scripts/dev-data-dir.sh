#!/usr/bin/env sh
# Emits eval-able `export` lines pointing a dev launch at an ephemeral
# per-worktree copy of the installed release's data dir AND desktop config
# tree. A dev build running unmerged migrations or buggy write paths must
# never mutate the real install's state — and because both apps hot-reload
# the config tree (flows, actions.yml, settings.yaml), sharing it means an
# edit or delete in dev instantly applies to the release app too.
#
# Usage: eval "$(./build/scripts/dev-data-dir.sh)"
#
# Rules:
# - An explicit HIVE_DATA_DIR / HIVE_DESKTOP_CONFIG is respected: that
#   override is neither copied nor re-exported — operator intent wins, the
#   same precedence ApplyBootstrap() follows. Setting both is the opt-out for
#   deliberately running dev against real state:
#   HIVE_DATA_DIR="$HOME/.local/share/hive" \
#   HIVE_DESKTOP_CONFIG="$HOME/.config/hive/desktop/profiles.yaml" mi desktop:dev
# - Otherwise the release's dirs are resolved the way the app resolves them
#   (bootstrap.yaml overrides, then XDG defaults) and snapshotted into a cache
#   dir keyed to this worktree, so parallel worktrees each get their own copy.
#   The copy is remade on every launch: dev always starts from the install's
#   current state, and a second concurrent launch from the *same* worktree
#   resets the first one's copy.
#
# Only the databases, small desktop state files, and the config tree are
# copied. The data dir root also holds repos/, context/, and logs/ trees that
# are large and not needed to render a realistic feed — and dev-launched
# sessions cloning into the copy's repos/ is exactly the isolation this
# exists for.
#
# Not covered: the OS keychain (dev signs in with the real GitHub token; a
# sign-out in dev deletes the real token) and bootstrap.yaml itself
# (BootstrapPath() is fixed to the default XDG location, so the System
# settings directory-override screen in dev writes the real pointer file).
set -eu

if [ -n "${HIVE_DATA_DIR:-}" ] && [ -n "${HIVE_DESKTOP_CONFIG:-}" ]; then
  echo "dev data dir: HIVE_DATA_DIR and HIVE_DESKTOP_CONFIG set; using real state" >&2
  exit 0
fi

# Single-quote a value for the emitted `export` lines.
sq() { printf %s "$1" | sed "s/'/'\\\\''/g"; }

# Release dir resolution: bootstrap.yaml pointer first (mirrors
# ApplyBootstrap), then the XDG default. yaml.Marshal only quotes strings
# when needed; strip either quote style.
bootstrap="${XDG_CONFIG_HOME:-$HOME/.config}/hive/desktop/bootstrap.yaml"
bootstrap_field() {
  [ -f "${bootstrap}" ] || return 0
  val="$(sed -n "s/^$1:[[:space:]]*//p" "${bootstrap}" | head -n 1)"
  val="${val#\"}"; val="${val%\"}"
  val="${val#\'}"; val="${val%\'}"
  printf %s "${val}"
}
src_data="$(bootstrap_field data_dir)"
[ -n "${src_data}" ] || src_data="${XDG_DATA_HOME:-$HOME/.local/share}/hive"
src_config="$(bootstrap_field config_dir)"
[ -n "${src_config}" ] || src_config="${XDG_CONFIG_HOME:-$HOME/.config}/hive/desktop"

worktree="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
key="$(printf '%s' "${worktree}" | cksum | awk '{print $1}')"
root="${XDG_CACHE_HOME:-$HOME/.cache}/hive/desktop-dev/$(basename "${worktree}")-${key}"
marker="${root}/.hive-dev-data"

if [ -e "${root}" ]; then
  if [ ! -f "${marker}" ]; then
    echo "error: ${root} exists but has no .hive-dev-data marker; refusing to delete it" >&2
    exit 1
  fi
  rm -rf "${root}"
fi
mkdir -p "${root}/data/desktop" "${root}/config"
: >"${marker}"

# Snapshot one SQLite database. VACUUM INTO takes a WAL read snapshot, so the
# copy is consistent even while the release app is writing; the raw-copy
# fallback must bring the -wal/-shm sidecars along or recent writes are lost.
copy_db() {
  src_db="$1"
  dst_db="$2"
  [ -f "${src_db}" ] || return 0
  if command -v sqlite3 >/dev/null 2>&1; then
    dst_sql="$(printf '%s' "${dst_db}" | sed "s/'/''/g")"
    if sqlite3 -cmd '.timeout 5000' "${src_db}" "VACUUM INTO '${dst_sql}'"; then
      return 0
    fi
    echo "warn: VACUUM INTO failed for ${src_db}; falling back to a file copy" >&2
    rm -f "${dst_db}"
  fi
  cp "${src_db}" "${dst_db}"
  for ext in -wal -shm; do
    if [ -f "${src_db}${ext}" ]; then cp "${src_db}${ext}" "${dst_db}${ext}"; fi
  done
}

if [ -z "${HIVE_DATA_DIR:-}" ]; then
  copy_db "${src_data}/hive.db" "${root}/data/hive.db"
  copy_db "${src_data}/desktop/desktop-pipeline.db" "${root}/data/desktop/desktop-pipeline.db"
  # Small per-app state (read state, legacy profiles) rides along so the dev
  # feed shows the install's read/unread marks.
  for f in "${src_data}/desktop/"*.json; do
    if [ -f "${f}" ]; then cp -p "${f}" "${root}/data/desktop/"; fi
  done
  echo "dev data dir: ephemeral copy of ${src_data}" >&2
  printf "export HIVE_DATA_DIR='%s/data'\n" "$(sq "${root}")"
fi

if [ -z "${HIVE_DESKTOP_CONFIG:-}" ]; then
  if [ -d "${src_config}" ]; then
    cp -Rp "${src_config}/." "${root}/config/"
    # The copied pointer file is inert (BootstrapPath ignores overrides);
    # drop it so the copy doesn't look like it re-points anywhere.
    rm -f "${root}/config/bootstrap.yaml"
  fi
  echo "dev config dir: ephemeral copy of ${src_config}" >&2
  printf "export HIVE_DESKTOP_CONFIG='%s/config/profiles.yaml'\n" "$(sq "${root}")"
fi
