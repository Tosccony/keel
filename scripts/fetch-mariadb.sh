#!/usr/bin/env bash
# Downloads MariaDB's official Windows or Linux build (version and checksum
# in packaging/mariadb/mariadb.env), checks it, and trims it to what Keel
# uses (scripts/trim-mariadb.sh). macOS has no official build; see
# scripts/build-mariadb.sh.
#
# Result: dist/mariadb/<os>-x86_64/mariadb
# Usage: scripts/fetch-mariadb.sh windows|linux
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/mariadb/mariadb.env
source "$ROOT/packaging/mariadb/mariadb.env"
OS="${1:?usage: fetch-mariadb.sh windows|linux}"
V="$MARIADB_VERSION"

case "$OS" in
  windows)
    NAME="mariadb-$V-winx64"
    FILE="$NAME.zip"
    URL="https://archive.mariadb.org/mariadb-$V/winx64-packages/$FILE"
    SHA="$MARIADB_WINDOWS_SHA256"
    ;;
  linux)
    NAME="mariadb-$V-linux-systemd-x86_64"
    FILE="$NAME.tar.gz"
    URL="https://archive.mariadb.org/mariadb-$V/bintar-linux-systemd-x86_64/$FILE"
    SHA="$MARIADB_LINUX_SHA256"
    ;;
  *)
    echo "fetch-mariadb: unknown system $OS (want windows or linux)" >&2
    exit 2
    ;;
esac

WORK="$ROOT/dist/mariadb-build/$OS-x86_64"
mkdir -p "$WORK"
if [ ! -f "$WORK/$FILE" ]; then
  curl -fsSL --retry 3 -o "$WORK/$FILE.part" "$URL"
  mv "$WORK/$FILE.part" "$WORK/$FILE"
fi

if command -v sha256sum >/dev/null; then
  got="$(sha256sum "$WORK/$FILE" | cut -d' ' -f1)"
else
  got="$(shasum -a 256 "$WORK/$FILE" | cut -d' ' -f1)"
fi
if [ "$got" != "$SHA" ]; then
  echo "fetch-mariadb: $FILE has checksum $got, expected $SHA" >&2
  exit 1
fi

rm -rf "$WORK/unpacked"
mkdir -p "$WORK/unpacked"
case "$FILE" in
  *.zip)
    if command -v unzip >/dev/null; then
      unzip -q "$WORK/$FILE" -d "$WORK/unpacked"
    else
      7z x -bd -y -o"$WORK/unpacked" "$WORK/$FILE" >/dev/null # Windows runners have 7-Zip
    fi
    ;;
  *) tar -xzf "$WORK/$FILE" -C "$WORK/unpacked" \
      --exclude='*/mariadb-test/*' --exclude='*/mysql-test/*' --exclude='*/include/*' ;;
esac

"$ROOT/scripts/trim-mariadb.sh" "$WORK/unpacked/$NAME" "$ROOT/dist/mariadb/$OS-x86_64/mariadb"
echo "Ready: dist/mariadb/$OS-x86_64/mariadb"
