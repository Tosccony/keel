#!/usr/bin/env bash
# Downloads the runtimes Keel ships (MariaDB, Apache, PHP, phpMyAdmin) for
# one system from the releases the MariaDB and Web workflows publish,
# checks each against its release's SHA256SUMS.txt, and unpacks them.
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
# get TAG ASSET: download a release asset once and check it against the
# release's SHA256SUMS.txt.
get() {
  local tag="$1" asset="$2" base="https://github.com/$REPO/releases/download/$1"
  mkdir -p "$WORK/$tag"
  if [ ! -f "$WORK/$tag/SHA256SUMS.txt" ]; then
    curl -fsSL --retry 3 -o "$WORK/$tag/SHA256SUMS.txt" "$base/SHA256SUMS.txt" ||
      { echo "fetch-runtimes: release $tag isn't published (run its workflow with publish ticked)" >&2; exit 1; }
  fi
  if [ ! -f "$WORK/$tag/$asset" ]; then
    curl -fsSL --retry 3 -o "$WORK/$tag/$asset.part" "$base/$asset"
    mv "$WORK/$tag/$asset.part" "$WORK/$tag/$asset"
  fi
  local want
  want="$(awk -v f="$asset" '$2 == f || $2 == "*"f {print $1}' "$WORK/$tag/SHA256SUMS.txt")"
  if [ -z "$want" ] || [ "$(sha256 "$WORK/$tag/$asset")" != "$want" ]; then
    echo "fetch-runtimes: $asset doesn't match $tag's SHA256SUMS.txt" >&2
    exit 1
  fi
  echo "$WORK/$tag/$asset"
}

WEB_TAG="web-$HTTPD_VERSION-php$PHP_VERSION"
for part in mariadb apache php phpmyadmin; do rm -rf "${DEST:?}/$part"; done
# Each download is its own assignment so a failed one stops the script
# (an exit inside $(...) only ends the subshell).
mariadb="$(get "mariadb-$MARIADB_VERSION" "mariadb-$MARIADB_VERSION-$PLATFORM.tar.gz")"
apache="$(get "$WEB_TAG" "apache-$HTTPD_VERSION-$PLATFORM.tar.gz")"
php="$(get "$WEB_TAG" "php-$PHP_VERSION-$PLATFORM.tar.gz")"
pma="$(get "$WEB_TAG" "phpmyadmin-$PHPMYADMIN_VERSION.tar.gz")"
for archive in "$mariadb" "$apache" "$php" "$pma"; do tar -xzf "$archive" -C "$DEST"; done
for part in mariadb apache php phpmyadmin; do
  [ -d "$DEST/$part" ] || { echo "fetch-runtimes: $part is missing after unpacking" >&2; exit 1; }
done
du -sh "$DEST"/*
