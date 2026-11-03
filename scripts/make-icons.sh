#!/usr/bin/env bash
# Rebuilds every app icon from cmd/keel/icon.svg. Run on a Mac after
# changing the logo, and commit the results:
#   packaging/macos/Keel.icns         macOS app icon
#   packaging/windows/keel.ico        installer, Start menu and desktop icon
#   packaging/linux/keel.png          Linux menu icon
# Needs rsvg-convert and ImageMagick (brew install librsvg imagemagick).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SVG="$ROOT/cmd/keel/icon.svg"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# macOS: Apple's grid puts the rounded square at 824 of 1024 pixels, with
# a soft shadow underneath.
rsvg-convert -w 824 -h 824 "$SVG" -o "$WORK/body.png"
magick "$WORK/body.png" -background none -gravity center -extent 1024x1024 \
  \( +clone -background black -shadow 45x14+0+10 \) +swap \
  -background none -layers merge +repage \
  -gravity center -crop 1024x1024+0+0 +repage "$WORK/mac-1024.png"
ICONSET="$WORK/Keel.iconset"
mkdir "$ICONSET"
for size in 16 32 128 256 512; do
  magick "$WORK/mac-1024.png" -resize "${size}x${size}" "$ICONSET/icon_${size}x${size}.png"
  magick "$WORK/mac-1024.png" -resize "$((size * 2))x$((size * 2))" "$ICONSET/icon_${size}x${size}@2x.png"
done
iconutil -c icns "$ICONSET" -o "$ROOT/packaging/macos/Keel.icns"

# Windows: each size drawn straight from the SVG, so small ones stay sharp.
ICO_PARTS=()
for size in 16 24 32 48 64 128 256; do
  rsvg-convert -w "$size" -h "$size" "$SVG" -o "$WORK/win-$size.png"
  ICO_PARTS+=("$WORK/win-$size.png")
done
magick "${ICO_PARTS[@]}" "$ROOT/packaging/windows/keel.ico"

# Linux.
rsvg-convert -w 512 -h 512 "$SVG" -o "$ROOT/packaging/linux/keel.png"

echo "Icons rebuilt."
