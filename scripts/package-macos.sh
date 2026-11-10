#!/usr/bin/env bash
# Builds a universal (Apple Silicon + Intel) Keel.app and wraps it in
# dist/Keel-macOS.dmg. Run on a Mac.
# Usage: scripts/package-macos.sh <version>
set -euo pipefail

VERSION="${1:-0.0.0-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$ROOT/dist/macos"
APP="$WORK/Keel.app"
DMG="$ROOT/dist/Keel-macOS.dmg"

export CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"

rm -rf "$WORK" "$DMG"
mkdir -p "$WORK"

# One binary per CPU type, then glue them into a universal binary.
for arch in arm64 amd64; do
  GOOS=darwin GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o "$WORK/keel-$arch" "$ROOT/cmd/keel"
done

mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
lipo -create -output "$APP/Contents/MacOS/keel" "$WORK/keel-arm64" "$WORK/keel-amd64"
sed "s/__VERSION__/$VERSION/g" "$ROOT/packaging/macos/Info.plist" > "$APP/Contents/Info.plist"
cp "$ROOT/packaging/macos/Keel.icns" "$APP/Contents/Resources/"

# The runtimes Keel ships (scripts/fetch-runtimes.sh macos-universal ...),
# where Keel looks for them: Contents/Resources.
RUNTIMES="${RUNTIMES:-$ROOT/dist/runtimes/macos-universal}"
if [ -d "$RUNTIMES" ]; then
  for part in mariadb apache php phpmyadmin; do
    cp -R "$RUNTIMES/$part" "$APP/Contents/Resources/"
  done
  # Each program and plugin needs its own signature: Apple Silicon won't
  # run unsigned code, and the app's signature only seals them as files.
  find "$APP/Contents/Resources" -type f | while read -r f; do
    if file "$f" | grep -q "Mach-O"; then codesign --force --sign - "$f"; fi
  done
else
  echo "package-macos: no runtimes in $RUNTIMES; building Keel without them" >&2
fi

# Ad-hoc signature (Apple Silicon refuses to run unsigned code).
# Swap "-" for a "Developer ID Application" identity once there is one.
codesign --force --deep --sign - "$APP"

# Disk image with an Applications shortcut for drag-to-install.
STAGE="$WORK/dmg"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "Keel" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null

echo "Built $DMG"
