#!/usr/bin/env bash
# Builds the PHP that Keel ships on macOS and Linux with static-php-cli
# (versions and checksums in packaging/web/web.env): php and php-fpm as
# single programs with every extension compiled in, built from the source
# of PHP and each library it uses. Neither system has an official PHP
# build Keel can carry.
#
# Result: dist/php/<os>-<arch>/php, holding php, php-fpm and the licences.
#
# Usage: scripts/build-php.sh [arch]   (default: this machine's)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/web/web.env
source "$ROOT/packaging/web/web.env"

case "$(uname -s)" in
  Darwin) OS=macos ;;
  Linux) OS=linux ;;
  *) echo "build-php: use fetch-web-windows.sh on Windows" >&2; exit 2 ;;
esac
ARCH="${1:-$(uname -m)}"
[ "$ARCH" = aarch64 ] && ARCH=arm64
SPC_ARCH="$ARCH"
[ "$ARCH" = arm64 ] && SPC_ARCH=aarch64
sumvar="SPC_SHA256_${OS}_${ARCH}"
SPC_SHA256="${!sumvar}"

WORK="$ROOT/dist/web-build/php-$OS-$ARCH"
OUT="$ROOT/dist/php/$OS-$ARCH/php"
mkdir -p "$WORK"
TARBALL="$WORK/spc-$OS-$SPC_ARCH.tar.gz"
if [ ! -f "$TARBALL" ]; then
  curl -fsSL --retry 3 -o "$TARBALL.part" \
    "https://github.com/crazywhalecc/static-php-cli/releases/download/$SPC_VERSION/spc-$OS-$SPC_ARCH.tar.gz"
  mv "$TARBALL.part" "$TARBALL"
fi
if command -v sha256sum >/dev/null; then got="$(sha256sum "$TARBALL" | cut -d' ' -f1)"; else got="$(shasum -a 256 "$TARBALL" | cut -d' ' -f1)"; fi
if [ "$got" != "$SPC_SHA256" ]; then
  echo "build-php: static-php-cli doesn't match its checksum" >&2
  exit 1
fi
tar -xzf "$TARBALL" -C "$WORK"
cd "$WORK"

# What XAMPP's PHP offers that a PHP course, Laravel, WordPress and
# phpMyAdmin use.
EXTS="bcmath,calendar,ctype,curl,dom,exif,fileinfo,filter,gd,iconv,intl,mbstring,mysqli,mysqlnd,opcache,openssl,pdo,pdo_mysql,pdo_sqlite,phar,session,simplexml,sockets,sodium,sqlite3,tokenizer,xml,xmlreader,xmlwriter,zip,zlib"
./spc doctor --auto-fix
./spc download --with-php="$PHP_VERSION" --for-extensions="$EXTS"
./spc build "$EXTS" --build-cli --build-fpm

rm -rf "$OUT"
mkdir -p "$OUT/licenses"
cp buildroot/bin/php buildroot/bin/php-fpm "$OUT/"
if [ -d buildroot/license ]; then cp -R buildroot/license/. "$OUT/licenses/"; fi
"$OUT/php" -v
"$OUT/php" -m | tr '\n' ' '
echo
echo "Built $OUT"
