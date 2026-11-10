#!/usr/bin/env bash
# Downloads phpMyAdmin (version and checksum in
# packaging/phpmyadmin/phpmyadmin.env), checks it, and prepares it for Keel:
#
# - Keel installs phpMyAdmin where it can't write (Program Files, inside
#   Keel.app), so vendor_config.php, the file phpMyAdmin keeps for
#   distributors, is changed to read its config file, temp folder and
#   cache folder from paths Keel's Apache passes in (KEEL_PMA_*). Without
#   them it behaves as shipped.
# - The update check is off: Keel ships the updates.
# - The setup/ wizard is removed; Keel writes the config.
#
# Usage: scripts/fetch-phpmyadmin.sh [DEST]   (default: dist/phpmyadmin)
# For a development build: scripts/fetch-phpmyadmin.sh ~/personal/keel-lab/phpmyadmin
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/phpmyadmin/phpmyadmin.env
source "$ROOT/packaging/phpmyadmin/phpmyadmin.env"
V="$PHPMYADMIN_VERSION"
DEST="${1:-$ROOT/dist/phpmyadmin}"
WORK="$ROOT/dist/phpmyadmin-build"
ZIP="$WORK/phpMyAdmin-$V-all-languages.zip"

mkdir -p "$WORK"
if [ ! -f "$ZIP" ]; then
  curl -fsSL --retry 3 -o "$ZIP.part" "https://files.phpmyadmin.net/phpMyAdmin/$V/phpMyAdmin-$V-all-languages.zip"
  mv "$ZIP.part" "$ZIP"
fi
if command -v sha256sum >/dev/null; then
  got="$(sha256sum "$ZIP" | cut -d' ' -f1)"
else
  got="$(shasum -a 256 "$ZIP" | cut -d' ' -f1)"
fi
if [ "$got" != "$PHPMYADMIN_SHA256" ]; then
  echo "fetch-phpmyadmin: checksum $got, expected $PHPMYADMIN_SHA256" >&2
  exit 1
fi

rm -rf "$WORK/unpacked" "$DEST"
mkdir -p "$WORK/unpacked" "$(dirname "$DEST")"
if command -v unzip >/dev/null; then
  unzip -q "$ZIP" -d "$WORK/unpacked"
else
  7z x -bd -y -o"$WORK/unpacked" "$ZIP" >/dev/null
fi
mv "$WORK/unpacked/phpMyAdmin-$V-all-languages" "$DEST"
rm -rf "$DEST/setup"

VC="$DEST/libraries/vendor_config.php"
perl -0pi -e '
  s/'"'"'configFile'"'"' => ROOT_PATH \. '"'"'config\.inc\.php'"'"',/'"'"'configFile'"'"' => \$_SERVER['"'"'KEEL_PMA_CONFIG'"'"'] ?? (getenv('"'"'KEEL_PMA_CONFIG'"'"') ?: ROOT_PATH . '"'"'config.inc.php'"'"'),/;
  s/'"'"'tempDir'"'"' => ROOT_PATH \. '"'"'tmp'"'"' \. DIRECTORY_SEPARATOR,/'"'"'tempDir'"'"' => \$_SERVER['"'"'KEEL_PMA_TEMP'"'"'] ?? (getenv('"'"'KEEL_PMA_TEMP'"'"') ?: ROOT_PATH . '"'"'tmp'"'"' . DIRECTORY_SEPARATOR),/;
  s/'"'"'cacheDir'"'"' => ROOT_PATH \. '"'"'libraries'"'"' \. DIRECTORY_SEPARATOR \. '"'"'cache'"'"' \. DIRECTORY_SEPARATOR,/'"'"'cacheDir'"'"' => \$_SERVER['"'"'KEEL_PMA_CACHE'"'"'] ?? (getenv('"'"'KEEL_PMA_CACHE'"'"') ?: ROOT_PATH . '"'"'libraries'"'"' . DIRECTORY_SEPARATOR . '"'"'cache'"'"' . DIRECTORY_SEPARATOR),/;
  s/'"'"'versionCheckDefault'"'"' => true,/'"'"'versionCheckDefault'"'"' => false,/;
' "$VC"
for key in KEEL_PMA_CONFIG KEEL_PMA_TEMP KEEL_PMA_CACHE "'versionCheckDefault' => false"; do
  if ! grep -q "$key" "$VC"; then
    echo "fetch-phpmyadmin: couldn't change $VC ($key); has phpMyAdmin changed the file?" >&2
    exit 1
  fi
done
echo "Ready: $DEST"
