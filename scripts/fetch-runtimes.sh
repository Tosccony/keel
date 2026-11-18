#!/usr/bin/env bash
# Downloads the runtimes Keel ships (MariaDB, Apache, PHP, phpMyAdmin) for
# one system from the releases the MariaDB and Web workflows publish,
# checks each against the checksum pinned in packaging/*/*.env, and unpacks
# them.
#
# Result: DEST/mariadb, DEST/apache, DEST/php, DEST/phpmyadmin
# Usage: scripts/fetch-runtimes.sh windows-x86_64|macos-universal|linux-x86_64 DEST
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PLATFORM="${1:?usage: fetch-runtimes.sh PLATFORM DEST}"
DEST="${2:?usage: fetch-runtimes.sh PLATFORM DEST}"
REPO="${GITHUB_REPOSITORY:-Tosccony/keel}"
# shellcheck source=../packaging/mariadb/mariadb.env
source "$ROOT/packaging/mariadb/mariadb.env"
# shellcheck source=../packaging/web/web.env
source "$ROOT/packaging/web/web.env"
# shellcheck source=../packaging/phpmyadmin/phpmyadmin.env
source "$ROOT/packaging/phpmyadmin/phpmyadmin.env"

WORK="$ROOT/dist/runtime-downloads"
mkdir -p "$WORK" "$DEST"
sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
# get TAG ASSET SHA256: download a release asset once and check it against
# its pinned checksum. The release's own SHA256SUMS.txt isn't trusted:
# whoever could replace an archive there could replace the sums too.
get() {
  local tag="$1" asset="$2" want="$3" base="https://github.com/$REPO/releases/download/$1"
  if [ -z "$want" ]; then
    echo "fetch-runtimes: no pinned checksum for $asset; copy it from $tag's SHA256SUMS.txt into packaging/" >&2
    exit 1
  fi
  mkdir -p "$WORK/$tag"
  if [ ! -f "$WORK/$tag/$asset" ]; then
    curl -fsSL --retry 3 -o "$WORK/$tag/$asset.part" "$base/$asset" ||
      { echo "fetch-runtimes: $asset isn't in release $tag (run its workflow with publish ticked)" >&2; exit 1; }
    mv "$WORK/$tag/$asset.part" "$WORK/$tag/$asset"
  fi
  if [ "$(sha256 "$WORK/$tag/$asset")" != "$want" ]; then
    echo "fetch-runtimes: $asset doesn't match its pinned checksum" >&2
    exit 1
  fi
  echo "$WORK/$tag/$asset"
}
# pin NAME: the value of the .env variable NAME, or "" if it isn't set.
pin() { echo "${!1:-}"; }

WEB_TAG="web-$HTTPD_VERSION-php$PHP_VERSION"
for part in mariadb apache php phpmyadmin; do rm -rf "${DEST:?}/$part"; done
# Each download is its own assignment so a failed one stops the script
# (an exit inside $(...) only ends the subshell).
P="${PLATFORM//-/_}" # windows_x86_64, as in the .env names
mariadb="$(get "mariadb-$MARIADB_VERSION" "mariadb-$MARIADB_VERSION-$PLATFORM.tar.gz" "$(pin "MARIADB_RUNTIME_SHA256_$P")")"
apache="$(get "$WEB_TAG" "apache-$HTTPD_VERSION-$PLATFORM.tar.gz" "$(pin "APACHE_RUNTIME_SHA256_$P")")"
php="$(get "$WEB_TAG" "php-$PHP_VERSION-$PLATFORM.tar.gz" "$(pin "PHP_RUNTIME_SHA256_$P")")"
pma="$(get "$WEB_TAG" "phpmyadmin-$PHPMYADMIN_VERSION.tar.gz" "$(pin PHPMYADMIN_RUNTIME_SHA256)")"
for archive in "$mariadb" "$apache" "$php" "$pma"; do tar -xzf "$archive" -C "$DEST"; done
for part in mariadb apache php phpmyadmin; do
  [ -d "$DEST/$part" ] || { echo "fetch-runtimes: $part is missing after unpacking" >&2; exit 1; }
done
du -sh "$DEST"/*
