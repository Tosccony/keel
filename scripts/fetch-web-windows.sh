#!/usr/bin/env bash
# Downloads Apache Lounge's Apache and php.net's PHP for Windows (files and
# checksums in packaging/web/web.env), checks them, and keeps what Keel
# uses.
#
# Results: dist/apache/windows-x86_64/apache (bin/, modules/,
# conf/mime.types, licence) and dist/php/windows-x86_64/php (all of PHP
# except its developer files).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/web/web.env
source "$ROOT/packaging/web/web.env"
WORK="$ROOT/dist/web-build/windows"
mkdir -p "$WORK"

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
# fetch FILE SHA256 URL...: the first URL that works, checked.
fetch() {
  local file="$WORK/$1" sum="$2"
  shift 2
  if [ ! -f "$file" ]; then
    for url in "$@"; do
      if curl -fsSL --retry 3 -A "Mozilla/5.0 (Windows NT 10.0; Win64; x64) keel-build" -o "$file.part" "$url"; then
        mv "$file.part" "$file"
        break
      fi
    done
  fi
  if [ ! -f "$file" ] || [ "$(sha256 "$file")" != "$sum" ]; then
    echo "fetch-web-windows: $(basename "$file") is missing or doesn't match its checksum" >&2
    exit 1
  fi
}
unpack() {
  rm -rf "$2"
  mkdir -p "$2"
  if command -v unzip >/dev/null; then unzip -q "$1" -d "$2"; else 7z x -bd -y -o"$2" "$1" >/dev/null; fi
}

fetch "$APACHE_WINDOWS_FILE" "$APACHE_WINDOWS_SHA256" \
  "https://www.apachelounge.com/download/VS18/binaries/$APACHE_WINDOWS_FILE"
fetch "$PHP_WINDOWS_FILE" "$PHP_WINDOWS_SHA256" \
  "https://windows.php.net/downloads/releases/$PHP_WINDOWS_FILE" \
  "https://windows.php.net/downloads/releases/archives/$PHP_WINDOWS_FILE"

unpack "$WORK/$APACHE_WINDOWS_FILE" "$WORK/apache-unpacked"
A="$WORK/apache-unpacked/Apache24"
OUT="$ROOT/dist/apache/windows-x86_64/apache"
rm -rf "$OUT"
mkdir -p "$OUT/conf"
cp -R "$A/bin" "$A/modules" "$OUT/"
cp "$A/conf/mime.types" "$OUT/conf/"
for f in LICENSE.txt NOTICE.txt ABOUT_APACHE.txt; do
  if [ -f "$A/$f" ]; then cp "$A/$f" "$OUT/"; fi
done
rm -f "$OUT"/bin/*.pdb "$OUT"/modules/*.pdb

unpack "$WORK/$PHP_WINDOWS_FILE" "$WORK/php-unpacked"
OUT_PHP="$ROOT/dist/php/windows-x86_64/php"
rm -rf "$OUT_PHP"
mkdir -p "$(dirname "$OUT_PHP")"
mv "$WORK/php-unpacked" "$OUT_PHP"
rm -rf "$OUT_PHP/dev" "$OUT_PHP"/php.ini-*

for need in "$OUT/bin/httpd.exe" "$OUT/modules/mod_rewrite.so" "$OUT/modules/mod_proxy_fcgi.so" \
  "$OUT_PHP/php.exe" "$OUT_PHP/php8apache2_4.dll" "$OUT_PHP/ext/php_mysqli.dll"; do
  if [ ! -f "$need" ]; then
    echo "fetch-web-windows: $need is missing" >&2
    exit 1
  fi
done
du -sh "$OUT" "$OUT_PHP"
