#!/usr/bin/env sh
# Prints the HIVE_DATA_DIR a dev launch should use: an ephemeral per-worktree
# copy of the installed release's databases. A dev build running unmerged
# migrations or buggy write paths must never mutate the real install's data.
#
# Rules:
# - An explicit HIVE_DATA_DIR is printed unchanged and nothing is copied —
#   operator intent wins, the same precedence ApplyBootstrap() follows. This is
#   also the opt-out for deliberately running dev against real data:
#   HIVE_DATA_DIR="$HOME/.local/share/hive" mise run desktop:dev
# - Otherwise the release data dir is resolved the way the app resolves it
#   (bootstrap.yaml data_dir override, then XDG_DATA_HOME, then ~/.local/share)
#   and its databases are snapshotted into a cache dir keyed to this worktree,
#   so parallel worktrees each get their own copy. The copy is remade on every
#   launch: dev always starts from the install's current state, and a second
#   concurrent launch from the *same* worktree resets the first one's copy.
#
# Only the databases and the small desktop state files are copied. The data dir
# root also holds repos/, context/, and logs/ trees that are large and not
# needed to render a realistic feed — and dev-launched sessions cloning into
# the copy's repos/ is exactly the isolation this exists for.
set -eu

if [ -n "${HIVE_DATA_DIR:-}" ]; then
  printf '%s\n' "${HIVE_DATA_DIR}"
  exit 0
fi

# Source data dir: bootstrap.yaml pointer first (mirrors ApplyBootstrap), then
# the XDG default. yaml.Marshal only quotes strings when needed; strip either
# quote style.
bootstrap="${XDG_CONFIG_HOME:-$HOME/.config}/hive/desktop/bootstrap.yaml"
src=""
if [ -f "${bootstrap}" ]; then
  src="$(sed -n 's/^data_dir:[[:space:]]*//p' "${bootstrap}" | head -n 1)"
  src="${src#\"}"; src="${src%\"}"
  src="${src#\'}"; src="${src%\'}"
fi
if [ -z "${src}" ]; then
  src="${XDG_DATA_HOME:-$HOME/.local/share}/hive"
fi

worktree="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
key="$(printf '%s' "${worktree}" | cksum | awk '{print $1}')"
dest="${XDG_CACHE_HOME:-$HOME/.cache}/hive/desktop-dev/$(basename "${worktree}")-${key}"
marker="${dest}/.hive-dev-data"

if [ -e "${dest}" ]; then
  if [ ! -f "${marker}" ]; then
    echo "error: ${dest} exists but has no .hive-dev-data marker; refusing to delete it" >&2
    exit 1
  fi
  rm -rf "${dest}"
fi
mkdir -p "${dest}/desktop"
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

copy_db "${src}/hive.db" "${dest}/hive.db"
copy_db "${src}/desktop/desktop-pipeline.db" "${dest}/desktop/desktop-pipeline.db"

# Small per-app state (read state, legacy profiles) rides along so the dev
# feed shows the install's read/unread marks.
for f in "${src}/desktop/"*.json; do
  if [ -f "${f}" ]; then cp -p "${f}" "${dest}/desktop/"; fi
done

echo "dev data dir: ephemeral copy of ${src}" >&2
printf '%s\n' "${dest}"
